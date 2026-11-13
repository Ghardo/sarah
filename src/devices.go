package main

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Ghardo/sane"
)

// --- Device registry ---
//
// SANE device names are not stable:
//   - pixma reports a USB scanner as "pixma:04A9176D_<serial>", but only while
//     nobody holds it open. While it is open (e.g. by sarah's button polling)
//     the serial cannot be read and the same scanner is listed as "pixma:04A9176D",
//     a name that sane_open then rejects.
//   - USB names like "genesys:libusb:001:003" contain the USB address, which
//     changes when the scanner is plugged in again.
//
// The registry remembers which devices are present and finds a device again
// under the name it has now (same backend, vendor and model).

type deviceRegistry struct {
	mu      sync.Mutex
	present map[string]sane.Device // devices of the last discovery, by name
	seen    map[string]sane.Device // every device seen since the start
}

var devices = &deviceRegistry{
	present: make(map[string]sane.Device),
	seen:    make(map[string]sane.Device),
}

// discoverDevices lists the SANE devices and updates the registry.
// The caller must hold saneDeviceMutex.
func discoverDevices() ([]sane.Device, error) {
	devs, err := sane.Devices()
	if err != nil {
		return nil, err
	}
	devs = devices.update(devs, openWatcherDevices())
	return devs, nil
}

// update stores the result of a discovery. Devices listed under a shortened or
// changed name keep the name they are known by (see above), so the returned list
// only contains names that can be opened.
func (r *deviceRegistry) update(devs []sane.Device, open []string) []sane.Device {
	r.mu.Lock()
	defer r.mu.Unlock()

	listed := make(map[string]bool, len(devs))
	for _, d := range devs {
		listed[d.Name] = true
	}
	isOpen := make(map[string]bool, len(open))
	for _, name := range open {
		isOpen[name] = true
	}

	// Devices sarah holds open itself: the backend may list them under another name meanwhile.
	for _, name := range open {
		info, ok := r.seen[name]
		if listed[name] || !ok {
			continue
		}
		if i := uniqueMatch(devs, func(d sane.Device) bool { return !isOpen[d.Name] && sameModel(d, info) }); i >= 0 {
			delete(listed, devs[i].Name)
			devs[i].Name = name
			listed[name] = true
		}
	}

	// Devices listed without the serial that they had before (pixma while another program holds them).
	for i, d := range devs {
		var full string
		n := 0
		for name, info := range r.seen {
			if !listed[name] && strings.HasPrefix(name, d.Name+"_") && sameModel(d, info) {
				full = name
				n++
			}
		}
		if n == 1 {
			delete(listed, d.Name)
			devs[i].Name = full
			listed[full] = true
		}
	}

	r.present = make(map[string]sane.Device, len(devs))
	for _, d := range devs {
		r.present[d.Name] = d
		r.seen[d.Name] = d
	}
	return devs
}

// resolve returns the name under which a device is present now, and whether it is present.
// vendor and model identify the device if its name is no longer listed (they are stored
// with the watchers). loose also accepts the only present device of the same backend; it
// is used for watchers saved before vendor and model were stored.
func (r *deviceRegistry) resolve(name, vendor, model string, loose bool) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.present[name]; ok {
		return name, true
	}
	if d, ok := r.seen[name]; ok {
		vendor, model = d.Vendor, d.Model
	}

	match, n := "", 0
	for present, d := range r.present {
		if backendOf(present) != backendOf(name) {
			continue
		}
		switch {
		case strings.HasPrefix(name, present+"_"):
			// The device is only listed without its serial (it is busy): the full name stays valid.
			return name, true
		case strings.HasPrefix(present, name+"_"):
			// The name lacks the serial, the device is listed with it.
		case vendor != "" || model != "":
			if d.Vendor != vendor || d.Model != model {
				continue
			}
		case !loose:
			continue
		}
		match = present
		n++
	}
	if n == 1 {
		return match, true
	}
	return name, false
}

// canonicalDevice returns the name under which the device can be opened now
// (the name itself if the device is not known).
func canonicalDevice(name string) string {
	resolved, _ := devices.resolve(name, "", "", false)
	return resolved
}

// markGone removes a device that can no longer be opened. Returns whether it was present.
func (r *deviceRegistry) markGone(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.present[name]
	delete(r.present, name)
	return ok
}

// info returns vendor and model of a device seen since the start.
func (r *deviceRegistry) info(name string) (sane.Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.seen[name]
	return d, ok
}

// list returns the devices of the last discovery, sorted by name.
func (r *deviceRegistry) list() []sane.Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	devs := make([]sane.Device, 0, len(r.present))
	for _, d := range r.present {
		devs = append(devs, d)
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].Name < devs[j].Name })
	return devs
}

// names returns the names of the present devices.
func (r *deviceRegistry) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.present))
	for name := range r.present {
		names = append(names, name)
	}
	return names
}

func backendOf(name string) string {
	backend, _, _ := strings.Cut(name, ":")
	return backend
}

func sameModel(a, b sane.Device) bool {
	return backendOf(a.Name) == backendOf(b.Name) && a.Vendor == b.Vendor && a.Model == b.Model
}

// uniqueMatch returns the index of the only device matching ok, or -1.
func uniqueMatch(devs []sane.Device, ok func(sane.Device) bool) int {
	found := -1
	for i, d := range devs {
		if !ok(d) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

// openWatcherDevices returns the devices the button polling holds open.
func openWatcherDevices() []string {
	watcherConnMutex.Lock()
	defer watcherConnMutex.Unlock()
	names := make([]string, 0, len(watcherConns))
	for name := range watcherConns {
		names = append(names, name)
	}
	return names
}

// lockSane waits up to d for saneDeviceMutex. Returns whether it is held now.
func lockSane(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if saneDeviceMutex.TryLock() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// --- Watchers and devices ---

// watcherPaused holds the watchers whose device is not present (guarded by watcherMutex).
var watcherPaused = make(map[string]bool)

func watcherKeyOf(device, sensor string) string {
	return device + ":" + sensor
}

// syncWatcherDevices matches the watchers with the present devices: a watcher whose
// device is present under another name is moved to that name, a watcher whose device
// is missing is paused until it is back. Returns whether a watcher is paused.
func syncWatcherDevices() bool {
	watcherMutex.Lock()
	defer watcherMutex.Unlock()

	type move struct {
		from, to string
		req      ScanRequest
	}
	var moves []move
	changed := false
	paused := false

	for key, req := range activeWatchers {
		loose := req.DeviceVendor == "" && req.DeviceModel == ""
		name, ok := devices.resolve(req.Device, req.DeviceVendor, req.DeviceModel, loose)
		if !ok {
			paused = true
			if !watcherPaused[key] {
				watcherPaused[key] = true
				changed = true
				log.Printf("Watcher '%s': device is not present, paused until it is back", key)
			}
			continue
		}
		if watcherPaused[key] {
			delete(watcherPaused, key)
			changed = true
			log.Printf("Watcher '%s': device is present again", key)
		}

		info, _ := devices.info(name)
		if name == req.Device && info.Vendor == req.DeviceVendor && info.Model == req.DeviceModel {
			continue
		}
		to := watcherKeyOf(name, req.WatchSensor)
		if _, taken := activeWatchers[to]; taken && to != key {
			log.Printf("Watcher '%s': device is now '%s', but a watcher for '%s' already exists", key, name, to)
			continue
		}
		req.Device, req.DeviceVendor, req.DeviceModel = name, info.Vendor, info.Model
		moves = append(moves, move{key, to, req})
	}

	if len(moves) > 0 {
		configs, err := loadWatchersFromFile()
		if err != nil {
			log.Printf("ERROR loading watcher config to update device names: %v", err)
			configs = nil
		}
		for _, m := range moves {
			delete(activeWatchers, m.from)
			activeWatchers[m.to] = m.req
			if configs != nil {
				delete(configs, m.from)
				configs[m.to] = m.req
			}
			if m.from != m.to {
				log.Printf("Watcher '%s' moved to '%s'", m.from, m.to)
			}
		}
		if configs != nil {
			if err := saveWatchersToFile(configs); err != nil {
				log.Printf("ERROR saving watcher config with updated device names: %v", err)
			}
		}
		changed = true
	}

	if changed {
		events.publish("watchers-changed", map[string]any{})
	}
	return paused
}

// --- Buttons ---

// deviceButtons are the options of an open device that the button polling reads.
type deviceButtons struct {
	buttons []sane.Option
	// The backend keeps a press until the device is opened again (pixma, recognised
	// by its option button-update). Reading button-1 … fetches the state from the scanner.
	latched bool
}

func buttonOptions(opts []sane.Option) deviceButtons {
	var b deviceButtons
	for _, o := range opts {
		if isWatchableButton(o) {
			b.buttons = append(b.buttons, o)
		}
		if o.Type == sane.TypeButton && o.Name == "button-update" {
			b.latched = true
		}
	}
	return b
}

// isWatchableButton tells whether a watcher can watch the option (keep in sync with sensorOptions in web/src/options.ts).
// Most backends put the buttons into the group "Sensors". pixma uses the group "Buttons",
// where only button-1, button-2 … are buttons; the other options there describe the press.
func isWatchableButton(o sane.Option) bool {
	if !o.IsActive || !o.IsDetectable || (o.Type != sane.TypeBool && o.Type != sane.TypeInt) {
		return false
	}
	switch o.Group {
	case "Sensors":
		return true
	case "Buttons":
		return strings.HasPrefix(o.Name, "button-")
	}
	return false
}
