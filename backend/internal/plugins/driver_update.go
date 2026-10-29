package plugins

// Independent driver updates use additional, signed assets on the running
// TitanOS release. The OS manifest and original plugin assets stay immutable;
// no plugin-only GitHub release can confuse older NAS release-feed readers.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

type DriverActivation struct {
	Set     string `json:"set"`
	BootID  string `json:"bootId"`
	Version string `json:"version"`
}
type DriverVersion struct {
	Local       bool   `json:"local"`
	Driver      string `json:"driver"`
	Toolkit     string `json:"toolkit"`
	Recommended bool   `json:"recommended"`
	Version     string `json:"version"`
	SHA256      string `json:"sha256"`
	Revision    uint64 `json:"revision"`
	Size        int64  `json:"size"`
	Downloaded  bool   `json:"downloaded"`
}
type DriverUpdateStatus struct {
	Source     *DriverSource     `json:"source,omitempty"`
	Versions   []DriverVersion   `json:"versions"`
	State      string            `json:"state"` // idle, checking, downloading, ready, current, unavailable, error
	Automatic  bool              `json:"automatic"`
	Version    string            `json:"version,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	Error      string            `json:"error,omitempty"`
	Checked    string            `json:"checked,omitempty"`
	Pending    *DriverActivation `json:"pending,omitempty"`
	CanRestore bool              `json:"canRestore"`
}

var driverSetRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
var driverHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (m *Manager) loadDriverSelection(s *Settings) error {
	if s.DriverSet != "" && !driverSetRE.MatchString(s.DriverSet) {
		return ErrInvalid
	}
	if p := s.PendingDriver; p != nil {
		if !driverSetRE.MatchString(p.Set) || p.BootID == "" {
			return ErrInvalid
		}
		if m.bootID != "" && p.BootID != m.bootID {
			s.DriverSet, s.PendingDriver = p.Set, nil
		}
	}
	return nil
}
func (m *Manager) driverSnapshot(s Settings) DriverUpdateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.driverStatus
	status.Versions = slices.Clone(status.Versions)
	if status.State == "" {
		status.State = "idle"
	}
	status.Automatic = s.AutomaticDrivers == nil || *s.AutomaticDrivers
	status.Pending, status.CanRestore = s.PendingDriver, s.DriverSet != ""
	return status
}
func (m *Manager) SetAutomaticDrivers(enabled bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	settings.AutomaticDrivers = &enabled
	return m.save(settings)
}
func (m *Manager) driverCache(volume string) string {
	return filepath.Join(m.baseDir(volume), "driver-updates", m.Release)
}
func (m *Manager) validDriver(data []byte, sig []byte) (Manifest, error) {
	if minisign.Verify(minisign.LoadKeys(m.keyDir), data, string(sig)) != nil {
		return Manifest{}, fmt.Errorf("%w: driver signature", ErrInvalid)
	}
	manifest, err := parseManifest(data, "nvidia"+manifestSuffix)
	if err != nil {
		return manifest, err
	}
	// Drivers of a kernel set in the plugin repository name no TitanOS
	// release: they fit every base with that exact kernel.
	if manifest.Name != "nvidia" || (manifest.TitanOS != m.Release && manifest.TitanOS != "") || manifest.Kernel != m.kernel || manifest.Revision == 0 || !driverHashRE.MatchString(manifest.SHA256) {
		return manifest, fmt.Errorf("%w: driver target or revision", ErrInvalid)
	}
	return manifest, nil
}
func (m *Manager) cachedDriver(dir string, verifyImage bool) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "nvidia"+manifestSuffix))
	if err != nil {
		return Manifest{}, err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "nvidia"+manifestSuffix+".minisig"))
	var manifest Manifest
	if err == nil {
		manifest, err = m.validDriver(data, sig)
	} else if os.IsNotExist(err) {
		manifest, err = m.localDriver(dir)
	}
	if err != nil {
		return manifest, err
	}
	if _, err := os.Stat(filepath.Join(dir, ".ready")); err != nil {
		return manifest, err
	}
	if verifyImage {
		sum, size, err := hashFile(filepath.Join(dir, "nvidia.raw"), maxImageBytes)
		if err != nil || size != manifest.Size || sum != manifest.SHA256 {
			return manifest, fmt.Errorf("%w: cached driver integrity", ErrInvalid)
		}
	}
	return manifest, nil
}
func (m *Manager) newestCached(volume string) (Manifest, error) {
	var newest Manifest
	entries, err := os.ReadDir(m.driverCache(volume))
	if os.IsNotExist(err) {
		return newest, nil
	}
	if err != nil {
		return newest, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !driverHashRE.MatchString(entry.Name()) {
			continue
		}
		manifest, err := m.cachedDriver(filepath.Join(m.driverCache(volume), entry.Name()), false)
		if err != nil {
			return newest, err
		} // fail closed, including the revision floor
		if manifest.SHA256 != entry.Name() {
			return newest, ErrInvalid
		}
		m.mu.Lock()
		found := false
		for i := range m.driverStatus.Versions {
			if m.driverStatus.Versions[i].SHA256 == manifest.SHA256 {
				m.driverStatus.Versions[i].Downloaded = true
				found = true
			}
		}
		if !found {
			m.driverStatus.Versions = append(m.driverStatus.Versions, DriverVersion{Version: manifest.Version, SHA256: manifest.SHA256, Revision: manifest.Revision, Size: manifest.Size, Downloaded: true, Local: strings.HasSuffix(manifest.Version, "-local")})
		}
		m.mu.Unlock()
		if manifest.Revision > newest.Revision {
			newest = manifest
		}
	}
	return newest, nil
}

// No actions lock during HTTP: status and unrelated VMs remain responsive.
// Downloading only writes an isolated cache directory, never live plugins.
func (m *Manager) CheckDriverUpdate(ctx context.Context, mounted []string) error {
	if m.LocalBuilds {
		return m.checkLocalDriver(ctx, mounted, false)
	}
	return m.checkDriverUpdate(ctx, mounted, "")
}
func (m *Manager) checkDriverUpdate(ctx context.Context, mounted []string, selected string) (err error) {
	if !m.driverActions.TryLock() {
		return nil
	}
	defer m.driverActions.Unlock()
	m.actions.Lock()
	settings := m.current()
	installed := m.installed(settings.Volume)["nvidia"]
	eligible := !m.Disabled && releaseVersion.MatchString(m.Release) && settings.Volume != "" && slices.Contains(mounted, settings.Volume) && installed.Name == "nvidia" && m.official(filepath.Join(m.dir(settings.Volume), "nvidia"+manifestSuffix))
	if eligible {
		m.mu.Lock()
		m.driverVolume = settings.Volume
		m.mu.Unlock()
	}
	m.actions.Unlock()
	if !eligible {
		return ErrNotFound
	}
	m.mu.Lock()
	m.driverAttempt = time.Now()
	m.driverStatus.State, m.driverStatus.Error = "checking", ""
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.driverVolume = ""
		m.driverStatus.Checked = time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			m.driverStatus.State, m.driverStatus.Error = "error", err.Error()
			if errors.Is(err, ErrRelease) {
				m.driverStatus.State, m.driverStatus.Error = "unavailable", ""
			}
		}
	}()
	cached, err := m.newestCached(settings.Volume)
	if err != nil {
		return err
	}
	// Restore a previously downloaded choice after restarting titand, even
	// when GitHub is temporarily unavailable. Activation re-verifies its image.
	if cached.Revision > installed.Revision {
		m.mu.Lock()
		m.driverStatus.Version, m.driverStatus.SHA256 = cached.Version, cached.SHA256
		m.mu.Unlock()
	}
	metaCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	base, err := m.driverBase(metaCtx)
	if err != nil {
		return err
	}
	catalog, err := m.fetchDriverVersions(metaCtx, base)
	if err != nil {
		return err
	}
	if len(catalog) == 0 {
		return ErrRelease
	}
	if catalog[0].Revision < cached.Revision || catalog[0].Revision < installed.Revision {
		return fmt.Errorf("%w: driver catalog went backwards", ErrInvalid)
	}
	for i := range catalog {
		cached, e := m.cachedDriver(filepath.Join(m.driverCache(settings.Volume), catalog[i].SHA256), false)
		catalog[i].Downloaded = e == nil && cached.SHA256 == catalog[i].SHA256 && cached.Revision == catalog[i].Revision
	}
	m.mu.Lock()
	remote := slices.Clone(catalog)
	for _, local := range m.driverStatus.Versions {
		if local.Downloaded && !slices.ContainsFunc(remote, func(v DriverVersion) bool { return v.SHA256 == local.SHA256 }) {
			local.Recommended = false
			catalog = append(catalog, local)
		}
	}
	m.driverStatus.Versions = catalog
	m.mu.Unlock()
	choice := catalog[0]
	for _, entry := range catalog {
		if entry.Recommended {
			choice = entry
		}
	}
	if selected != "" {
		found := false
		for _, entry := range catalog {
			if entry.SHA256 == selected {
				choice, found = entry, true
			}
		}
		if !found {
			return ErrNotFound
		}
	}
	if selected == "" && choice.SHA256 == installed.SHA256 {
		m.mu.Lock()
		m.driverStatus.State, m.driverStatus.Version, m.driverStatus.SHA256 = "current", "", ""
		m.mu.Unlock()
		return nil
	}
	asset := "nvidia-update-" + choice.SHA256 + manifestSuffix
	data, err := m.fetchMetadata(metaCtx, base+asset, 64<<10)
	if err != nil {
		return err
	}
	sig, err := m.fetchMetadata(metaCtx, base+asset+".minisig", 4096)
	if err != nil {
		return err
	}
	candidate, err := m.validDriver(data, sig)
	if err != nil {
		return err
	}
	if candidate.SHA256 != choice.SHA256 || candidate.Revision != choice.Revision || candidate.Version != choice.Version || candidate.Size != choice.Size {
		return ErrInvalid
	}
	destination := filepath.Join(m.driverCache(settings.Volume), candidate.SHA256)
	if _, err := os.Stat(destination); err == nil {
		existing, err := m.cachedDriver(destination, true)
		if err != nil {
			return err
		}
		if existing.Revision != candidate.Revision || existing.Version != candidate.Version {
			return ErrInvalid
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		m.mu.Lock()
		m.driverStatus.State = "downloading"
		m.mu.Unlock()
		if err := os.MkdirAll(m.driverCache(settings.Volume), 0o700); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(m.driverCache(settings.Volume), ".download-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		for name, bytes := range map[string][]byte{"nvidia" + manifestSuffix: data, "nvidia" + manifestSuffix + ".minisig": sig} {
			if err := os.WriteFile(filepath.Join(stage, name), bytes, 0o600); err != nil {
				return err
			}
		}
		if err := m.downloadImage(ctx, base+"nvidia-update-"+candidate.SHA256+".raw", filepath.Join(stage, "nvidia.raw"), candidate); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, ".ready"), []byte(m.Release+"\n"), 0o600); err != nil {
			return err
		}
		if err := syncTree(stage); err != nil {
			return err
		}
		if err := os.Rename(stage, destination); err != nil {
			return err
		}
		if err := syncDirectory(m.driverCache(settings.Volume)); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.driverStatus.State, m.driverStatus.Version, m.driverStatus.SHA256 = "ready", candidate.Version, candidate.SHA256
	for i := range m.driverStatus.Versions {
		if m.driverStatus.Versions[i].SHA256 == candidate.SHA256 {
			m.driverStatus.Versions[i].Downloaded = true
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) RequestDriverUpdate(hash string, mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if hash != "" && !driverHashRE.MatchString(hash) {
		return ErrInvalid
	}
	if _, err := m.releaseBase(); err != nil {
		return err
	}
	settings := m.current()
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	if m.installed(settings.Volume)["nvidia"].Name == "" {
		return ErrNotFound
	}
	select {
	case m.driverRequests <- hash:
	default:
	}
	return nil
}

// driverBase is where driver updates come from: the kernel set of the
// plugin repository for the running kernel, otherwise the TitanOS release.
func (m *Manager) driverBase(ctx context.Context) (string, error) {
	base, err := m.releaseBase()
	if err != nil {
		return "", err
	}
	_, kernel, err := m.fetchRepoSets(ctx, repoTarget{base: m.Release, kernel: m.kernel, kernelPackage: m.kernelPackage}, m.fetchMetadata)
	switch {
	case errors.Is(err, errNoPluginSet):
		return base, nil
	case err != nil:
		return "", err
	case kernel == nil:
		return base, nil
	}
	return kernel.base, nil
}

func (m *Manager) fetchDriverVersions(ctx context.Context, base string) ([]DriverVersion, error) {
	data, err := m.fetchMetadata(ctx, base+"nvidia-updates.json", 64<<10)
	if err != nil {
		return nil, err
	}
	sig, err := m.fetchMetadata(ctx, base+"nvidia-updates.json.minisig", 4096)
	if err != nil {
		return nil, err
	}
	if minisign.Verify(minisign.LoadKeys(m.keyDir), data, string(sig)) != nil {
		return nil, fmt.Errorf("%w: driver catalog signature", ErrInvalid)
	}
	var catalog struct {
		TitanOS       string          `json:"titanos"`
		Kernel        string          `json:"kernel"`
		KernelPackage string          `json:"kernelPackage"`
		Versions      []DriverVersion `json:"versions"`
		Recommended   string          `json:"recommended"`
	}
	if json.Unmarshal(data, &catalog) != nil {
		return nil, ErrInvalid
	}
	// A kernel set's list names the kernel package instead of a release.
	fits := catalog.TitanOS == m.Release || (catalog.TitanOS == "" && catalog.KernelPackage != "" && catalog.KernelPackage == m.kernelPackage)
	if !fits || catalog.Kernel != m.kernel || len(catalog.Versions) > 32 {
		return nil, ErrInvalid
	}
	seen, revisions := map[string]bool{}, map[uint64]bool{}
	recommended := false
	for i, entry := range catalog.Versions {
		if !driverHashRE.MatchString(entry.SHA256) || entry.Revision == 0 || revisions[entry.Revision] || seen[entry.SHA256] || len(entry.Version) == 0 || len(entry.Version) > 160 || entry.Size <= 0 || entry.Size > maxImageBytes {
			return nil, ErrInvalid
		}
		seen[entry.SHA256], revisions[entry.Revision] = true, true
		catalog.Versions[i].Downloaded = false
		catalog.Versions[i].Recommended = entry.SHA256 == catalog.Recommended
		recommended = recommended || catalog.Versions[i].Recommended
	}
	if len(catalog.Versions) > 0 && !recommended {
		return nil, ErrInvalid
	}
	sort.Slice(catalog.Versions, func(i, j int) bool { return catalog.Versions[i].Revision > catalog.Versions[j].Revision })
	return catalog.Versions, nil
}

func (m *Manager) runDriverUpdates(ctx context.Context, mounted func() []string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	cacheLoaded := false
	for {
		m.mu.Lock()
		loaded, due := m.loaded, time.Since(m.driverAttempt) >= 24*time.Hour
		m.mu.Unlock()
		settings := m.current()
		if loaded && !cacheLoaded && settings.Volume != "" && slices.Contains(mounted(), settings.Volume) {
			_, _ = m.newestCached(settings.Volume)
			cacheLoaded = true
		}
		if loaded && due && (settings.AutomaticDrivers == nil || *settings.AutomaticDrivers) {
			_ = m.CheckDriverUpdate(ctx, mounted())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case hash := <-m.driverRequests:
			if m.LocalBuilds {
				_ = m.checkLocalDriver(ctx, mounted(), hash == "prepare")
			} else {
				_ = m.checkDriverUpdate(ctx, mounted(), hash)
			}
		}
	}
}

// SelectDriver prepares a complete set using hard links on the data volume.
// The cached choice is switched only on a different boot ID, not a titand restart.
// Other plugin mutations are blocked until the choice is cancelled or booted.
func (m *Manager) SelectDriver(hash string, mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	settings := m.current()
	if hash == "cancel" {
		settings.PendingDriver = nil
		return m.save(settings)
	}
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	if m.bootID == "" {
		return fmt.Errorf("%w: missing boot identity", ErrInvalid)
	}
	if m.installed(settings.Volume)["nvidia"].Name == "" {
		return ErrNotFound
	}
	if err := m.ensureReleaseSet(settings.Volume); err != nil {
		return err
	}
	source := filepath.Join(m.driverCache(settings.Volume), hash)
	var manifest Manifest
	var err error
	if hash == "bundled" {
		source = m.releaseDir(settings.Volume, m.Release)
		manifest, err = readManifest(filepath.Join(source, "nvidia"+manifestSuffix))
		if err == nil && (!m.official(filepath.Join(source, "nvidia"+manifestSuffix)) || !m.compatible(manifest)) {
			err = ErrInvalid
		}
		if err == nil {
			sum, size, e := hashFile(filepath.Join(source, "nvidia.raw"), maxImageBytes)
			if e != nil || sum != strings.ToLower(manifest.SHA256) || size != manifest.Size {
				err = ErrInvalid
			}
		}
	} else {
		if !driverHashRE.MatchString(hash) {
			return ErrInvalid
		}
		manifest, err = m.cachedDriver(source, true)
		if err == nil && manifest.SHA256 != hash {
			err = ErrInvalid
		}
	}
	if err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(random[:])
	parent := filepath.Join(m.releaseDir(settings.Volume, m.Release), ".sets")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	destination := filepath.Join(parent, id)
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(destination)
		}
	}()
	for name := range m.installed(settings.Volume) {
		from := m.dir(settings.Volume)
		if name == "nvidia" {
			from = source
		}
		for _, suffix := range []string{".raw", manifestSuffix, manifestSuffix + ".minisig", ".local-build.json"} {
			err := os.Link(filepath.Join(from, name+suffix), filepath.Join(destination, name+suffix))
			if os.IsNotExist(err) && (suffix == ".local-build.json" || (suffix == manifestSuffix+".minisig" && (name != "nvidia" || strings.HasSuffix(manifest.Version, "-local")))) {
				continue
			}
			if err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(destination, ".ready"), []byte(m.Release+"\n"), 0o600); err != nil {
		return err
	}
	if err := syncTree(destination); err != nil {
		return err
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	settings.PendingDriver = &DriverActivation{Set: id, BootID: m.bootID, Version: manifest.Version}
	if err := m.save(settings); err != nil {
		return err
	}
	committed = true
	return nil
}
