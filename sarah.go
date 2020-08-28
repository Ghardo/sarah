package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/schachmat/ingo"
	"github.com/tjgq/sane"
)

type ScanRequest struct {
	Device  string                 `json:"Device"`
	Options map[string]interface{} `json:"Options"`
}

var (
	lastScanImage  []byte
	allowedOrigins *string
	version        = "1.0.3"
)

func main() {
	port := flag.Int("port", 7575, "The api listen on this port.")
	allowedOrigins = flag.String("origin", "http://localhost:8080", " Comma separated list of allowed origin for cors")
	useTls := flag.Bool("tls", false, "Use tls for this service.")
	certFile := flag.String("cert", "", "The cert file for tls encryption.")
	certKeyFile := flag.String("cert-key", "", "The key file for tls cert")

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

	router := gin.Default()
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = strings.Split(*allowedOrigins, ",")
	router.Use(cors.New(corsConfig))

	router.POST("/scan", scan)
	router.GET("/", hello)
	router.GET("/list", list)
	router.GET("/config", config)
	router.GET("/image", getScanImage)
	router.GET("/version", getVersion)

	if *useTls {
		router.RunTLS(strings.Join([]string{":", strconv.Itoa(*port)}, ""), *certFile, *certKeyFile)
	} else {
		router.Run(strings.Join([]string{":", strconv.Itoa(*port)}, ""))
	}
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

func hello(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("Hi, i am sarah. Version: "+version))
}

func getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": version})
}

func scan(c *gin.Context) {

	var sr ScanRequest
	if err := c.ShouldBindJSON(&sr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var err error
	lastScanImage, err = doScan(sr)
	if handleError(c, err) {
		return
	}

	reader := bytes.NewReader(lastScanImage)
	img, _, err := image.DecodeConfig(reader)

	if err != nil {
		handleInternalError(c, err) // handle error somehow
	}

	c.JSON(http.StatusOK, gin.H{"width": img.Width, "height": img.Height})
}

func getScanImage(c *gin.Context) {
	reader := bytes.NewReader(lastScanImage)
	contentType := "image/png"
	contentDispostion := fmt.Sprintf("attachment; filename=scan.png")
	extraHeaders := map[string]string{
		"Content-Disposition": contentDispostion,
	}
	c.DataFromReader(http.StatusOK, int64(reader.Len()), contentType, reader, extraHeaders)
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
	return nil, errors.New("unknown device")
}

func doScan(sr ScanRequest) (b []byte, err error) {
	var c *sane.Conn

	c, err = openDevice(sr.Device)
	if err != nil {
		return b, err
	}
	defer c.Close()

	for oName, oValue := range sr.Options {
		var o *sane.Option
		o, err = findOption(c.Options(), oName)

		if err != nil {
			return b, err
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

	var img *sane.Image
	img, err = c.ReadImage()
	if err != nil {
		return b, err
	}

	buf := new(bytes.Buffer)

	if err := png.Encode(buf, img); err != nil {
		return b, err
	}

	return buf.Bytes(), nil
}

func findOption(opts []sane.Option, name string) (*sane.Option, error) {
	for _, o := range opts {
		if o.Name == name {
			return &o, nil
		}
	}
	return nil, errors.New("no such option")
}

func handleError(c *gin.Context, err error) bool {

	if err == nil {
		return false
	}

	if err == sane.ErrUnsupported {
		handleBadRequest(c, err)
		return true
	}

	if err == sane.ErrCancelled {
		handleBadRequest(c, err)
		return true
	}

	if err == sane.ErrBusy {
		handleBadRequest(c, err)
		return true
	}

	if err == sane.ErrInvalid {
		handleBadRequest(c, err)
		return true
	}

	if err == sane.ErrJammed {
		handleInternalError(c, err)
		return true
	}

	if err == sane.ErrEmpty {
		handleInternalError(c, err)
		return true
	}

	if err == sane.ErrCoverOpen {
		handleInternalError(c, err)
		return true
	}

	if err == sane.ErrIo {
		handleInternalError(c, err)
		return true
	}

	if err == sane.ErrNoMem {
		handleInternalError(c, err)
		return true
	}

	if err == sane.ErrDenied {
		handleInternalError(c, err)
		return true
	}

	if err == errors.New("no such option") {
		handleBadRequest(c, err)
		return true
	}

	if err == errors.New("unknown device") {
		handleBadRequest(c, err)
		return true
	}

	handleInternalError(c, err)
	return true
}

func handleBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	log.Printf(err.Error())

}

func handleInternalError(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	log.Printf(err.Error())
}
