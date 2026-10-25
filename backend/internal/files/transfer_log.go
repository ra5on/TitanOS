package files

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

type transferProgress struct {
	State    string    `json:"state"`
	Error    string    `json:"error"`
	Current  string    `json:"current"`
	Finished time.Time `json:"finished"`
	Copied   int64     `json:"copied"`
	Done     int64     `json:"done"`
	Paused   bool      `json:"paused"`
	Priority int       `json:"priority"`
	Rate     int64     `json:"rate"`
}
type transferPart struct {
	Path     string `json:"path"`
	Identity string `json:"identity"`
}
type transferEvent struct {
	Part     *transferPart    `json:"part,omitempty"`
	Pending  *string          `json:"pending,omitempty"`
	Rel      string           `json:"rel,omitempty"`
	Target   string           `json:"target,omitempty"`
	Version  string           `json:"version,omitempty"`
	Skip     bool             `json:"skip,omitempty"`
	Complete bool             `json:"complete,omitempty"`
	Top      bool             `json:"top,omitempty"`
	Progress transferProgress `json:"progress"`
}
type transferEnd struct {
	Finished time.Time `json:"finished"`
}

func progressLocked(job *Job) transferProgress {
	return transferProgress{job.State, job.Error, job.Current, job.Finished, job.c.committed.Load(), job.c.count.Load(), job.Paused, job.Priority, job.Rate}
}
func (p transferProgress) apply(job *Job) {
	job.State = p.State
	job.Error = p.Error
	job.Current = p.Current
	job.Finished = p.Finished
	job.Copied = p.Copied
	job.Done = p.Done
	job.Paused = p.Paused
	job.Priority = p.Priority
	job.Rate = p.Rate
}
func repairTransferTail(file *os.File) error {
	st, err := file.Stat()
	if err != nil || st.Size() == 0 {
		return err
	}
	size := min(st.Size(), int64(65536))
	b := make([]byte, size)
	if _, err := file.ReadAt(b, st.Size()-size); err != nil {
		return err
	}
	if b[len(b)-1] == '\n' {
		return nil
	}
	last := bytes.LastIndexByte(b, '\n')
	if last < 0 && st.Size() > size {
		return errors.New("invalid transfer log tail")
	}
	return file.Truncate(st.Size() - size + int64(last+1))
}
func (m *Manager) appendTransfer(job *Job, event transferEvent) error {
	t := job.durable
	if t == nil {
		return nil
	}
	t.logMu.Lock()
	defer t.logMu.Unlock()
	m.jobs.mu.Lock()
	if t.forgotten {
		m.jobs.mu.Unlock()
		return ErrNoJob
	}
	event.Progress = progressLocked(job)
	m.jobs.mu.Unlock()
	if t.store != nil {
		if err := t.store.apply(event); err != nil {
			return mapError(err)
		}
		return m.afterTransferEvent(job, event)
	}
	root, err := m.transferRoot(t.Volume, t.User)
	if err != nil {
		return err
	}
	defer root.Close()
	name := job.ID + ".events"
	stBefore, statErr := root.Lstat(name)
	created := errors.Is(statErr, os.ErrNotExist)
	if st, err := stBefore, statErr; err == nil && !st.Mode().IsRegular() {
		return errors.New("invalid transfer event log")
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := repairTransferTail(file); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(data) > 60000 {
		return errors.New("transfer event exceeds limit")
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if created {
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return err
		}
	}
	return m.afterTransferEvent(job, event)
}
func (m *Manager) afterTransferEvent(job *Job, event transferEvent) error {
	t := job.durable
	m.jobs.mu.Lock()
	if event.Pending != nil {
		t.Pending = *event.Pending
	}
	if event.Part != nil {
		t.Part = event.Part.Path
		t.PartIdentity = event.Part.Identity
	}
	m.jobs.mu.Unlock()
	if event.Progress.State != JobRunning && t.Pending == "" {
		root, err := m.transferRoot(t.Volume, t.User)
		if err != nil {
			return err
		}
		defer root.Close()
		if err := volumedata.WriteJSON(root, job.ID+".status.json", transferEnd{event.Progress.Finished}); err != nil {
			return err
		}
		return pruneTransfers(root)
	}
	return nil
}
func readTransferEvents(root *os.Root, id string, t *transfer) error {
	if t.Skipped == nil {
		t.Skipped = map[string]bool{}
	}
	if t.Completed == nil {
		t.Completed = map[string]bool{}
	}
	if t.CompletedTop == nil {
		t.CompletedTop = map[string]bool{}
	}
	if t.Targets == nil {
		t.Targets = map[string]string{}
	}
	if t.Destinations == nil {
		t.Destinations = map[string]string{}
	}
	file, err := root.Open(id + ".events")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return errors.New("invalid transfer event log")
	}
	// Only complete, fsynced-before-mutation records are authoritative. An
	// incomplete completion keeps the preceding publication intent in force.
	reader := bufio.NewReaderSize(file, 65536)
	for {
		line, err := reader.ReadSlice('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(line) > 65536 {
			return errors.New("transfer event exceeds limit")
		}
		var event transferEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if event.Rel != "" {
			if clean, err := clean(event.Rel); err != nil || clean != event.Rel {
				return ErrInvalid
			}
			if event.Target != "" {
				if target, err := clean(event.Target); err != nil || target != event.Target {
					return ErrInvalid
				}
				t.Targets[event.Rel] = event.Target
			}
			if event.Complete {
				t.Completed[event.Rel] = true
			}
			if event.Skip {
				t.Skipped[event.Rel] = true
			}
			if event.Top {
				t.CompletedTop[event.Rel] = true
			}
			if event.Version != "" {
				t.Destinations[event.Target] = event.Version
			}
		}
		if event.Pending != nil {
			t.Pending = *event.Pending
		}
		if event.Part != nil {
			t.Part = event.Part.Path
			t.PartIdentity = event.Part.Identity
		}
		event.Progress.apply(&t.Job)
	}
}
func (m *Manager) transferTarget(job *Job, rel, target string) error {
	return m.appendTransfer(job, transferEvent{Rel: rel, Target: target})
}
func (m *Manager) transferTop(job *Job, rel string) error {
	empty := ""
	return m.appendTransfer(job, transferEvent{Pending: &empty, Rel: rel, Top: true})
}
func syncTransferParent(root *os.Root, value string) error {
	dir, err := root.Open(path.Dir(value))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (m *Manager) transferSkipped(job *Job, rel string) error {
	if job.durable == nil {
		return nil
	}
	if !job.paged() {
		job.durable.Skipped[rel] = true
	}
	return m.appendTransfer(job, transferEvent{Rel: rel, Complete: true, Skip: true})
}

func (m *Manager) transferPart(job *Job, file *os.File, name string) error {
	if job.durable == nil {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrInvalid
	}
	return m.appendTransfer(job, transferEvent{Part: &transferPart{Path: name, Identity: fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)}})
}
