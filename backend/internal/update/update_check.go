package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

// --- looking for a release --------------------------------------------------

func (m *Manager) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TitanOS/"+m.version)
	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than expected", url)
	}
	return data, nil
}

var (
	assetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,120}$`)
	hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)
	feedTag   = regexp.MustCompile(`/releases/tag/([A-Za-z0-9][A-Za-z0-9._+-]{0,120})"`)
	// debianVersion is a Debian package version, e.g. 6.16.12-1~bpo13+1.
	debianVersion = regexp.MustCompile(`^[0-9][0-9A-Za-z.+~:-]{0,79}$`)
	cmdlineSet    = regexp.MustCompile(`^[\x21-\x7e]+( [\x21-\x7e]+)*$`)
)

// parseManifest reads a manifest and refuses whatever TitanOS would not
// know how to install.
func parseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("%w: %v", ErrManifest, err)
	}
	bad := func(what string) (Manifest, error) { return Manifest{}, fmt.Errorf("%w: %s", ErrManifest, what) }
	if _, ok := parseVersion(manifest.Version); !ok {
		return bad("version")
	}
	if manifest.MinVersion != "" {
		if _, ok := parseVersion(manifest.MinVersion); !ok {
			return bad("minimum version")
		}
	}
	if !assetName.MatchString(manifest.Tag) {
		return bad("tag")
	}
	if manifest.Channel != ChannelStable && manifest.Channel != ChannelBeta {
		return bad("channel")
	}
	if !cmdlineSet.MatchString(manifest.Cmdline) || !strings.Contains(manifest.Cmdline, "live-media-path=/titanos/@SLOT@") || len(manifest.Cmdline) > 1000 {
		return bad("kernel command line")
	}
	if len(manifest.Changelog) > 100_000 {
		return bad("changelog")
	}
	var names []string
	for _, file := range manifest.Files {
		if !slices.Contains(slotFiles, file.Name) || slices.Contains(names, file.Name) || !assetName.MatchString(file.Asset) || !hexSHA256.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxFile {
			return bad("file " + file.Name)
		}
		names = append(names, file.Name)
	}
	if len(names) != len(slotFiles) {
		return bad("files")
	}
	for name, variant := range manifest.Variants {
		if name != VariantNew || variant.Kernel == "" || !assetName.MatchString(variant.Kernel) || variant.KernelPackage != "" && !debianVersion.MatchString(variant.KernelPackage) {
			return bad("variant " + name)
		}
		var names []string
		for _, file := range variant.Files {
			if !slices.Contains(slotFiles, file.Name) || slices.Contains(names, file.Name) || !assetName.MatchString(file.Asset) || !hexSHA256.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxFile {
				return bad("variant file " + file.Name)
			}
			names = append(names, file.Name)
		}
		if len(names) != len(slotFiles) {
			return bad("variant files")
		}
	}
	plugins := map[string]bool{}
	for _, plugin := range manifest.Plugins {
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`).MatchString(plugin.Name) || plugin.Asset != plugin.Name+".plugin.json" || plugins[plugin.Name] {
			return bad("plugins")
		}
		plugins[plugin.Name] = true
	}
	return manifest, nil
}

// latest fetches and verifies the manifest of the newest release of the channel.
func (m *Manager) latest(ctx context.Context, channel string) (Manifest, []byte, []byte, error) {
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return Manifest{}, nil, nil, ErrNoKey
	}
	source := m.source()
	base := source + "/releases/latest/download/"
	if channel == ChannelBeta {
		// "latest" leaves out pre-releases; the feed lists every release,
		// the newest first.
		feed, err := m.fetch(ctx, source+"/releases.atom", 2<<20)
		if err != nil {
			return Manifest{}, nil, nil, err
		}
		match := feedTag.FindSubmatch(feed)
		if match == nil {
			return Manifest{}, nil, nil, fmt.Errorf("%w: no release found", ErrNothing)
		}
		base = source + "/releases/download/" + string(match[1]) + "/"
	}
	data, err := m.fetch(ctx, base+"manifest.json", maxManifest)
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	signature, err := m.fetch(ctx, base+"manifest.json.minisig", 4096)
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	if minisign.Verify(keys, data, string(signature)) != nil {
		return Manifest{}, nil, nil, ErrSignature
	}
	manifest, err := parseManifest(data)
	return manifest, data, signature, err
}

// CheckAfterLogin starts a bounded check without delaying authentication.
// Concurrent administrator logins share one check; ordinary page reloads do
// not trigger it. Check emits the existing, version-deduplicated notification.
// With checks set to manual it does nothing.
func (m *Manager) CheckAfterLogin() bool {
	m.mu.Lock()
	if m.Disabled || !m.loaded || m.loginCheckRunning || m.settings.Manual() {
		m.mu.Unlock()
		return false
	}
	m.loginCheckRunning = true
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); m.loginCheckRunning = false; m.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := m.Check(ctx); err != nil {
			slog.Warn("update check after login", "err", err)
		}
	}()
	return true
}

// Check looks for a release that is newer than what is running.
func (m *Manager) Check(ctx context.Context) error {
	m.mu.Lock()
	channel := m.settings.Channel
	m.mu.Unlock()
	manifest, raw, signature, err := m.latest(ctx, channel)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCheck, m.checkError, m.available, m.availableRaw, m.availableSig = m.now(), "", nil, nil, nil
	if err != nil {
		m.checkError = err.Error()
		return err
	}
	m.found, m.foundRaw, m.foundSig = &manifest, raw, signature
	m.evaluateLocked(true)
	return nil
}

// evaluateLocked decides from the release found last and the kernel choice
// what can be installed: a newer release, the running version with the
// other kernel, or nothing. m.mu is held.
func (m *Manager) evaluateLocked(announce bool) {
	m.available, m.availableRaw, m.availableSig, m.kernelMissing = nil, nil, nil, ""
	manifest := m.found
	if manifest == nil {
		return
	}
	found, _ := parseVersion(manifest.Version)
	running, release := parseVersion(m.version)
	wanted := m.wantedVariant()
	_, hasVariant := manifest.forVariant(wanted)
	switch {
	// A build that is not a release is older than any release.
	case !release || found.compare(running) > 0:
		if !hasVariant && !m.appKind(manifest) {
			m.kernelMissing = manifest.Version
			return
		}
	case found.compare(running) == 0 && wanted != normalVariant(m.Variant) && hasVariant:
		// The running version with the chosen kernel: no announcement.
		m.available, m.availableRaw, m.availableSig = manifest, m.foundRaw, m.foundSig
		return
	default:
		return
	}
	m.available, m.availableRaw, m.availableSig = manifest, m.foundRaw, m.foundSig
	if announce && m.announced != manifest.Version && m.OnEvent != nil {
		m.announced = manifest.Version
		go m.OnEvent("update_available", manifest.Version)
	}
}
