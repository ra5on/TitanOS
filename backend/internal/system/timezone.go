package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const settingsFile = "system/settings.json"

// ErrInvalidTimezone means the name is not a time zone the system knows.
var ErrInvalidTimezone = errors.New("system: no such time zone")

var timezoneRE = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+){0,2}$`)

// Settings keeps what is set for the system as a whole. For now that is
// the time zone; it lives in /config/system/settings.json and is put in
// place on every start, as the root file system is fresh each time.
type Settings struct {
	store *config.Store
	// Disabled leaves the system alone. Set in development mode.
	Disabled bool
	// zoneinfo is where the time zones are, localtime the link to the one in use.
	zoneinfo, localtime, timezoneFile string

	mu       sync.Mutex
	timezone string
}

// NewSettings returns the settings of the real system.
func NewSettings(store *config.Store) *Settings {
	return &Settings{store: store, zoneinfo: "/usr/share/zoneinfo", localtime: "/etc/localtime", timezoneFile: "/etc/timezone"}
}

// Load reads the settings and puts them in place.
func (s *Settings) Load() error {
	var file struct {
		Timezone string `json:"timezone"`
	}
	err := s.store.ReadJSON(settingsFile, &file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if file.Timezone == "" {
		return nil
	}
	return s.apply(file.Timezone)
}

func (s *Settings) apply(timezone string) error {
	if !timezoneRE.MatchString(timezone) {
		return ErrInvalidTimezone
	}
	zone := filepath.Join(s.zoneinfo, timezone)
	if info, err := os.Stat(zone); err != nil || !info.Mode().IsRegular() {
		return ErrInvalidTimezone
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Disabled {
		// A new link in one step, so that there is never none.
		temporary := s.localtime + ".new"
		os.Remove(temporary)
		if err := os.Symlink(zone, temporary); err != nil {
			return err
		}
		if err := os.Rename(temporary, s.localtime); err != nil {
			return err
		}
		os.WriteFile(s.timezoneFile, []byte(timezone+"\n"), 0o644)
	}
	s.timezone = timezone
	return nil
}

// SetTimezone changes the time zone of the NAS and remembers it.
func (s *Settings) SetTimezone(timezone string) error {
	if err := s.apply(timezone); err != nil {
		return err
	}
	return s.store.WriteJSON(settingsFile, map[string]string{"timezone": timezone})
}

// CurrentTimezone returns the time zone in use.
func (s *Settings) CurrentTimezone() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timezone != "" {
		return s.timezone
	}
	if target, err := os.Readlink(s.localtime); err == nil {
		if _, zone, found := strings.Cut(target, "zoneinfo/"); found && zone != "" {
			return zone
		}
	}
	return Timezone()
}
