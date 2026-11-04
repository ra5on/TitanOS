package files

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Large transfer state lives in paged, transactional storage on the volume.
// RAM holds a directory batch and one entry, not a map of the entire tree.
// The small .json descriptor deliberately blocks older apps from replaying a
// plan whose publication state they cannot read.
type transferEntry struct {
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Complete bool   `json:"complete,omitempty"`
	Skip     bool   `json:"skip,omitempty"`
	Top      bool   `json:"top,omitempty"`
}
type transferStoreState struct {
	ID, User, Volume string
	Ready            bool
	Progress         transferProgress
	Pending          string
	Part             transferPart
}
type transferStore struct {
	mu               sync.Mutex
	manager          *Manager
	volume, user, id string
	active           bool
	created          bool
	db               *bolt.DB
	root             *os.Root
}

var entryBucket = []byte("entries")
var destinationBucket = []byte("destinations")
var stateBucket = []byte("state")
var stateKey = []byte("current")

func (s *transferStore) open(create bool) error {
	if s.db != nil {
		return nil
	}
	root, err := s.manager.transferRoot(s.volume, s.user)
	if err != nil {
		return err
	}
	name := s.id + ".db"
	db, err := bolt.Open(name, 0600, &bolt.Options{Timeout: time.Second, OpenFile: func(_ string, flags int, mode os.FileMode) (*os.File, error) {
		if create {
			flags |= os.O_EXCL
		} else {
			flags &^= os.O_CREATE
		}
		f, e := root.OpenFile(name, flags|syscall.O_NOFOLLOW, mode)
		if e != nil {
			return nil, e
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return nil, e
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
			f.Close()
			return nil, ErrProtected
		}
		return f, nil
	}})
	if err != nil {
		root.Close()
		return err
	}
	s.db, s.root = db, root
	if create {
		s.created = true
	}
	return nil
}
func (s *transferStore) closeLocked() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.root.Close()
	s.db, s.root = nil, nil
	return err
}
func (s *transferStore) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = false
	return s.closeLocked()
}
func (s *transferStore) keepOpen() { s.mu.Lock(); s.active = true; s.mu.Unlock() }
func (s *transferStore) transaction(write bool, fn func(*bolt.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.open(false); err != nil {
		return err
	}
	var err error
	if write {
		err = s.db.Update(fn)
	} else {
		err = s.db.View(fn)
	}
	if !s.active {
		if e := s.closeLocked(); err == nil {
			err = e
		}
	}
	return err
}
func storeEntry(b *bolt.Bucket, rel string) (transferEntry, error) {
	var entry transferEntry
	if b == nil {
		return entry, errors.New("missing transfer entries")
	}
	if data := b.Get([]byte(rel)); data != nil {
		if err := json.Unmarshal(data, &entry); err != nil {
			return entry, err
		}
	}
	return entry, nil
}
func putStoreEntry(b *bolt.Bucket, rel string, entry transferEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return b.Put([]byte(rel), data)
}
func (s *transferStore) entry(rel string) (transferEntry, error) {
	var entry transferEntry
	err := s.transaction(false, func(tx *bolt.Tx) error { var e error; entry, e = storeEntry(tx.Bucket(entryBucket), rel); return e })
	return entry, err
}
func (s *transferStore) summary() (transferStoreState, error) {
	var state transferStoreState
	err := s.transaction(false, func(tx *bolt.Tx) error {
		b := tx.Bucket(stateBucket)
		if b == nil {
			return errors.New("missing transfer state")
		}
		if err := json.Unmarshal(b.Get(stateKey), &state); err != nil {
			return err
		}
		if !state.Ready || state.ID != s.id || state.User != s.user || state.Volume != s.volume {
			return ErrInvalid
		}
		return nil
	})
	return state, err
}
// create makes the plan database and publishes the descriptor. The database
// stays open for fill.
func (s *transferStore) create(publish func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.open(true); err != nil {
		return err
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{entryBucket, destinationBucket, stateBucket} {
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = s.closeLocked() // closing after a failed start
		return err
	}
	if err := publish(); err != nil {
		_ = s.closeLocked() // closing after a failed start
		return err
	}
	return nil
}

// fill writes the plan entries the walk finds and marks the plan ready.
// Every transaction and directory read is bounded, independently of the
// total file count.
func (s *transferStore) fill(job *Job, walk func(add func(string, transferEntry) error) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() { _ = s.closeLocked() }() // closing what was only written
	if s.db == nil {
		return errors.New("transfer plan is not open")
	}
	batch := make(map[string]transferEntry, 1024)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(entryBucket)
			for rel, entry := range batch {
				if err := putStoreEntry(b, rel, entry); err != nil {
					return err
				}
			}
			return nil
		})
		clear(batch)
		return err
	}
	if err := walk(func(rel string, entry transferEntry) error {
		batch[rel] = entry
		if len(batch) >= 1024 {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	s.manager.jobs.mu.Lock()
	state := transferStoreState{ID: s.id, User: s.user, Volume: s.volume, Ready: true, Progress: progressLocked(job)}
	s.manager.jobs.mu.Unlock()
	if err := s.db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return tx.Bucket(stateBucket).Put(stateKey, data)
	}); err != nil {
		return err
	}
	return syncTransferParent(s.root, s.id+".db")
}

// destination returns the version recorded for a published target.
func (s *transferStore) destination(target string) (string, error) {
	var version string
	err := s.transaction(false, func(tx *bolt.Tx) error {
		b := tx.Bucket(destinationBucket)
		if b == nil {
			return errors.New("missing transfer destinations")
		}
		version = string(b.Get([]byte(target)))
		return nil
	})
	return version, err
}
func (s *transferStore) apply(event transferEvent) error {
	return s.transaction(true, func(tx *bolt.Tx) error {
		b := tx.Bucket(stateBucket)
		if b == nil {
			return errors.New("missing transfer state")
		}
		var state transferStoreState
		if err := json.Unmarshal(b.Get(stateKey), &state); err != nil {
			return err
		}
		if !state.Ready || state.ID != s.id || state.User != s.user || state.Volume != s.volume {
			return ErrInvalid
		}
		if event.Rel != "" {
			entries := tx.Bucket(entryBucket)
			entry, err := storeEntry(entries, event.Rel)
			if err != nil {
				return err
			}
			if event.Target != "" {
				entry.Target = event.Target
			}
			if event.Source != "" {
				entry.Source = event.Source
			}
			entry.Complete = entry.Complete || event.Complete
			entry.Skip = entry.Skip || event.Skip
			entry.Top = entry.Top || event.Top
			if err := putStoreEntry(entries, event.Rel, entry); err != nil {
				return err
			}
			if event.Version != "" {
				if err := tx.Bucket(destinationBucket).Put([]byte(event.Target), []byte(event.Version)); err != nil {
					return err
				}
			}
		}
		if event.Pending != nil {
			state.Pending = *event.Pending
		}
		if event.Part != nil {
			state.Part = *event.Part
		}
		state.Progress = event.Progress
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return b.Put(stateKey, data)
	})
}
func (s *transferStore) verifyDestinations(check func(string, string) error) error {
	return s.transaction(false, func(tx *bolt.Tx) error {
		b := tx.Bucket(destinationBucket)
		if b == nil {
			return errors.New("missing transfer destinations")
		}
		return b.ForEach(func(k, v []byte) error { return check(string(k), string(v)) })
	})
}
func (j *Job) paged() bool { return j.durable != nil && j.durable.store != nil }
func (j *Job) entry(rel string) (transferEntry, error) {
	if j.paged() {
		return j.durable.store.entry(rel)
	}
	entry := transferEntry{Target: j.targets[rel], Complete: j.completed[rel], Top: j.completedTop[rel], Skip: j.skipped[rel]}
	if j.durable != nil {
		entry.Source = j.durable.Sources[rel]
		entry.Skip = entry.Skip || j.durable.Skipped[rel]
	}
	return entry, nil
}
func (j *Job) closeStore() {
	if j.paged() {
		_ = j.durable.store.close() // cleanup, best effort: nothing more can be done if it fails
	}
}
func (j *Job) keepStoreOpen() {
	if j.paged() {
		j.durable.store.keepOpen()
	}
}
func (j *Job) sourceVersion(rel string) (string, error) {
	entry, err := j.entry(rel)
	return entry.Source, err
}
