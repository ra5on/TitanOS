package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const powerButtonFile = "system/power-button.json"
const powerButtonDropin = "90-titanos-power-button.conf"

var ErrPowerButtonChoice = errors.New("invalid power button action")

type PowerButtonSettings struct {
	Action string `json:"action"`
}
type PowerButtonStatus struct {
	Settings PowerButtonSettings `json:"settings"`
	ReadOnly bool                `json:"readOnly"`
	Error    string              `json:"error,omitempty"`
}

// Settings are read once at startup. Only a deliberate administrator change
// writes /config. Reloading logind does not stop sessions, VMs or containers.
type PowerButtonManager struct {
	store    *config.Store
	runtime  *config.Store
	reload   func() error
	Disabled bool
	mu       sync.Mutex
	settings PowerButtonSettings
	loaded   bool
	problem  string
}

func NewPowerButtonManager(store *config.Store) *PowerButtonManager {
	return &PowerButtonManager{store: store, runtime: config.Open("/run/systemd/logind.conf.d"), settings: PowerButtonSettings{Action: "poweroff"}, reload: func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "systemctl", "reload", "systemd-logind.service").CombinedOutput()
		if err != nil {
			return fmt.Errorf("reload logind: %w: %s", err, output)
		}
		return nil
	}}
}
func validPowerButton(s PowerButtonSettings) bool {
	return s.Action == "ignore" || s.Action == "reboot" || s.Action == "poweroff"
}
func (m *PowerButtonManager) Status() PowerButtonStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return PowerButtonStatus{m.settings, m.Disabled || !m.loaded, m.problem}
}
func (m *PowerButtonManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := PowerButtonSettings{Action: "poweroff"}
	err := m.store.ReadJSON(powerButtonFile, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.problem = err.Error()
		return err
	}
	if !validPowerButton(s) {
		m.problem = ErrPowerButtonChoice.Error()
		return ErrPowerButtonChoice
	}
	m.loaded, m.settings = true, s
	if !m.Disabled {
		_, err = m.apply(s)
		if err != nil {
			m.problem = err.Error()
		}
		return err
	}
	return nil
}

// Return a rollback that restores the exact previous runtime file, including
// its absence. The caller uses it when persistent storage fails.
func (m *PowerButtonManager) apply(s PowerButtonSettings) (func() error, error) {
	before, err := m.runtime.Read(powerButtonDropin)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	missing := errors.Is(err, fs.ErrNotExist)
	restore := func() error {
		var err error
		if missing {
			err = os.Remove(filepath.Join(m.runtime.Dir(), powerButtonDropin))
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		} else {
			err = m.runtime.Write(powerButtonDropin, before)
		}
		if err != nil {
			return err
		}
		return m.reload()
	}
	content := []byte("[Login]\nHandlePowerKey=" + s.Action + "\nPowerKeyIgnoreInhibited=no\n")
	if err := m.runtime.Write(powerButtonDropin, content); err != nil {
		return nil, err
	}
	if err := m.reload(); err != nil {
		return nil, errors.Join(err, restore())
	}
	return restore, nil
}
func (m *PowerButtonManager) Set(s PowerButtonSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded {
		return errors.New("power button settings unavailable")
	}
	if !validPowerButton(s) {
		return ErrPowerButtonChoice
	}
	if s == m.settings && m.problem == "" {
		return nil
	}
	rollback, err := m.apply(s)
	if err != nil {
		m.problem = err.Error()
		return err
	}
	if err := m.store.WriteJSON(powerButtonFile, s); err != nil {
		err = errors.Join(err, rollback())
		m.problem = err.Error()
		return err
	}
	m.settings, m.problem = s, ""
	return nil
}
