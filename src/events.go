package main

import (
	"bytes"
	"image"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// --- Server-Sent Events (GET /events) ---
//
// Ereignisse:
//   hello            beim Verbinden: {version, scanning: [scanInfo]}
//   scan-started     scanInfo
//   scan-completed   scanInfo (mit filename, width, height)
//   scan-failed      scanInfo (mit error)
//   button-pressed   {device, sensor, watcher}  watcher = ob ein Watcher dafür existiert
//   watchers-changed {}
//   ping             alle 25 s, hält Proxies/Browser bei Laune

type serverEvent struct {
	name string
	data any
}

type eventBroker struct {
	mu      sync.Mutex
	clients map[chan serverEvent]struct{}
}

var events = &eventBroker{clients: make(map[chan serverEvent]struct{})}

func (b *eventBroker) subscribe() chan serverEvent {
	ch := make(chan serverEvent, 32)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *eventBroker) unsubscribe(ch chan serverEvent) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

// publish verschickt ein Ereignis an alle Clients. Hängt ein Client hinterher,
// wird das Ereignis für ihn verworfen, statt den Scanner-Loop zu blockieren.
func (b *eventBroker) publish(name string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- serverEvent{name, data}:
		default:
		}
	}
}

func eventsHandler(c *gin.Context) {
	ch := events.subscribe()
	defer events.unsubscribe(ch)

	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no") // nginx: nicht puffern
	c.Status(http.StatusOK)
	c.SSEvent("hello", gin.H{"version": version, "scanning": runningScans()})
	c.Writer.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	c.Stream(func(io.Writer) bool {
		select {
		case ev := <-ch:
			c.SSEvent(ev.name, ev.data)
			return true
		case <-ping.C:
			c.SSEvent("ping", time.Now().Unix())
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

// --- Laufende Scans ---

type scanInfo struct {
	ID       int64     `json:"id"`
	Device   string    `json:"device"`
	Source   string    `json:"source"` // "api" oder "button"
	Sensor   string    `json:"sensor,omitempty"`
	Started  time.Time `json:"started"`
	Filename string    `json:"filename,omitempty"`
	Width    int       `json:"width,omitempty"`
	Height   int       `json:"height,omitempty"`
	Error    string    `json:"error,omitempty"`
}

var (
	scansMutex sync.Mutex
	scansNext  int64
	scansOpen  = make(map[int64]scanInfo)
)

// beginScan meldet einen Scan als laufend an und verschickt scan-started.
func beginScan(device, source, sensor string) *scanInfo {
	scansMutex.Lock()
	scansNext++
	s := scanInfo{ID: scansNext, Device: device, Source: source, Sensor: sensor, Started: time.Now()}
	scansOpen[s.ID] = s
	scansMutex.Unlock()

	events.publish("scan-started", s)
	return &s
}

func (s *scanInfo) finish() {
	scansMutex.Lock()
	delete(scansOpen, s.ID)
	scansMutex.Unlock()
}

func (s *scanInfo) done(filename string, img []byte) {
	s.finish()
	s.Filename = filename
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(img)); err == nil {
		s.Width, s.Height = cfg.Width, cfg.Height
	}
	events.publish("scan-completed", *s)
}

func (s *scanInfo) fail(err error) {
	s.finish()
	s.Error = err.Error()
	events.publish("scan-failed", *s)
}

func runningScans() []scanInfo {
	scansMutex.Lock()
	defer scansMutex.Unlock()
	list := make([]scanInfo, 0, len(scansOpen))
	for _, s := range scansOpen {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}
