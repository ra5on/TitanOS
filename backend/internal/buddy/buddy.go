// Package buddy is the receiving side of the Backup-Buddy (PLAN 54-G): two
// TitanOS keep each other's backups. The sending NAS runs restic with a
// repository password only it knows; this side stores what arrives and can
// read none of it.
//
// The server speaks the REST protocol of restic (versions 1 and 2) for one
// folder per partner, .titanos/buddy/<partner>/ on the chosen volume, and a
// pairing request with a one-time code. It is append-only:
//
//   - a file that exists is never replaced;
//   - config and keys are never deleted;
//   - locks are deleted (restic removes its own lock after every run);
//   - a deleted data, index or snapshot file is not removed but moved to
//     the quarantine .trash/<deleted at>/<type>/… of the partner's folder.
//     This side empties a quarantine only ProtectDays (30 by default) after
//     the delete. restic forget --prune works as usual; a sender that was
//     taken over and deletes everything (restic deduplicates, so today's
//     snapshot uses packs of months ago) leaves all of it in quarantine,
//     from where the person on this NAS can put it back (docs/agent54/family.md).
//     The quarantine is never listed to the partner and counts to its quota.
//
// Each partner has its own quota; an upload that would go beyond it is
// refused with 507 Insufficient Storage before anything is kept. Space is
// reserved before the copy, so parallel uploads cannot pass it together;
// a partner has at most maxLocks lock files and maxConcurrent requests at
// once.
package buddy

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	// Dir is where the backups of the partners lie on the volume.
	Dir = ".titanos/buddy"
	// DefaultPort is where the server listens, on the Tailscale addresses.
	DefaultPort = 8752
	// DefaultProtectDays is how long data stays undeletable.
	DefaultProtectDays = 30
	maxBlob            = 1 << 30
	maxPairBody        = 16 << 10
	maxLock            = 64 << 10
	maxLocks           = 64
	maxConcurrent      = 8
	// Trash is the quarantine folder inside a partner's folder.
	Trash = ".trash"
	// purgeEvery is how often a partner's quarantine is looked at.
	purgeEvery = time.Hour
)

var (
	blobForm  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	repoTypes = []string{"data", "index", "keys", "locks", "snapshots"}
	idForm    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
)

// ValidID tells whether a name can be a partner's folder.
func ValidID(id string) bool { return idForm.MatchString(id) }

// Partner is what the server needs to know about one partner.
type Partner struct {
	ID string
	// Hash is the SHA-256 of the token the partner signs in with.
	Hash string
	// Quota is how many bytes the partner may keep here.
	Quota int64
}

// HashToken returns the hex SHA-256 of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken returns a long random token.
func NewToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// Server answers the partners.
type Server struct {
	// Partner returns a partner by its id.
	Partner func(id string) (Partner, bool)
	// Volume is where the partners' backups are kept, "" if none is set.
	Volume func() string
	// Mounted returns the mounted volumes.
	Mounted func() []string
	// ProtectDays is the protection period; 0 means DefaultProtectDays.
	ProtectDays func() int
	// Pair answers a pairing request (see pair.go); nil refuses pairing.
	Pair func(PairRequest) (PairResponse, error)
	// Now is the clock (tests).
	Now       func() time.Time
	MountRoot string

	mu       sync.Mutex
	usage    map[string]int64
	reserved map[string]int64
	active   map[string]int
	purged   map[string]time.Time
}

// NewServer returns a server for the volumes under /mnt.
func NewServer() *Server {
	return &Server{MountRoot: "/mnt", Now: time.Now, usage: map[string]int64{}, reserved: map[string]int64{}, active: map[string]int{}, purged: map[string]time.Time{}}
}

// Usage returns what a partner keeps here, as far as it is known: it is
// counted when the partner first writes after a start of titand, never by
// a look of the web UI (which would wake a sleeping disk).
func (s *Server) Usage(id string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.usage[id]
	return n, ok
}

// Forget drops what is known of a partner (after it was removed).
func (s *Server) Forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.usage, id)
}

// HasData tells whether a folder for id exists on the volume (an id whose
// folder is there is never given to another partner). Without the volume
// it cannot tell and says no; the random part of ids keeps them apart then.
func (s *Server) HasData(id string) bool {
	root, err := volumedata.OpenVolume(s.MountRoot, s.Volume(), s.Mounted())
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = root.Lstat(filepath.Join(Dir, id))
	return !errors.Is(err, fs.ErrNotExist)
}

// RemoveData deletes the folder of a partner. Only the person on this NAS
// does that, never a partner.
func (s *Server) RemoveData(id string) error {
	if !ValidID(id) {
		return errors.New("buddy: invalid id")
	}
	root, err := volumedata.OpenVolume(s.MountRoot, s.Volume(), s.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	s.Forget(id)
	return root.RemoveAll(filepath.Join(Dir, id))
}

func (s *Server) protect() time.Duration {
	days := DefaultProtectDays
	if s.ProtectDays != nil {
		if d := s.ProtectDays(); d > 0 {
			days = d
		}
	}
	return time.Duration(days) * 24 * time.Hour
}

// authorize returns the partner of a request; the same answer for a wrong
// name, a wrong token and someone else's folder.
func (s *Server) authorize(r *http.Request, id string) (Partner, bool) {
	user, token, ok := r.BasicAuth()
	if !ok || s.Partner == nil || !ValidID(id) {
		return Partner{}, false
	}
	p, found := s.Partner(user)
	if !found {
		// Compare anyway, so that the time tells nothing.
		subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(HashToken("")))
		return Partner{}, false
	}
	match := subtle.ConstantTimeCompare([]byte(p.Hash), []byte(HashToken(token)))
	if match != 1 || p.ID != id || user != id {
		return Partner{}, false
	}
	return p, true
}

// Handler serves /v1/pair and /v1/<partner>/… (the restic REST protocol).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/v1/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rest == "pair" {
			s.servePair(w, r)
			return
		}
		id, inside, _ := strings.Cut(rest, "/")
		p, ok := s.authorize(r, id)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="TitanOS Backup-Buddy"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !s.enter(p.ID) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "too many requests at once", http.StatusTooManyRequests)
			return
		}
		defer s.leave(p.ID)
		volume := ""
		if s.Volume != nil {
			volume = s.Volume()
		}
		var mounted []string
		if s.Mounted != nil {
			mounted = s.Mounted()
		}
		// Each request pins the mounted volume; one that went away is
		// refused, nothing lands in the RAM below the mount point.
		root, err := volumedata.Open(s.MountRoot, volume, "buddy", mounted)
		if err != nil {
			slog.Warn("buddy volume unavailable", "volume", volume, "err", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		defer root.Close()
		if err := root.Mkdir(p.ID, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			fail(w, err)
			return
		}
		repo, err := root.OpenRoot(p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		defer repo.Close()
		s.count(p.ID, repo)
		s.purge(p.ID, repo)
		s.serveRepo(w, r, p, repo, strings.TrimSuffix(inside, "/"), strings.HasSuffix(inside, "/"))
	})
}

// count adds up what a partner keeps, once per start of titand; the disk
// is awake, the partner just sent a request.
func (s *Server) count(id string, repo *os.Root) {
	s.mu.Lock()
	_, known := s.usage[id]
	s.mu.Unlock()
	if known {
		return
	}
	var total int64
	_ = fs.WalkDir(repo.FS(), ".", func(_ string, entry fs.DirEntry, err error) error { // unreadable entries are skipped
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	s.mu.Lock()
	if _, known := s.usage[id]; !known {
		s.usage[id] = total
	}
	s.mu.Unlock()
}

// enter counts a request of a partner; false when it has too many at once.
func (s *Server) enter(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] >= maxConcurrent {
		return false
	}
	s.active[id]++
	return true
}

func (s *Server) leave(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[id]--
}

// reserve takes n bytes of a partner's room before an upload; false if
// they are not there. Release gives back what was not used.
func (s *Server) reserve(id string, quota, n int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usage[id]+s.reserved[id]+n > quota {
		return false
	}
	s.reserved[id] += n
	return true
}

func (s *Server) release(id string, n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserved[id] = max(s.reserved[id]-n, 0)
}

func (s *Server) room(id string, quota int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return quota - s.usage[id] - s.reserved[id]
}

// purge empties the quarantines of a partner that are older than the
// protection period, at most once an hour (only on a request: the disk
// is awake then).
func (s *Server) purge(id string, repo *os.Root) {
	now := s.Now()
	s.mu.Lock()
	due := now.Sub(s.purged[id]) >= purgeEvery
	if due {
		s.purged[id] = now
	}
	s.mu.Unlock()
	if !due {
		return
	}
	entries, err := fs.ReadDir(repo.FS(), Trash)
	if err != nil {
		return
	}
	for _, e := range entries {
		at, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.IsDir() || now.Sub(time.Unix(0, at)) < s.protect() {
			continue
		}
		dir := filepath.Join(Trash, e.Name())
		var size int64
		_ = fs.WalkDir(repo.FS(), dir, func(_ string, d fs.DirEntry, err error) error { // unreadable entries are skipped
			if err == nil && d.Type().IsRegular() {
				if info, err := d.Info(); err == nil {
					size += info.Size()
				}
			}
			return nil
		})
		if err := repo.RemoveAll(dir); err != nil {
			slog.Warn("buddy quarantine not emptied", "partner", id, "err", err)
			continue
		}
		s.add(id, -size)
	}
}

func (s *Server) add(id string, n int64) {
	s.mu.Lock()
	s.usage[id] = max(s.usage[id]+n, 0)
	s.mu.Unlock()
}

func (s *Server) serveRepo(w http.ResponseWriter, r *http.Request, p Partner, repo *os.Root, rest string, dir bool) {
	switch {
	case rest == "":
		if r.Method != http.MethodPost || r.URL.Query().Get("create") != "true" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, sub := range repoTypes {
			if err := repo.MkdirAll(sub, 0o700); err != nil {
				fail(w, err)
				return
			}
		}
		for i := range 256 {
			if err := repo.MkdirAll(filepath.Join("data", fmt.Sprintf("%02x", i)), 0o700); err != nil {
				fail(w, err)
				return
			}
		}
	case rest == "config":
		s.serveFile(w, r, p, repo, "config", "config")
	default:
		kind, name, _ := strings.Cut(rest, "/")
		if !isType(kind) {
			http.NotFound(w, r)
			return
		}
		if name == "" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead || !dir && rest != kind {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			list(w, r, repo, kind)
			return
		}
		if !blobForm.MatchString(name) {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(kind, name)
		if kind == "data" {
			path = filepath.Join("data", name[:2], name)
		}
		s.serveFile(w, r, p, repo, path, kind)
	}
}

func isType(kind string) bool {
	for _, t := range repoTypes {
		if t == kind {
			return true
		}
	}
	return false
}

// list answers the content of one type, as names (v1) or with sizes (v2).
func list(w http.ResponseWriter, r *http.Request, repo *os.Root, kind string) {
	type item struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	items := []item{}
	collect := func(folder string) {
		entries, _ := fs.ReadDir(repo.FS(), folder)
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && blobForm.MatchString(entry.Name()) {
				items = append(items, item{entry.Name(), info.Size()})
			}
		}
	}
	if kind == "data" {
		subs, _ := fs.ReadDir(repo.FS(), "data")
		for _, sub := range subs {
			if sub.IsDir() {
				collect(filepath.Join("data", sub.Name()))
			}
		}
	} else {
		collect(kind)
	}
	if strings.Contains(r.Header.Get("Accept"), "application/vnd.x.restic.rest.v2") {
		w.Header().Set("Content-Type", "application/vnd.x.restic.rest.v2")
		_ = json.NewEncoder(w).Encode(items)
		return
	}
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	w.Header().Set("Content-Type", "application/vnd.x.restic.rest.v1")
	_ = json.NewEncoder(w).Encode(names)
}

// serveFile handles HEAD, GET, POST and DELETE of one file.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, p Partner, repo *os.Root, path, kind string) {
	switch r.Method {
	case http.MethodHead, http.MethodGet:
		file, err := repo.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "", time.Time{}, file)
	case http.MethodPost:
		s.upload(w, r, p, repo, path)
	case http.MethodDelete:
		s.remove(w, r, p, repo, path, kind)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// upload keeps a new file: written under a hidden name, linked to its name
// when whole (a link fails if the name exists, a rename would replace it).
func (s *Server) upload(w http.ResponseWriter, r *http.Request, p Partner, repo *os.Root, path string) {
	if _, err := repo.Lstat(path); err == nil {
		http.Error(w, "already exists; this repository is append-only", http.StatusForbidden)
		return
	}
	quota := p.Quota
	limit := int64(maxBlob)
	// A lock is tiny and lets restic remove old data when the quota is
	// full: up to maxLocks of them may go beyond it.
	if strings.HasPrefix(path, "locks"+string(filepath.Separator)) {
		if locks, _ := fs.ReadDir(repo.FS(), "locks"); len(locks) >= maxLocks {
			http.Error(w, "too many locks", http.StatusInsufficientStorage)
			return
		}
		quota, limit = max(quota, 0)+maxLock*maxLocks, maxLock
	}
	// Reserve before copying: the length if known, else the room left.
	want := r.ContentLength
	if want < 0 {
		want = min(s.room(p.ID, quota), limit)
	}
	if want > limit || want <= 0 && r.ContentLength != 0 || !s.reserve(p.ID, quota, want) {
		http.Error(w, "quota of this partner reached (insufficient storage)", http.StatusInsufficientStorage)
		return
	}
	defer s.release(p.ID, want)
	limit = want
	if err := repo.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fail(w, err)
		return
	}
	raw := make([]byte, 6)
	_, _ = rand.Read(raw)
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+hex.EncodeToString(raw)+".part")
	out, err := repo.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fail(w, err)
		return
	}
	defer repo.Remove(temp)
	n, err := io.Copy(out, io.LimitReader(r.Body, limit+1))
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && n > limit {
		http.Error(w, "quota of this partner reached (insufficient storage)", http.StatusInsufficientStorage)
		return
	}
	if err == nil {
		if err = repo.Link(temp, path); errors.Is(err, fs.ErrExist) {
			http.Error(w, "already exists; this repository is append-only", http.StatusForbidden)
			return
		}
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.add(p.ID, n)
}

// remove deletes a lock; data, index and snapshots go to the quarantine.
func (s *Server) remove(w http.ResponseWriter, r *http.Request, p Partner, repo *os.Root, path, kind string) {
	info, err := repo.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	switch kind {
	case "locks":
		if err := repo.Remove(path); err != nil {
			fail(w, err)
			return
		}
		s.add(p.ID, -info.Size())
	case "data", "index", "snapshots":
		// One quarantine folder per second keeps related deletes together.
		stamp := s.Now().Truncate(time.Second).UnixNano()
		target := filepath.Join(Trash, strconv.FormatInt(stamp, 10), path)
		if err := repo.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			fail(w, err)
			return
		}
		if _, err := repo.Lstat(target); err == nil {
			target += "." + strconv.FormatInt(s.Now().UnixNano(), 10)
		}
		if err := repo.Rename(path, target); err != nil {
			fail(w, err)
			return
		}
	default: // config, keys
		http.Error(w, "append-only: the repository stays", http.StatusForbidden)
		return
	}
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, os.ErrPermission) {
		status = http.StatusForbidden
	}
	slog.Warn("buddy", "err", err)
	http.Error(w, "storage error", status)
}
