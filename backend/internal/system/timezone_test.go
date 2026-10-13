package system

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestTimezone(t *testing.T) {
	root := t.TempDir()
	store := config.Open(filepath.Join(root, "config"))
	newSettings := func() *Settings {
		s := NewSettings(store)
		s.zoneinfo, s.localtime, s.timezoneFile = filepath.Join(root, "zoneinfo"), filepath.Join(root, "localtime"), filepath.Join(root, "timezone")
		return s
	}
	os.MkdirAll(filepath.Join(root, "zoneinfo", "Europe"), 0o755)
	os.WriteFile(filepath.Join(root, "zoneinfo", "Europe", "Berlin"), []byte("TZif"), 0o644)
	os.WriteFile(filepath.Join(root, "zoneinfo", "UTC"), []byte("TZif"), 0o644)

	s := newSettings()
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Europe/Atlantis", "../../etc/passwd", "Europe", "Europe/Berlin; reboot"} {
		if err := s.SetTimezone(bad); !errors.Is(err, ErrInvalidTimezone) {
			t.Errorf("SetTimezone(%q) = %v", bad, err)
		}
	}
	if err := s.SetTimezone("Europe/Berlin"); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(filepath.Join(root, "localtime")); target != filepath.Join(root, "zoneinfo", "Europe", "Berlin") {
		t.Errorf("localtime -> %q", target)
	}
	if s.CurrentTimezone() != "Europe/Berlin" {
		t.Errorf("CurrentTimezone = %q", s.CurrentTimezone())
	}
	// A fresh root file system gets the time zone back from /config.
	os.Remove(filepath.Join(root, "localtime"))
	again := newSettings()
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(filepath.Join(root, "localtime")); target == "" || again.CurrentTimezone() != "Europe/Berlin" {
		t.Errorf("after a restart: %q, %q", target, again.CurrentTimezone())
	}
}
