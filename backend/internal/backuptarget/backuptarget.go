// Package backuptarget lets this NAS take in the backups of other TitanOS.
//
// A backup job on the other NAS uses its "rest" destination: restic talks to
// the small server in this package, which stores what restic sends in a folder
// of its own for that NAS. The data is encrypted by the sending NAS with the
// password of its job; this side cannot read it.
//
// The server only listens on the Tailscale addresses of this NAS, never in the
// home network or the internet. Each sending NAS has its own name and a long
// random token (kept only as a hash) and reaches nothing but its own folder.
package backuptarget

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
)

const (
	settingsFile = "backup/target.json"
	// DefaultPort is where the server listens, on the Tailscale addresses.
	DefaultPort = 8750
	targetDir   = ".titanos/backup-target"
	maxSources  = 20
	maxBlob     = 1 << 30
	// CodePrefix starts a pairing code.
	CodePrefix = "titanos-backup:"
)

var (
	ErrNotLoaded = errors.New("backuptarget: settings not loaded from /config")
	ErrInvalid   = errors.New("backuptarget: invalid request")
	ErrExists    = errors.New("backuptarget: this name is used already")
	ErrNotFound  = errors.New("backuptarget: no such source")
	ErrTooMany   = errors.New("backuptarget: too many sources")
	ErrNoNetwork = errors.New("backuptarget: Tailscale is not connected")
	ErrDisabled  = errors.New("backuptarget: not available in development mode")

	blobForm      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	repoTypes     = []string{"data", "index", "keys", "locks", "snapshots"}
	repoTypesForm = regexp.MustCompile(`^(data|index|keys|locks|snapshots)$`)
)

// Source is a NAS that may send its backups here.
type Source struct {
	Name string `json:"name"`
	// Hash is the SHA-256 of the token; the token itself is shown once.
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
}

// Settings is /config/backup/target.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Volume is where the backups of the others are kept.
	Volume  string   `json:"volume"`
	Port    int      `json:"port,omitempty"`
	Sources []Source `json:"sources"`
}

// SourceView is a source as the web UI sees it.
type SourceView struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	// Bytes is what the source stores here; Last is when it last wrote.
	Bytes uint64     `json:"bytes"`
	Last  *time.Time `json:"last"`
}

// View is what the web UI shows.
type View struct {
	Enabled bool         `json:"enabled"`
	Volume  string       `json:"volume"`
	Port    int          `json:"port"`
	Sources []SourceView `json:"sources"`
	// Listening is where the server answers right now.
	Listening []string `json:"listening"`
	// Problem is why it does not: no volume, no Tailscale.
	Problem string `json:"problem"`
}

// Manager keeps the settings and runs the server.
type Manager struct {
	store *config.Store
	// Disabled refuses to serve: in development mode there is no Tailscale.
	Disabled bool
	// Addresses returns the Tailscale addresses of this NAS; Host the name
	// others reach it by (empty if unknown).
	Addresses func() []string
	Host      func() string
	// Mounted returns the volumes that are mounted.
	Mounted   func() []string
	mountRoot string

	mu        sync.Mutex
	loaded    bool
	settings  Settings
	servers   map[string]*http.Server
	problem   string
	statCache map[string]stat
}

type stat struct {
	at    time.Time
	bytes uint64
	last  time.Time
}

// NewManager returns a manager for the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, mountRoot: "/mnt", servers: map[string]*http.Server{}, statCache: map[string]stat{}}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	settings := Settings{Sources: []Source{}}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return nil
}

func (m *Manager) save(settings Settings) error {
	return m.store.WriteJSON(settingsFile, settings)
}

func (m *Manager) port() int {
	if m.settings.Port > 0 {
		return m.settings.Port
	}
	return DefaultPort
}

// repoDir is the folder of a source.
func (m *Manager) repoDir(volume, name string) string {
	return filepath.Join(m.mountRoot, volume, targetDir, name)
}

// Configure switches the server on or off and picks the volume.
func (m *Manager) Configure(enabled bool, volume string, mounted []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if enabled && (!naming.ValidVolume(volume) || !slices.Contains(mounted, volume)) {
		return fmt.Errorf("%w: volume", ErrInvalid)
	}
	next := m.settings
	next.Enabled = enabled
	if volume != "" {
		next.Volume = volume
	}
	if err := m.save(next); err != nil {
		return err
	}
	m.settings = next
	return nil
}

// Pair adds a source and returns the code the other NAS is given. The code
// holds the address, the name and the token; it is shown once.
func (m *Manager) Pair(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return "", ErrNotLoaded
	}
	if !naming.ValidBackup(name) {
		return "", fmt.Errorf("%w: name", ErrInvalid)
	}
	if len(m.settings.Sources) >= maxSources {
		return "", ErrTooMany
	}
	if slices.ContainsFunc(m.settings.Sources, func(s Source) bool { return strings.EqualFold(s.Name, name) }) {
		return "", ErrExists
	}
	host := ""
	if m.Host != nil {
		host = m.Host()
	}
	if host == "" && m.Addresses != nil {
		for _, address := range m.Addresses() {
			if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
				host = address
				break
			}
		}
	}
	if host == "" {
		return "", ErrNoNetwork
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	next := m.settings
	next.Sources = append(slices.Clone(m.settings.Sources), Source{Name: name, Hash: hashToken(token), Created: time.Now().UTC().Truncate(time.Second)})
	if err := m.save(next); err != nil {
		return "", err
	}
	m.settings = next
	payload, _ := json.Marshal(map[string]any{"v": 1, "url": fmt.Sprintf("http://%s/%s", net.JoinHostPort(host, strconv.Itoa(m.port())), name), "user": name, "password": token})
	return CodePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Remove forgets a source. Its backups stay unless deleteData is set.
func (m *Manager) Remove(name string, deleteData bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	at := slices.IndexFunc(m.settings.Sources, func(s Source) bool { return s.Name == name })
	if at < 0 {
		return ErrNotFound
	}
	next := m.settings
	next.Sources = slices.Delete(slices.Clone(m.settings.Sources), at, at+1)
	if err := m.save(next); err != nil {
		return err
	}
	m.settings = next
	delete(m.statCache, name)
	if deleteData && m.settings.Volume != "" {
		if dir := m.repoDir(m.settings.Volume, name); strings.HasPrefix(dir, filepath.Join(m.mountRoot, m.settings.Volume, targetDir)+string(filepath.Separator)) {
			os.RemoveAll(dir)
		}
	}
	return nil
}

// View returns the settings with what each source stores.
func (m *Manager) View() View {
	m.mu.Lock()
	defer m.mu.Unlock()
	view := View{Enabled: m.settings.Enabled, Volume: m.settings.Volume, Port: m.port(), Sources: []SourceView{}, Listening: []string{}, Problem: m.problem}
	for address := range m.servers {
		view.Listening = append(view.Listening, address)
	}
	slices.Sort(view.Listening)
	for _, source := range m.settings.Sources {
		item := SourceView{Name: source.Name, Created: source.Created}
		if m.settings.Volume != "" {
			st := m.measure(source.Name)
			item.Bytes = st.bytes
			if !st.last.IsZero() {
				last := st.last
				item.Last = &last
			}
		}
		view.Sources = append(view.Sources, item)
	}
	return view
}

// measure adds up what a source stores; the result is kept for a minute.
// The caller holds the lock.
func (m *Manager) measure(name string) stat {
	if cached, ok := m.statCache[name]; ok && time.Since(cached.at) < time.Minute {
		return cached
	}
	st := stat{at: time.Now()}
	filepath.WalkDir(m.repoDir(m.settings.Volume, name), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			st.bytes += uint64(info.Size())
			if info.ModTime().After(st.last) {
				st.last = info.ModTime()
			}
		}
		return nil
	})
	m.statCache[name] = st
	return st
}

// authorize returns the source a request belongs to, or "".
func (m *Manager) authorize(r *http.Request) (name, volume string) {
	user, token, ok := r.BasicAuth()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !ok || !m.settings.Enabled || m.settings.Volume == "" {
		return "", ""
	}
	sum := hashToken(token)
	for _, source := range m.settings.Sources {
		// Both are compared in full so that timing tells nothing.
		matchName := subtle.ConstantTimeCompare([]byte(source.Name), []byte(user))
		matchToken := subtle.ConstantTimeCompare([]byte(source.Hash), []byte(sum))
		if matchName&matchToken == 1 {
			return source.Name, m.settings.Volume
		}
	}
	return "", ""
}

// Run keeps the server listening on the Tailscale addresses while it is on,
// until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		m.reconcile()
		select {
		case <-ctx.Done():
			m.stop(nil)
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) reconcile() {
	m.mu.Lock()
	settings, loaded := m.settings, m.loaded
	m.mu.Unlock()
	if !loaded {
		m.Load()
		return
	}
	var want []string
	problem := ""
	switch {
	case !settings.Enabled:
	case settings.Volume == "" || m.Mounted != nil && !slices.Contains(m.Mounted(), settings.Volume):
		problem = "volume"
	default:
		for _, address := range m.addresses() {
			want = append(want, net.JoinHostPort(address, strconv.Itoa(m.port())))
		}
		if len(want) == 0 {
			problem = "tailscale"
		}
	}
	m.mu.Lock()
	m.problem = problem
	m.mu.Unlock()
	m.stop(want)
	for _, address := range want {
		m.mu.Lock()
		_, running := m.servers[address]
		m.mu.Unlock()
		if running {
			continue
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			slog.Warn("backup target cannot listen", "address", address, "err", err)
			continue
		}
		server := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute}
		m.mu.Lock()
		m.servers[address] = server
		m.mu.Unlock()
		go server.Serve(listener)
		slog.Info("backup target listening", "address", address)
	}
}

func (m *Manager) addresses() []string {
	if m.Addresses == nil {
		return nil
	}
	var out []string
	for _, address := range m.Addresses() {
		// Only Tailscale's own addresses: 100.64.0.0/10 and fd7a:115c:a1e0::/48.
		if ip := net.ParseIP(address); ip != nil && (ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64 || strings.HasPrefix(strings.ToLower(address), "fd7a:115c:a1e0:")) {
			out = append(out, address)
		}
	}
	return out
}

// stop closes the servers that should not run any more (all if keep is nil).
func (m *Manager) stop(keep []string) {
	m.mu.Lock()
	var gone []*http.Server
	for address, server := range m.servers {
		if !slices.Contains(keep, address) {
			gone = append(gone, server)
			delete(m.servers, address)
		}
	}
	m.mu.Unlock()
	for _, server := range gone {
		server.Close()
	}
}

// Handler speaks the REST protocol of restic (version 2) for one folder per
// source: /<name>/config, /<name>/<type>/, /<name>/<type>/<id>.
func (m *Manager) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, volume := m.authorize(r)
		trimmed := strings.Trim(r.URL.Path, "/")
		first, rest, _ := strings.Cut(trimmed, "/")
		if name == "" || first != name {
			// The same answer for a wrong name, a wrong token and someone else's folder.
			w.Header().Set("WWW-Authenticate", `Basic realm="TitanOS backup"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		m.serveRepo(w, r, m.repoDir(volume, name), rest)
	})
}

func (m *Manager) serveRepo(w http.ResponseWriter, r *http.Request, dir, rest string) {
	switch {
	case rest == "" && r.Method == http.MethodPost && r.URL.Query().Get("create") == "true":
		for _, sub := range append([]string{""}, repoTypes...) {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
				fail(w, err)
				return
			}
		}
		for i := range 256 {
			os.MkdirAll(filepath.Join(dir, "data", fmt.Sprintf("%02x", i)), 0o700)
		}
	case rest == "config":
		m.serveFile(w, r, filepath.Join(dir, "config"), true)
	default:
		kind, id, _ := strings.Cut(rest, "/")
		if !repoTypesForm.MatchString(kind) {
			http.NotFound(w, r)
			return
		}
		if id == "" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			m.list(w, r, dir, kind)
			return
		}
		if !blobForm.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(dir, kind, id)
		if kind == "data" {
			path = filepath.Join(dir, "data", id[:2], id)
		}
		m.serveFile(w, r, path, false)
	}
}

// list answers the content of one type, as names or (version 2) with sizes.
func (m *Manager) list(w http.ResponseWriter, r *http.Request, dir, kind string) {
	type item struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	items := []item{}
	collect := func(folder string) {
		entries, _ := os.ReadDir(folder)
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && blobForm.MatchString(entry.Name()) {
				items = append(items, item{entry.Name(), info.Size()})
			}
		}
	}
	if kind == "data" {
		subs, _ := os.ReadDir(filepath.Join(dir, "data"))
		for _, sub := range subs {
			if sub.IsDir() {
				collect(filepath.Join(dir, "data", sub.Name()))
			}
		}
	} else {
		collect(filepath.Join(dir, kind))
	}
	if strings.Contains(r.Header.Get("Accept"), "application/vnd.x.restic.rest.v2") {
		w.Header().Set("Content-Type", "application/vnd.x.restic.rest.v2")
		json.NewEncoder(w).Encode(items)
		return
	}
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	w.Header().Set("Content-Type", "application/vnd.x.restic.rest.v1")
	json.NewEncoder(w).Encode(names)
}

// serveFile handles HEAD, GET, POST and DELETE of one file. A file is
// written under a hidden name and renamed when it is whole; one that exists
// is never overwritten.
func (m *Manager) serveFile(w http.ResponseWriter, r *http.Request, path string, isConfig bool) {
	switch r.Method {
	case http.MethodHead, http.MethodGet:
		file, err := os.Open(path)
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
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			fail(w, err)
			return
		}
		if _, err := os.Lstat(path); err == nil {
			http.Error(w, "already exists", http.StatusForbidden)
			return
		}
		raw := make([]byte, 6)
		rand.Read(raw)
		temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+hex.EncodeToString(raw)+".part")
		out, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			fail(w, err)
			return
		}
		_, err = io.Copy(out, io.LimitReader(r.Body, maxBlob+1))
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			if info, statErr := os.Stat(temp); statErr == nil && info.Size() > maxBlob {
				err = errors.New("blob too large")
			}
		}
		if err == nil {
			err = os.Rename(temp, path)
		}
		if err != nil {
			os.Remove(temp)
			fail(w, err)
			return
		}
	case http.MethodDelete:
		if isConfig {
			http.Error(w, "the repository stays", http.StatusForbidden)
			return
		}
		if err := os.Remove(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			fail(w, err)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, os.ErrPermission) {
		status = http.StatusForbidden
	}
	slog.Warn("backup target", "err", err)
	http.Error(w, "storage error", status)
}
