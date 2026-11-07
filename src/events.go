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
// Events:
//   hello            on connect: {version, scanning: [scanInfo]}
//   scan-started     scanInfo
//   scan-completed   scanInfo (with filename, width, height)
//   scan-failed      scanInfo (with error)
//   button-pressed   {device, sensor, watcher}  watcher = whether a watcher exists for it
//   watchers-changed {}
//   ping             every SARAH_EVENTS_PING (default 25 s), keeps proxies/browsers happy

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

// publish sends an event to all clients. If a client falls behind,
// the event is dropped for it instead of blocking the scanner loop.
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
	c.Header("X-Accel-Buffering", "no") // nginx: do not buffer
	c.Status(http.StatusOK)
	c.SSEvent("hello", gin.H{"version": version, "scanning": runningScans()})
	c.Writer.Flush()

	ping := time.NewTicker(settings.EventsPing)
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

// --- Running scans ---

type scanInfo struct {
	ID       int64     `json:"id"`
	Device   string    `json:"device"`
	Source   string    `json:"source"` // "api", "button" or "preview"
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

// beginScan registers a scan as running and sends scan-started.
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
