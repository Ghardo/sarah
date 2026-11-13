package main

import (
	"bytes"
	// "context" // removed
	"encoding/json" // needed for JSON storage
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv" // needed for counter conversion
	"strings"
	"sync"
	"time"

	"github.com/Ghardo/sane"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type ScanRequest struct {
	Device      string         `json:"Device"`
	WatchSensor string         `json:"WatchSensor,omitempty"`
	Filename    string         `json:"Filename,omitempty"` // used as a template
	NewDoc      bool           `json:"NewDoc,omitempty"`
	Format      string         `json:"Format,omitempty"`      // "png" (default) or "jpeg"
	Quality     int            `json:"Quality,omitempty"`     // JPEG quality 1–100, default 85
	PostProcess string         `json:"PostProcess,omitempty"` // "" = default, "none" = none, else the ID (postprocess.go)
	Options     map[string]any `json:"Options"`
	// Vendor and model of the device, stored with watchers so the device is found
	// again when its name changes (e.g. new USB address after plugging it in again)
	DeviceVendor string `json:"DeviceVendor,omitempty"`
	DeviceModel  string `json:"DeviceModel,omitempty"`
}

type Config struct {
	Port           int
	AllowedOrigins []string
	UseTLS         bool
	CertFile       string
	KeyFile        string
	ScanDir        string
	// Path to the file storing the watchers
	WatcherConfigPath string
	// Serve the web UI at /ui/ (SARAH_UI_ENABLED, default: true)
	UIEnabled bool
	// Additional language files for the web UI (SARAH_LOCALES_DIR, default: ./locales)
	LocalesDir string
	// Directory of the post-processes including manifest.json (SARAH_POST_PROCESS_DIR, default: ./post-process)
	PostProcessDir string
}

var (
	watcherMutex sync.Mutex
	// This map only contains the *running* watchers
	activeWatchers = make(map[string]ScanRequest)
	// Holds the path to the configuration file
	watcherConfigFile = "watchers.json" // default name

	// Global lock for all SANE device access
	saneDeviceMutex sync.Mutex

	// Global scan counter (not persisted) and its lock
	scanCounter  int
	docCounter   int
	counterMutex sync.Mutex

	// Map of open watcher connections (global so that doScan can close them)
	watcherConnMutex sync.Mutex
	watcherConns     = make(map[string]*sane.Conn)
)

const version = "2.10.0"

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if cfg.WatcherConfigPath != "" {
		watcherConfigFile = cfg.WatcherConfigPath
	}

	if err := os.MkdirAll(cfg.ScanDir, settings.DirMode); err != nil {
		log.Fatalf("Failed to create scan directory %s: %v", cfg.ScanDir, err)
	}
	log.Printf("Scan directory is ready at %s", cfg.ScanDir)

	if err := sane.Init(); err != nil {
		log.Fatalf("Failed to initialize SANE: %v", err)
	}
	defer sane.Exit()

	if err := initPostProcesses(cfg.PostProcessDir); err != nil {
		log.Fatalf("Failed to initialize post-processes: %v", err)
	}

	// Load saved watchers
	initializePersistentWatchers(cfg.ScanDir)

	// Start the ONE global watcher loop
	go globalWatcherLoop(cfg.ScanDir, settings.PollInterval)

	router := gin.Default()

	corsConfig := cors.DefaultConfig()
	if len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*" {
		corsConfig.AllowAllOrigins = true
	} else {
		corsConfig.AllowOrigins = cfg.AllowedOrigins
	}
	router.Use(cors.New(corsConfig))

	router.POST("/scan", scanHandler(cfg.ScanDir))
	router.GET("/", hello)
	router.GET("/list", listDevices)
	router.GET("/config", getDeviceConfig)
	router.GET("/scans/:filename", getScanImage(cfg.ScanDir))
	router.GET("/version", getVersion)
	router.GET("/defaults", getDefaults)
	router.POST("/watch", startWatcher(cfg.ScanDir))
	router.DELETE("/watch", stopWatcher)
	router.DELETE("/watch/all", stopAllWatchers)
	router.GET("/watch/list", listWatchers)
	router.POST("/preview", previewHandler)
	router.GET("/preview/last", getLastScan)
	router.GET("/events", eventsHandler)
	registerPostProcessRoutes(router)

	if cfg.UIEnabled {
		uiActive = registerUI(router)
		if uiActive {
			registerLocales(router, cfg.LocalesDir)
		}
	} else {
		log.Println("Web UI disabled (SARAH_UI_ENABLED=false)")
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Starting server at %s (TLS: %t)", addr, cfg.UseTLS)

	if cfg.UseTLS {
		router.RunTLS(addr, cfg.CertFile, cfg.KeyFile)
	} else {
		router.Run(addr)
	}
}

// --- Watcher functions ---

func initializePersistentWatchers(scanDir string) {
	time.Sleep(settings.WatcherStartDelay)
	log.Println("Initializing persistent watchers...")

	watcherMutex.Lock()
	defer watcherMutex.Unlock()

	allConfigs, err := loadWatchersFromFile()
	if err != nil {
		log.Printf("Error loading watcher config: %v", err)
		return
	}

	log.Printf("Found %d saved watchers. Checking device availability...", len(allConfigs))

	startedCount := 0
	for key, config := range allConfigs {
		activeWatchers[key] = config
		log.Printf("Loaded watcher for '%s'", key)
		startedCount++
	}
	log.Printf("Watcher initialization complete. %d watchers loaded into active poll list.", startedCount)
}

func startWatcher(scanDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var sr ScanRequest
		if err := c.ShouldBindJSON(&sr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}
		if sr.Device == "" || sr.WatchSensor == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Device and WatchSensor are required"})
			return
		}
		if err := normalizeFormat(&sr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		sr.Device = canonicalDevice(sr.Device)
		if info, ok := devices.info(sr.Device); ok {
			sr.DeviceVendor, sr.DeviceModel = info.Vendor, info.Model
		}

		watcherKey := watcherKeyOf(sr.Device, sr.WatchSensor)

		allConfigs, _ := loadWatchersFromFile()
		allConfigs[watcherKey] = sr
		if err := saveWatchersToFile(allConfigs); err != nil {
			log.Printf("ERROR saving watcher config: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save watcher configuration"})
			return
		}

		watcherMutex.Lock()
		activeWatchers[watcherKey] = sr
		watcherMutex.Unlock()

		log.Printf("Watcher started and saved for '%s'", watcherKey)
		events.publish("watchers-changed", gin.H{})
		c.JSON(http.StatusOK, gin.H{"message": "Watcher started and saved: " + watcherKey})
	}
}

func stopWatcher(c *gin.Context) {
	var sr struct {
		Device      string `json:"Device"`
		WatchSensor string `json:"WatchSensor"`
	}
	if err := c.ShouldBindJSON(&sr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON. 'Device' and 'WatchSensor' are required."})
		return
	}
	if sr.Device == "" || sr.WatchSensor == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device and WatchSensor are required"})
		return
	}

	watcherKey := watcherKeyOf(sr.Device, sr.WatchSensor)

	watcherMutex.Lock()
	delete(activeWatchers, watcherKey)
	delete(watcherPaused, watcherKey)
	watcherMutex.Unlock()

	allConfigs, err := loadWatchersFromFile()
	if err != nil {
		log.Printf("ERROR loading watcher config for deletion: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load watcher config"})
		return
	}

	if _, ok := allConfigs[watcherKey]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "Watcher was not found in config (already stopped or never existed)."})
		return
	}

	delete(allConfigs, watcherKey)
	if err := saveWatchersToFile(allConfigs); err != nil {
		log.Printf("ERROR saving watcher config after deletion: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save config after deletion"})
		return
	}

	events.publish("watchers-changed", gin.H{})
	c.JSON(http.StatusOK, gin.H{"message": "Watcher stopped and configuration removed: " + watcherKey})
}

func stopAllWatchers(c *gin.Context) {
	watcherMutex.Lock()
	if len(activeWatchers) == 0 && len(mustLoadWatchersFromFile()) == 0 {
		watcherMutex.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"message": "No active or saved watchers to stop."})
		return
	}

	log.Println("Stopping ALL active watchers (clearing list)...")
	activeWatchers = make(map[string]ScanRequest)
	watcherPaused = make(map[string]bool)
	watcherMutex.Unlock()

	if err := saveWatchersToFile(make(map[string]ScanRequest)); err != nil {
		log.Printf("ERROR clearing watcher config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear watcher configuration"})
		return
	}

	events.publish("watchers-changed", gin.H{})
	c.JSON(http.StatusOK, gin.H{"message": "All active watchers stopped and configuration file cleared."})
}

// globalWatcherLoop: this ONE goroutine polls the buttons of all present devices one after another.
// Watchers whose device is missing are paused; the devices are looked for again every
// SARAH_DISCOVERY_INTERVAL until they are back.
func globalWatcherLoop(scanDir string, pollInterval time.Duration) {
	cooldowns := make(map[string]time.Time)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Cooldown for devices so the scan process has time to take over access
	deviceCooldowns := make(map[string]time.Time)

	// Buttons per open connection, so conn.Options() is not called on every loop
	deviceButtonOpts := make(map[string]deviceButtons)

	// Consecutive read errors per device
	errorCounts := make(map[string]int)

	saneDeviceMutex.Lock()
	if devs, err := discoverDevices(); err == nil {
		names := make([]string, 0, len(devs))
		for _, d := range devs {
			names = append(names, d.Name)
		}
		log.Printf("Initial discovery: Found %d devices: %v", len(names), names)
	} else {
		log.Printf("Initial discovery failed: %v", err)
	}
	saneDeviceMutex.Unlock()
	lastDiscovery := time.Now()

	log.Printf(">>> Global Watcher Loop started (poll interval %s) <<<", pollInterval)

	for {
		<-ticker.C

		// Look for the devices of paused watchers again now and then
		if syncWatcherDevices() && time.Since(lastDiscovery) >= settings.DiscoveryInterval {
			saneDeviceMutex.Lock()
			if _, err := discoverDevices(); err != nil {
				log.Printf("Device discovery failed: %v", err)
			}
			saneDeviceMutex.Unlock()
			lastDiscovery = time.Now()
			syncWatcherDevices()
		}

		watcherMutex.Lock()
		// Snapshot of the active watchers
		currentWatchers := make(map[string]ScanRequest, len(activeWatchers))
		for key, req := range activeWatchers {
			currentWatchers[key] = req
		}
		watcherMutex.Unlock()

		// All present devices are polled, so button events are reported even without a watcher.
		// The devices of paused watchers are not present and therefore skipped.
		for _, device := range devices.names() {
			if time.Now().Before(deviceCooldowns[device]) {
				continue
			}

			// Lock order everywhere: saneDeviceMutex -> watcherConnMutex
			saneDeviceMutex.Lock()

			conn := currentWatcherConn(device)
			if conn == nil {
				c, err := openDevice(device)
				if err != nil {
					saneDeviceMutex.Unlock()
					// Busy: another program uses the scanner, try again on the next tick
					if !strings.Contains(err.Error(), "device busy") && devices.markGone(device) {
						log.Printf("Device '%s' can no longer be opened (%v); not polled until it is found again", device, err)
					}
					continue
				}
				deviceButtonOpts[device] = buttonOptions(c.Options())
				conn = c
				watcherConnMutex.Lock()
				watcherConns[device] = conn
				watcherConnMutex.Unlock()
			}

			opts := deviceButtonOpts[device]
			var eventDetected bool
			var pressedButton string

			for _, opt := range opts.buttons {
				val, err := conn.GetOption(opt.Name)
				if err != nil {
					// Some drivers return "invalid argument" for sensors that are not ready.
					// That is not a lost connection, so it is ignored silently to avoid log spam.
					if strings.Contains(err.Error(), "invalid argument") {
						continue
					}

					// Log the error, but do not stop right away: a 'break' here would prevent
					// the following buttons from ever being checked.
					log.Printf("Watcher: Could not get option '%s' on device '%s': %v. Skipping this option.", opt.Name, device, err)
					errorCounts[device]++
					continue
				}
				errorCounts[device] = 0 // reset on success

				var pressed bool
				switch v := val.(type) {
				case bool:
					pressed = v
				case int:
					pressed = v != 0
				}

				if pressed {
					pressedButton = opt.Name
					eventDetected = true
					break
				}
			}

			switch {
			case errorCounts[device] > settings.SensorErrorLimit:
				log.Printf("Watcher: Too many consecutive errors on device '%s'. Assuming connection is lost.", device)
				takeWatcherConn(device)
				conn.Close()
				delete(errorCounts, device)
				eventDetected = false
			case eventDetected && opts.latched:
				// The backend keeps the press until the device is opened again
				takeWatcherConn(device)
				conn.Close()
			}
			saneDeviceMutex.Unlock()

			if eventDetected {
				watcherKey := watcherKeyOf(device, pressedButton)

				if time.Now().Before(cooldowns[watcherKey]) {
					continue
				}

				log.Printf("Button Event detected: Device='%s', Button='%s'", device, pressedButton)

				req, exists := currentWatchers[watcherKey]
				events.publish("button-pressed", gin.H{"device": device, "sensor": pressedButton, "watcher": exists})

				if exists {
					// Close the connection so the scan process gets exclusive access
					// (doScan may already have closed it)
					saneDeviceMutex.Lock()
					if takeWatcherConn(device) == conn {
						conn.Close()
					}
					saneDeviceMutex.Unlock()
					deviceCooldowns[device] = time.Now().Add(settings.ScanCooldown)

					log.Printf("Executing scan for watcher: %s", watcherKey)
					go executeWatchedScan(req, scanDir, watcherKey)
				} else {
					// Only log, keep the connection open!
					cooldowns[watcherKey] = time.Now().Add(settings.ButtonCooldown)
					log.Printf("No watcher configured for button '%s' on device '%s'", pressedButton, device)
				}
			}
		}
	}
}

// executeWatchedScan: scan logic started as a goroutine.
func executeWatchedScan(sr ScanRequest, scanDir string, watcherKey string) {
	scan := beginScan(sr.Device, "button", sr.WatchSensor)

	// Older or hand-edited watchers.json: validate the format here as well
	if err := normalizeFormat(&sr); err != nil {
		log.Printf("Watcher (%s): invalid format: %v", watcherKey, err)
		scan.fail(err)
		return
	}

	imgBytes, err := doScan(sr)
	if err != nil {
		log.Printf("Watcher scan execution error (%s): %v", watcherKey, err)
		scan.fail(err)
		return
	}

	log.Printf("Watcher (%s): scan completed", watcherKey)

	go func() {
		// File name logic
		filename, doc, index := generateFilename(sr)
		filePath := filepath.Join(scanDir, filename)

		if err := writeScanFile(filePath, imgBytes); err != nil {
			log.Printf("Watcher error (save file, %s): %v", watcherKey, err)
			scan.fail(err)
			return
		}

		log.Printf("Watcher (%s): saved to %s", watcherKey, filename)
		storeLastScan(filename, imgBytes, formatMime(sr.Format))
		scan.done(filename, imgBytes)

		runPostProcess(sr.PostProcess, postProcessJob{
			ScanDir: scanDir, Filename: filename, Format: sr.Format,
			Source: "button", Device: sr.Device, Sensor: sr.WatchSensor,
			Doc: doc, Index: index, NewDoc: sr.NewDoc,
		})
	}()
}

type WatcherStatus struct {
	Config ScanRequest `json:"config"`
	Active bool        `json:"active"`
	// false while the device is missing; the watcher is paused until it is back
	DevicePresent bool `json:"device_present"`
}

func listWatchers(c *gin.Context) {
	allConfigs, err := loadWatchersFromFile()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not read watcher config file"})
		return
	}

	responseList := make([]WatcherStatus, 0, len(allConfigs))

	watcherMutex.Lock()
	defer watcherMutex.Unlock()

	for key, config := range allConfigs {
		_, isActive := activeWatchers[key]

		status := WatcherStatus{
			Config:        config,
			Active:        isActive,
			DevicePresent: !watcherPaused[key],
		}
		responseList = append(responseList, status)
	}

	c.JSON(http.StatusOK, gin.H{"active_watchers": responseList})
}

// --- Helpers for JSON storage ---

func loadWatchersFromFile() (map[string]ScanRequest, error) {
	configs := make(map[string]ScanRequest)

	data, err := os.ReadFile(watcherConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return configs, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return configs, nil
	}

	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}

	return configs, nil
}

func mustLoadWatchersFromFile() map[string]ScanRequest {
	configs, err := loadWatchersFromFile()
	if err != nil {
		log.Printf("Warning: could not load watcher file: %v. Returning empty map.", err)
		return make(map[string]ScanRequest)
	}
	return configs
}

func saveWatchersToFile(configs map[string]ScanRequest) error {
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(watcherConfigFile, data, 0644)
}

// --- Central function for building file names ---
// generateFilename treats filenameTemplate as a template and replaces placeholders.
// It also returns the counters {doc} and {index} that were used.
func generateFilename(sr ScanRequest) (string, int, int) {
	// 1. Get the dynamic values
	counterMutex.Lock()
	if sr.NewDoc {
		docCounter++
		scanCounter = 0
	}
	if docCounter == 0 {
		docCounter = 1
	}
	scanCounter++
	index := scanCounter
	docIndex := docCounter
	counterMutex.Unlock()
	indexStr := strconv.Itoa(index)
	docStr := strconv.Itoa(docIndex)

	now := time.Now()
	dateStr := now.Format(settings.DateFormat)
	timeStr := now.Format(settings.TimeFormat)

	// 2. Determine the base template
	var baseTemplate string
	if sr.Filename != "" {
		baseTemplate = filepath.Base(sr.Filename) // prevents path traversal
		if baseTemplate == "." || baseTemplate == "" {
			baseTemplate = settings.FilenameTemplate // Fallback
		}
	} else {
		baseTemplate = settings.FilenameTemplate // Default
	}

	// 3. Remove an image extension if present (the matching one is appended at the end)
	for _, ext := range []string{".png", ".jpg", ".jpeg"} {
		if strings.HasSuffix(strings.ToLower(baseTemplate), ext) {
			baseTemplate = baseTemplate[:len(baseTemplate)-len(ext)]
			break
		}
	}

	// 4. Replace the placeholders
	result := strings.ReplaceAll(baseTemplate, "{index}", indexStr)
	result = strings.ReplaceAll(result, "{date}", dateStr)
	result = strings.ReplaceAll(result, "{time}", timeStr)
	result = strings.ReplaceAll(result, "{doc}", docStr)

	// 5. Append the extension matching the format
	return result + formatExt(sr.Format), docIndex, index
}

// --- HTTP handlers ---

func hello(c *gin.Context) {
	// With the web UI enabled, browsers land there directly; API clients still get the text.
	if uiActive && strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Redirect(http.StatusFound, uiPrefix+"/")
		return
	}
	c.Data(http.StatusOK, "text/html; charset-utf-8", []byte("Hi, i am sarah. Version: "+version))
}
func getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": version})
}
func getDeviceConfig(c *gin.Context) {
	device := c.Query("device")
	if device == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'device' is required"})
		return
	}
	device = canonicalDevice(device)

	saneDeviceMutex.Lock()
	defer saneDeviceMutex.Unlock()

	// If the watcher holds the device open, a second open fails with "device busy".
	// In that case simply return the options of the existing connection.
	if conn := currentWatcherConn(device); conn != nil {
		c.JSON(http.StatusOK, conn.Options())
		return
	}

	sc, err := openDevice(device)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	defer sc.Close()

	c.JSON(http.StatusOK, sc.Options())
}
func listDevices(c *gin.Context) {
	var devs []sane.Device
	if lockSane(time.Second) {
		var err error
		devs, err = discoverDevices()
		saneDeviceMutex.Unlock()
		if err != nil {
			log.Printf("ERROR: Failed to list SANE devices: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list devices"})
			return
		}
	} else {
		// A scan is running: answer with the last discovery instead of waiting for it
		devs = devices.list()
	}
	if len(devs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No devices found."})
		return
	}
	c.JSON(http.StatusOK, devs)
}

func scanHandler(scanDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var sr ScanRequest
		if err := c.ShouldBindJSON(&sr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
			return
		}
		if err := normalizeFormat(&sr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		scan := beginScan(sr.Device, "api", "")

		imgBytes, err := doScan(sr)
		if err != nil {
			log.Printf("ERROR: Scan failed: %v", err)
			scan.fail(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		filename, doc, index := generateFilename(sr)
		filePath := filepath.Join(scanDir, filename)

		if err := writeScanFile(filePath, imgBytes); err != nil {
			log.Printf("ERROR: Failed to save scan to disk %s: %v", filePath, err)
			scan.fail(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save scan"})
			return
		}

		storeLastScan(filename, imgBytes, formatMime(sr.Format))
		scan.done(filename, imgBytes)

		go runPostProcess(sr.PostProcess, postProcessJob{
			ScanDir: scanDir, Filename: filename, Format: sr.Format,
			Source: "api", Device: sr.Device,
			Doc: doc, Index: index, NewDoc: sr.NewDoc,
		})

		imgConfig, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
		if err != nil {
			log.Printf("WARN: Could not decode image config %s: %v", filename, err)
			c.JSON(http.StatusOK, gin.H{
				"message":  "Scan completed, but could not read dimensions.",
				"filename": filename,
			})
			return
		}
		log.Printf("Scan successful: device=%s, file=%s", sr.Device, filename)
		c.JSON(http.StatusOK, gin.H{
			"filename": filename,
			"width":    imgConfig.Width,
			"height":   imgConfig.Height,
		})
	}
}
func getScanImage(scanDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		filename := c.Param("filename")
		filePath := filepath.Join(scanDir, filename)
		if !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(scanDir)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
			return
		}
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}
		c.FileAttachment(filePath, filename)
	}
}
func loadConfig() (Config, error) {
	portStr := getEnv("SARAH_PORT", "7575")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Config{}, fmt.Errorf("invalid SARAH_PORT: %w", err)
	}
	useTLSStr := getEnv("SARAH_USE_TLS", "false")
	useTLS, err := strconv.ParseBool(useTLSStr)
	if err != nil {
		return Config{}, fmt.Errorf("invalid SARAH_USE_TLS: %w", err)
	}
	uiEnabled, err := strconv.ParseBool(getEnv("SARAH_UI_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid SARAH_UI_ENABLED: %w", err)
	}
	if settings, err = loadSettings(); err != nil {
		return Config{}, err
	}
	var origins []string
	originsStr := getEnv("SARAH_ALLOWED_ORIGINS", "")
	if originsStr == "" || originsStr == "*" {
		origins = []string{"*"}
	} else {
		origins = strings.Split(originsStr, ",")
	}
	cfg := Config{
		Port:              port,
		AllowedOrigins:    origins,
		UseTLS:            useTLS,
		CertFile:          getEnv("SARAH_CERT_FILE", ""),
		KeyFile:           getEnv("SARAH_KEY_FILE", ""),
		ScanDir:           getEnv("SARAH_SCAN_DIR", "scans"),
		WatcherConfigPath: getEnv("SARAH_WATCHER_CONFIG", "watchers.json"),
		UIEnabled:         uiEnabled,
		LocalesDir:        getEnv("SARAH_LOCALES_DIR", "locales"),
		PostProcessDir:    getEnv("SARAH_POST_PROCESS_DIR", "post-process"),
	}
	if cfg.UseTLS && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return Config{}, errors.New("SARAH_USE_TLS is true, but SARAH_CERT_FILE or SARAH_KEY_FILE are not set")
	}
	return cfg, nil
}
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// doScan scans with the options from sr and returns the scan in the format of sr (PNG or JPEG),
// with the scan resolution in the metadata.
func doScan(sr ScanRequest) ([]byte, error) {
	s, err := scanDevice(sr.Device, func([]sane.Option) map[string]any { return sr.Options })
	if err != nil {
		return nil, err
	}
	return encodeImage(s.img, sr.Format, sr.Quality, s.dpiX, s.dpiY)
}

// scannedImage is a scan together with the resolution actually used (0 = unknown).
type scannedImage struct {
	img        image.Image
	dpiX, dpiY float64
}

// scanDevice opens the device, sets the options from options(...) and returns the scanned image.
// options gets the options of the opened device, so e.g. the preview knows the limits.
func scanDevice(device string, options func([]sane.Option) map[string]any) (*scannedImage, error) {
	saneDeviceMutex.Lock()
	defer saneDeviceMutex.Unlock()

	device = canonicalDevice(device)

	// If the watcher holds the device open, it is forcibly closed here.
	// This happens under the same lock as opening, so the watcher
	// cannot grab the device again in between.
	if conn := takeWatcherConn(device); conn != nil {
		conn.Close()
	}

	c, err := openDevice(device)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	sr := ScanRequest{Device: device, Options: options(c.Options())}
	for oName, oValue := range sr.Options {
		var o *sane.Option
		o, err = findOption(c.Options(), oName)
		if err != nil {
			return nil, err
		}
		if !o.IsSettable {
			continue
		}
		var v any
		switch o.Type {
		case sane.TypeBool:
			v = oValue.(bool)
		case sane.TypeInt:
			v = int(oValue.(float64))
		case sane.TypeFloat:
			v = oValue.(float64)
		case sane.TypeString:
			v = oValue.(string)
		}
		if _, err := c.SetOption(o.Name, v); err != nil {
			return nil, fmt.Errorf("failed to set option '%s' on device '%s': %w", oName, sr.Device, err)
		}
	}
	// Read the resolution back from the device after setting the options: the driver
	// may have rounded it, and without a value in the request the device default applies.
	dpiX, dpiY := scanResolution(c)

	img, err := c.ReadImage()
	if err != nil {
		return nil, err
	}
	return &scannedImage{img: img, dpiX: dpiX, dpiY: dpiY}, nil
}

// scanResolution returns the configured resolution in dpi (0 if unknown).
// Some drivers have separate x-/y-resolution, otherwise resolution applies to both axes.
func scanResolution(c *sane.Conn) (float64, float64) {
	get := func(name string) float64 {
		o, err := findOption(c.Options(), name)
		if err != nil || !o.IsActive {
			return 0
		}
		v, err := c.GetOption(name)
		if err != nil {
			return 0
		}
		switch n := v.(type) {
		case []int:
			if len(n) > 0 {
				return float64(n[0])
			}
		case []float64:
			if len(n) > 0 {
				return n[0]
			}
		}
		return toFloat(v)
	}
	res := get("resolution")
	x, y := get("x-resolution"), get("y-resolution")
	if x == 0 {
		x = res
	}
	if y == 0 {
		y = res
	}
	if y == 0 {
		y = x
	}
	return x, y
}

func openDevice(name string) (*sane.Conn, error) {
	return sane.Open(name)
}

// currentWatcherConn returns the open watcher connection of a device (or nil).
// The caller must hold saneDeviceMutex so the connection is not closed concurrently.
func currentWatcherConn(device string) *sane.Conn {
	watcherConnMutex.Lock()
	defer watcherConnMutex.Unlock()
	return watcherConns[device]
}

// takeWatcherConn removes the watcher connection of a device from the map and returns it (or nil).
// The caller must hold saneDeviceMutex and is responsible for closing it afterwards.
func takeWatcherConn(device string) *sane.Conn {
	watcherConnMutex.Lock()
	defer watcherConnMutex.Unlock()
	conn := watcherConns[device]
	delete(watcherConns, device)
	return conn
}

func findOption(opts []sane.Option, name string) (*sane.Option, error) {
	for _, o := range opts {
		if o.Name == name {
			return &o, nil
		}
	}
	return nil, errors.New("no such option")
}
