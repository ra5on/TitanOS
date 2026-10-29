package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
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

var (
	ErrInvalidIdleTime = errors.New("storage: unsupported disk idle time")
	// ErrUnknownSleepTarget is a disk sleep override for a volume or disk
	// that does not exist (any more), or a disk key that is no stable identity.
	ErrUnknownSleepTarget = errors.New("storage: unknown volume or disk for disk sleep")
)

// Where the effective idle time of a disk comes from.
const (
	SleepFromDefault = "default" // the global value
	SleepFromVolume  = "volume"  // the override of the disk's volume
	SleepFromDisk    = "disk"    // the disk's own override
)

// DiskSleepSettings applies to all rotational disks, never to SSDs or the stick.
// Only durations are persistent: device names are discovered afresh each time.
// IdleMinutes is the default; Volumes (by volume name) and Disks (by
// sleepDiskID: serial or WWN) override it, 0 meaning never sleep. A missing
// entry inherits. Older titand ignore the two optional maps and keep the
// global value.
type DiskSleepSettings struct {
	IdleMinutes int            `json:"idleMinutes"`
	Volumes     map[string]int `json:"volumes,omitempty"`
	Disks       map[string]int `json:"disks,omitempty"`
}

func (s DiskSleepSettings) equal(other DiskSleepSettings) bool {
	return s.IdleMinutes == other.IdleMinutes && maps.Equal(s.Volumes, other.Volumes) && maps.Equal(s.Disks, other.Disks)
}

func (s DiskSleepSettings) clone() DiskSleepSettings {
	s.Volumes, s.Disks = maps.Clone(s.Volumes), maps.Clone(s.Disks)
	return s
}

// effective resolves the idle time of a disk: disk > volume > global.
// diskID is empty for a disk without stable identity, volume for a disk
// outside every volume.
func (s DiskSleepSettings) effective(diskID, volume string) (int, string) {
	if minutes, ok := s.Disks[diskID]; ok && diskID != "" {
		return minutes, SleepFromDisk
	}
	if minutes, ok := s.Volumes[volume]; ok && volume != "" {
		return minutes, SleepFromVolume
	}
	return s.IdleMinutes, SleepFromDefault
}

// anyActive tells whether some disk may sleep at all.
func (s DiskSleepSettings) anyActive() bool {
	if s.IdleMinutes != 0 {
		return true
	}
	for _, overrides := range []map[string]int{s.Volumes, s.Disks} {
		for _, minutes := range overrides {
			if minutes != 0 {
				return true
			}
		}
	}
	return false
}

func (s DiskSleepSettings) validate() error {
	if !validIdleMinutes(s.IdleMinutes) {
		return fmt.Errorf("%w: %d", ErrInvalidIdleTime, s.IdleMinutes)
	}
	for _, overrides := range []map[string]int{s.Volumes, s.Disks} {
		for key, minutes := range overrides {
			if !validIdleMinutes(minutes) {
				return fmt.Errorf("%w: %s: %d", ErrInvalidIdleTime, key, minutes)
			}
		}
	}
	for key := range s.Disks {
		if !validSleepDiskID(key) {
			return fmt.Errorf("%w: disk %q", ErrUnknownSleepTarget, key)
		}
	}
	return nil
}

// normalized drops empty maps so a file without overrides stays exactly
// what older titand wrote.
func (s DiskSleepSettings) normalized() DiskSleepSettings {
	s = s.clone()
	if len(s.Volumes) == 0 {
		s.Volumes = nil
	}
	if len(s.Disks) == 0 {
		s.Disks = nil
	}
	return s
}

// sleepDiskID is the stable identity a disk's own setting is stored under:
// its serial number, or its WWN if it reports no serial. Never a device
// name, which changes between starts. Empty if the disk has neither.
func sleepDiskID(device blockDevice) string {
	if serial := strings.TrimSpace(device.Serial); serial != "" {
		return "serial:" + serial
	}
	if wwn := strings.ToLower(strings.TrimSpace(device.WWN)); wwn != "" {
		return "wwn:" + wwn
	}
	return ""
}

func validSleepDiskID(id string) bool {
	value, ok := strings.CutPrefix(id, "serial:")
	if !ok {
		value, ok = strings.CutPrefix(id, "wwn:")
	}
	return ok && value != "" && len(id) <= 128 && strings.TrimSpace(value) == value &&
		!strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// sleepDiskIDs returns the identity of every disk by device path. Disks
// that share an identity (USB enclosures reporting the same serial) get none:
// one setting must never silently apply to another disk.
func sleepDiskIDs(devices []blockDevice) map[string]string {
	count := map[string]int{}
	for _, device := range devices {
		if isDisk(device) {
			count[sleepDiskID(device)]++
		}
	}
	ids := map[string]string{}
	for _, device := range devices {
		if id := sleepDiskID(device); isDisk(device) && id != "" && count[id] == 1 {
			ids[device.Name] = id
		}
	}
	return ids
}

type DiskSleepError struct {
	Disk    string `json:"disk"`
	Message string `json:"message"`
}

// DiskSleepVolume is a configured volume with its setting.
type DiskSleepVolume struct {
	Name string `json:"name"`
	// Override is nil when the volume inherits the default.
	Override  *int   `json:"override"`
	Effective int    `json:"effective"`
	Origin    string `json:"origin"`
}

// DiskSleepDisk is a disk TitanOS can put to sleep, as the settings show it.
// Only the RAM state of the sleep timer is used; nothing reads the disk.
type DiskSleepDisk struct {
	// ID is empty for a disk without unique serial or WWN: it can only
	// inherit.
	ID        string `json:"id"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Serial    string `json:"serial"`
	SizeBytes uint64 `json:"sizeBytes"`
	Volume    string `json:"volume,omitempty"`
	Override  *int   `json:"override"`
	Effective int    `json:"effective"`
	Origin    string `json:"origin"`
	// State is "asleep" after a standby command without activity since,
	// "active" otherwise.
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

type DiskSleepStatus struct {
	DiskSleepSettings
	Available bool             `json:"available"`
	Errors    []DiskSleepError `json:"errors"`
	// Set only where disks were listed (storage status, the disk sleep
	// routes). VolumeStatus and DiskStatus list what exists now;
	// MissingVolumes and MissingDisks are stored overrides without their
	// volume or disk, removed when the user saves next.
	VolumeStatus   []DiskSleepVolume `json:"volumeStatus,omitempty"`
	DiskStatus     []DiskSleepDisk   `json:"diskStatus,omitempty"`
	MissingVolumes []string          `json:"missingVolumes,omitempty"`
	MissingDisks   []string          `json:"missingDisks,omitempty"`
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
	if err := settings.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	m.diskSleep, m.diskSleepLoaded = settings.normalized(), true
	m.mu.Unlock()
	m.resetSleepSamples()
	return nil
}

// DiskSleep returns the settings and errors from RAM, without disk lists.
func (m *Manager) DiskSleep() DiskSleepStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return DiskSleepStatus{DiskSleepSettings: m.diskSleep.clone(), Available: m.diskSleepLoaded, Errors: append([]DiskSleepError{}, m.diskSleepErrors...)}
}

// DiskSleepStatus adds the volumes and disks with their effective values.
// It lists disks like Status (lsblk) and reads neither a disk nor /config.
func (m *Manager) DiskSleepStatus(ctx context.Context) (DiskSleepStatus, error) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return DiskSleepStatus{}, err
	}
	return m.diskSleepStatus(devices), nil
}

func (m *Manager) diskSleepStatus(devices []blockDevice) DiskSleepStatus {
	status := m.DiskSleep()
	settings := status.DiskSleepSettings
	configured := m.configured()
	override := func(values map[string]int, key string) *int {
		if minutes, ok := values[key]; ok && key != "" {
			return &minutes
		}
		return nil
	}
	known := map[string]bool{}
	status.VolumeStatus = []DiskSleepVolume{}
	for _, volume := range configured {
		known[volume.Name] = true
		minutes, origin := settings.effective("", volume.Name)
		status.VolumeStatus = append(status.VolumeStatus, DiskSleepVolume{Name: volume.Name, Override: override(settings.Volumes, volume.Name), Effective: minutes, Origin: origin})
	}
	for name := range settings.Volumes {
		if !known[name] {
			status.MissingVolumes = append(status.MissingVolumes, name)
		}
	}
	errorsByDisk := map[string]string{}
	for _, problem := range status.Errors {
		errorsByDisk[problem.Disk] = problem.Message
	}
	m.mu.RLock()
	asleep := m.diskSleepAsleep
	m.mu.RUnlock()
	ids := sleepDiskIDs(devices)
	byUUID := volumesByUUID(configured)
	present := map[string]bool{}
	status.DiskStatus = []DiskSleepDisk{}
	for _, device := range devices {
		if !isDisk(device) || standbyArgs(device) == nil {
			continue
		}
		disk := classify(device, byUUID)
		id := ids[device.Name]
		present[id] = true
		minutes, origin := settings.effective(id, disk.Volume)
		state := "active"
		if asleep[disk.Name] {
			state = "asleep"
		}
		status.DiskStatus = append(status.DiskStatus, DiskSleepDisk{
			ID: id, Name: disk.Name, Model: disk.Model, Serial: disk.Serial, SizeBytes: disk.SizeBytes, Volume: disk.Volume,
			Override: override(settings.Disks, id), Effective: minutes, Origin: origin, State: state, Error: errorsByDisk[disk.Name],
		})
	}
	for id := range settings.Disks {
		if !present[id] {
			status.MissingDisks = append(status.MissingDisks, id)
		}
	}
	sort.Strings(status.MissingVolumes)
	sort.Strings(status.MissingDisks)
	sort.Slice(status.DiskStatus, func(i, j int) bool { return status.DiskStatus[i].Name < status.DiskStatus[j].Name })
	return status
}

// sleepableDiskIDs are the identities of the present disks TitanOS can put
// to sleep.
func sleepableDiskIDs(devices []blockDevice) map[string]bool {
	ids := sleepDiskIDs(devices)
	result := map[string]bool{}
	for _, device := range devices {
		if id := ids[device.Name]; id != "" && standbyArgs(device) != nil {
			result[id] = true
		}
	}
	return result
}

// SetDiskSleep replaces the settings. A new or changed override must name a
// configured volume or a present disk; an entry stored unchanged is kept
// even while its disk is away, so changing only the default drops nothing.
// The file is written only if something changed.
func (m *Manager) SetDiskSleep(ctx context.Context, settings DiskSleepSettings) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	return m.setDiskSleepLocked(ctx, settings)
}

// UpdateDiskSleep sets the default and the overrides that are given (nil
// keeps the stored ones). The stored ones are read under the same lock as
// the change is made: two changes at once, one of the volumes and one of
// the disks, both stay.
func (m *Manager) UpdateDiskSleep(ctx context.Context, idleMinutes int, volumes, disks *map[string]int) (DiskSleepSettings, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	settings := m.DiskSleep().DiskSleepSettings
	settings.IdleMinutes = idleMinutes
	if volumes != nil {
		settings.Volumes = *volumes
	}
	if disks != nil {
		settings.Disks = *disks
	}
	return settings, m.setDiskSleepLocked(ctx, settings)
}

func (m *Manager) setDiskSleepLocked(ctx context.Context, settings DiskSleepSettings) error {
	if m.ReadOnly {
		return ErrReadOnly
	}
	settings = settings.normalized()
	if err := settings.validate(); err != nil {
		return err
	}
	current := m.DiskSleep()
	if !current.Available {
		return ErrNotLoaded
	}
	volumes := map[string]bool{}
	for _, volume := range m.configured() {
		volumes[volume.Name] = true
	}
	for name, minutes := range settings.Volumes {
		if old, ok := current.Volumes[name]; !volumes[name] && (!ok || old != minutes) {
			return fmt.Errorf("%w: volume %q", ErrUnknownSleepTarget, name)
		}
	}
	var disks map[string]bool
	for id, minutes := range settings.Disks {
		if old, ok := current.Disks[id]; ok && old == minutes {
			continue
		}
		if disks == nil {
			devices, err := m.blockDevices(ctx)
			if err != nil {
				return err
			}
			disks = sleepableDiskIDs(devices)
		}
		if !disks[id] {
			return fmt.Errorf("%w: disk %q", ErrUnknownSleepTarget, id)
		}
	}
	if !settings.equal(current.DiskSleepSettings) {
		if err := m.store.WriteJSON(diskSleepFile, settings); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.diskSleep, m.diskSleepErrors = settings, nil
	m.mu.Unlock()
	// A changed timeout starts a full new idle period. Off needs no disk command
	// and does not spin up disks that are already asleep.
	m.resetSleepSamples()
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
	if m.ReadOnly || !settings.Available || !settings.anyActive() || ctx.Err() != nil {
		m.resetSleepSamples()
		return nil
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		m.resetSleepSamples() // an observation gap never counts as confirmed idle
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
	busyBtrfs := map[string]bool{}
	ids := sleepDiskIDs(devices)
	byUUID := volumesByUUID(m.configured())
	m.mu.RLock()
	for _, volume := range m.volumes {
		if state := m.btrfsProgress[volume.Name]; state.State == VolumeChecking || state.State == VolumeRebuilding {
			busyBtrfs[volume.FilesystemUUID] = true
		}
	}
	m.mu.RUnlock()
	next := map[string]sleepSample{}
	var problems []DiskSleepError
	for _, device := range devices {
		name := filepath.Base(device.Name)
		args := standbyArgs(device)
		device.walk(func(part blockDevice) {
			if busyParts[filepath.Base(part.KName)] || part.FSType == "btrfs" && busyBtrfs[part.UUID] {
				busy[name] = true
			}
		})
		if args == nil || busy[name] {
			continue
		}
		// Members of a volume take its value, a disk's own value comes
		// first. Never sleeping keeps no sample: switching sleep on again
		// starts a full idle period.
		minutes, _ := settings.effective(ids[device.Name], classify(device, byUUID).Volume)
		if minutes == 0 {
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
		if !previous.attempted && now.Sub(previous.lastIO) >= time.Duration(minutes)*time.Minute {
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
				slog.Info("disk entered standby", "disk", name, "idleMinutes", minutes)
			}
		}
		if previous.err != "" {
			problems = append(problems, DiskSleepError{Disk: name, Message: previous.err})
		}
		next[name] = previous
	}
	m.sleepSamples = next
	sort.Slice(problems, func(i, j int) bool { return problems[i].Disk < problems[j].Disk })
	asleep := map[string]bool{}
	for name, sample := range next {
		if sample.attempted && sample.err == "" {
			asleep[name] = true
		}
	}
	m.mu.Lock()
	m.diskSleepErrors, m.diskSleepAsleep = problems, asleep
	m.mu.Unlock()
	return nil
}

// resetSleepSamples starts every timer afresh (caller holds actions). What
// is known to sleep is forgotten too: without a sample it is unknown.
func (m *Manager) resetSleepSamples() {
	m.sleepSamples = nil
	m.mu.Lock()
	m.diskSleepAsleep = nil
	m.mu.Unlock()
}
