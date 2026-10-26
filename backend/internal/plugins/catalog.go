package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

var (
	ErrDownload = errors.New("plugins: download failed")
	ErrRelease  = errors.New("plugins: no published plugin catalog for this version")
)

// CatalogStatus explains an empty or stale catalog without hiding local plugins.
type CatalogStatus struct {
	Release string `json:"release"`
	State   string `json:"state"` // idle, loading, ready, unavailable, error, development
	Error   string `json:"error,omitempty"`
}

type catalogEntry struct {
	plugin    Plugin
	manifest  []byte
	signature []byte
}

func catalogClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("refusing an insecure download redirect")
		}
		return nil
	}}
}

func (m *Manager) releaseBase() (string, error) {
	// A development commit must never borrow a driver from the latest release.
	if !releaseVersion.MatchString(m.Release) || strings.HasPrefix(m.Release, "v") {
		return "", ErrRelease
	}
	return m.releaseSource + "/releases/download/v" + m.Release + "/", nil
}

func (m *Manager) catalogSnapshot() (CatalogStatus, []Plugin) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.catalog.Release != m.Release {
		return CatalogStatus{Release: m.Release, State: "idle"}, nil
	}
	return m.catalog, slices.Clone(m.catalogPlugins)
}

// RefreshCatalog only fetches small signed metadata. Ordinary status polling
// stays offline, including after an error. The button can force a retry.
func (m *Manager) RefreshCatalog(ctx context.Context, force bool) error {
	m.mu.Lock()
	if m.catalogLoading || (!force && m.catalog.Release == m.Release && time.Since(m.catalogAttempt) < 10*time.Minute) {
		m.mu.Unlock()
		return nil
	}
	if m.catalog.Release != m.Release {
		m.catalogPlugins = nil
	}
	m.catalog = CatalogStatus{Release: m.Release, State: "loading"}
	m.catalogAttempt, m.catalogLoading = time.Now(), true
	m.mu.Unlock()

	entries, err := m.fetchCatalog(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.catalogLoading = false
	if err != nil {
		m.catalog.State, m.catalog.Error = "error", err.Error()
		if errors.Is(err, ErrRelease) {
			m.catalog.State = "unavailable"
			if _, versionErr := m.releaseBase(); versionErr != nil {
				m.catalog.State = "development"
			}
		}
		return err
	}
	m.catalog.State, m.catalog.Error = "ready", ""
	m.catalogPlugins = nil
	for _, entry := range entries {
		m.catalogPlugins = append(m.catalogPlugins, entry.plugin)
	}
	return nil
}

func (m *Manager) fetchMetadata(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TitanOS/"+m.Release)
	response, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrRelease
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrDownload, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: metadata exceeds size limit", ErrInvalid)
	}
	return data, nil
}

func (m *Manager) fetchCatalog(ctx context.Context) ([]catalogEntry, error) {
	base, err := m.releaseBase()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no trusted plugin keys", ErrInvalid)
	}
	data, err := m.fetchMetadata(ctx, base+"manifest.json", 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := m.fetchMetadata(ctx, base+"manifest.json.minisig", 4096)
	if err != nil {
		return nil, err
	}
	if minisign.Verify(keys, data, string(sig)) != nil {
		return nil, fmt.Errorf("%w: release signature", ErrInvalid)
	}
	var release struct {
		Version string `json:"version"`
		Tag     string `json:"tag"`
		Kernel  string `json:"kernel"`
		Plugins []struct {
			Name  string `json:"name"`
			Asset string `json:"asset"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &release) != nil || release.Version != m.Release || release.Tag != "v"+m.Release || release.Kernel == "" || len(release.Plugins) > 32 {
		return nil, fmt.Errorf("%w: release identity or plugin inventory", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, item := range release.Plugins {
		if !nameRE.MatchString(item.Name) || item.Asset != item.Name+manifestSuffix || seen[item.Name] {
			return nil, fmt.Errorf("%w: plugin inventory", ErrInvalid)
		}
		seen[item.Name] = true
	}
	var entries []catalogEntry
	for _, item := range release.Plugins {
		data, err := m.fetchMetadata(ctx, base+item.Asset, 64<<10)
		if err != nil {
			return nil, err
		}
		sig, err := m.fetchMetadata(ctx, base+item.Asset+".minisig", 4096)
		if err != nil {
			return nil, err
		}
		if minisign.Verify(keys, data, string(sig)) != nil {
			return nil, fmt.Errorf("%w: signature for %s", ErrInvalid, item.Name)
		}
		manifest, err := parseManifest(data, item.Asset)
		if err != nil {
			return nil, err
		}
		if (manifest.Version != m.Release && !strings.HasSuffix(manifest.Version, "-titanos-"+m.Release)) || (manifest.Kernel != "" && manifest.Kernel != release.Kernel) {
			return nil, fmt.Errorf("%w: plugin %s targets another release or kernel", ErrInvalid, item.Name)
		}
		plugin := Plugin{Manifest: manifest, Official: true, Download: true, LicenseURL: base + item.Name + "-licenses.tar.gz"}
		if !m.compatible(manifest) {
			plugin.Problem = "kernel"
		}
		entries = append(entries, catalogEntry{plugin: plugin, manifest: data, signature: sig})
	}
	return entries, nil
}

// InstallOfficial accepts a name, never a client-supplied URL. Downloads go
// directly to the selected data volume; nothing is staged in RAM or on USB.
func (m *Manager) InstallOfficial(ctx context.Context, name, volume string, mounted []string) error {
	return m.InstallOfficialWithActivation(ctx, name, volume, mounted, true)
}

// InstallOfficialWithActivation verifies the same signed release in either
// mode; deferred activation changes only the live system integration.
func (m *Manager) InstallOfficialWithActivation(ctx context.Context, name, volume string, mounted []string, activate bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	if m.Disabled {
		return ErrDisabled
	}
	if !nameRE.MatchString(name) {
		return ErrNotFound
	}
	settings := m.current()
	if settings.Volume != "" {
		volume = settings.Volume
	}
	if !slices.Contains(mounted, volume) || volume == "" {
		return ErrUnknownVolume
	}
	if _, exists := m.installed(volume)[name]; exists {
		return ErrExists
	}
	// Revalidate the signed release and plugin at install time, not just when
	// the page was opened. Untrusted metadata never reaches the volume.
	entries, err := m.fetchCatalog(ctx)
	if err != nil {
		return err
	}
	var selected *catalogEntry
	for i := range entries {
		if entries[i].plugin.Name == name {
			selected = &entries[i]
			break
		}
	}
	if selected == nil {
		return ErrNotFound
	}
	if !m.compatible(selected.plugin.Manifest) {
		return fmt.Errorf("%w: plugin targets another kernel", ErrInvalid)
	}
	base, _ := m.releaseBase()
	dir := m.baseDir(volume)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, name+manifestSuffix)
	if err := os.WriteFile(path, selected.manifest, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(path+".minisig", selected.signature, 0o600); err != nil {
		return err
	}
	if err := m.downloadImage(ctx, base+name+".raw", filepath.Join(stage, name+".raw"), selected.plugin.Manifest); err != nil {
		return err
	}
	return m.install(ctx, path, volume, "", mounted, activate)
}

// downloadStall bounds a transfer that stops delivering bytes. There is no
// total deadline: a 2 GiB build SDK on a slow line must still finish.
var downloadStall = 2 * time.Minute

var errDownloadChecksum = errors.New("image checksum or size")

// downloadImage fetches an image into dest and verifies size and SHA-256.
// An existing partial dest is resumed with an HTTP Range request; a resumed
// file whose final hash does not match is deleted and fetched once more from
// the start. The caller must own dest's directory.
func (m *Manager) downloadImage(ctx context.Context, url, dest string, manifest Manifest) error {
	for attempt := 0; ; attempt++ {
		resumed, err := m.downloadAttempt(ctx, url, dest, manifest)
		if err != nil && resumed && attempt == 0 && errors.Is(err, errDownloadChecksum) {
			continue
		}
		return err
	}
}

func (m *Manager) downloadAttempt(ctx context.Context, url, dest string, manifest Manifest) (resumed bool, err error) {
	if manifest.Size < 0 {
		return false, fmt.Errorf("%w: image size", ErrInvalid)
	}
	file, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return false, err
	}
	defer file.Close()
	discard := func(cause error) error {
		file.Close()
		os.Remove(dest)
		return cause
	}
	st, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !st.Mode().IsRegular() {
		return false, fmt.Errorf("%w: partial download is not a regular file", ErrInvalid)
	}
	hash := sha256.New()
	offset := st.Size()
	if offset > manifest.Size {
		offset = 0
	}
	if offset > 0 {
		if _, err := io.Copy(hash, io.NewSectionReader(file, 0, offset)); err != nil {
			return false, err
		}
		if offset == manifest.Size {
			if hex.EncodeToString(hash.Sum(nil)) == strings.ToLower(manifest.SHA256) {
				return true, file.Sync()
			}
			hash.Reset()
			offset = 0
		}
	}
	resumed = offset > 0
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Bound a stalled transfer, including waiting for response headers.
	var stalled atomic.Bool
	watchdog := time.AfterFunc(downloadStall, func() { stalled.Store(true); cancel() })
	defer watchdog.Stop()
	transferErr := func(cause error) error {
		if stalled.Load() {
			return fmt.Errorf("%w: no data for %s", ErrDownload, downloadStall)
		}
		return fmt.Errorf("%w: %v", ErrDownload, cause)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return resumed, err
	}
	req.Header.Set("User-Agent", "TitanOS/"+m.Release)
	if resumed {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := m.client.Do(req)
	if err != nil {
		return resumed, transferErr(err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusPartialContent && resumed:
		var start, end, total int64
		if _, err := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != offset || end != manifest.Size-1 || total != manifest.Size {
			return resumed, discard(fmt.Errorf("%w: unexpected Content-Range", ErrInvalid))
		}
	case response.StatusCode == http.StatusOK:
		// The server ignored the range: start over.
		hash.Reset()
		offset, resumed = 0, false
	default:
		return resumed, fmt.Errorf("%w: HTTP %d", ErrDownload, response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != manifest.Size-offset {
		return resumed, discard(fmt.Errorf("%w: image size", ErrInvalid))
	}
	if err := file.Truncate(offset); err != nil {
		return resumed, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return resumed, err
	}
	reader := io.LimitReader(response.Body, manifest.Size-offset+1)
	buffer := make([]byte, 256<<10)
	total := offset
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			watchdog.Reset(downloadStall)
			total += int64(n)
			if total > manifest.Size {
				return resumed, discard(fmt.Errorf("%w: image exceeds size", ErrInvalid))
			}
			if _, err := file.Write(buffer[:n]); err != nil {
				return resumed, err
			}
			hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// Keep the verified-length prefix for the next attempt.
			return resumed, transferErr(readErr)
		}
	}
	if total != manifest.Size || hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(manifest.SHA256) {
		if total < manifest.Size {
			return resumed, fmt.Errorf("%w: transfer ended early", ErrDownload)
		}
		return resumed, discard(fmt.Errorf("%w: %w", ErrInvalid, errDownloadChecksum))
	}
	return resumed, file.Sync()
}
