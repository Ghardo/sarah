package main

import (
	"log"
	"math"
	"net/http"

	"github.com/Ghardo/sane"
	"github.com/gin-gonic/gin"
)

// --- Preview scan for the area selection in the web UI ---
//
// Always scans the whole bed at a low resolution. No file is saved and no
// post-scan script runs, so the preview never ends up in paperless. The PNG
// is returned directly in the response.

// previewResolution is the desired preview resolution in dpi.
const previewResolution = 75

var geometryOptions = []string{"tl-x", "tl-y", "br-x", "br-y"}

func previewHandler(c *gin.Context) {
	var sr ScanRequest
	if err := c.ShouldBindJSON(&sr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	if sr.Device == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is required"})
		return
	}

	scan := beginScan(sr.Device, "preview", "")
	raw, err := scanDevice(sr.Device, func(opts []sane.Option) map[string]any {
		return previewOptions(opts, sr.Options)
	})
	var img []byte
	if err == nil {
		img, err = encodeImage(raw.img, formatPNG, 0, raw.dpiX, raw.dpiY)
	}
	if err != nil {
		log.Printf("ERROR: Preview scan failed: %v", err)
		scan.fail(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	scan.done("", img)

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", img)
}

// previewOptions keeps the options from the form (e.g. mode, source) but sets
// the area to the whole bed and lowers the resolution.
func previewOptions(opts []sane.Option, base map[string]any) map[string]any {
	values := make(map[string]any, len(base)+6)
	for k, v := range base {
		values[k] = v
	}

	for _, name := range geometryOptions {
		delete(values, name)
		o, err := findOption(opts, name)
		if err != nil || !o.IsActive || !o.IsSettable || o.ConstrRange == nil {
			continue
		}
		if name == "br-x" || name == "br-y" {
			values[name] = toFloat(o.ConstrRange.Max)
		} else {
			values[name] = toFloat(o.ConstrRange.Min)
		}
	}

	if o, err := findOption(opts, "resolution"); err == nil && o.IsActive && o.IsSettable {
		if r, ok := closestAllowed(o, previewResolution); ok {
			values["resolution"] = r
		}
	}

	// Many backends have their own preview option (faster, no calibration).
	if o, err := findOption(opts, "preview"); err == nil && o.IsActive && o.IsSettable && o.Type == sane.TypeBool {
		values["preview"] = true
	}
	return values
}

// closestAllowed returns the allowed value of a numeric option closest to want.
func closestAllowed(o *sane.Option, want float64) (float64, bool) {
	switch {
	case len(o.ConstrSet) > 0:
		best, found := 0.0, false
		for _, v := range o.ConstrSet {
			f := toFloat(v)
			if !found || math.Abs(f-want) < math.Abs(best-want) {
				best, found = f, true
			}
		}
		return best, found
	case o.ConstrRange != nil:
		lo, hi := toFloat(o.ConstrRange.Min), toFloat(o.ConstrRange.Max)
		v := math.Min(math.Max(want, lo), hi)
		if q := toFloat(o.ConstrRange.Quant); q > 0 {
			v = lo + math.Round((v-lo)/q)*q
			v = math.Min(v, hi)
		}
		return v, true
	default:
		return want, true
	}
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	default:
		return 0
	}
}
