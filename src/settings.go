package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// --- Settings via environment variables ---
//
// Everything that is not set per scan or watcher in the web UI.
// Durations as Go duration ("50ms", "2s", "1m") or as a plain number in milliseconds.

type Settings struct {
	// Poll interval of the scanner buttons (SARAH_POLL_INTERVAL)
	PollInterval time.Duration
	// Delay at startup before the saved watchers are loaded (SARAH_WATCHER_START_DELAY)
	WatcherStartDelay time.Duration
	// Pause of the button polling of a device after a watcher scan was triggered (SARAH_SCAN_COOLDOWN)
	ScanCooldown time.Duration
	// Debounce of a button without watcher, so a press is reported only once (SARAH_BUTTON_COOLDOWN)
	ButtonCooldown time.Duration
	// Consecutive errors reading the buttons after which the connection is reopened (SARAH_SENSOR_ERROR_LIMIT)
	SensorErrorLimit int
	// Resolution of the preview for the area selection in dpi (SARAH_PREVIEW_RESOLUTION)
	PreviewResolution int
	// JPEG quality if a scan or watcher does not set one (SARAH_JPEG_QUALITY)
	JPEGQuality int
	// File name template if a scan or watcher does not set one (SARAH_FILENAME_TEMPLATE)
	FilenameTemplate string
	// Go layouts for {date} and {time} in the file name (SARAH_DATE_FORMAT, SARAH_TIME_FORMAT)
	DateFormat string
	TimeFormat string
	// Permissions of saved scans and of the created scan directory (SARAH_FILE_MODE, SARAH_DIR_MODE)
	FileMode os.FileMode
	DirMode  os.FileMode
	// Interval of the keep-alive pings in the event stream (SARAH_EVENTS_PING)
	EventsPing time.Duration
}

// settings applies to the whole service and is set at startup in loadConfig.
var settings = defaultSettings()

func defaultSettings() Settings {
	return Settings{
		PollInterval:      50 * time.Millisecond,
		WatcherStartDelay: 5 * time.Second,
		ScanCooldown:      2 * time.Second,
		ButtonCooldown:    1 * time.Second,
		SensorErrorLimit:  5,
		PreviewResolution: 75,
		JPEGQuality:       85,
		FilenameTemplate:  "scan_{index}_{date}_{time}",
		DateFormat:        "20060102",
		TimeFormat:        "150405",
		FileMode:          0644,
		DirMode:           0755,
		EventsPing:        25 * time.Second,
	}
}

// loadSettings reads the environment. Invalid values are an error, so sarah
// does not silently run with something other than intended.
func loadSettings() (Settings, error) {
	s := defaultSettings()
	var errs []string
	check := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	check(envDuration("SARAH_POLL_INTERVAL", &s.PollInterval, 10*time.Millisecond))
	check(envDuration("SARAH_WATCHER_START_DELAY", &s.WatcherStartDelay, 0))
	check(envDuration("SARAH_SCAN_COOLDOWN", &s.ScanCooldown, 0))
	check(envDuration("SARAH_BUTTON_COOLDOWN", &s.ButtonCooldown, 0))
	check(envDuration("SARAH_EVENTS_PING", &s.EventsPing, time.Second))
	check(envInt("SARAH_SENSOR_ERROR_LIMIT", &s.SensorErrorLimit, 1, 1000))
	check(envInt("SARAH_PREVIEW_RESOLUTION", &s.PreviewResolution, 10, 1200))
	check(envInt("SARAH_JPEG_QUALITY", &s.JPEGQuality, 1, 100))
	check(envFileMode("SARAH_FILE_MODE", &s.FileMode))
	check(envFileMode("SARAH_DIR_MODE", &s.DirMode))

	if v, ok := os.LookupEnv("SARAH_FILENAME_TEMPLATE"); ok {
		v = strings.TrimSpace(v)
		if v == "" || strings.ContainsAny(v, `/\`) {
			check(fmt.Errorf("SARAH_FILENAME_TEMPLATE: %q must not be empty or contain / or \\", v))
		} else {
			s.FilenameTemplate = v
		}
	}
	check(envLayout("SARAH_DATE_FORMAT", &s.DateFormat))
	check(envLayout("SARAH_TIME_FORMAT", &s.TimeFormat))

	if len(errs) > 0 {
		return s, fmt.Errorf("invalid settings: %s", strings.Join(errs, "; "))
	}
	return s, nil
}

// envDuration accepts a Go duration ("50ms", "2s") or milliseconds ("100").
func envDuration(key string, dst *time.Duration, min time.Duration) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	v = strings.TrimSpace(v)
	d, err := time.ParseDuration(v)
	if err != nil {
		ms, errMs := strconv.Atoi(v)
		if errMs != nil {
			return fmt.Errorf("%s: %q is neither a duration (e.g. 50ms, 2s) nor milliseconds", key, v)
		}
		d = time.Duration(ms) * time.Millisecond
	}
	if d < min {
		return fmt.Errorf("%s: %s is below the minimum of %s", key, d, min)
	}
	*dst = d
	return nil
}

func envInt(key string, dst *int, min, max int) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < min || n > max {
		return fmt.Errorf("%s: %q must be a number between %d and %d", key, v, min, max)
	}
	*dst = n
	return nil
}

// envFileMode accepts Unix permissions as an octal number ("0644", "664").
func envFileMode(key string, dst *os.FileMode) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(v), 8, 32)
	if err != nil || n > 0o777 {
		return fmt.Errorf("%s: %q must be an octal mode like 0644", key, v)
	}
	*dst = os.FileMode(n)
	return nil
}

// envLayout accepts a Go time layout. Path separators are not allowed, the result ends up in the file name.
func envLayout(key string, dst *string) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if strings.TrimSpace(v) == "" || strings.ContainsAny(v, `/\`) {
		return fmt.Errorf("%s: %q must not be empty or contain / or \\", key, v)
	}
	*dst = v
	return nil
}

// writeScanFile saves a scan with SARAH_FILE_MODE. The permissions are set
// explicitly afterwards, because os.WriteFile applies the umask (0664 would become 0644).
func writeScanFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, settings.FileMode); err != nil {
		return err
	}
	return os.Chmod(path, settings.FileMode)
}

// getDefaults returns the defaults shown by the web UI (GET /defaults).
func getDefaults(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"filenameTemplate": settings.FilenameTemplate,
		"jpegQuality":      settings.JPEGQuality,
	})
}
