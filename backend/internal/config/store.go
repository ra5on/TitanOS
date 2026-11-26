// Package config reads and writes the persistent configuration in /config,
// which lives on the USB stick.
//
// The stick is only mounted while it is being used (see /etc/fstab in the
// image). Nothing in here keeps a file or directory open, and callers must
// not touch the store on a timer: every access wakes the stick up.
//
// Sticks wear out, so the store writes only what is new: a file that holds
// the same bytes already is left alone, as is removing what is not there.
// Every write that does happen is named in the journal, so that "what
// keeps writing to the stick?" can be answered from the log.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// State are the files of /config (slash separated, globs as in path.Match)
// that hold state of the moment rather than configuration: who is logged
// in, a backup or update that is under way, addresses blocked by
// themselves, when something last happened. They must survive a restart
// and are needed before a volume is mounted, so they stay on the stick, and
// they are part of the backups of /config. But a change to them is no
// change of the configuration: it does not count for Changes, so it never
// starts a backup of /config by itself (PLAN 79-12; each such backup also
// writes to a volume and wakes its disks). New state that is written
// without the owner changing a setting belongs in this list, or better in
// RAM or .titanos/<function> on a volume.
var State = []string{
	"sessions/remembered.json",        // logins that stay (auth)
	"sessions/locked.json",            // which of them are locked, written at lock and unlock (auth, 1.03)
	"backup/application-pending.json", // pointer to a cut-off application backup, written only when one is found (appbackup)
	"firewall/blocked.json",           // addresses blocked for good after failed logins (firewall)
	"system/startup-held.json",        // restart policies held over a shutdown (startup)
	"system/update-pending.json",      // an update waiting for its restart (update)
	"system/update-failed.json",       // a release that failed its trial (update)
	"system/health-report-sent.json",  // when the report last went out (healthreport)
	"ddns/heartbeat-paused",           // healthcheck paused for a planned shutdown (ddns)
	"plugins/catalog-serial.json",     // highest plugin catalog seen (plugins)
}

// IsState tells whether a file of /config is one of State.
func IsState(name string) bool {
	for _, pattern := range State {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// countsAsChange tells whether writing a file changes what a backup of
// /config should hold soon: not state, and not what backups leave out.
func countsAsChange(name string) bool {
	return !IsState(name) && !LeftOutOfBackups(name)
}

// Store is a directory holding the configuration. All names are slash
// separated paths relative to that directory.
type Store struct {
	dir string
	// changes counts the changes of the configuration since titand
	// started (state files excluded, see State).
	changes atomic.Uint64
	// writes counts every write that reached the stick, state included.
	writes atomic.Uint64
	// unsynced are the directories whose last flush failed after a file
	// in them was replaced or removed: the change may not be durable yet.
	unsyncedMu sync.Mutex
	unsynced   map[string]bool
	// OnWriteError hears about writes that failed on the medium (a worn
	// stick); set once at the start.
	OnWriteError func(error)
}

// Open returns the store in dir. It does not access the directory.
func Open(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the directory of the store.
func (s *Store) Dir() string { return s.dir }

// Changes returns a number that grows with every change of the
// configuration made through the store. The backup of the stick uses it to
// tell, without waking the stick, whether there is something new to save.
// Writes that changed nothing and writes of state files (State) and of
// files no backup holds (NotBackedUp) do not count.
func (s *Store) Changes() uint64 { return s.changes.Load() }

// Writes returns how often the store really wrote to or removed from the
// stick since titand started, state files included. Tests use it to make
// sure that reading and idling never write.
func (s *Store) Writes() uint64 { return s.writes.Load() }

// flush syncs a directory after a change in it and remembers a failure,
// so that a later write of the same content does not pass for done.
func (s *Store) flush(dir string) error {
	err := syncDir(dir)
	s.unsyncedMu.Lock()
	defer s.unsyncedMu.Unlock()
	if err != nil {
		if s.unsynced == nil {
			s.unsynced = map[string]bool{}
		}
		s.unsynced[dir] = true
		return err
	}
	delete(s.unsynced, dir)
	return nil
}

// settle flushes a directory whose last flush failed; nothing otherwise.
func (s *Store) settle(dir string) error {
	s.unsyncedMu.Lock()
	pending := s.unsynced[dir]
	s.unsyncedMu.Unlock()
	if !pending {
		return nil
	}
	return s.flush(dir)
}

// finishChange counts actual writes, including ones whose flush fails. Only a
// confirmed publication earns the success entry in the system log.
func (s *Store) finishChange(name, what, dir string) error {
	s.writes.Add(1)
	if countsAsChange(name) {
		s.changes.Add(1)
	}
	if err := s.flush(dir); err != nil {
		slog.Warn("configuration change on the stick was not confirmed", "file", name, "change", what, "err", err)
		return err
	}
	slog.Info("configuration on the stick changed", "file", name, "change", what)
	return nil
}

// errInvalidName is a name no file can have; it says nothing about the stick.
var errInvalidName = errors.New("config: invalid name")

func (s *Store) path(name string) (string, error) {
	if !fs.ValidPath(name) || name == "." {
		return "", fmt.Errorf("%w %q", errInvalidName, name)
	}
	return filepath.Join(s.dir, filepath.FromSlash(name)), nil
}

// Read returns the content of a file. A missing file is fs.ErrNotExist.
func (s *Store) Read(name string) ([]byte, error) {
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// ReadJSON decodes a JSON file into v.
func (s *Store) ReadJSON(name string, v any) error {
	data, err := s.Read(name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("config: %s: %w", name, err)
	}
	return nil
}

// Write replaces a file atomically: the content goes to a temporary file in
// the same directory, is flushed to the stick and then renamed over the old
// file. Success confirms the directory entry as well as the data. On FAT
// the rename itself has no journal if the write is interrupted before then.
// A file that holds exactly data already is not written again.
func (s *Store) Write(name string, data []byte) error {
	err := s.write(name, data)
	if err != nil && s.OnWriteError != nil && !errors.Is(err, errInvalidName) {
		s.OnWriteError(err)
	}
	return err
}

func (s *Store) write(name string, data []byte) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	// Nothing new: no write, no wear. Only a regular file counts; anything
	// else in its place is replaced as before.
	if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() && info.Size() == int64(len(data)) {
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
			// The content is there. If the write that put it there could
			// not flush the directory, a retry must not report success
			// without it: the new name may still be lost in a power cut.
			return s.settle(filepath.Dir(p))
		}
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	err = s.finishChange(name, "written", dir)
	removeStaleTemps(dir, filepath.Base(tmp.Name()))
	return err
}

// staleTemp is how old a temporary file must be to count as the leftover
// of a write a crash broke off; one of a running write is younger.
const staleTemp = 10 * time.Minute

// removeStaleTemps removes temporary files a crash left in dir. It runs
// only when a write already touches the stick there, never on its own: no
// write without a change of the configuration.
func removeStaleTemps(dir, own string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".tmp-") || e.Name() == own || !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTemp {
			_ = os.Remove(filepath.Join(dir, e.Name())) // best effort; the next write in dir tries again
		}
	}
}

// WriteJSON encodes v as indented JSON and writes it atomically.
func (s *Store) WriteJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.Write(name, append(data, '\n'))
}

// Remove deletes a file. A missing file is not an error.
func (s *Store) Remove(name string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing was there: nothing changed. Only a removal whose
			// flush failed before is flushed now.
			return s.settle(filepath.Dir(p))
		}
		return err
	}
	return s.finishChange(name, "removed", filepath.Dir(p))
}

// RemoveAll deletes a directory with everything in it. A missing directory
// is not an error.
func (s *Store) RemoveAll(name string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return s.settle(filepath.Dir(p)) // as in Remove
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	return s.finishChange(name, "removed", filepath.Dir(p))
}

// List returns the names of the entries in a directory. A missing directory
// is empty. It fails if the store itself is not there (stick missing).
func (s *Store) List(name string) ([]string, error) {
	if _, err := os.Stat(s.dir); err != nil {
		return nil, err
	}
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// syncDir flushes a directory; a variable so that tests can make it fail.
var syncDir = syncDirectory

func syncDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// FAT can leave the renamed inode and its allocation metadata dirty even
	// after fsync on the pre-rename file and its directory. Flush this
	// filesystem after changing a name on the boot stick. Unchanged
	// settings take the no-write path and do not wake or flush the stick.
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(d.Fd()), &stat); err != nil {
		return err
	}
	if stat.Type == unix.MSDOS_SUPER_MAGIC {
		return unix.Syncfs(int(d.Fd()))
	}
	// Some filesystems cannot sync a directory; the data itself is already flushed.
	if err := d.Sync(); err != nil && !errors.Is(err, errors.ErrUnsupported) && !isInvalid(err) {
		return err
	}
	return nil
}
