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

// Das gebaute Vue-Frontend (web/ -> pnpm build) landet in src/ui/dist.
// "all:ui" bettet auch den Platzhalter ein, damit der Go-Build ohne
// gebautes Frontend nicht fehlschlägt.
//
//go:embed all:ui
var uiFiles embed.FS

const uiPrefix = "/ui"

// uiActive ist true, wenn das Web-UI ausgeliefert wird.
var uiActive bool

// registerUI hängt das Web-UI unter /ui/ ein. Gibt false zurück, wenn kein
// gebautes Frontend eingebettet ist.
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
					// Vite versieht Assets mit Hash im Namen
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

// --- Vorschau des letzten Scans ---
//
// Das Post-Scan-Skript verschiebt bzw. löscht die PNG oft sofort, daher hält
// der Dienst den letzten Scan zusätzlich im Speicher.

var (
	lastScanMutex sync.Mutex
	lastScanName  string
	lastScanData  []byte
)

func storeLastScan(name string, data []byte) {
	lastScanMutex.Lock()
	defer lastScanMutex.Unlock()
	lastScanName = name
	lastScanData = data
}

func getLastScan(c *gin.Context) {
	lastScanMutex.Lock()
	name, data := lastScanName, lastScanData
	lastScanMutex.Unlock()

	if data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No scan available yet"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Scan-Filename", name)
	c.Data(http.StatusOK, "image/png", data)
}
