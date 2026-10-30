// Package catalog knows the apps that can be installed: Compose templates
// from the official catalog (the repository ra5on/TitanOS-Apps) and from
// sources the administrator adds.
//
// A source is a Git repository, an archive or a single compose file behind
// an HTTP address. Templates are kept in memory only; they are fetched
// again after a restart and once a day.
package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	sourcesFile = "docker/sources.json"
	// BuiltinID is the official source. It cannot be removed.
	BuiltinID = "titanos"
	// OfficialURL is where the official apps are fetched from.
	OfficialURL = "https://github.com/ra5on/TitanOS-Apps"

	refreshInterval = 24 * time.Hour
	// retryInterval is how long a source that could not be fetched is left alone.
	retryInterval = 15 * time.Minute
	maxSources    = 20
)

var (
	ErrNotLoaded  = errors.New("catalog: sources could not be read from /config")
	ErrInvalidURL = errors.New("catalog: not an http or https address")
	ErrExists     = errors.New("catalog: this source exists")
	ErrNotFound   = errors.New("catalog: not found")
	ErrTooMany    = errors.New("catalog: too many sources")
	// ErrUnreachable means the address did not answer with something readable.
	ErrUnreachable = errors.New("catalog: the source could not be fetched")
	// ErrEmpty means the source was read but holds no compose file.
	ErrEmpty = errors.New("catalog: no apps in this source")
)

// Source is an address apps are read from, as stored in /config.
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// SourceStatus is a source with what the last fetch found.
type SourceStatus struct {
	Source
	Builtin bool `json:"builtin"`
	// Format is "titanos" for templates with an x-titanos block, "casaos"
	// for those of a CasaOS store, "compose" for plain compose files.
	Format string `json:"format"`
	Count  int    `json:"count"`
	// Updated is when the source was last fetched; zero if never.
	Updated time.Time `json:"updated"`
	// Error is why the last fetch failed: unreachable, empty.
	Error string `json:"error"`
}

// App is a template as shown in the store.
type App struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Name   string `json:"name"`
	// Category is one of the keys the web UI knows (media, photos, …) or
	// whatever the template says.
	Category string `json:"category"`
	// Description maps a language ("de", "en") to a text; a template that
	// has only one text uses the key "".
	Description map[string]string `json:"description"`
	// Image is the image of the first service.
	Icon     string `json:"icon"`
	Image    string `json:"image"`
	Services int    `json:"services"`

	compose string
}

type state struct {
	apps    []App
	format  string
	updated time.Time
	tried   time.Time
	err     string
	// etag is the version the server gave what was fetched last.
	etag string
}

// Manager keeps the sources and their templates.
type Manager struct {
	store  *config.Store
	client *http.Client

	mu      sync.Mutex
	loaded  bool
	sources []Source
	states  map[string]*state
	// official is the address of the official source; tests point it elsewhere.
	official string
}

// NewManager returns a manager. The official catalog is empty until it has
// been fetched; without a network the store stays empty.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, client: newClient(), states: map[string]*state{}, official: OfficialURL}
}

// SetOfficialURL points the official source somewhere else (mirrors, tests).
func (m *Manager) SetOfficialURL(address string) {
	m.mu.Lock()
	m.official = address
	m.mu.Unlock()
}

// SetClient replaces the client sources are fetched with, before Load.
// Tests use it to reach their servers on loopback, which the default client
// refuses.
func (m *Manager) SetClient(client *http.Client) { m.client = client }

// Load reads the sources from /config.
func (m *Manager) Load() error {
	var file struct{ Sources []Source }
	err := m.store.ReadJSON(sourcesFile, &file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Older versions allowed the official repository to be added a second time.
	// Remove only that duplicate; installed apps and unrelated sources are untouched.
	sources := slices.DeleteFunc(slices.Clone(file.Sources), func(source Source) bool {
		return sameSource(source.URL, m.official) || sameSource(source.URL, OfficialURL)
	})
	if len(sources) != len(file.Sources) {
		if err := m.store.WriteJSON(sourcesFile, struct {
			Sources []Source `json:"sources"`
		}{sources}); err != nil {
			return err
		}
	}
	m.sources = sources
	m.loaded = true
	return nil
}

// Run fetches the sources after the start and once a day, until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			m.Load()
		} else {
			m.refresh(ctx, false)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Refresh fetches all sources now.
func (m *Manager) Refresh(ctx context.Context) {
	m.refresh(ctx, true)
}

func (m *Manager) refresh(ctx context.Context, all bool) {
	m.mu.Lock()
	var due []Source
	now := time.Now()
	for _, source := range append([]Source{{ID: BuiltinID, Name: "TitanOS-Apps", URL: m.official}}, m.sources...) {
		st := m.states[source.ID]
		switch {
		case all || st == nil:
		case st.err != "" && now.Sub(st.tried) < retryInterval:
			continue
		case st.err == "" && now.Sub(st.updated) < refreshInterval:
			continue
		}
		due = append(due, source)
	}
	m.mu.Unlock()

	for _, source := range due {
		m.mu.Lock()
		etag := ""
		if st := m.states[source.ID]; st != nil {
			etag = st.etag
		}
		m.mu.Unlock()
		apps, format, etag, err := m.fetch(ctx, source, etag)
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		st := m.states[source.ID]
		if st == nil {
			st = &state{}
			m.states[source.ID] = st
		}
		st.tried = time.Now()
		switch {
		case errors.Is(err, errNotModified):
			st.updated, st.err = st.tried, ""
		case err != nil:
			// What was read before stays usable.
			st.err = errorCode(err)
			slog.Warn("app source not fetched", "source", source.URL, "err", err)
		default:
			st.apps, st.format, st.etag, st.updated, st.err = apps, format, etag, st.tried, ""
		}
		m.mu.Unlock()
	}
	if len(due) > 0 {
		// Reading hundreds of templates leaves a lot behind. The system
		// runs from RAM: hand it back now instead of some minutes later.
		debug.FreeOSMemory()
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrEmpty):
		return "empty"
	default:
		return "unreachable"
	}
}

func (m *Manager) fetch(ctx context.Context, source Source, etag string) ([]App, string, string, error) {
	files, etag, err := download(ctx, m.client, source.URL, etag)
	if err != nil {
		return nil, "", etag, err
	}
	apps, format, err := parse(source.ID, files)
	return apps, format, etag, err
}

// Status returns the sources and all their apps.
func (m *Manager) Status() ([]SourceStatus, []App) {
	m.mu.Lock()
	defer m.mu.Unlock()
	official := SourceStatus{Source: Source{ID: BuiltinID, Name: "TitanOS-Apps", URL: m.official}, Builtin: true}
	var apps []App
	if st := m.states[BuiltinID]; st != nil {
		official.Format, official.Count, official.Updated, official.Error = st.format, len(st.apps), st.updated, st.err
		apps = slices.Clone(st.apps)
	}
	sources := []SourceStatus{official}
	for _, source := range m.sources {
		status := SourceStatus{Source: source}
		if st := m.states[source.ID]; st != nil {
			status.Format, status.Count, status.Updated, status.Error = st.format, len(st.apps), st.updated, st.err
			apps = append(apps, st.apps...)
		}
		sources = append(sources, status)
	}
	sort.SliceStable(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return sources, apps
}

// Add fetches a new source and keeps it if it holds apps.
func (m *Manager) Add(ctx context.Context, name, address string) error {
	address = strings.TrimSpace(address)
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || len(address) > 500 {
		return ErrInvalidURL
	}
	name = strings.TrimSpace(name)
	if name == "" {
		// "github.com/someone/apps" says more than the full address.
		name = parsed.Host + strings.TrimSuffix(parsed.Path, "/")
	}
	if len(name) > 80 {
		name = name[:80]
	}

	m.mu.Lock()
	switch {
	case !m.loaded:
		err = ErrNotLoaded
	case len(m.sources) >= maxSources:
		err = ErrTooMany
	case sameSource(address, m.official) || sameSource(address, OfficialURL) || slices.ContainsFunc(m.sources, func(s Source) bool { return sameSource(s.URL, address) }):
		err = ErrExists
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}

	id := make([]byte, 6)
	rand.Read(id)
	source := Source{ID: hex.EncodeToString(id), Name: name, URL: address}
	apps, format, etag, err := m.fetch(ctx, source, "")
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if sameSource(address, m.official) || sameSource(address, OfficialURL) || slices.ContainsFunc(m.sources, func(s Source) bool { return sameSource(s.URL, address) }) {
		return ErrExists
	}
	if len(m.sources) >= maxSources {
		return ErrTooMany
	}
	sources := append(slices.Clone(m.sources), source)
	if err := m.store.WriteJSON(sourcesFile, struct {
		Sources []Source `json:"sources"`
	}{sources}); err != nil {
		return err
	}
	now := time.Now()
	m.sources = sources
	m.states[source.ID] = &state{apps: apps, format: format, etag: etag, updated: now, tried: now}
	debug.FreeOSMemory()
	return nil
}

// Remove forgets a source. Apps installed from it stay.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	at := slices.IndexFunc(m.sources, func(s Source) bool { return s.ID == id })
	if at < 0 {
		return ErrNotFound
	}
	sources := slices.Delete(slices.Clone(m.sources), at, at+1)
	if err := m.store.WriteJSON(sourcesFile, struct {
		Sources []Source `json:"sources"`
	}{sources}); err != nil {
		return err
	}
	m.sources = sources
	delete(m.states, id)
	return nil
}

// Compose returns the compose file of a template.
func (m *Manager) Compose(source, app string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[source]
	if st == nil {
		return "", ErrNotFound
	}
	for _, candidate := range st.apps {
		if candidate.ID == app {
			return candidate.compose, nil
		}
	}
	return "", ErrNotFound
}
