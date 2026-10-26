package plugins

// Local driver builds trust two independent inputs: TitanOS signs the exact-
// kernel SDK, NVIDIA signs its package index. The NAS authenticates its output
// with a private local key. That receipt never counts as an official signature.
import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/minisign"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const localKeyFile = "plugins/nvidia/build-key.json"
const localReceipt = "nvidia.local-build.json"

type DriverSource struct {
	Driver     string `json:"driver"`
	Toolkit    string `json:"toolkit"`
	Kernel     string `json:"kernel"`
	Version    string `json:"version"`
	Downloaded bool   `json:"downloaded"`
}
type buildSDK struct {
	Asset   string `json:"asset"`
	Kernel  string `json:"kernel"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}
type buildReceipt struct {
	ManifestSHA256 string `json:"manifestSHA256"`
	SDK            string `json:"sdk"`
	PackagesSHA256 string `json:"packagesSHA256"`
	MAC            string `json:"mac"`
}

func (m *Manager) loadBuildKey() error {
	var data struct {
		Key string `json:"privateKey"`
	}
	err := m.store.ReadJSON(localKeyFile, &data)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	key, err := hex.DecodeString(data.Key)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("%w: local build key", ErrInvalid)
	}
	m.mu.Lock()
	m.buildKey = key
	m.mu.Unlock()
	return nil
}
func (m *Manager) ensureBuildKey() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.buildKey) == 32 {
		return nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := m.store.WriteJSON(localKeyFile, struct {
		Key string `json:"privateKey"`
	}{hex.EncodeToString(key)}); err != nil {
		return err
	}
	m.buildKey = key
	return nil
}
func (m *Manager) receiptMAC(r buildReceipt) string {
	m.mu.Lock()
	key := slices.Clone(m.buildKey)
	m.mu.Unlock()
	if len(key) != 32 {
		return ""
	}
	h := hmac.New(sha256.New, key)
	fmt.Fprintf(h, "TitanOS local NVIDIA build v1\n%s\n%s\n%s\n", r.ManifestSHA256, r.SDK, r.PackagesSHA256)
	return hex.EncodeToString(h.Sum(nil))
}
func (m *Manager) localDriver(dir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "nvidia"+manifestSuffix))
	if err != nil {
		return Manifest{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, localReceipt))
	if err != nil {
		return Manifest{}, err
	}
	var r buildReceipt
	sum := sha256.Sum256(data)
	if json.Unmarshal(raw, &r) != nil || r.ManifestSHA256 != hex.EncodeToString(sum[:]) || !driverHashRE.MatchString(r.SDK) || !driverHashRE.MatchString(r.PackagesSHA256) || r.MAC == "" || !hmac.Equal([]byte(m.receiptMAC(r)), []byte(r.MAC)) {
		return Manifest{}, ErrInvalid
	}
	manifest, err := parseManifest(data, "nvidia"+manifestSuffix)
	if err != nil || !manifest.LocalBuild || manifest.Name != "nvidia" || manifest.Kernel != m.kernel || manifest.TitanOS != m.Release || !strings.HasSuffix(manifest.Version, "-local") {
		return Manifest{}, ErrInvalid
	}
	return manifest, nil
}
func (m *Manager) buildDir(volume string) string {
	return filepath.Join(m.baseDir(volume), "nvidia-build", m.Release)
}

// The extracted SDK is reused between builds. Its parent directories must
// therefore be private to titand, never pre-seeded writable share directories.
func (m *Manager) secureBuildDir(volume string) error {
	root, err := os.OpenRoot(filepath.Join(m.mountRoot, volume))
	if err != nil {
		return err
	}
	defer root.Close()
	path := ""
	for _, part := range []string{".titanos", "plugins", "nvidia-build", m.Release} {
		path = filepath.Join(path, part)
		if err := root.Mkdir(path, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		st, err := root.Lstat(path)
		if err != nil {
			return err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || st.Mode().Perm()&0022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
			return errors.New("unsafe NVIDIA build directory permissions")
		}
	}
	return nil
}
func (m *Manager) fetchBuildSDK(ctx context.Context) (buildSDK, error) {
	var sdk buildSDK
	base, err := m.releaseBase()
	if err != nil {
		return sdk, err
	}
	data, err := m.fetchMetadata(ctx, base+"manifest.json", 1<<20)
	if err != nil {
		return sdk, err
	}
	sig, err := m.fetchMetadata(ctx, base+"manifest.json.minisig", 4096)
	if err != nil {
		return sdk, err
	}
	if minisign.Verify(minisign.LoadKeys(m.keyDir), data, string(sig)) != nil {
		return sdk, ErrInvalid
	}
	var release struct {
		Version, Tag, Kernel string
		SDK                  buildSDK `json:"nvidiaBuildSDK"`
	}
	if json.Unmarshal(data, &release) != nil || release.Version != m.Release || release.Tag != "v"+m.Release || release.Kernel != m.kernel {
		return sdk, ErrInvalid
	}
	sdk = release.SDK
	if sdk.Asset == "" {
		return sdk, ErrRelease
	}
	if sdk.Asset != "nvidia-build-sdk.tar.zst" || sdk.Version != m.Release || sdk.Kernel != m.kernel || sdk.Size <= 0 || sdk.Size > 2<<30 || !driverHashRE.MatchString(sdk.SHA256) {
		return sdk, ErrInvalid
	}
	return sdk, nil
}
func (m *Manager) buildPhase(state string) { m.mu.Lock(); m.driverStatus.State = state; m.mu.Unlock() }
func (m *Manager) prepareSDK(ctx context.Context, volume string, sdk buildSDK) (string, error) {
	parent := m.buildDir(volume)
	if err := m.secureBuildDir(volume); err != nil {
		return "", err
	}
	// The SDK and temporary build output must never spill into the RAM overlay.
	var fs syscall.Statfs_t
	if err := syscall.Statfs(parent, &fs); err != nil {
		return "", err
	}
	if fs.Type == 0x01021994 || fs.Type == 0x794c7630 {
		return "", fmt.Errorf("%w: build directory is not a data filesystem", ErrUnknownVolume)
	}
	if fs.Bavail*uint64(fs.Bsize) < 5<<30 {
		return "", errors.New("at least 5 GiB free on the plugin volume is required for the driver build")
	}
	archive := filepath.Join(parent, sdk.Asset)
	sum, size, err := hashFile(archive, 2<<30)
	if err != nil || sum != sdk.SHA256 || size != sdk.Size {
		m.buildPhase("environment")
		base, _ := m.releaseBase()
		// A previous interrupted transfer is resumed; downloadImage verifies
		// the complete file and restarts on a hash mismatch.
		part := archive + ".part"
		if err := m.downloadImage(ctx, base+sdk.Asset, part, Manifest{SHA256: sdk.SHA256, Size: sdk.Size}); err != nil {
			return "", err
		}
		if err := os.Rename(part, archive); err != nil {
			return "", err
		}
	}
	root := filepath.Join(parent, "root")
	marker := filepath.Join(parent, "sdk.sha256")
	ready, _ := os.ReadFile(marker)
	if string(ready) != sdk.SHA256 {
		temporary := filepath.Join(parent, "extracting")
		os.RemoveAll(temporary)
		if err := os.Mkdir(temporary, 0700); err != nil {
			return "", err
		}
		if _, err := m.runner.Run(ctx, "", "tar", "--zstd", "-xf", archive, "-C", temporary); err != nil {
			return "", err
		}
		// Keep authenticated packages and build results across an SDK refresh.
		if _, err := os.Stat(filepath.Join(root, "work")); err == nil {
			if err := os.Rename(filepath.Join(root, "work"), filepath.Join(temporary, "work")); err != nil {
				return "", err
			}
		}
		if err := os.RemoveAll(root); err != nil {
			return "", err
		}
		if err := os.Rename(temporary, root); err != nil {
			return "", err
		}
		if err := os.WriteFile(marker, []byte(sdk.SHA256), 0600); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "work"), 0700); err != nil {
		return "", err
	}
	return root, nil
}

// buildLogPath lies beside the sandbox root, in titand's private build
// directory. The sandbox (uid 0, writable /work) cannot see or replace it, so
// PID 1 never follows a sandbox-planted symlink when opening it for append.
func buildLogPath(root string) string {
	return filepath.Join(filepath.Dir(root), "build.log")
}

// resetBuildLog recreates the log as a fresh, private regular file.
func resetBuildLog(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	return file.Close()
}

// buildLogTail returns at most the last 4 KiB of the log, never through a link.
func buildLogTail(path string) string {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	offset := max(int64(0), st.Size()-4096)
	buf := make([]byte, st.Size()-offset)
	n, _ := file.ReadAt(buf, offset)
	return string(buf[:n])
}

// sandboxFile opens a file the build sandbox produced below root/work. The
// sandbox runs as uid 0 with a writable /work, so nothing there is trusted:
// no symlinks in any component, no FIFOs/devices, no hard links, bounded size.
// The sandbox has exited (systemd-run --wait kills its cgroup) before titand
// reads, and os.Root additionally confines every lookup to /work.
func sandboxFile(root, name string, limit int64) (*os.File, int64, error) {
	invalid := fmt.Errorf("%w: unsafe build output %s", ErrInvalid, name)
	work := filepath.Join(root, "work")
	if st, err := os.Lstat(work); err != nil || !st.IsDir() {
		return nil, 0, invalid
	}
	dir, err := os.OpenRoot(work)
	if err != nil {
		return nil, 0, err
	}
	defer dir.Close()
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := range parts[:len(parts)-1] {
		if st, err := dir.Lstat(filepath.Join(parts[:i+1]...)); err != nil || !st.IsDir() {
			return nil, 0, invalid
		}
	}
	link, err := dir.Lstat(name)
	if err != nil {
		return nil, 0, err
	}
	if !safeSandboxEntry(link) || link.Size() > limit {
		return nil, 0, invalid
	}
	file, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, 0, err
	}
	st, err := file.Stat()
	if err != nil || !os.SameFile(link, st) || !safeSandboxEntry(st) || st.Size() > limit {
		file.Close()
		return nil, 0, invalid
	}
	return file, st.Size(), nil
}

// safeSandboxEntry accepts directories and singly linked regular files.
func safeSandboxEntry(st fs.FileInfo) bool {
	if st.IsDir() {
		return true
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	return st.Mode().IsRegular() && ok && sys.Nlink == 1
}

func readSandboxFile(root, name string, limit int64) ([]byte, error) {
	file, size, err := sandboxFile(root, name, limit)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data := make([]byte, size)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, err
	}
	return data, nil
}

func hashSandboxFile(root, name string, limit int64) (string, int64, error) {
	file, _, err := sandboxFile(root, name, limit)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, limit+1))
	return hex.EncodeToString(hash.Sum(nil)), n, err
}

// checkSandboxTree rejects anything but plain directories and singly linked
// regular files before titand adopts a sandbox-written tree into its cache,
// whose readers follow paths normally.
func checkSandboxTree(path string) error {
	if st, err := os.Lstat(path); err != nil || !st.IsDir() {
		return fmt.Errorf("%w: unsafe build output", ErrInvalid)
	}
	return filepath.WalkDir(path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := entry.Info()
		if err != nil {
			return err
		}
		if !safeSandboxEntry(st) {
			return fmt.Errorf("%w: unsafe build output %s", ErrInvalid, entry.Name())
		}
		return nil
	})
}

// writeFresh replaces path with a new private file without following links.
func writeFresh(path string, data []byte) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func buildCommand(root, action string) []string {
	args := []string{"--wait", "--collect", "--quiet", "--unit=titanos-nvidia-build", "--property=RootDirectory=" + root, "--property=WorkingDirectory=/usr/lib/titanos/nvidia-build", "--property=PrivateDevices=yes", "--property=PrivateTmp=yes", "--property=ProtectSystem=strict", "--property=ReadWritePaths=+/work", "--property=NoNewPrivileges=yes", "--property=CapabilityBoundingSet=", "--property=ProtectKernelTunables=yes", "--property=ProtectControlGroups=yes", "--property=RestrictSUIDSGID=yes", "--property=MemoryMax=2G", "--property=CPUQuota=200%", "--property=TasksMax=256", "--property=RuntimeMaxSec=7200", "--property=Nice=10", "--property=StandardOutput=append:" + buildLogPath(root), "--property=StandardError=inherit"}
	if action == "build" {
		args = append(args, "--property=PrivateNetwork=yes")
	} else {
		args = append(args, "--property=BindReadOnlyPaths=/etc/resolv.conf")
	}
	return append(args, "python3", "/usr/lib/titanos/nvidia-build/scripts/nvidia-nas-build.py", action)
}
func (m *Manager) checkLocalDriver(ctx context.Context, mounted []string, prepare bool) (err error) {
	if !m.driverActions.TryLock() {
		return ErrBusy
	}
	defer m.driverActions.Unlock()
	m.actions.Lock()
	settings := m.current()
	manifest := m.installed(settings.Volume)["nvidia"]
	eligible := !m.Disabled && settings.PendingDriver == nil && settings.Volume != "" && slices.Contains(mounted, settings.Volume) && manifest.Name == "nvidia"
	if eligible {
		_, localErr := m.localDriver(m.dir(settings.Volume))
		eligible = m.official(filepath.Join(m.dir(settings.Volume), "nvidia"+manifestSuffix)) || localErr == nil
	}
	if eligible {
		m.mu.Lock()
		m.driverVolume = settings.Volume
		m.driverAttempt = time.Now()
		m.driverStatus.State = "checking"
		m.driverStatus.Error = ""
		m.mu.Unlock()
	}
	m.actions.Unlock()
	if !eligible {
		return ErrNotFound
	}
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.driverVolume = ""
		m.driverStatus.Checked = time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			m.driverStatus.State = "error"
			m.driverStatus.Error = err.Error()
			if errors.Is(err, ErrRelease) {
				m.driverStatus.State = "unavailable"
				m.driverStatus.Error = ""
			}
		}
	}()
	// The SDK download is bounded by a stall timeout and resumes, not by the
	// build deadline: a slow line must not make a 2 GiB SDK unreachable.
	sdk, err := m.fetchBuildSDK(ctx)
	if err != nil {
		return err
	}
	root, err := m.prepareSDK(ctx, settings.Volume, sdk)
	if err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	if !prepare {
		m.buildPhase("downloading")
		if _, err = m.runBuildJob(operation, root, "check"); err != nil {
			return err
		}
	}
	data, err := readSandboxFile(root, "source-status.json", 1<<20)
	if err != nil {
		return err
	}
	var source DriverSource
	if json.Unmarshal(data, &source) != nil || source.Kernel != m.kernel || source.Version != m.Release || !source.Downloaded {
		return ErrInvalid
	}
	m.mu.Lock()
	m.driverStatus.Source = &source
	m.driverStatus.State = "sourceReady"
	m.mu.Unlock()
	if !prepare {
		return nil
	}
	if err = m.ensureBuildKey(); err != nil {
		return err
	}
	m.buildPhase("building")
	if _, err = m.runBuildJob(operation, root, "build"); err != nil {
		return err
	}
	result := filepath.Join(root, "work/result")
	if err = checkSandboxTree(result); err != nil {
		return err
	}
	image, size, err := hashSandboxFile(root, "result/nvidia.raw", maxImageBytes)
	if err != nil {
		return err
	}
	raw, err := readSandboxFile(root, "result/nvidia"+manifestSuffix, 1<<20)
	if err != nil {
		return err
	}
	built, err := parseManifest(raw, "nvidia"+manifestSuffix)
	if err != nil || !built.LocalBuild || built.SHA256 != image || built.Size != size || built.Kernel != m.kernel || built.TitanOS != m.Release || built.Version != source.Driver+"-titanos-"+m.Release+"-local" {
		return ErrInvalid
	}
	// Only titand can attest output after the sandbox has exited successfully.
	packageSum, _, err := hashSandboxFile(root, "packages.lock.json", 1<<20)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	receipt := buildReceipt{ManifestSHA256: hex.EncodeToString(sum[:]), SDK: sdk.SHA256, PackagesSHA256: packageSum}
	receipt.MAC = m.receiptMAC(receipt)
	encoded, _ := json.Marshal(receipt)
	if err = writeFresh(filepath.Join(result, localReceipt), encoded); err != nil {
		return err
	}
	if err = writeFresh(filepath.Join(result, ".ready"), []byte(m.Release)); err != nil {
		return err
	}
	if err = os.MkdirAll(m.driverCache(settings.Volume), 0700); err != nil {
		return err
	}
	destination := filepath.Join(m.driverCache(settings.Volume), image)
	if _, e := os.Stat(destination); e == nil {
		if _, e = m.cachedDriver(destination, true); e != nil {
			return e
		}
	} else if err = os.Rename(result, destination); err != nil {
		return err
	}
	if err = syncTree(destination); err != nil {
		return err
	}
	m.mu.Lock()
	m.driverStatus.State = "ready"
	m.driverStatus.Version = built.Version
	m.driverStatus.SHA256 = image
	if !slices.ContainsFunc(m.driverStatus.Versions, func(v DriverVersion) bool { return v.SHA256 == image }) {
		m.driverStatus.Versions = append(m.driverStatus.Versions, DriverVersion{Driver: source.Driver, Toolkit: source.Toolkit, Version: built.Version, SHA256: image, Size: size, Downloaded: true, Local: true})
	}
	m.mu.Unlock()
	return nil
}
func (m *Manager) RequestDriverBuild(mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled || !m.LocalBuilds {
		return ErrDisabled
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	s := m.current()
	if s.PendingDriver != nil {
		return ErrDriverPending
	}
	if !slices.Contains(mounted, s.Volume) {
		return ErrUnknownVolume
	}
	if m.installed(s.Volume)["nvidia"].Name == "" {
		return ErrNotFound
	}
	select {
	case m.driverRequests <- "prepare":
	default:
		return ErrBusy
	}
	return nil
}

func (m *Manager) runBuildJob(ctx context.Context, root, action string) (string, error) {
	logPath := buildLogPath(root)
	if err := resetBuildLog(logPath); err != nil {
		return "", err
	}
	output, err := m.runner.Run(ctx, "", "systemd-run", buildCommand(root, action)...)
	if ctx.Err() != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, stopErr := m.runner.Run(cleanup, "", "systemctl", "stop", "titanos-nvidia-build.service")
		return output, errors.Join(err, stopErr)
	}
	if err != nil {
		if tail := buildLogTail(logPath); tail != "" {
			err = fmt.Errorf("%w\n%s", err, tail)
		}
	}
	return output, err
}

// Busy reports whether a driver is being downloaded or built right now.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.driverStatus.State {
	case "checking", "downloading", "environment", "building":
		return true
	}
	return false
}
