package hardware

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

// UAS trouble of some USB-SATA bridges is avoided by driving them with
// usb-storage instead of uas: the kernel parameter
// usb-storage.quirks=<vid>:<pid>:u. TitanOS writes it only when the owner
// asks for it on the hardware page (decision of 5.11.2026), never on its
// own. Items for other devices or with other flags that are already in the
// boot entries, e.g. set by hand, are kept as they are.

// UASParam is the kernel parameter of the usb-storage quirks.
const UASParam = "usb-storage.quirks"

const uasConfig = "system/uas-quirks.json"

// maxUASDevices keeps the parameter short; the kernel takes up to 128
// characters for it.
const maxUASDevices = 8

var (
	// ErrUASDevice is a device ID that is not vendor:product in hex.
	ErrUASDevice = errors.New("hardware: device must be vvvv:pppp in hex")
	// ErrUASFull is one device too many.
	ErrUASFull = errors.New("hardware: too many USB quirks")
	usbID      = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{4}$`)
	quirkItem  = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{4}:[a-z]*$`)
)

// UASSettings is /config/system/uas-quirks.json.
type UASSettings struct {
	// Devices are the vendor:product IDs TitanOS drives with usb-storage.
	Devices []string `json:"devices"`
}

// UASStatus is the state for the hardware page.
type UASStatus struct {
	Devices []string `json:"devices"`
	// Active are the devices with the u flag on the command line the NAS
	// started with.
	Active []string `json:"active"`
	// Pending are devices whose setting differs from what is active; a
	// restart makes it so.
	Pending  []string          `json:"pending"`
	Entries  map[string]string `json:"entries"`
	Deferred bool              `json:"deferred"`
	Writable bool              `json:"writable"`
	Problem  string            `json:"problem,omitempty"`
}

// UASQuirks keeps the usb-storage quirks that TitanOS manages.
type UASQuirks struct {
	store *config.Store
	boot  BootParams
	// Disabled leaves the stick alone: development mode.
	Disabled   bool
	proc       string
	retryEvery time.Duration

	mu       sync.Mutex
	loaded   bool
	settings UASSettings
	// managed holds every device this run has set or taken out, so that
	// their items are not kept as foreign ones.
	managed map[string]bool
	entries update.ParamState
	problem string
	wake    chan struct{}
}

// NewUASQuirks returns the setting for the real system.
func NewUASQuirks(store *config.Store, boot BootParams, disabled bool) *UASQuirks {
	return &UASQuirks{store: store, boot: boot, Disabled: disabled, proc: "/proc",
		retryEvery: 30 * time.Second, managed: map[string]bool{}, wake: make(chan struct{}, 1)}
}

// Load reads the setting from /config, once at the start.
func (q *UASQuirks) Load() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	var settings UASSettings
	err := q.store.ReadJSON(uasConfig, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	settings.Devices = slices.DeleteFunc(settings.Devices, func(d string) bool { return !usbID.MatchString(d) })
	q.settings, q.loaded = settings, true
	for _, d := range settings.Devices {
		q.managed[d] = true
	}
	return nil
}

// quirkValue merges the devices into the value of the boot entries: items
// of other devices stay, items of managed devices are replaced.
func quirkValue(current string, devices []string, managed map[string]bool) string {
	var items []string
	for _, item := range strings.Split(current, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		id := item
		if len(item) >= 9 {
			id = item[:9]
		}
		if managed[id] || slices.Contains(devices, id) || slices.Contains(items, item) {
			continue
		}
		items = append(items, item)
	}
	for _, d := range devices {
		items = append(items, d+":u")
	}
	return strings.Join(items, ",")
}

// currentValue is what the boot entries hold: the value of any entry that
// has the parameter, merged, so a slot without it does not lose the others'.
func currentValue(state update.ParamState) string {
	var items []string
	for _, slot := range slices.Sorted(mapKeys(state.Entries)) {
		for _, item := range strings.Split(state.Entries[slot], ",") {
			if item != "" && !slices.Contains(items, item) {
				items = append(items, item)
			}
		}
	}
	return strings.Join(items, ",")
}

func mapKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// apply writes the setting into the boot entries.
func (q *UASQuirks) apply(devices []string) (update.ParamState, error) {
	value := quirkValue(currentValue(q.boot.KernelParam(UASParam)), devices, q.managed)
	return q.boot.SetKernelParam(UASParam, value)
}

// Run brings the boot entries in line with the setting after the start
// (an older version may have written an entry without it) and while the
// running entry is on trial. Nothing is written without a setting.
func (q *UASQuirks) Run(ctx context.Context) {
	if q.Disabled || q.boot == nil {
		return
	}
	tries := 0
	for {
		q.mu.Lock()
		devices := slices.Clone(q.settings.Devices)
		deferred := false
		if len(devices) > 0 {
			state, err := q.apply(devices)
			switch {
			case errors.Is(err, update.ErrBusy):
				deferred = true
			case err != nil && !errors.Is(err, update.ErrDisabled):
				q.problem = err.Error()
				slog.Warn("USB quirks not written to the boot entries", "err", err)
			case err == nil:
				q.entries, q.problem, deferred = state, "", state.Deferred
			}
		}
		q.mu.Unlock()
		var retry <-chan time.Time
		if deferred && tries < 40 {
			tries++
			retry = time.After(q.retryEvery)
		}
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
			tries = 0
		case <-retry:
		}
	}
}

// Set switches usb-storage for one device on or off. It takes effect with
// the next start of the NAS.
func (q *UASQuirks) Set(device string, on bool) error {
	device = strings.ToLower(strings.TrimSpace(device))
	if !usbID.MatchString(device) {
		return ErrUASDevice
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.loaded {
		return errors.New("hardware: the setting could not be read from /config")
	}
	if q.Disabled || q.boot == nil {
		return ErrBootUnavailable
	}
	devices := slices.DeleteFunc(slices.Clone(q.settings.Devices), func(d string) bool { return d == device })
	if on {
		devices = append(devices, device)
	}
	slices.Sort(devices)
	if len(devices) > maxUASDevices {
		return ErrUASFull
	}
	wasManaged := q.managed[device]
	q.managed[device] = true
	state, err := q.apply(devices)
	if errors.Is(err, update.ErrDisabled) {
		q.managed[device] = wasManaged
		return ErrBootUnavailable
	}
	if err != nil {
		q.managed[device] = wasManaged
		return err
	}
	settings := UASSettings{Devices: devices}
	if err := q.store.WriteJSON(uasConfig, settings); err != nil {
		if back, e := q.apply(q.settings.Devices); e == nil {
			q.entries = back
		}
		return err
	}
	q.settings, q.entries, q.problem = settings, state, ""
	slog.Info("USB quirk changed, effective after a restart", "device", device, "usb-storage", on)
	if state.Deferred {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// activeQuirks are the devices with the u flag on the running command line.
func activeQuirks(cmdline string) []string {
	active := []string{}
	for _, field := range strings.Fields(cmdline) {
		key, value, _ := strings.Cut(field, "=")
		if key != UASParam && key != "usb_storage.quirks" {
			continue
		}
		active = active[:0]
		for _, item := range strings.Split(strings.ToLower(value), ",") {
			if quirkItem.MatchString(item) && strings.Contains(item[10:], "u") {
				active = append(active, item[:9])
			}
		}
	}
	slices.Sort(active)
	return slices.Compact(active)
}

// Status reports the setting and what is active. It may read the stick
// once; meant for a page being opened.
func (q *UASQuirks) Status() UASStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	data, _ := os.ReadFile(filepath.Join(q.proc, "cmdline"))
	status := UASStatus{Devices: slices.Clone(q.settings.Devices), Active: activeQuirks(string(data)), Pending: []string{},
		Entries: map[string]string{}, Writable: !q.Disabled && q.boot != nil, Problem: q.problem}
	if status.Devices == nil {
		status.Devices = []string{}
	}
	for _, d := range status.Devices {
		if !slices.Contains(status.Active, d) {
			status.Pending = append(status.Pending, d)
		}
	}
	for d := range q.managed {
		if !slices.Contains(status.Devices, d) && slices.Contains(status.Active, d) {
			status.Pending = append(status.Pending, d)
		}
	}
	slices.Sort(status.Pending)
	if status.Writable {
		q.entries = q.boot.KernelParam(UASParam)
	}
	if q.entries.Entries != nil {
		status.Entries, status.Deferred = q.entries.Entries, q.entries.Deferred
	}
	return status
}
