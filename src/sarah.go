package main

import (
	"bytes"
	// "context" // ENTFERNT
	"encoding/json" // Nötig für die JSON-Speicherung
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/exec" // Nötig für SARAH_POST_SCAN_PROCESS
	"path/filepath"
	"strconv" // NEU: Nötig für Zähler-Umwandlung
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
	Filename    string         `json:"Filename,omitempty"` // Wird jetzt als Template verwendet
	NewDoc      bool           `json:"NewDoc,omitempty"`
	Options     map[string]any `json:"Options"`
}

type Config struct {
	Port           int
	AllowedOrigins []string
	UseTLS         bool
	CertFile       string
	KeyFile        string
	ScanDir        string
	// Pfad zur Speicherdatei für die Watcher
	WatcherConfigPath string
	// Web-UI unter /ui/ ausliefern (SARAH_UI_ENABLED, Standard: true)
	UIEnabled bool
	// Zusätzliche Sprachdateien fürs Web-UI (SARAH_LOCALES_DIR, Standard: ./locales)
	LocalesDir string
}

var (
	watcherMutex sync.Mutex
	// Diese Map enthält nur die *laufenden* Watcher
	activeWatchers = make(map[string]ScanRequest)
	// Hält den Pfad zur Konfigurationsdatei
	watcherConfigFile = "watchers.json" // Standardname

	// Globale Sperre für alle SANE-Gerätezugriffe
	saneDeviceMutex sync.Mutex

	// Globaler Scan-Zähler (nicht persistent) und zugehörige Sperre
	scanCounter  int
	docCounter   int
	counterMutex sync.Mutex

	// Map für offene Verbindungen des Watchers (Global, damit doScan sie schließen kann)
	watcherConnMutex sync.Mutex
	watcherConns     = make(map[string]*sane.Conn)
)

const version = "2.5.0-template-filename" // NEUE VERSION

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if cfg.WatcherConfigPath != "" {
		watcherConfigFile = cfg.WatcherConfigPath
	}

	if err := os.MkdirAll(cfg.ScanDir, 0755); err != nil {
		log.Fatalf("Failed to create scan directory %s: %v", cfg.ScanDir, err)
	}
	log.Printf("Scan directory is ready at %s", cfg.ScanDir)

	if err := sane.Init(); err != nil {
		log.Fatalf("Failed to initialize SANE: %v", err)
	}
	defer sane.Exit()

	// Lade gespeicherte Watcher
	initializePersistentWatchers(cfg.ScanDir)

	// Starte die EINE globale Watcher-Schleife
	go globalWatcherLoop(cfg.ScanDir)

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
	router.POST("/watch", startWatcher(cfg.ScanDir))
	router.DELETE("/watch", stopWatcher)
	router.DELETE("/watch/all", stopAllWatchers)
	router.GET("/watch/list", listWatchers)
	router.GET("/preview/last", getLastScan)
	router.GET("/events", eventsHandler)

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

// --- Watcher-Funktionen ---

func initializePersistentWatchers(scanDir string) {
	time.Sleep(5 * time.Second)
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

		watcherKey := fmt.Sprintf("%s:%s", sr.Device, sr.WatchSensor)

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

	watcherKey := fmt.Sprintf("%s:%s", sr.Device, sr.WatchSensor)

	watcherMutex.Lock()
	delete(activeWatchers, watcherKey)
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
	watcherMutex.Unlock()

	if err := saveWatchersToFile(make(map[string]ScanRequest)); err != nil {
		log.Printf("ERROR clearing watcher config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear watcher configuration"})
		return
	}

	events.publish("watchers-changed", gin.H{})
	c.JSON(http.StatusOK, gin.H{"message": "All active watchers stopped and configuration file cleared."})
}

// globalWatcherLoop: Diese EINE Goroutine pollt alle aktiven Watcher nacheinander.
func globalWatcherLoop(scanDir string) {
	postScanScript := os.Getenv("SARAH_POST_SCAN_PROCESS")
	cooldowns := make(map[string]time.Time)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	// Cooldown für Geräte, damit der Scan-Prozess Zeit hat, den Zugriff zu übernehmen
	deviceCooldowns := make(map[string]time.Time)

	// Cache für Sensor-Optionen, um conn.Options() nicht in jedem Loop aufzurufen
	deviceSensors := make(map[string][]sane.Option)

	// Einmaliges Discovery beim Start, da sich die Geräte laut User nicht ändern
	var knownDevices []string
	saneDeviceMutex.Lock()
	if devs, err := sane.Devices(); err == nil {
		for _, d := range devs {
			knownDevices = append(knownDevices, d.Name)
		}
		log.Printf("Initial discovery: Found %d devices: %v", len(knownDevices), knownDevices)
	} else {
		log.Printf("Initial discovery failed: %v", err)
	}
	saneDeviceMutex.Unlock()

	log.Println(">>> Global Watcher Loop started <<<")

	for {
		<-ticker.C

		watcherMutex.Lock()
		// Snapshot der aktiven Watcher und Identifikation der zu pollenden Geräte
		currentWatchers := make(map[string]ScanRequest)
		devicesToPoll := make(map[string]bool)
		for key, req := range activeWatchers {
			currentWatchers[key] = req
			devicesToPoll[req.Device] = true
		}
		watcherMutex.Unlock()

		// Füge automatisch erkannte Geräte hinzu, damit Button-Events auch ohne Watcher geloggt werden
		for _, dev := range knownDevices {
			devicesToPoll[dev] = true
		}

		if len(devicesToPoll) == 0 {
			continue
		}

		for device := range devicesToPoll {
			if time.Now().Before(deviceCooldowns[device]) {
				continue
			}

			// Sperr-Reihenfolge überall: saneDeviceMutex -> watcherConnMutex
			saneDeviceMutex.Lock()

			conn := currentWatcherConn(device)
			if conn == nil {
				c, err := openDevice(device)
				if err != nil {
					saneDeviceMutex.Unlock()
					if !strings.Contains(err.Error(), "device busy") {
						log.Printf("Watcher poll error (%s): %v", device, err)
					}
					continue
				}

				// Optionen einmalig abrufen und cachen
				rawOpts := c.Options()
				var cached []sane.Option
				for _, opt := range rawOpts {
					if opt.Group == "Sensors" && opt.IsDetectable {
						if opt.Type == sane.TypeBool || opt.Type == sane.TypeInt {
							cached = append(cached, opt)
						}
					}
				}
				deviceSensors[device] = cached

				conn = c
				watcherConnMutex.Lock()
				watcherConns[device] = conn
				watcherConnMutex.Unlock()
			}

			// Verwende gecachte Optionen statt jedes Mal conn.Options() aufzurufen
			opts := deviceSensors[device]
			var eventDetected bool
			var pressedButton string
			var errorCount int

			for _, opt := range opts {
				// Strategie-Wechsel: Wir prüfen nur Optionen in der Gruppe "Sensors".
				// Das ist zuverlässiger als Namens-Heuristiken, da SANE-Buttons dort liegen.
				if opt.Group != "Sensors" {
					continue
				}

				if !opt.IsDetectable {
					continue
				}

				isBool := opt.Type == sane.TypeBool
				isInt := opt.Type == sane.TypeInt

				if !isBool && !isInt {
					continue
				}

				val, err := conn.GetOption(opt.Name)
				if err != nil {
					// Manche Treiber werfen "invalid argument" für Sensoren, die gerade nicht bereit sind.
					// Das ist kein Verbindungsabbruch, also ignorieren wir es stillschweigend, um Log-Spam zu vermeiden.
					if strings.Contains(err.Error(), "invalid argument") {
						continue
					}

					// Logge den Fehler, aber brich nicht sofort ab.
					// Das ist der entscheidende Punkt: Wenn eine andere Sensor-Option vor 'extra' einen Fehler wirft,
					// würde ein 'break' hier verhindern, dass 'extra' jemals geprüft wird.
					log.Printf("Watcher: Could not get option '%s' on device '%s': %v. Skipping this option.", opt.Name, device, err)

					errorCount++
					if errorCount > 5 {
						log.Printf("Watcher: Too many consecutive errors on device '%s'. Assuming connection is lost.", device)
						takeWatcherConn(device)
						conn.Close()
						eventDetected = false
						break
					}
					continue
				}
				errorCount = 0 // Reset bei Erfolg

				var pressed bool
				if isBool {
					pressed, _ = val.(bool)
				} else if isInt {
					if v, ok := val.(int); ok && v != 0 {
						pressed = true
					}
				}

				if pressed {
					pressedButton = opt.Name
					eventDetected = true
					break
				}
			}
			saneDeviceMutex.Unlock()

			if eventDetected {
				watcherKey := fmt.Sprintf("%s:%s", device, pressedButton)

				if time.Now().Before(cooldowns[watcherKey]) {
					continue
				}

				log.Printf("Button Event detected: Device='%s', Button='%s'", device, pressedButton)

				req, exists := currentWatchers[watcherKey]
				events.publish("button-pressed", gin.H{"device": device, "sensor": pressedButton, "watcher": exists})

				if exists {
					// Verbindung schließen, damit der Scan-Prozess exklusiven Zugriff hat
					// (doScan kann sie inzwischen schon geschlossen haben)
					saneDeviceMutex.Lock()
					if takeWatcherConn(device) == conn {
						conn.Close()
					}
					saneDeviceMutex.Unlock()
					deviceCooldowns[device] = time.Now().Add(2 * time.Second)

					log.Printf("Executing scan for watcher: %s", watcherKey)
					go executeWatchedScan(req, scanDir, postScanScript, watcherKey)
				} else {
					// Nur loggen, Verbindung offen lassen!
					cooldowns[watcherKey] = time.Now().Add(1 * time.Second)
					log.Printf("No watcher configured for button '%s' on device '%s'", pressedButton, device)
				}
			}
		}
	}
}

// executeWatchedScan: Scan-Logik, die als Goroutine gestartet wird.
func executeWatchedScan(sr ScanRequest, scanDir string, postScanScript string, watcherKey string) {
	scan := beginScan(sr.Device, "button", sr.WatchSensor)

	imgBytes, err := doScan(sr)
	if err != nil {
		log.Printf("Watcher scan execution error (%s): %v", watcherKey, err)
		scan.fail(err)
		return
	}

	log.Printf("Watcher (%s): scan completed", watcherKey)

	go func() {
		// Dateinamen-Logik
		filename := generateFilename(sr)
		filePath := filepath.Join(scanDir, filename)

		if err := os.WriteFile(filePath, imgBytes, 0644); err != nil {
			log.Printf("Watcher error (save file, %s): %v", watcherKey, err)
			scan.fail(err)
			return
		}

		log.Printf("Watcher (%s): saved to %s", watcherKey, filename)
		storeLastScan(filename, imgBytes)
		scan.done(filename, imgBytes)

		if postScanScript != "" {
			log.Printf("Watcher (%s): Executing post-scan process: %s", watcherKey, postScanScript)
			executePostScanScript(postScanScript, scanDir, filename)
		} else {
			log.Printf("Watcher (%s): No post-scan process configured.", watcherKey)
		}
	}()
}

type WatcherStatus struct {
	Config ScanRequest `json:"config"`
	Active bool        `json:"active"`
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
			Config: config,
			Active: isActive,
		}
		responseList = append(responseList, status)
	}

	c.JSON(http.StatusOK, gin.H{"active_watchers": responseList})
}

// --- Hilfsfunktionen für JSON-Speicherung ---

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

// --- Post-Processing-Funktion ---
func executePostScanScript(scriptPath string, scanDir string, filename string) {
	log.Printf("Starting post-scan script execution: %s", scriptPath)

	cmd := exec.Command(scriptPath)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("SARAH_SCAN_DIR=%s", scanDir))
	cmd.Env = append(cmd.Env, fmt.Sprintf("SARAH_SCAN_FILENAME=%s", filename))

	output, err := cmd.CombinedOutput()

	outStr := strings.TrimSpace(string(output))
	if outStr == "" {
		outStr = "<no output>"
	}

	if err != nil {
		log.Printf("ERROR: Post-scan script '%s' failed: %v. Output: %s", scriptPath, err, outStr)
	} else {
		log.Printf("INFO: Post-scan script '%s' finished successfully. Output: %s", scriptPath, outStr)
	}
}

// --- KORRIGIERT: Zentrale Funktion zur Dateinamenerstellung ---
// generateFilename behandelt filenameTemplate als Vorlage und ersetzt Platzhalter.
func generateFilename(sr ScanRequest) string {
	// 1. Hole die dynamischen Werte
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
	dateStr := now.Format("20060102")
	timeStr := now.Format("150405")

	// 2. Bestimme die Basisschablone
	var baseTemplate string
	if sr.Filename != "" {
		baseTemplate = filepath.Base(sr.Filename) // Verhindert Path Traversal
		if baseTemplate == "." || baseTemplate == "" {
			baseTemplate = "scan_{index}_{date}_{time}" // Fallback
		}
	} else {
		baseTemplate = "scan_{index}_{date}_{time}" // Default
	}

	// 3. Entferne .png, falls vorhanden (wird am Ende sicher hinzugefügt)
	if strings.HasSuffix(strings.ToLower(baseTemplate), ".png") {
		baseTemplate = baseTemplate[:len(baseTemplate)-4]
	}

	// 4. Führe die Ersetzungen durch
	result := strings.ReplaceAll(baseTemplate, "{index}", indexStr)
	result = strings.ReplaceAll(result, "{date}", dateStr)
	result = strings.ReplaceAll(result, "{time}", timeStr)
	result = strings.ReplaceAll(result, "{doc}", docStr)

	// 5. Stelle sicher, dass die Datei mit .png endet
	return result + ".png"
}

// --- Restlicher Code ---

func hello(c *gin.Context) {
	// Browser landen bei aktivem Web-UI direkt dort, API-Clients bekommen weiter den Text.
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

	saneDeviceMutex.Lock()
	defer saneDeviceMutex.Unlock()

	// Hält der Watcher das Gerät offen, scheitert ein zweites Open mit "device busy".
	// Dann einfach die Optionen der bestehenden Verbindung liefern.
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
	devs, err := sane.Devices()
	if err != nil {
		log.Printf("ERROR: Failed to list SANE devices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list devices"})
		return
	}
	if len(devs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "No devices found."})
		return
	}
	c.JSON(http.StatusOK, devs)
}

func scanHandler(scanDir string) gin.HandlerFunc {
	postScanScript := os.Getenv("SARAH_POST_SCAN_PROCESS")

	return func(c *gin.Context) {
		var sr ScanRequest
		if err := c.ShouldBindJSON(&sr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
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

		// --- GEÄNDERT: Neue Template-Logik verwenden ---
		filename := generateFilename(sr)
		filePath := filepath.Join(scanDir, filename)
		// --- ENDE ÄNDERUNG ---

		if err := os.WriteFile(filePath, imgBytes, 0644); err != nil {
			log.Printf("ERROR: Failed to save scan to disk %s: %v", filePath, err)
			scan.fail(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save scan"})
			return
		}

		storeLastScan(filename, imgBytes)
		scan.done(filename, imgBytes)

		if postScanScript != "" {
			log.Printf("Executing post-scan process: %s", postScanScript)
			go executePostScanScript(postScanScript, scanDir, filename)
		}

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
func doScan(sr ScanRequest) ([]byte, error) {
	saneDeviceMutex.Lock()
	defer saneDeviceMutex.Unlock()

	// Falls der Watcher das Gerät offen hält, schließen wir es hier erzwungen.
	// Das passiert unter demselben Lock wie das Öffnen, damit der Watcher
	// das Gerät nicht zwischendurch wieder belegt.
	if conn := takeWatcherConn(sr.Device); conn != nil {
		conn.Close()
	}

	c, err := openDevice(sr.Device)
	if err != nil {
		return nil, err
	}
	defer c.Close()

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
	img, err := c.ReadImage()
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func openDevice(name string) (*sane.Conn, error) {
	return sane.Open(name)
}

// currentWatcherConn liefert die offene Watcher-Verbindung eines Geräts (oder nil).
// Aufrufer muss saneDeviceMutex halten, damit die Verbindung nicht parallel geschlossen wird.
func currentWatcherConn(device string) *sane.Conn {
	watcherConnMutex.Lock()
	defer watcherConnMutex.Unlock()
	return watcherConns[device]
}

// takeWatcherConn entfernt die Watcher-Verbindung eines Geräts aus der Map und gibt sie zurück (oder nil).
// Aufrufer muss saneDeviceMutex halten und ist danach fürs Schließen zuständig.
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
