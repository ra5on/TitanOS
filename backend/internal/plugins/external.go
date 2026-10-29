package plugins

// Community plugins from the internet: a single link to a <name>.plugin.json,
// or sources that list several plugins. They are never official unless their
// manifest carries a signature of a trusted key, install with the same
// warning and confirmation as files from a share, and come from the
// address the owner entered, never from content. Images go straight to the
// data volume like official downloads.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/httpfetch"
	"github.com/ra5on/TitanOS/backend/internal/minisign"
	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

const (
	sourcesPath = "plugins/sources.json"
	maxSources  = 16
	maxSourced  = 64
)

// Source is a list of community plugins at an address the owner added.
type Source struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	URL   string    `json:"url"`
	Added time.Time `json:"added"`
}

// SourceState is what the last catalog refresh found in a source; RAM only.
type SourceState struct {
	Source
	Plugins int    `json:"plugins"`
	Error   string `json:"error,omitempty"`
}

// sourceIndex is the list a source serves. The set index of the plugin
// repository (repo.go) is one too: its "manifest" is a SHA-256 of the
// manifest, which is then checked.
type sourceIndex struct {
	Name    string `json:"name"`
	Plugins []struct {
		Name string `json:"name"`
		// URL of the manifest, relative to the list; default <name>.plugin.json.
		URL      string `json:"url"`
		Manifest string `json:"manifest"`
	} `json:"plugins"`
}

func externalClient() *http.Client {
	// Owners and administrators manage plugins; their own servers in the
	// home network are allowed, loopback and the like never.
	return httpfetch.NewClient(httpfetch.Policy{Allow: func(a netip.AddrPort) bool { return netguard.LAN(a.Addr()) }})
}

// guarded is the client for community addresses. The person chose the
// host of a link or a source list, so it may be in the home network; a host
// that only a list names (public) may not.
func (m *Manager) guarded(public bool) *http.Client {
	if public {
		if m.publicClient == nil {
			m.publicClient = httpfetch.NewClient(httpfetch.Policy{Allow: func(a netip.AddrPort) bool { return netguard.Public(a.Addr()) }})
		}
		return m.publicClient
	}
	if m.externalClient == nil {
		m.externalClient = externalClient()
	}
	return m.externalClient
}

// loadSources reads the list once; caller holds mu.
func (m *Manager) loadSources() error {
	var file struct {
		Sources []Source `json:"sources"`
	}
	if err := m.store.ReadJSON(sourcesPath, &file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	m.sources, m.sourcesLoaded = file.Sources, true
	return nil
}

// Sources returns the sources with what the last refresh found.
func (m *Manager) Sources() []SourceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := []SourceState{}
	for _, source := range m.sources {
		state, ok := m.sourceStates[source.ID]
		if !ok {
			state = SourceState{}
		}
		state.Source = source
		states = append(states, state)
	}
	return states
}

func validLink(raw string) (*url.URL, error) {
	u, ok := httpfetch.ValidURL(strings.TrimSpace(raw))
	if !ok || u.Fragment != "" {
		return nil, fmt.Errorf("%w: not an http(s) link", ErrInvalid)
	}
	return u, nil
}

// AddSource checks that the address serves a plugin list and keeps it.
func (m *Manager) AddSource(ctx context.Context, raw string) (SourceState, error) {
	u, err := validLink(raw)
	if err != nil {
		return SourceState{}, err
	}
	m.mu.Lock()
	if len(m.sources) >= maxSources {
		m.mu.Unlock()
		return SourceState{}, fmt.Errorf("%w: at most %d sources", ErrInvalid, maxSources)
	}
	for _, source := range m.sources {
		if source.URL == u.String() {
			m.mu.Unlock()
			return SourceState{}, ErrExists
		}
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	index, entries, err := m.fetchSource(ctx, u.String())
	if err != nil {
		return SourceState{}, err
	}
	id := make([]byte, 8)
	rand.Read(id)
	source := Source{ID: hex.EncodeToString(id), Name: index.Name, URL: u.String(), Added: time.Now().UTC()}
	if source.Name == "" || len(source.Name) > 80 {
		source.Name = u.Host
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sources := append(slices.Clone(m.sources), source)
	if err := m.store.WriteJSON(sourcesPath, map[string]any{"sources": sources}); err != nil {
		return SourceState{}, err
	}
	m.sources = sources
	for i := range entries {
		entries[i].plugin.Source = source.ID
	}
	m.setSourceEntries(source.ID, entries, nil)
	state := m.sourceStates[source.ID]
	state.Source = source
	return state, nil
}

// RemoveSource forgets a source; installed plugins from it stay installed.
func (m *Manager) RemoveSource(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.IndexFunc(m.sources, func(s Source) bool { return s.ID == id })
	if index < 0 {
		return ErrNotFound
	}
	sources := slices.Delete(slices.Clone(m.sources), index, index+1)
	if err := m.store.WriteJSON(sourcesPath, map[string]any{"sources": sources}); err != nil {
		return err
	}
	m.sources = sources
	m.setSourceEntries(id, nil, nil)
	delete(m.sourceStates, id)
	return nil
}

// setSourceEntries replaces what a source contributes; caller holds mu.
func (m *Manager) setSourceEntries(id string, entries []catalogEntry, err error) {
	if m.sourceStates == nil {
		m.sourceStates = map[string]SourceState{}
	}
	state := SourceState{Plugins: len(entries)}
	if err != nil {
		state.Error = err.Error()
		// A source that cannot be reached keeps what it offered before.
		for _, entry := range m.sourceEntries {
			if entry.plugin.Source == id {
				state.Plugins++
			}
		}
		m.sourceStates[id] = state
		return
	}
	m.sourceEntries = slices.DeleteFunc(m.sourceEntries, func(e catalogEntry) bool { return e.plugin.Source == id })
	m.sourceEntries = append(m.sourceEntries, entries...)
	m.sourceStates[id] = state
}

// refreshSources reads every source again; called with the catalog refresh.
func (m *Manager) refreshSources(ctx context.Context) {
	m.mu.Lock()
	sources := slices.Clone(m.sources)
	m.mu.Unlock()
	// All at once: one that does not answer costs its minute, not the
	// minutes of all that come after it.
	var wg sync.WaitGroup
	for _, source := range sources {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, time.Minute)
			_, entries, err := m.fetchSource(ctx, source.URL)
			cancel()
			for i := range entries {
				entries[i].plugin.Source = source.ID
			}
			m.mu.Lock()
			m.setSourceEntries(source.ID, entries, err)
			m.mu.Unlock()
		})
	}
	wg.Wait()
}

func (m *Manager) fetchSource(ctx context.Context, listURL string) (sourceIndex, []catalogEntry, error) {
	var index sourceIndex
	data, err := m.fetchMetadataWith(ctx, m.guarded(false), listURL, 256<<10)
	if errors.Is(err, ErrRelease) {
		return index, nil, fmt.Errorf("%w: nothing at this address", ErrDownload)
	}
	if err != nil {
		return index, nil, err
	}
	if json.Unmarshal(data, &index) != nil || len(index.Plugins) > maxSourced {
		return index, nil, fmt.Errorf("%w: not a plugin list", ErrInvalid)
	}
	base, _ := url.Parse(listURL)
	var entries []catalogEntry
	seen := map[string]bool{}
	for _, item := range index.Plugins {
		if !nameRE.MatchString(item.Name) || seen[item.Name] {
			return index, nil, fmt.Errorf("%w: plugin list", ErrInvalid)
		}
		seen[item.Name] = true
		link := item.URL
		if link == "" {
			link = item.Name + manifestSuffix
		}
		ref, err := url.Parse(link)
		if err != nil {
			return index, nil, fmt.Errorf("%w: plugin list", ErrInvalid)
		}
		manifestURL := base.ResolveReference(ref)
		if _, err := validLink(manifestURL.String()); err != nil {
			return index, nil, err
		}
		// The list chose this host, not the person: no home network then.
		public := manifestURL.Scheme != base.Scheme || manifestURL.Host != base.Host
		entry, err := m.fetchExternal(ctx, manifestURL.String(), public)
		if err != nil {
			return index, nil, fmt.Errorf("%s: %w", item.Name, err)
		}
		if entry.plugin.Name != item.Name || (driverHashRE.MatchString(item.Manifest) && sha256Hex(entry.manifest) != item.Manifest) {
			return index, nil, fmt.Errorf("%w: %s does not match the list", ErrInvalid, item.Name)
		}
		entries = append(entries, entry)
	}
	return index, entries, nil
}

// fetchExternal reads a manifest and, if there is one, its signature. The
// image is expected next to it as <name>.raw.
func (m *Manager) fetchExternal(ctx context.Context, manifestURL string, public bool) (catalogEntry, error) {
	u, err := validLink(manifestURL)
	if err != nil {
		return catalogEntry{}, err
	}
	file := path.Base(u.Path)
	if !strings.HasSuffix(file, manifestSuffix) {
		return catalogEntry{}, fmt.Errorf("%w: the link must point to a <name>%s", ErrInvalid, manifestSuffix)
	}
	client := m.guarded(public)
	data, err := m.fetchMetadataWith(ctx, client, u.String(), 64<<10)
	if errors.Is(err, ErrRelease) {
		return catalogEntry{}, fmt.Errorf("%w: nothing at this address", ErrDownload)
	}
	if err != nil {
		return catalogEntry{}, err
	}
	manifest, err := parseManifest(data, file)
	if err != nil {
		return catalogEntry{}, err
	}
	// Signature and image sit next to the manifest; a query of the link
	// belongs to the manifest only.
	next := *u
	next.RawQuery, next.Fragment, next.RawPath = "", "", ""
	next.Path += ".minisig"
	sig, err := m.fetchMetadataWith(ctx, client, next.String(), 4096)
	if err != nil && !errors.Is(err, ErrRelease) {
		return catalogEntry{}, err
	}
	official := sig != nil && minisign.Verify(minisign.LoadKeys(m.keyDir), data, string(sig)) == nil
	if !official {
		sig = nil
	}
	next.Path = strings.TrimSuffix(u.Path, file)
	base := next.String()
	plugin := Plugin{Manifest: manifest, Official: official, Download: true, URL: u.String()}
	if !m.compatible(manifest) {
		plugin.Problem = "kernel"
	}
	return catalogEntry{plugin: plugin, manifest: data, signature: sig, base: base, external: true, public: public}, nil
}

// InspectLink shows what a link offers before anything is downloaded.
func (m *Manager) InspectLink(ctx context.Context, link string) (Plugin, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	entry, err := m.fetchExternal(ctx, link, false)
	return entry.plugin, err
}

// ExternalRequest names a community plugin: a link, or a plugin of a source.
type ExternalRequest struct {
	URL      string `json:"url"`
	Source   string `json:"source"`
	Name     string `json:"name"`
	Volume   string `json:"volume"`
	Confirm  string `json:"confirm"`
	Activate *bool  `json:"activate"`
}

// externalEntry fetches the manifest again at install time; what the page
// showed is not trusted.
func (m *Manager) externalEntry(ctx context.Context, req ExternalRequest) (catalogEntry, error) {
	if req.URL != "" && req.Source != "" {
		return catalogEntry{}, fmt.Errorf("%w: a link or a source, not both", ErrInvalid)
	}
	if req.URL != "" {
		return m.fetchExternal(ctx, req.URL, false)
	}
	m.mu.Lock()
	index := slices.IndexFunc(m.sources, func(s Source) bool { return s.ID == req.Source })
	var source Source
	if index >= 0 {
		source = m.sources[index]
	}
	m.mu.Unlock()
	if index < 0 {
		return catalogEntry{}, ErrNotFound
	}
	_, entries, err := m.fetchSource(ctx, source.URL)
	if err != nil {
		return catalogEntry{}, err
	}
	for _, entry := range entries {
		if entry.plugin.Name == req.Name {
			entry.plugin.Source = source.ID
			return entry, nil
		}
	}
	return catalogEntry{}, ErrNotFound
}

// InstallExternal downloads a community plugin onto the data volume and
// installs it like a file from a share: unsigned ones only with the plugin's
// name as confirmation. An official plugin of the same name is never
// replaced by one from elsewhere.
func (m *Manager) InstallExternal(ctx context.Context, req ExternalRequest, mounted []string) error {
	m.actions.Lock()
	volume, err := m.installVolume(req.Volume, mounted)
	m.actions.Unlock()
	if err != nil {
		return err
	}
	tr := m.track(req.Name, "install", StepSignature, StepDownload, StepChecksum, StepWrite)
	ctx = withTracker(ctx, tr)
	err = m.installExternal(ctx, req, volume, mounted)
	tr.finish(err)
	return err
}

func (m *Manager) installExternal(ctx context.Context, req ExternalRequest, volume string, mounted []string) error {
	tr := trackerOf(ctx)
	tr.begin(StepSignature)
	entry, err := m.externalEntry(ctx, req)
	if err != nil {
		return err
	}
	name := entry.plugin.Name
	tr.name(name, entry.plugin.Title)
	if !entry.plugin.Official && req.Confirm != name {
		return ErrNotConfirmed
	}
	if m.officialName(name) {
		return fmt.Errorf("%w: %s is an official plugin", ErrExists, name)
	}
	if !m.compatible(entry.plugin.Manifest) {
		return fmt.Errorf("%w: plugin targets another kernel", ErrInvalid)
	}
	m.mu.Lock()
	if m.downloading[name] {
		m.mu.Unlock()
		return ErrDownloading
	}
	if m.downloading == nil {
		m.downloading = map[string]bool{}
	}
	m.downloading[name] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.downloading, name)
		m.mu.Unlock()
	}()
	path, cleanup, err := m.stageDownload(ctx, entry, volume)
	if err != nil {
		return err
	}
	defer cleanup()
	activate := req.Activate == nil || *req.Activate
	m.actions.Lock()
	defer m.actions.Unlock()
	if again, err := m.officialInstallAllowed(name, volume, mounted); err != nil {
		return err
	} else if again != volume {
		return ErrUnknownVolume
	}
	return m.install(ctx, path, volume, req.Confirm, mounted, activate)
}

// stageDownload writes manifest, signature and image of an entry into a
// private folder on the plugin volume.
func (m *Manager) stageDownload(ctx context.Context, entry catalogEntry, volume string) (string, func(), error) {
	tr := trackerOf(ctx)
	dir := m.baseDir(volume)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	stage, err := os.MkdirTemp(dir, ".download-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(stage) }
	name := entry.plugin.Name
	path := filepath.Join(stage, name+manifestSuffix)
	if err := os.WriteFile(path, entry.manifest, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	if entry.signature != nil {
		if err := os.WriteFile(path+".minisig", entry.signature, 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	tr.begin(StepDownload)
	tr.bytes(0, entry.plugin.Size)
	client := m.client
	if entry.external {
		client = m.guarded(entry.public)
	}
	if err := m.downloadImageWith(ctx, client, entry.base+name+".raw", filepath.Join(stage, name+".raw"), entry.plugin.Manifest); err != nil {
		cleanup()
		return "", nil, err
	}
	tr.complete()
	return path, cleanup, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// officialName reports whether the name belongs to an official plugin: one
// the catalog offers or one the image knows. The catalog alone is empty
// until it was loaded once since the start, offline for good; the table in
// the image also has the plugins of kernel sets other than the running one.
func (m *Manager) officialName(name string) bool {
	_, plugins := m.catalogSnapshot()
	if slices.ContainsFunc(plugins, func(p Plugin) bool { return p.Name == name }) {
		return true
	}
	if m.catalogTable == "" || !nameRE.MatchString(name) {
		return false
	}
	_, err := os.Stat(filepath.Join(m.catalogTable, name+".json"))
	return err == nil
}

// sourceSnapshot returns the plugins of all sources from the last refresh.
func (m *Manager) sourceSnapshot() []catalogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sourceEntries)
}
