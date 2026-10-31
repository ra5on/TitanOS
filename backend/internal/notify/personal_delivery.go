package notify

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type Delivery struct {
	Time     time.Time `json:"time"`
	OK       bool      `json:"ok"`
	Attempts int       `json:"attempts"`
}

func enabledPersonal(p Personal) map[string]bool {
	return map[string]bool{"email": p.Email.Enabled, "ntfy": p.Ntfy.Enabled, "telegram": p.Telegram.Enabled, "discord": p.Discord.Enabled, "homeassistant": p.HomeAssistant.Enabled}
}

func (m *Manager) deliverPersonal(user, channel string, send func() error) bool {
	ok := false
	attempts := 0
	for attempt := range 3 {
		attempts++
		if send() == nil {
			ok = true
			break
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * m.retryPause)
		}
	}
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	if state := m.people.users[user]; state != nil {
		if state.delivery == nil {
			state.delivery = map[string]Delivery{}
		}
		state.delivery[channel] = Delivery{m.now(), ok, attempts}
	}
	return ok
}
func (m *Manager) Deliveries(user string) map[string]Delivery {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	out := map[string]Delivery{}
	if state := m.people.users[user]; state != nil {
		for k, v := range state.delivery {
			out[k] = v
		}
	}
	return out
}

// Configure before the first account is loaded. No periodic /config access.
func (m *Manager) SetPersonalDataDir(dir func() (string, bool)) {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	m.people.dir = dir
}

var notifyAccount = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func (m *Manager) quietStore(user string) (*config.Store, error) {
	if !notifyAccount.MatchString(user) || m.people.dir == nil {
		return nil, invalid("persistent notifications unavailable")
	}
	base, ok := m.people.dir()
	if !ok {
		return nil, invalid("notification volume unavailable")
	}
	dir := filepath.Join(base, user, "notifications")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return config.Open(dir), nil
}
func (m *Manager) saveQuiet(user string, messages []PersonalMessage) error {
	store, err := m.quietStore(user)
	if err != nil {
		return err
	}
	copy := slices.Clone(messages)
	return store.WriteJSON("pending.json", copy)
}
func (m *Manager) restoreQuiet(user string, state *personalState) error {
	store, err := m.quietStore(user)
	if err != nil {
		return err
	}
	if err = store.ReadJSON("pending.json", &state.held); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(state.held) > 50 {
		state.held = state.held[len(state.held)-50:]
	}
	if len(state.held) > 0 {
		delay := time.Second
		if end, quiet := quietUntil(state.settings.Quiet, m.now()); quiet {
			delay = end.Sub(m.now())
		}
		state.flush = time.AfterFunc(delay, func() { m.flushHeld(user) })
	}
	return nil
}

// ResumePersonal reads each account configuration once; retries touch only the
// data volume when it was unavailable during boot.
func (m *Manager) ResumePersonal(users []string) {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	for _, user := range users {
		state, err := m.personalLocked(user)
		if err != nil {
			continue
		}
		if state.restorePending {
			if m.restoreQuiet(user, state) == nil {
				state.restorePending = false
			}
		}
	}
}
