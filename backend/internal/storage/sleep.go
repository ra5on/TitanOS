package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const diskSleepFile = "storage/disk-sleep.json"

var ErrInvalidIdleTime = errors.New("storage: unsupported disk idle time")

// DiskSleepSettings applies to all rotational disks, never to SSDs or the stick.
// Only the duration is persistent: device names are discovered afresh each time.
type DiskSleepSettings struct {
	IdleMinutes int `json:"idleMinutes"`
}

type DiskSleepError struct {
	Disk    string `json:"disk"`
	Message string `json:"message"`
}

type DiskSleepStatus struct {
	DiskSleepSettings
	Available bool             `json:"available"`
	Errors    []DiskSleepError `json:"errors"`
}

func validIdleMinutes(minutes int) bool {
	return slices.Contains([]int{0, 5, 10, 15, 30, 60, 120, 180}, minutes)
}

func (m *Manager) loadDiskSleep() error {
	m.actions.Lock()
	defer m.actions.Unlock()
	var settings DiskSleepSettings
	if err := m.store.ReadJSON(diskSleepFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !validIdleMinutes(settings.IdleMinutes) {
		return fmt.Errorf("%w: %d", ErrInvalidIdleTime, settings.IdleMinutes)
	}
	m.mu.Lock()
	m.diskSleep, m.diskSleepLoaded = settings, true
	m.mu.Unlock()
	m.sleepSamples = nil
	return nil
}

func (m *Manager) DiskSleep() DiskSleepStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return DiskSleepStatus{DiskSleepSettings: m.diskSleep, Available: m.diskSleepLoaded, Errors: append([]DiskSleepError{}, m.diskSleepErrors...)}
}

func (m *Manager) SetDiskSleep(settings DiskSleepSettings) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if !validIdleMinutes(settings.IdleMinutes) {
		return fmt.Errorf("%w: %d", ErrInvalidIdleTime, settings.IdleMinutes)
	}
	current := m.DiskSleep()
	if !current.Available {
		return ErrNotLoaded
	}
	if settings != current.DiskSleepSettings {
		if err := m.store.WriteJSON(diskSleepFile, settings); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.diskSleep, m.diskSleepErrors = settings, nil
	m.mu.Unlock()
	// A changed timeout starts a full new idle period. Off needs no disk command
	// and does not spin up disks that are already asleep.
	m.sleepSamples = nil
	return nil
}

// RunDiskSleep observes kernel counters without reading disk data or /config.
// hd-idle is used only for one-shot standby commands; its daemon stays masked.
func (m *Manager) RunDiskSleep(ctx context.Context) {
	if m.ReadOnly {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	lastError := ""
	for {
		var err error
		if !m.DiskSleep().Available {
			err = m.loadDiskSleep()
		}
		if err == nil {
			err = m.sleepIdleDisks(ctx, time.Now())
		}
		if err != nil && err.Error() != lastError {
			slog.Warn("disk sleep unavailable", "err", err)
		}
		lastError = ""
		if err != nil {
			lastError = err.Error()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type diskIO struct {
	counters [7]uint64 // reads/sectors, writes/sectors, discards/sectors, flushes
	inFlight uint64
}

func readDiskIO(path string) (diskIO, error) {
	var io diskIO
	data, err := os.ReadFile(path)
	if err != nil {
		return io, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 11 && len(fields) != 15 && len(fields) < 17 {
		return io, errors.New("incomplete disk I/O counters")
	}
	values := make([]uint64, len(fields))
	for i, field := range fields {
		values[i], err = strconv.ParseUint(field, 10, 64)
		if err != nil {
			return io, err
		}
	}
	for i, field := range []int{0, 2, 4, 6, 11, 13, 15} {
		if field < len(values) {
			io.counters[i] = values[field]
		}
	}
	io.inFlight = values[8]
	return io, nil
}

type sleepSample struct {
	identity        string
	io              diskIO
	lastIO, sampled time.Time
	attempted       bool
	err             string
}

var sleepDeviceName = regexp.MustCompile(`^/dev/sd[a-z]+$`)

func standbyArgs(device blockDevice) []string {
	if !isDisk(device) || !device.Rota || device.Removable || !sleepDeviceName.MatchString(device.Name) || classify(device, nil).Role == RoleStick {
		return nil
	}
	switch device.Transport {
	case "ata", "sata":
		return []string{"-c", "ata", "-t", device.Name}
	case "sas", "spi", "scsi", "":
		// SAS must use standby power condition 3. A plain STOP would require
		// an explicit START instead of waking automatically on the next I/O.
		return []string{"-c", "scsi", "-p", "3", "-t", device.Name}
	case "usb":
		return []string{"-c", "scsi", "-p", "0", "-t", device.Name}
	default:
		return nil // NVMe, network-backed disks and unknown transports
	}
}

func (m *Manager) sleepIdentity(device blockDevice) string {
	return strings.Join([]string{device.Serial, device.Model, device.Transport, strconv.FormatUint(device.Size, 10),
		readSysfs(filepath.Join(m.sysBlock, filepath.Base(device.Name), "diskseq"))}, "\x00")
}

func (m *Manager) sleepIdleDisks(ctx context.Context, now time.Time) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	settings := m.DiskSleep()
	if m.ReadOnly || !settings.Available || settings.IdleMinutes == 0 || ctx.Err() != nil {
		m.sleepSamples = nil
		return nil
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		m.sleepSamples = nil // an observation gap never counts as confirmed idle
		return err
	}
	busy := m.testing()
	if m.Claimed != nil {
		for disk := range m.Claimed() {
			busy[disk] = true
		}
	}
	// Exclude every array doing maintenance, even arrays not owned by TitanOS.
	busyParts := map[string]bool{}
	for _, array := range m.arrays(ctx) {
		if array.syncAction != "idle" && array.syncAction != "" {
			for _, member := range array.members {
				busyParts[member.device] = true
			}
		}
	}
	next := map[string]sleepSample{}
	var problems []DiskSleepError
	for _, device := range devices {
		name := filepath.Base(device.Name)
		args := standbyArgs(device)
		device.walk(func(part blockDevice) {
			if busyParts[filepath.Base(part.KName)] {
				busy[name] = true
			}
		})
		if args == nil || busy[name] {
			continue
		}
		stat := filepath.Join(m.sysBlock, name, "stat")
		io, err := readDiskIO(stat)
		if err != nil {
			continue // unknown activity must never be treated as idle
		}
		identity := m.sleepIdentity(device)
		previous, known := m.sleepSamples[name]
		if !known || previous.identity != identity || previous.io.counters != io.counters || io.inFlight != 0 || now.Sub(previous.sampled) > 30*time.Second || now.Before(previous.sampled) {
			next[name] = sleepSample{identity: identity, io: io, lastIO: now, sampled: now}
			continue
		}
		previous.sampled = now
		if !previous.attempted && now.Sub(previous.lastIO) >= time.Duration(settings.IdleMinutes)*time.Minute {
			// Check counters and the kernel's device identity again immediately
			// before issuing standby. The actions lock excludes disk tests here.
			fresh, err := readDiskIO(stat)
			if err != nil || fresh != io || m.sleepIdentity(device) != identity {
				continue
			}
			commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err = m.runner.Run(commandCtx, "", "hd-idle", args...)
			cancel()
			previous.attempted = true
			if err != nil {
				previous.err = err.Error()
				slog.Warn("disk standby failed", "disk", name, "err", err)
			} else {
				slog.Info("disk entered standby", "disk", name, "idleMinutes", settings.IdleMinutes)
			}
		}
		if previous.err != "" {
			problems = append(problems, DiskSleepError{Disk: name, Message: previous.err})
		}
		next[name] = previous
	}
	m.sleepSamples = next
	sort.Slice(problems, func(i, j int) bool { return problems[i].Disk < problems[j].Disk })
	m.mu.Lock()
	m.diskSleepErrors = problems
	m.mu.Unlock()
	return nil
}
