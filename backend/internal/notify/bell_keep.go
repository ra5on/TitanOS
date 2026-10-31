package notify

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
)

// The bell keeps its messages on the volume of the personal data
// (.titanos/notify/bell.json), so that they survive a restart. Saved when a
// message comes or goes; loaded the first time the folder is there, older
// messages before the ones of this start.

const bellFile = "bell.json"

// goKeepBell saves the messages in the background; WaitBell waits for it.
func (m *Manager) goKeepBell() {
	m.bellPending.Add(1)
	go func() {
		defer m.bellPending.Done()
		m.keepBell()
	}()
}

// WaitBell waits until the messages handed to the background are saved.
func (m *Manager) WaitBell() {
	m.bellPending.Wait()
}

// keepBell saves the messages; called without m.mu held. Writes go one
// after the other, each with the messages of its moment.
func (m *Manager) keepBell() {
	m.bellWrite.Lock()
	defer m.bellWrite.Unlock()
	dir := m.bellDir()
	if dir == "" {
		return
	}
	m.mu.Lock()
	m.loadBellLocked(dir)
	data, err := json.Marshal(m.messages)
	m.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp := filepath.Join(dir, "."+bellFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("bell not kept", "err", err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(dir, bellFile)); err != nil {
		slog.Warn("bell not kept", "err", err)
	}
}

func (m *Manager) bellDir() string {
	m.people.mu.Lock()
	dir := m.people.dir
	m.people.mu.Unlock()
	if dir == nil {
		return ""
	}
	base, ok := dir()
	if !ok {
		return ""
	}
	return base
}

// loadBellLocked reads the kept messages once. Called with m.mu held.
func (m *Manager) loadBellLocked(dir string) {
	if m.bellLoaded {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, bellFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			m.bellLoaded = true
		}
		return
	}
	m.bellLoaded = true
	var kept []Message
	if json.Unmarshal(data, &kept) != nil {
		return
	}
	// The kept ones are older; give them ids below the ones of this start.
	slices.SortFunc(kept, func(a, b Message) int { return a.Time.Compare(b.Time) })
	merged := make([]Message, 0, len(kept)+len(m.messages))
	for _, message := range kept {
		message.ID = m.nextID
		m.nextID++
		merged = append(merged, message)
	}
	for _, message := range m.messages {
		message.ID = m.nextID
		m.nextID++
		merged = append(merged, message)
	}
	if len(merged) > keep {
		merged = merged[len(merged)-keep:]
	}
	m.messages = merged
}

// LoadBell reads the kept messages once a volume is there.
func (m *Manager) LoadBell() {
	dir := m.bellDir()
	if dir == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadBellLocked(dir)
}
