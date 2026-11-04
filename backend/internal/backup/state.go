package backup

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

// USBProgress returns only cached run states, without accessing disks or /config.
func (m *Manager) USBProgress() map[string]State {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]State)
	for _, job := range m.jobs {
		if job.Destination.Type == "usb-copy" {
			if state := m.states[job.ID]; state != nil {
				result[job.ID] = *state
			}
		}
	}
	return result
}

// stateFile is where a job remembers its runs: on the volume of its first
// share that is there.
func (m *Manager) stateFile(job Job) string {
	if volume := m.homeVolume(job); volume != "" {
		return filepath.Join(m.mountRoot, volume, stateDir, job.ID+".json")
	}
	return ""
}

func (m *Manager) state(job Job) State {
	m.mu.Lock()
	if state := m.states[job.ID]; state != nil {
		defer m.mu.Unlock()
		return *state
	}
	m.mu.Unlock()
	var state State
	file := m.stateFile(job)
	if file == "" {
		// Its shares are not there, so neither is what it remembers.
		// Nothing is kept: the next look may find them.
		state.StatusError = "backup state volume unavailable"
		return state
	}
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		state.StatusError = "backup state could not be read"
		return state
	}
	if err == nil && json.Unmarshal(data, &state) != nil {
		state = State{StatusError: "backup state is invalid"}
		return state
	}

	state.Running, state.Phase, state.Percent, state.BandwidthLimit = false, "", 0, 0
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.states[job.ID]; existing != nil {
		return *existing
	}
	m.states[job.ID] = &state
	return state
}

func (m *Manager) update(job Job, change func(*State)) {
	m.state(job) // makes sure it is loaded
	m.mu.Lock()
	state := m.states[job.ID]
	if state == nil {
		state = &State{}
		m.states[job.ID] = state
	}
	change(state)
	copied := *state
	m.mu.Unlock()
	if copied.Running {
		return // written when the run ends
	}
	if file := m.stateFile(job); file != "" {
		if err := writeState(file, copied); err != nil {
			slog.Warn("backup state not saved", "job", job.ID, "err", err)
		}
	}
}

// writeState replaces the state file of a job durably: the new content is
// synced before it takes the old name, and the rename itself afterwards.
func writeState(file string, state State) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
