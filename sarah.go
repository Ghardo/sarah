package main

import (
	"bufio"
	"flag"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/schachmat/ingo"
	"github.com/tjgq/sane"
)

type ScanRequest struct {
	Device  string                 `json:"device"`
	Options map[string]interface{} `json:"options"`
}

var scanFile string
var scanPath string

func main() {

	port := flag.Int("port", 7575, "The api listen on this port.")
	flag.StringVar(&scanPath, "path", "/tmp", "The path scans saved.")

	if os.Getenv("SARAHRC") == "" {
		os.Setenv("SARAHRC", "/etc/sarahrc")
	}

	if err := ingo.Parse("sarah"); err != nil {
		log.Fatal(err)
	}

	if err := sane.Init(); err != nil {
		die(err)
	}
	defer sane.Exit()

	os.MkdirAll(scanPath, os.ModePerm)

	router := gin.Default()
	router.Use(cors.Default())

	router.POST("/scan", scan)
	router.GET("/", last)
	router.GET("/list", list)
	router.GET("/config", config)
	router.GET("/last", last)

	router.Run(strings.Join([]string{":", strconv.Itoa(*port)}, ""))
}

func die(v ...interface{}) {
	if len(v) > 0 {
		fmt.Fprintln(os.Stderr, v...)
	}
	os.Exit(1)
}

func list(c *gin.Context) {
	devs, err := sane.Devices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if devs == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "No devices found."})
		return
	}

	c.JSON(http.StatusOK, devs)
}

func config(c *gin.Context) {

	device := c.Query("device")

	if device == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No device given"})
		return
	}

	sc, err := openDevice(device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer sc.Close()

	c.JSON(http.StatusOK, sc.Options())
}

func last(c *gin.Context) {
	if c.GetHeader("Accept") == "application/json" {
		c.JSON(http.StatusOK, gin.H{"path": scanPath, "file": scanFile})
	} else {
		lastScan := fmt.Sprintf("%s/%s", scanPath, scanFile)
		readImage(c, lastScan)
	}
}

func scan(c *gin.Context) {

	var sr ScanRequest
	if err := c.ShouldBindJSON(&sr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	scanFile = getScanFilename()
	lastScan := fmt.Sprintf("%s/%s", scanPath, scanFile)
	err := doScan(sr, lastScan)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	last(c)
}

func getScanFilename() string {
	t := time.Now()
	return fmt.Sprintf("scan-%04d%02d%02d%02d%02d%02d.png", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
}

func readImage(c *gin.Context, filename string) {

	f, err := os.Open(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		log.Fatal(err)
	}

	fi, err := f.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}

	reader := bufio.NewReader(f)

	contentLength := fi.Size()
	contentType := "image/png"

	contentDispostion := fmt.Sprintf("attachment; filename=\"%s\"", filename)

	extraHeaders := map[string]string{
		"Content-Disposition": contentDispostion,
	}

	c.DataFromReader(http.StatusOK, contentLength, contentType, reader, extraHeaders)
}

func openDevice(name string) (*sane.Conn, error) {
	c, err := sane.Open(name)
	if err == nil {
		return c, nil
	}
	// Try a substring match over the available devices
	devs, err := sane.Devices()
	if err != nil {
		return nil, err
	}
	for _, d := range devs {
		if strings.Contains(d.Name, name) {
			return sane.Open(d.Name)
		}
	}
	return nil, fmt.Errorf("no device named %s", name)
}

func doScan(sr ScanRequest, fileName string) error {
	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer f.Close()

	c, err := openDevice(sr.Device)
	if err != nil {
		return err
	}
	defer c.Close()

	for oName, oValue := range sr.Options {
		o, err := findOption(c.Options(), oName)

		if err != nil {
			return err
		}

		if o.IsSettable {

			var v interface{}

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

			c.SetOption(o.Name, v)
		}
	}

	img, err := c.ReadImage()
	if err != nil {
		return err
	}

	if err := png.Encode(f, img); err != nil {
		return err
	}

	return nil
}

func findOption(opts []sane.Option, name string) (*sane.Option, error) {
	for _, o := range opts {
		if o.Name == name {
			return &o, nil
		}
	}
	return nil, fmt.Errorf("no such option %s", name)
}
