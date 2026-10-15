package system

import (
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPowerButtonPersistenceAndRollback(t *testing.T) {
	store := config.Open(t.TempDir())
	m := NewPowerButtonManager(store)
	m.runtime = config.Open(t.TempDir())
	calls := 0
	fail := false
	m.reload = func() error {
		calls++
		if fail {
			return errors.New("reload failed")
		}
		return nil
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"ignore", "reboot", "poweroff"} {
		if err := m.Set(PowerButtonSettings{action}); err != nil {
			t.Fatal(err)
		}
		data, _ := m.runtime.Read(powerButtonDropin)
		if !strings.Contains(string(data), "HandlePowerKey="+action+"\n") {
			t.Fatal(string(data))
		}
	}
	before := calls
	if err := m.Set(PowerButtonSettings{"poweroff\nHandleSuspendKey=reboot"}); !errors.Is(err, ErrPowerButtonChoice) || calls != before {
		t.Fatal(err, calls)
	}
	fail = true
	if err := m.Set(PowerButtonSettings{"ignore"}); err == nil || m.Status().Settings.Action != "poweroff" {
		t.Fatal(err)
	}
	data, _ := m.runtime.Read(powerButtonDropin)
	if !strings.Contains(string(data), "HandlePowerKey=poweroff\n") {
		t.Fatal(string(data))
	}
	fail = false
	// A blocked configuration file must roll back the runtime change as well.
	if err := os.Remove(filepath.Join(store.Dir(), powerButtonFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(store.Dir(), powerButtonFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(PowerButtonSettings{"reboot"}); err == nil || m.Status().Settings.Action != "poweroff" {
		t.Fatal(err)
	}
	data, _ = m.runtime.Read(powerButtonDropin)
	if !strings.Contains(string(data), "HandlePowerKey=poweroff\n") {
		t.Fatal(string(data))
	}
}
func TestPowerButtonReadOnlyAndStartup(t *testing.T) {
	store := config.Open(t.TempDir())
	if err := store.WriteJSON(powerButtonFile, PowerButtonSettings{"ignore"}); err != nil {
		t.Fatal(err)
	}
	m := NewPowerButtonManager(store)
	m.runtime = config.Open(t.TempDir())
	m.Disabled = true
	m.reload = func() error { t.Fatal("read-only reload"); return nil }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.Status().Settings.Action != "ignore" {
		t.Fatal(m.Status())
	}
	if err := m.Set(PowerButtonSettings{"reboot"}); err == nil {
		t.Fatal("read-only write")
	}
	entries, _ := os.ReadDir(m.runtime.Dir())
	if len(entries) != 0 {
		t.Fatal(entries)
	}
	m.Disabled = false
	calls := 0
	m.reload = func() error { calls++; return nil }
	if err := m.Load(); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
