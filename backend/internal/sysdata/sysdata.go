// Package sysdata keeps the choice of where TitanOS stores its own data
// (PLAN 79-8): one volume for everything, and a volume of their own for the
// functions that have no settings file (kept journal, notifications, SMART
// history). The activity log, the copies of /config and the monitoring
// history keep their choice in their own settings.
package sysdata

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const settingsFile = "system/sysdata.json"

// The functions with a place of their own on the page. The value is the
// folder below .titanos on the volume.
const (
	Config     = "config"
	Logs       = "logs"
	Activity   = "activity"
	Notify     = "notify"
	Smart      = "smart"
	Monitoring = "monitoring"
)

// Functions are all functions in the order of the page.
var Functions = []string{Config, Logs, Activity, Notify, Smart, Monitoring}

// Folder is the folder of a function below .titanos.
func Folder(function string) string {
	if function == Config {
		return "backups"
	}
	return function
}

// stored are the functions whose own volume is kept here.
var stored = []string{Logs, Notify, Smart}

// Stored tells whether the own volume of a function is kept in Settings.
func Stored(function string) bool { return slices.Contains(stored, function) }

// ErrInvalid is returned for settings that cannot be stored.
var ErrInvalid = errors.New("invalid system data settings")

// Settings is /config/system/sysdata.json. The file is new with 0.77;
// older versions do not read it and choose the volume by themselves.
type Settings struct {
	// Volume is the volume for everything without a choice of its own;
	// empty lets TitanOS choose (a volume that never sleeps, else the
	// first one).
	Volume string `json:"volume,omitempty"`
	// Overrides are the own volumes of logs, notify and smart.
	Overrides map[string]string `json:"overrides,omitempty"`
	// Pending names, per function, volumes that still hold data from
	// before a change: a move that was cut short is taken up again.
	Pending map[string][]string `json:"pending,omitempty"`
}

func (s Settings) clone() Settings {
	out := Settings{Volume: s.Volume, Overrides: maps.Clone(s.Overrides), Pending: map[string][]string{}}
	for function, volumes := range s.Pending {
		out.Pending[function] = slices.Clone(volumes)
	}
	if len(out.Pending) == 0 {
		out.Pending = nil
	}
	return out
}

func validate(s Settings) error {
	if s.Volume != "" && !naming.ValidVolume(s.Volume) {
		return fmt.Errorf("%w: volume", ErrInvalid)
	}
	for function, volume := range s.Overrides {
		if !Stored(function) || !naming.ValidVolume(volume) {
			return fmt.Errorf("%w: %s", ErrInvalid, function)
		}
	}
	for function, volumes := range s.Pending {
		if !slices.Contains(Functions, function) || function == Config {
			return fmt.Errorf("%w: pending", ErrInvalid)
		}
		for _, volume := range volumes {
			if !naming.ValidVolume(volume) {
				return fmt.Errorf("%w: pending", ErrInvalid)
			}
		}
	}
	return nil
}

// Manager holds the settings and the sizes last measured.
type Manager struct {
	store *config.Store
	// MountRoot is where volumes are mounted.
	MountRoot string
	// Mounted returns the mounted volumes; Asleep tells whether reading a
	// volume would wake its disks. Both are needed for sizes.
	Mounted func() []string
	Asleep  func(volume string) bool

	mu       sync.Mutex
	settings Settings
	sizes    map[string]*size
	work     sync.WaitGroup
	now      func() time.Time
}

// New returns a manager; Load reads the settings.
func New(store *config.Store) *Manager {
	return &Manager{store: store, MountRoot: "/mnt", sizes: map[string]*size{}, now: time.Now}
}

// Load reads the settings from /config. A missing file means: TitanOS
// chooses. An unreadable one too, and the error says so.
func (m *Manager) Load() error {
	var s Settings
	if err := m.store.ReadJSON(settingsFile, &s); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validate(s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}

// Settings returns a copy of the settings.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.clone()
}

// Volume is the volume chosen for everything, "" for the automatic choice.
func (m *Manager) Volume() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.Volume
}

// Override is the own volume of logs, notify or smart, "" without one.
func (m *Manager) Override(function string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.Overrides[function]
}

// Pending are the volumes that still hold data of a function to move.
func (m *Manager) Pending(function string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.settings.Pending[function])
}

// Update changes the settings and stores them if they differ. change gets
// a copy and may alter it freely.
func (m *Manager) Update(change func(*Settings)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.settings.clone()
	if next.Overrides == nil {
		next.Overrides = map[string]string{}
	}
	if next.Pending == nil {
		next.Pending = map[string][]string{}
	}
	change(&next)
	maps.DeleteFunc(next.Overrides, func(_, volume string) bool { return volume == "" })
	maps.DeleteFunc(next.Pending, func(_ string, volumes []string) bool { return len(volumes) == 0 })
	if len(next.Overrides) == 0 {
		next.Overrides = nil
	}
	if len(next.Pending) == 0 {
		next.Pending = nil
	}
	if err := validate(next); err != nil {
		return err
	}
	if next.Volume == m.settings.Volume && maps.Equal(next.Overrides, m.settings.Overrides) && maps.EqualFunc(next.Pending, m.settings.Pending, slices.Equal) {
		return nil
	}
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		return err
	}
	m.settings = next
	return nil
}

// Settle forgets a volume whose data of a function were moved away.
func (m *Manager) Settle(function, from string) error {
	return m.Update(func(s *Settings) {
		s.Pending[function] = slices.DeleteFunc(s.Pending[function], func(volume string) bool { return volume == from })
	})
}

// AddPending notes in s that a function still has data on a volume.
func AddPending(s *Settings, function, from string) {
	if from == "" || function == Config || slices.Contains(s.Pending[function], from) {
		return
	}
	s.Pending[function] = append(s.Pending[function], from)
}

// --- sizes -----------------------------------------------------------------

// sizeMaxAge is how long a measured size is shown before it is measured
// again (only when someone looks at the page).
const sizeMaxAge = 10 * time.Minute

type size struct {
	bytes   int64
	at      time.Time
	running bool
	// aside are entries a move put next to the current ones
	// (volumedata.Asides), found when the size was measured.
	aside []string
}

// Size is the size of a function's folder on a volume as last measured;
// at is zero while it was never measured. Never reads a disk itself: an
// old or missing value is measured in the background, and only while the
// volume is mounted and its disks are awake.
func (m *Manager) Size(volume, function string) (bytes int64, at time.Time) {
	if volume == "" || m.Mounted == nil {
		return 0, time.Time{}
	}
	key := volume + "\x00" + function
	m.mu.Lock()
	entry := m.sizes[key]
	if entry == nil {
		entry = &size{}
		m.sizes[key] = entry
	}
	bytes, at = entry.bytes, entry.at
	stale := at.IsZero() || m.now().Sub(at) > sizeMaxAge
	start := stale && !entry.running
	if start {
		mounted := m.Mounted()
		if !slices.Contains(mounted, volume) || (m.Asleep != nil && m.Asleep(volume)) {
			start = false
		} else {
			entry.running = true
			m.work.Go(func() { m.measure(entry, volume, function, mounted) })
		}
	}
	m.mu.Unlock()
	return bytes, at
}

func (m *Manager) measure(entry *size, volume, function string, mounted []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	bytes, err := volumedata.Size(ctx, m.MountRoot, volume, Folder(function), mounted)
	aside, asideErr := volumedata.Asides(m.MountRoot, volume, Folder(function), mounted)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.running = false
	if err == nil {
		entry.bytes, entry.at = bytes, m.now()
	}
	if asideErr == nil {
		entry.aside = aside
	}
}

// Aside are the entries of a function's folder on a volume that a move
// put next to the current ones ("<name>.from-<volume>") because both
// volumes held the name with other content: kept, not used. As found when
// the size was last measured (Size starts that); never reads a disk itself.
func (m *Manager) Aside(volume, function string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.sizes[volume+"\x00"+function]; entry != nil {
		return slices.Clone(entry.aside)
	}
	return nil
}

// Wait waits for sizes being measured (tests).
func (m *Manager) Wait() { m.work.Wait() }
