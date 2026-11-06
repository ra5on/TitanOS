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
//   - locks may always be deleted (restic removes its own lock after every
//     run);
//   - data, index and snapshots may only be deleted once they are older
//     than the protection period of this side (ProtectDays, 30 days by
//     default). The owner of the data prunes with restic forget --prune as
//     usual; whatever is younger stays, so a sender that was taken over
//     cannot wipe the recent backups.
//
// Each partner has its own quota; an upload that would go beyond it is
// refused with 507 Insufficient Storage before anything is kept.
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

	mu    sync.Mutex
	usage map[string]int64
}

// NewServer returns a server for the volumes under /mnt.
func NewServer() *Server {
	return &Server{MountRoot: "/mnt", Now: time.Now, usage: map[string]int64{}}
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
	used, _ := s.Usage(p.ID)
	room := p.Quota - used
	// A lock is tiny and lets restic remove old data when the quota is full.
	if strings.HasPrefix(path, "locks"+string(filepath.Separator)) {
		room = max(room, maxLock)
	}
	if r.ContentLength > maxBlob || r.ContentLength > 0 && r.ContentLength > room || room <= 0 {
		http.Error(w, "quota of this partner reached (insufficient storage)", http.StatusInsufficientStorage)
		return
	}
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
	limit := min(room, maxBlob)
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

// remove deletes a file as far as the append-only rule allows.
func (s *Server) remove(w http.ResponseWriter, r *http.Request, p Partner, repo *os.Root, path, kind string) {
	info, err := repo.Lstat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch kind {
	case "locks":
	case "data", "index", "snapshots":
		if s.Now().Sub(info.ModTime()) < s.protect() {
			http.Error(w, "append-only: younger than the protection period of the partner", http.StatusForbidden)
			return
		}
	default: // config, keys
		http.Error(w, "append-only: the repository stays", http.StatusForbidden)
		return
	}
	if err := repo.Remove(path); err != nil {
		fail(w, err)
		return
	}
	s.add(p.ID, -info.Size())
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, os.ErrPermission) {
		status = http.StatusForbidden
	}
	slog.Warn("buddy", "err", err)
	http.Error(w, "storage error", status)
}
