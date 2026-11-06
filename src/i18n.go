package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// Sprachdateien für das Web-UI.
//
// Eingebaut sind die Dateien aus src/locales/. Zusätzlich werden Dateien aus
// SARAH_LOCALES_DIR (Standard: ./locales) gelesen – bei jedem Aufruf, also
// ohne Neubau und ohne Neustart. Gleichnamige Dateien dort ersetzen die
// eingebauten. Der Dateiname ist der Sprachcode (de.json, pt-BR.json),
// "_name" in der Datei der Anzeigename.
//
//   GET /i18n         -> [{"code":"de","name":"Deutsch"}, ...]
//   GET /i18n/:code   -> Inhalt der Sprachdatei

//go:embed locales/*.json
var builtinLocales embed.FS

var localeCode = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

type localeInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func registerLocales(router *gin.Engine, dir string) {
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		log.Printf("Web UI: additional languages are read from %s", dir)
	}

	router.GET("/i18n", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.JSON(http.StatusOK, listLocales(dir))
	})

	router.GET("/i18n/:code", func(c *gin.Context) {
		code := strings.TrimSuffix(c.Param("code"), ".json")
		if !localeCode.MatchString(code) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid language code"})
			return
		}
		data, err := readLocale(dir, code)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "language not found"})
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	})
}

// readLocale liefert die Sprachdatei, bevorzugt aus dem Verzeichnis.
func readLocale(dir, code string) ([]byte, error) {
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, code+".json")); err == nil {
			return data, nil
		}
	}
	return builtinLocales.ReadFile("locales/" + code + ".json")
}

func listLocales(dir string) []localeInfo {
	found := make(map[string]localeInfo)

	add := func(name string, data []byte, source string) {
		code := strings.TrimSuffix(name, ".json")
		if !localeCode.MatchString(code) {
			return
		}
		var meta struct {
			Name string `json:"_name"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			log.Printf("Web UI: ignoring invalid language file %s: %v", source, err)
			return
		}
		if meta.Name == "" {
			meta.Name = code
		}
		found[strings.ToLower(code)] = localeInfo{Code: code, Name: meta.Name}
	}

	if entries, err := fs.ReadDir(builtinLocales, "locales"); err == nil {
		for _, e := range entries {
			if data, err := builtinLocales.ReadFile("locales/" + e.Name()); err == nil {
				add(e.Name(), data, "builtin:"+e.Name())
			}
		}
	}
	if dir != "" {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				path := filepath.Join(dir, e.Name())
				if data, err := os.ReadFile(path); err == nil {
					add(e.Name(), data, path)
				}
			}
		}
	}

	list := make([]localeInfo, 0, len(found))
	for _, l := range found {
		list = append(list, l)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}
