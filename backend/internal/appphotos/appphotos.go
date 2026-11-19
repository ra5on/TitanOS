// Package appphotos keeps what the photo backup of the Android app
// (PLAN 116, TECHNIK §3) has put on the NAS: per account and paired device
// one index of the files the server itself finished and hashed. It lives
// on the system data volume below .titanos/apps/<account>/<device>/, never
// on the stick and never in RAM only. Without a volume every call is
// ErrUnavailable, never an empty index.
//
// The index is a log: one JSON line per finished file, appended and synced
// (photos.jsonl), so that the first backup of a phone with ten thousand
// pictures does not write the whole index again for every picture. Reading
// keeps the last line of each place; a log with many such repeats is
// written anew in one piece.
package appphotos

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Feature is the folder below .titanos on the system data volume.
const Feature = "apps"

// indexFile is the log of one device.
const indexFile = "photos.jsonl"

// maxIndex bounds the log of one device (about 300 000 entries).
const maxIndex = 64 << 20

var (
	// ErrUnavailable: no system data volume (yet), or its folder is unsafe.
	ErrUnavailable = errors.New("appphotos: no system data volume")
	// ErrInvalid: an account, device or entry that cannot be kept.
	ErrInvalid = errors.New("appphotos: invalid entry")
)

var (
	accountRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	deviceRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// Entry is one file the server finished: its SHA-256, size and the
// modification time it had then, in which root and where in it.
type Entry struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Root   string `json:"root"`
	Path   string `json:"path"`
	// ModTime is the modification time of the file on the NAS, in
	// nanoseconds; a file changed since is checked again.
	ModTime int64 `json:"mtime"`
	// At is when the server recorded it.
	At time.Time `json:"at"`
	// Device is filled in by Lookup: the device whose index holds it.
	Device string `json:"-"`
}

// ValidSum reports a SHA-256 written as 64 lower-case hex digits.
func ValidSum(sum string) bool {
	if len(sum) != 64 || strings.ToLower(sum) != sum {
		return false
	}
	_, err := hex.DecodeString(sum)
	return err == nil
}

// Store is the index of all accounts and devices.
type Store struct {
	// Volume opens .titanos/apps on the system data volume (create: make
	// it if missing) and gives a release for the hold against a move;
	// ErrUnavailable or fs.ErrNotExist without one.
	Volume func(create bool) (*os.Root, func(), error)

	mu sync.Mutex
}

// Record appends an entry to the index of a device.
func (s *Store) Record(account, device string, e Entry) error {
	if !accountRE.MatchString(account) || !deviceRE.MatchString(device) || !ValidSum(e.SHA256) || e.Size < 0 ||
		e.Root == "" || e.Path == "" || path.IsAbs(e.Path) || !fs.ValidPath(e.Path) {
		return ErrInvalid
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, release, err := s.open(true)
	if err != nil {
		return err
	}
	defer release()
	defer root.Close()
	dir := account + "/" + device
	for _, part := range []string{account, dir} {
		if err := root.Mkdir(part, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := private(root, part); err != nil {
			return err
		}
	}
	name := dir + "/" + indexFile
	if st, err := root.Lstat(name); err == nil && (!st.Mode().IsRegular() || st.Size()+int64(len(line)) > maxIndex) {
		// Too large or not a plain file: written anew, without repeats.
		if err := s.compact(root, name); err != nil {
			return err
		}
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(line)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	// A new log: its name is kept too.
	d, err := root.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// Ready makes sure the index can be kept: the folder on the system data
// volume is there (made if needed), else ErrUnavailable.
func (s *Store) Ready() error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, release, err := s.open(true)
	if err != nil {
		return err
	}
	root.Close()
	release()
	return nil
}

// Lookup returns what the account's devices have recorded, by SHA-256.
// Several devices may have put the same picture into different places.
func (s *Store) Lookup(account string) (map[string][]Entry, error) {
	if !accountRE.MatchString(account) {
		return nil, ErrInvalid
	}
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]Entry{}
	root, release, err := s.open(false)
	if errors.Is(err, fs.ErrNotExist) {
		// The folder is made with the first entry: none yet.
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer release()
	defer root.Close()
	devices, err := readDir(root, account)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, d := range devices {
		if !d.IsDir() || !deviceRE.MatchString(d.Name()) {
			continue
		}
		entries, repeats, err := load(root, account+"/"+d.Name()+"/"+indexFile)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if repeats > 1000 && repeats > len(entries) {
			_ = s.compact(root, account+"/"+d.Name()+"/"+indexFile) // read again next time
		}
		for _, e := range entries {
			e.Device = d.Name()
			out[e.SHA256] = append(out[e.SHA256], e)
		}
	}
	return out, nil
}

// Forget removes the index of an account that was deleted.
func (s *Store) Forget(account string) error {
	if s == nil {
		return nil
	}
	if !accountRE.MatchString(account) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, release, err := s.open(false)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	defer root.Close()
	if _, err := root.Lstat(account); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return root.RemoveAll(account)
}

func (s *Store) open(create bool) (*os.Root, func(), error) {
	if s == nil || s.Volume == nil {
		return nil, nil, ErrUnavailable
	}
	root, release, err := s.Volume(create)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		if errors.Is(err, volumedata.ErrUnavailable) {
			return nil, nil, ErrUnavailable
		}
		return nil, nil, errors.Join(ErrUnavailable, err)
	}
	return root, release, nil
}

// private refuses a folder that is a link, not ours or writable by others.
func private(root *os.Root, name string) error {
	st, err := root.Lstat(name)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || sys.Uid != uint32(os.Geteuid()) || st.Mode().Perm()&0022 != 0 {
		return errors.Join(ErrUnavailable, errors.New("unsafe folder "+name))
	}
	return nil
}

func readDir(root *os.Root, name string) ([]fs.DirEntry, error) {
	if err := private(root, name); err != nil {
		return nil, err
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

type place struct{ sum, root, path string }

// load reads a log: the last line of each place, in the order of their
// first line, and how many lines repeated a place.
func load(root *os.Root, name string) ([]Entry, int, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, 0, err
	}
	if !st.Mode().IsRegular() {
		return nil, 0, errors.Join(ErrUnavailable, errors.New("index is not a file"))
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var entries []Entry
	at := map[place]int{}
	repeats := 0
	reader := bufio.NewReaderSize(io.LimitReader(f, maxIndex), 64<<10)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var e Entry
			// A line cut short by a crash or garbage is skipped, never fatal.
			if json.Unmarshal(line, &e) == nil && ValidSum(e.SHA256) && e.Root != "" && e.Path != "" {
				key := place{e.SHA256, e.Root, e.Path}
				if i, ok := at[key]; ok {
					entries[i] = e
					repeats++
				} else {
					at[key] = len(entries)
					entries = append(entries, e)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, err
		}
	}
	return entries, repeats, nil
}

// compact writes a log anew with one line per place (atomically).
func (s *Store) compact(root *os.Root, name string) error {
	entries, _, err := load(root, name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// The newest last, so that a log at its limit loses the oldest.
	slices.SortStableFunc(entries, func(a, b Entry) int { return a.At.Compare(b.At) })
	var data []byte
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		data = append(data, append(line, '\n')...)
	}
	for len(data) > maxIndex*3/4 {
		cut := bytes.IndexByte(data, '\n')
		data = data[cut+1:]
	}
	dir, err := root.OpenRoot(path.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()
	return volumedata.Write(dir, path.Base(name), data)
}
