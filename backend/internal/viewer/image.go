package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// DevImage is the image a development build uses; nothing is downloaded.
const DevImage = "titanos-viewer:dev"

// imageRetry is the least time between two failed attempts to get the image.
var imageRetry = time.Minute

// maxImageSize bounds the image archive of a release.
const maxImageSize = 2 << 30

// ImageStatus is where the image of the remote view stands.
type ImageStatus struct {
	// State is "missing" (not looked for yet), "downloading", "loading",
	// "ready" or "error".
	State   string `json:"state"`
	Version string `json:"version"`
	// Bytes and Size count the download; Progress is Bytes/Size (0–1).
	Bytes    int64   `json:"bytes"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`
	// Dev is set in development mode (local image titanos-viewer:dev).
	Dev bool `json:"dev,omitempty"`
}

// ReleaseImage is the entry "viewer" of a release manifest.
type ReleaseImage struct {
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	ImageID string `json:"imageId"`
}

var (
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
	imageIDRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// ErrNoImage is a release without a viewer image (or a development
	// commit, which never borrows one from a release).
	ErrNoImage = errors.New("viewer: the release has no viewer image")
)

// ParseReleaseImage reads the entry "viewer" of a manifest whose signature
// was checked, for the release of version.
func ParseReleaseImage(manifest []byte, version string) (ReleaseImage, error) {
	var release struct {
		Version, Tag string
		Viewer       *ReleaseImage `json:"viewer"`
	}
	if err := json.Unmarshal(manifest, &release); err != nil {
		return ReleaseImage{}, fmt.Errorf("viewer: manifest: %w", err)
	}
	if release.Version != version || release.Tag != "v"+version {
		return ReleaseImage{}, errors.New("viewer: manifest of another release")
	}
	if release.Viewer == nil {
		return ReleaseImage{}, ErrNoImage
	}
	img := *release.Viewer
	if img.Asset != "titanos-viewer-"+version+".tar.zst" || !sha256Hex.MatchString(img.SHA256) ||
		img.Size <= 0 || img.Size > maxImageSize || !imageIDRE.MatchString(img.ImageID) {
		return ReleaseImage{}, errors.New("viewer: invalid viewer entry in the manifest")
	}
	return img, nil
}

// imageState is guarded by Manager.mu.
type imageState struct {
	status  ImageStatus
	id      string
	part    string
	running bool
	checked time.Time
}

// ImageStatus reports the state of the image; the download progress comes
// from the size of the partial file.
func (m *Manager) ImageStatus() ImageStatus {
	m.mu.Lock()
	st := m.image.status
	part := m.image.part
	m.mu.Unlock()
	st.Version, st.Dev = m.Version, m.Dev
	if st.State == "" {
		st.State = "missing"
	}
	if st.State == "downloading" && part != "" {
		if info, err := os.Stat(part); err == nil {
			st.Bytes = info.Size()
		}
	}
	if st.Size > 0 {
		st.Progress = min(1, float64(st.Bytes)/float64(st.Size))
	}
	return st
}

// PrepareImage starts getting the image if it is not ready (the button in
// the settings); an error a minute ago is retried at once.
func (m *Manager) PrepareImage() {
	m.mu.Lock()
	if m.image.status.State == "error" {
		m.image.checked = time.Time{}
	}
	m.mu.Unlock()
	m.readyImage()
}

// readyImage returns the ID of the loaded image, or "" while it is not
// ready; then it starts getting it (once at a time).
func (m *Manager) readyImage() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.image.status.State == "ready" {
		return m.image.id
	}
	// After a failure, a window that reconnects does not fetch again at
	// once: at most once a minute.
	if m.image.status.State == "error" && time.Since(m.image.checked) < imageRetry {
		return ""
	}
	if !m.image.running {
		m.image.running = true
		m.image.status = ImageStatus{State: "downloading"}
		if m.Dev {
			m.image.status.State = "loading"
		}
		go m.fetchImage()
	}
	return ""
}

// fetchImage gets the image: in development mode the local one, otherwise
// the signed one of this release, downloaded once to the data volume.
func (m *Manager) fetchImage() {
	ctx, cancel := context.WithTimeout(m.ctx(), 2*time.Hour)
	defer cancel()
	id, err := m.obtainImage(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.image.running, m.image.part, m.image.checked = false, "", time.Now()
	if err != nil {
		slog.Warn("remote view image not ready", "err", err)
		m.image.status = ImageStatus{State: "error", Error: err.Error()}
		return
	}
	m.image.id = id
	m.image.status = ImageStatus{State: "ready"}
	if !m.Dev {
		go m.pruneImages(id)
	}
}

// pruneImages removes the images of earlier versions (label
// titanos.viewer.image=1) once this one is loaded and verified. Docker
// refuses to remove an image a container still uses; that one stays.
func (m *Manager) pruneImages(keep string) {
	ctx, cancel := context.WithTimeout(m.ctx(), 5*time.Minute)
	defer cancel()
	images, err := m.Engine.ViewerImages(ctx)
	if err != nil {
		return
	}
	for _, img := range images {
		if img.ID == keep {
			continue
		}
		if err := m.Engine.ViewerRemoveImage(ctx, img.ID); err != nil {
			slog.Info("older remote view image kept", "image", img.ID, "tags", img.Tags, "err", err)
		} else {
			slog.Info("older remote view image removed", "image", img.ID, "tags", img.Tags)
		}
	}
}

func (m *Manager) obtainImage(ctx context.Context) (string, error) {
	if m.Dev {
		id, err := m.Engine.ViewerImageID(ctx, DevImage)
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf("viewer: development mode needs the local image %s (scripts/build-viewer-image.sh)", DevImage)
		}
		return id, nil
	}
	if m.Release == nil {
		return "", ErrNoImage
	}
	data, err := m.Release.ReleaseManifest(ctx, m.Version)
	if err != nil {
		return "", fmt.Errorf("viewer: release manifest: %w", err)
	}
	img, err := ParseReleaseImage(data, m.Version)
	if err != nil {
		return "", err
	}
	// Loaded before (the archive is removed after loading).
	if id, err := m.Engine.ViewerImageID(ctx, img.ImageID); err == nil && id == img.ImageID {
		return id, nil
	}
	m.mu.Lock()
	m.image.status.Size = img.Size
	m.mu.Unlock()
	dir, err := m.privateDir("image")
	if err != nil {
		return "", err
	}
	if err := checkSpace(dir, img.Size*3); err != nil {
		return "", err
	}
	archive := filepath.Join(dir, img.Asset)
	part := archive + ".part"
	m.mu.Lock()
	m.image.part = part
	m.mu.Unlock()
	// Archives of other versions are no longer needed.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if name := e.Name(); strings.HasPrefix(name, "titanos-viewer-") && name != img.Asset && name != img.Asset+".part" {
				os.Remove(filepath.Join(dir, name))
			}
		}
	}
	if err := m.Release.DownloadReleaseAsset(ctx, m.Version, img.Asset, part, img.SHA256, img.Size); err != nil {
		return "", fmt.Errorf("viewer: download: %w", err)
	}
	if err := os.Rename(part, archive); err != nil {
		return "", err
	}
	m.mu.Lock()
	m.image.part = ""
	m.image.status.State, m.image.status.Bytes = "loading", img.Size
	m.mu.Unlock()
	file, err := os.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := m.Engine.ViewerLoadImage(ctx, file); err != nil {
		return "", fmt.Errorf("viewer: docker load: %w", err)
	}
	id, err := m.Engine.ViewerImageID(ctx, img.ImageID)
	if err != nil || id != img.ImageID {
		return "", fmt.Errorf("viewer: the loaded image is not %s", img.ImageID)
	}
	// Docker keeps the image; the archive would only take space twice.
	os.Remove(archive)
	return id, nil
}

// checkSpace is spaceFor; tests replace it (their folders are in tmpfs).
var checkSpace = spaceFor

// spaceFor refuses a folder in RAM or without room for need bytes.
func spaceFor(dir string, need int64) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return err
	}
	// tmpfs and overlayfs: the RAM of the NAS, never for data.
	if st.Type == 0x01021994 || st.Type == 0x794c7630 {
		return errors.New("viewer: the folder is not on a data volume")
	}
	if int64(st.Bavail)*int64(st.Bsize) < need {
		return fmt.Errorf("viewer: not enough space on the volume (%d MiB needed)", need>>20)
	}
	return nil
}

var errUnsafeDir = errors.New("viewer: unsafe folder permissions")

// privateDir makes <volume>/.titanos/viewer/<parts…> as private folders of
// titand (owner root, 0700, no links) and returns its path.
func (m *Manager) privateDir(parts ...string) (string, error) {
	volume, err := m.dataVolume()
	if err != nil {
		return "", err
	}
	mount := filepath.Join(m.MountRoot, volume)
	root, err := os.OpenRoot(mount)
	if err != nil {
		return "", err
	}
	defer root.Close()
	path := ""
	for _, part := range append([]string{".titanos", "viewer"}, parts...) {
		path = filepath.Join(path, part)
		if err := ownedDir(root, path, os.Geteuid(), 0o700); err != nil {
			return "", err
		}
	}
	return filepath.Join(mount, path), nil
}

// ownedDir makes one folder below root (no links followed) and checks that
// it belongs to uid and that nobody else may write to it. A folder that
// exists keeps its mode if it is safe (.titanos is shared by features).
func ownedDir(root *os.Root, path string, uid int, mode fs.FileMode) error {
	if err := root.Mkdir(path, mode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	st, err := root.Lstat(path)
	if err != nil {
		return err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || st.Mode()&fs.ModeSymlink != 0 || st.Mode().Perm()&0o022 != 0 || !ok || owner.Uid != uint32(uid) {
		return fmt.Errorf("%w: %s", errUnsafeDir, path)
	}
	return nil
}
