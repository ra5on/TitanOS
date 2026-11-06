package plugins

import (
	"context"
	"encoding/json"

	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

// Signed assets of a TitanOS release other than plugins: the NVIDIA build
// SDK (local_build.go) and the image of the remote view (package viewer,
// PLAN 63). Both trust the release manifest only after its minisign
// signature and its version and tag were checked.

// releaseBaseFor is the download folder of one exact release. A development
// commit never borrows assets from a published release.
func (m *Manager) releaseBaseFor(version string) (string, error) {
	if !releaseVersion.MatchString(version) || len(version) > 0 && version[0] == 'v' {
		return "", ErrRelease
	}
	return m.releaseSource + "/releases/download/v" + version + "/", nil
}

// ReleaseManifest returns the manifest.json of the release of version after
// checking its signature and that it names that version and tag. Callers
// parse the fields they need from it.
func (m *Manager) ReleaseManifest(ctx context.Context, version string) ([]byte, error) {
	base, err := m.releaseBaseFor(version)
	if err != nil {
		return nil, err
	}
	data, err := m.fetchMetadata(ctx, base+"manifest.json", 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := m.fetchMetadata(ctx, base+"manifest.json.minisig", 4096)
	if err != nil {
		return nil, err
	}
	if minisign.Verify(minisign.LoadKeys(m.keyDir), data, string(sig)) != nil {
		return nil, ErrInvalid
	}
	var release struct{ Version, Tag string }
	if json.Unmarshal(data, &release) != nil || release.Version != version || release.Tag != "v"+version {
		return nil, ErrInvalid
	}
	return data, nil
}

// DownloadReleaseAsset fetches one asset of the release of version to dest
// (resumable: an existing partial file is continued and checked) and
// verifies size and SHA-256 from the signed manifest.
func (m *Manager) DownloadReleaseAsset(ctx context.Context, version, asset, dest, sha256 string, size int64) error {
	base, err := m.releaseBaseFor(version)
	if err != nil {
		return err
	}
	if size <= 0 || !driverHashRE.MatchString(sha256) {
		return ErrInvalid
	}
	return m.downloadImage(ctx, base+asset, dest, Manifest{SHA256: sha256, Size: size})
}
