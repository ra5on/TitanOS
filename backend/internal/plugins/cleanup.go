package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type OldRelease struct {
	Version     string `json:"version"`
	Bytes       int64  `json:"bytes"`
	Protected   bool   `json:"protected"`
	Reason      string `json:"reason,omitempty"`
	Fingerprint string `json:"fingerprint"`
}
type CleanupSelection struct {
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

func (m *Manager) cleanupRoot(mounted []string) (*os.Root, error) {
	volume := m.current().Volume
	if volume == "" || !slices.Contains(mounted, volume) {
		return nil, ErrUnknownVolume
	}
	path := filepath.Join(m.mountRoot, volume)
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnknownVolume
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.Root, error) { root.Close(); return nil, err }
	// Hold a descriptor to the actual mounted volume; never continue on an
	// empty mountpoint in the RAM overlay after a volume disappears.
	if m.mountRoot == "/mnt" {
		f, e := root.Open(".")
		if e != nil {
			return fail(e)
		}
		var mountedStat, parentStat unix.Statx_t
		e = unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &mountedStat)
		f.Close()
		if e != nil {
			return fail(e)
		}
		if e = unix.Statx(unix.AT_FDCWD, m.mountRoot, 0, unix.STATX_MNT_ID, &parentStat); e != nil || mountedStat.Mnt_id == parentStat.Mnt_id {
			return fail(ErrUnknownVolume)
		}
	}
	part := ""
	for _, name := range []string{".titanos", "plugins", "releases"} {
		part = filepath.Join(part, name)
		st, e := root.Lstat(part)
		if e != nil {
			return fail(e)
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || st.Mode().Perm()&0022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
			return fail(fmt.Errorf("%w: unsafe plugin release directory", ErrInvalid))
		}
	}
	child, err := root.OpenRoot(part)
	root.Close()
	return child, err
}

func releaseSize(root *os.Root, version string) (int64, string, error) {
	h := sha256.New()
	var total int64
	entries := 0
	err := fs.WalkDir(root.FS(), version, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > 100000 {
			return ErrInvalid
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symbolic link in release", ErrInvalid)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return ErrInvalid
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return ErrInvalid
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00%d\x00%d\n", path, st.Dev, st.Ino, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			if info.Size() < 0 || total > 1<<60-info.Size() {
				return ErrInvalid
			}
			total += info.Size()
		}
		return nil
	})
	return total, hex.EncodeToString(h.Sum(nil)), err
}

func (m *Manager) oldReleases(mounted []string) ([]OldRelease, error) {
	root, err := m.cleanupRoot(mounted)
	if errors.Is(err, fs.ErrNotExist) {
		return []OldRelease{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := file.ReadDir(-1)
	file.Close()
	if err != nil {
		return nil, err
	}
	protected := map[string]string{m.Release: "current"}
	if m.CleanupProtected != nil {
		for _, version := range m.CleanupProtected() {
			if version != "" {
				protected[strings.TrimPrefix(version, "v")] = "boot-or-update"
			}
		}
	}
	for _, set := range []map[string]Manifest{m.active, m.resident} {
		for _, p := range set {
			if p.TitanOS != "" {
				protected[strings.TrimPrefix(p.TitanOS, "v")] = "active"
			}
		}
	}
	// Resolve the actual sysext targets as well as manifest version hints.
	links, _ := os.ReadDir(m.extDir)
	for _, link := range links {
		target, e := filepath.EvalSymlinks(filepath.Join(m.extDir, link.Name()))
		if e != nil {
			continue
		}
		relative, e := filepath.Rel(filepath.Join(m.baseDir(m.current().Volume), "releases"), target)
		if e == nil && !strings.HasPrefix(relative, "..") {
			version := strings.Split(filepath.ToSlash(relative), "/")[0]
			protected[version] = "active"
		}
	}
	list := []OldRelease{}
	for _, entry := range entries {
		if !releaseVersion.MatchString(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		bytes, fingerprint, e := releaseSize(root, entry.Name())
		if e != nil {
			return nil, e
		}
		reason := protected[strings.TrimPrefix(entry.Name(), "v")]
		list = append(list, OldRelease{Version: entry.Name(), Bytes: bytes, Fingerprint: fingerprint, Protected: reason != "", Reason: reason})
	}
	slices.SortFunc(list, func(a, b OldRelease) int { return strings.Compare(a.Version, b.Version) })
	return list, nil
}

func (m *Manager) OldReleases(mounted []string) ([]OldRelease, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	return m.oldReleases(mounted)
}
func (m *Manager) Cleanup(mounted []string, selection []CleanupSelection) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if len(selection) == 0 || len(selection) > 100 {
		return ErrInvalid
	}
	list, err := m.oldReleases(mounted)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range selection {
		if seen[s.Version] {
			return ErrInvalid
		}
		seen[s.Version] = true
		i := slices.IndexFunc(list, func(r OldRelease) bool { return r.Version == s.Version })
		if i < 0 || list[i].Protected || list[i].Fingerprint != s.Fingerprint {
			return fmt.Errorf("%w: release selection changed or is protected; preview again", ErrInvalid)
		}
	}
	root, err := m.cleanupRoot(mounted)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, s := range selection {
		_, fingerprint, err := releaseSize(root, s.Version)
		if err != nil || fingerprint != s.Fingerprint {
			return fmt.Errorf("%w: release changed", ErrInvalid)
		}
		if err := root.RemoveAll(s.Version); err != nil {
			return err
		}
	}
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
