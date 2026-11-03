package update

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

// --- choosing a version -----------------------------------------------------

// ChoiceFloor is the oldest version that may be chosen. Older releases do not
// know the signing keys of 2026-11-03 (titanos-release-2, titanos-recovery-2):
// a NAS that went back to one would refuse every later update.
const ChoiceFloor = "0.46.2"

// maxChoices bounds how many releases of the feed are looked at; each costs
// two small downloads.
const maxChoices = 20

// versionsFor is how long a list of versions is kept.
const versionsFor = 10 * time.Minute

// Choice is a release that can be installed, newer or older.
type Choice struct {
	Version   string    `json:"version"`
	Channel   string    `json:"channel"`
	Released  time.Time `json:"released"`
	Kernel    string    `json:"kernel"`
	Size      int64     `json:"size"`
	Changelog string    `json:"changelog"`
	// Running is the version that runs now, Newest the newest one listed.
	Running bool `json:"running"`
	Newest  bool `json:"newest"`
}

type chosen struct {
	manifest       Manifest
	raw, signature []byte
}

// versionTags returns the release tags of the feed, newest first, that may
// be chosen on the channel.
func versionTags(feed []byte, channel string) []string {
	floor, _ := parseVersion(ChoiceFloor)
	var tags []string
	for _, match := range feedTag.FindAllSubmatch(feed, -1) {
		tag := string(match[1])
		v, ok := parseVersion(tag)
		if !ok || v.compare(floor) < 0 || v.pre != "" && channel != ChannelBeta || slices.Contains(tags, tag) {
			continue
		}
		tags = append(tags, tag)
	}
	slices.SortStableFunc(tags, func(a, b string) int {
		va, _ := parseVersion(a)
		vb, _ := parseVersion(b)
		return vb.compare(va)
	})
	if len(tags) > maxChoices {
		tags = tags[:maxChoices]
	}
	return tags
}

// release fetches and verifies the manifest of one release tag.
func (m *Manager) release(ctx context.Context, keys []minisign.PublicKey, tag string) (chosen, error) {
	base := m.source() + "/releases/download/" + tag + "/"
	raw, err := m.fetch(ctx, base+"manifest.json", maxManifest)
	if err != nil {
		return chosen{}, err
	}
	signature, err := m.fetch(ctx, base+"manifest.json.minisig", 4096)
	if err != nil {
		return chosen{}, err
	}
	if minisign.Verify(keys, raw, string(signature)) != nil {
		return chosen{}, ErrSignature
	}
	manifest, err := parseManifest(raw)
	if err != nil {
		return chosen{}, err
	}
	want, _ := parseVersion(tag)
	got, ok := parseVersion(manifest.Version)
	if !ok || got.compare(want) != 0 || manifest.Tag != tag {
		return chosen{}, fmt.Errorf("%w: %s holds the manifest of %s", ErrManifest, tag, manifest.Version)
	}
	return chosen{manifest: manifest, raw: raw, signature: signature}, nil
}

// Versions lists the published releases from ChoiceFloor on, newest first.
// A tag without a published, correctly signed manifest is left out.
func (m *Manager) Versions(ctx context.Context) ([]Choice, error) {
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return nil, ErrNoKey
	}
	m.mu.Lock()
	channel := m.settings.Channel
	if m.choices != nil && m.choicesChannel == channel && m.now().Sub(m.choicesAt) < versionsFor {
		list := slices.Clone(m.choices)
		m.mu.Unlock()
		return list, nil
	}
	m.mu.Unlock()
	feed, err := m.fetch(ctx, m.source()+"/releases.atom", 2<<20)
	if err != nil {
		return nil, err
	}
	running, _ := parseVersion(m.version)
	var list []Choice
	for _, tag := range versionTags(feed, channel) {
		release, err := m.release(ctx, keys, tag)
		if err != nil {
			// Tagged but not (yet) published, or not trusted: not offered.
			slog.Debug("release not offered", "tag", tag, "err", err)
			continue
		}
		v, _ := parseVersion(release.manifest.Version)
		list = append(list, Choice{
			Version: release.manifest.Version, Channel: release.manifest.Channel, Released: release.manifest.Released,
			Kernel: release.manifest.Kernel, Size: release.manifest.Size(), Changelog: release.manifest.Changelog,
			Running: v.compare(running) == 0, Newest: len(list) == 0,
		})
	}
	m.mu.Lock()
	m.choices, m.choicesChannel, m.choicesAt = list, channel, m.now()
	m.mu.Unlock()
	return slices.Clone(list), nil
}

// InstallVersion installs a chosen release into the other slot, as Install
// does with the one a check found. Choosing one that is older than the
// newest release switches automatic installation off: the next night would
// otherwise bring the newest one back.
func (m *Manager) InstallVersion(ctx context.Context, wanted string) error {
	if m.Disabled {
		return ErrDisabled
	}
	v, ok := parseVersion(wanted)
	floor, _ := parseVersion(ChoiceFloor)
	if !ok || v.compare(floor) < 0 {
		return fmt.Errorf("%w: versions before %s cannot be chosen", ErrInvalid, ChoiceFloor)
	}
	if running, ok := parseVersion(m.version); ok && v.compare(running) == 0 {
		return fmt.Errorf("%w: %s runs already", ErrNothing, wanted)
	}
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return ErrNoKey
	}
	release, err := m.release(ctx, keys, "v"+wanted)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.available, m.availableRaw, m.availableSig = &release.manifest, release.raw, release.signature
	// Older than what runs, or than the newest release the last check found.
	older := false
	if running, ok := parseVersion(m.version); ok && v.compare(running) < 0 {
		older = true
	}
	if m.found != nil {
		if newest, ok := parseVersion(m.found.Version); ok && v.compare(newest) < 0 {
			older = true
		}
	}
	settings := m.settings
	m.mu.Unlock()
	if err := m.Install(ctx); err != nil {
		m.mu.Lock()
		m.evaluateLocked(false)
		m.mu.Unlock()
		return err
	}
	if older && settings.AutoInstall {
		settings.AutoInstall = false
		if err := m.SetSettings(settings); err != nil {
			slog.Warn("automatic installation not switched off", "err", err)
		}
	}
	return nil
}
