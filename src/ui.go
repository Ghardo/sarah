package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// The built Vue frontend (web/ -> pnpm build) ends up in src/ui/dist.
// "all:ui" also embeds the placeholder so the Go build does not fail
// without a built frontend.
//
//go:embed all:ui
var uiFiles embed.FS

const uiPrefix = "/ui"

// uiActive is true when the web UI is served.
var uiActive bool

// registerUI mounts the web UI at /ui/. Returns false if no built
// frontend is embedded.
func registerUI(router *gin.Engine) bool {
	dist, err := fs.Sub(uiFiles, "ui/dist")
	if err != nil {
		log.Printf("Web UI not available: %v", err)
		return false
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		log.Println("Web UI not available: frontend was not built into this binary (run 'pnpm build' in web/)")
		return false
	}

	fileServer := http.StripPrefix(uiPrefix, http.FileServer(http.FS(dist)))

	router.GET(uiPrefix+"/*path", func(c *gin.Context) {
		p := strings.TrimPrefix(c.Param("path"), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(dist, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					// Vite puts a hash into asset names
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})

	log.Printf("Web UI enabled at %s/", uiPrefix)
	return true
}

// --- Preview of the last scan ---
//
// The post-scan script often moves or deletes the file right away, so the
// service additionally keeps the last scan in memory.

var (
	lastScanMutex sync.Mutex
	lastScanName  string
	lastScanData  []byte
	lastScanMime  string
)

func storeLastScan(name string, data []byte, mime string) {
	lastScanMutex.Lock()
	defer lastScanMutex.Unlock()
	lastScanName = name
	lastScanData = data
	lastScanMime = mime
}

func getLastScan(c *gin.Context) {
	lastScanMutex.Lock()
	name, data, mime := lastScanName, lastScanData, lastScanMime
	lastScanMutex.Unlock()

	if data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No scan available yet"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Scan-Filename", name)
	c.Data(http.StatusOK, mime, data)
}
