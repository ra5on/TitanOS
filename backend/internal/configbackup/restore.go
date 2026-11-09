package configbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Checking and restoring a backup, also one found on another disk
// (split from configbackup.go, PLAN 75-12).

// check reads a backup through and returns what it is. Nothing is written.
func check(r io.Reader) (Manifest, error) {
	unzip, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, ErrNotABackup
	}
	archive := tar.NewReader(unzip)
	manifest, err := manifestOf(archive)
	if err != nil {
		return Manifest{}, err
	}
	var total int64
	files, accounts := 0, 0
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, ErrNotABackup
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg || !validName(header.Name) {
			return Manifest{}, fmt.Errorf("%w: unexpected entry %q", ErrNotABackup, header.Name)
		}
		files++
		total += header.Size
		if files > maxFiles || total > maxContent {
			return Manifest{}, ErrTooLarge
		}
		if matched, _ := path.Match("users/*/account.json", header.Name); matched {
			accounts++
		}
		if _, err := io.Copy(io.Discard, archive); err != nil {
			return Manifest{}, ErrNotABackup
		}
	}
	// Without an account nobody could log in after the restore.
	if accounts == 0 {
		return Manifest{}, fmt.Errorf("%w: it holds no account", ErrNotABackup)
	}
	return manifest, nil
}

// validName accepts the names a backup made by TitanOS can hold.
func validName(name string) bool {
	if !fs.ValidPath(name) || name == "." || name == manifestName || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".tmp-") {
			return false
		}
	}
	return true
}

// restore writes the content of a backup to /config. The accounts come
// last: cut short, the stick still counts as new and can be restored again.
func (m *Manager) restore(r io.ReadSeeker, password string) (Manifest, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Manifest{}, err
	}
	head := make([]byte, len(encMagic))
	if n, _ := io.ReadFull(r, head); n == len(head) && encrypted(head) {
		_, _ = r.Seek(0, io.SeekStart) // the offset was just read from the same file; the copy checks what it reads
		data, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
		if err != nil {
			return Manifest{}, err
		}
		plain, err := unseal(data, password)
		if err != nil {
			return Manifest{}, err
		}
		r = bytes.NewReader(plain)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Manifest{}, err
	}
	manifest, err := check(r)
	if err != nil {
		return Manifest{}, err
	}
	for _, accounts := range []bool{false, true} {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return Manifest{}, err
		}
		unzip, err := gzip.NewReader(r)
		if err != nil {
			return Manifest{}, ErrNotABackup
		}
		archive := tar.NewReader(unzip)
		for {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return Manifest{}, ErrNotABackup
			}
			if header.Typeflag != tar.TypeReg || header.Name == manifestName || slices.Contains(skipped, header.Name) || leftOut(header.Name) || strings.HasPrefix(header.Name, "users/") != accounts {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(archive, maxContent))
			if err != nil {
				return Manifest{}, ErrNotABackup
			}
			if err := m.store.Write(header.Name, data); err != nil {
				return Manifest{}, err
			}
		}
	}
	slog.Info("configuration restored", "from", manifest.Time, "hostname", manifest.Hostname, "version", manifest.Version)
	return manifest, nil
}

// RestoreUpload restores from a backup that is being uploaded. The file is
// small and is kept in RAM while it is checked.
func (m *Manager) RestoreUpload(r io.Reader, password string) (Manifest, error) {
	if m.Disabled {
		return Manifest{}, ErrDisabled
	}
	m.work.Lock()
	defer m.work.Unlock()
	if err := os.MkdirAll(m.scanRoot, 0o700); err != nil {
		return Manifest{}, err
	}
	spool, err := os.CreateTemp(m.scanRoot, "upload-*.tar.gz")
	if err != nil {
		return Manifest{}, err
	}
	defer os.Remove(spool.Name())
	defer spool.Close()
	n, err := io.Copy(spool, io.LimitReader(r, maxArchive+1))
	if err != nil {
		return Manifest{}, err
	}
	if n > maxArchive {
		return Manifest{}, ErrTooLarge
	}
	return m.restore(spool, password)
}

// arrays lists the md arrays that are running, by device name.
func (m *Manager) arrays() []string {
	entries, _ := os.ReadDir(m.sysBlock)
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "md") && deviceName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// look mounts the file system of an array read-only, without replaying
// its journal, and hands its backup folder to fn. On a new stick TitanOS
// does not know which arrays are its volumes; nothing on them is changed.
func (m *Manager) look(ctx context.Context, device string, fn func(dir, label string) error) error {
	output, err := m.runner.Run(ctx, "", "blkid", "-o", "export", "/dev/"+device)
	if err != nil {
		return err
	}
	var kind, label string
	for line := range strings.SplitSeq(output, "\n") {
		if value, ok := strings.CutPrefix(line, "TYPE="); ok {
			kind = value
		}
		if value, ok := strings.CutPrefix(line, "LABEL="); ok {
			label = value
		}
	}
	options := "ro,nodev,nosuid,noexec"
	switch kind {
	case "ext4":
		options += ",noload"
	case "xfs":
		options += ",norecovery"
	case "btrfs":
		options += ",nologreplay,subvolid=5,degraded"
	default:
		return fmt.Errorf("configbackup: %s holds no file system of a volume", device)
	}
	dir := filepath.Join(m.scanRoot, device)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "mount", "-t", kind, "-o", options, "/dev/"+device, dir); err != nil {
		return err
	}
	defer func() {
		if _, err := m.runner.Run(context.WithoutCancel(ctx), "", "umount", dir); err != nil {
			slog.Warn("array not unmounted after looking for backups", "device", device, "err", err)
		}
	}()
	return fn(filepath.Join(dir, folder), label)
}

// Find looks for backups on the arrays of this machine that are not
// mounted as volumes: what a new stick needs to offer a restore.
func (m *Manager) Find(ctx context.Context) []Backup {
	if m.Disabled {
		return []Backup{}
	}
	m.work.Lock()
	defer m.work.Unlock()
	list := []Backup{}
	for _, device := range m.candidates(ctx) {
		err := m.look(ctx, device, func(dir, label string) error {
			for _, backup := range readFolder(dir) {
				backup.Device, backup.Label = device, label
				backup.Newer = newerRelease(backup.Version, m.version)
				list = append(list, backup)
			}
			return nil
		})
		if err != nil {
			slog.Info("no backups read from array", "device", device, "err", err)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list
}

// RestoreFound restores from a backup that Find reported.
func (m *Manager) RestoreFound(ctx context.Context, device, name, password string) (Manifest, error) {
	if m.Disabled {
		return Manifest{}, ErrDisabled
	}
	if !deviceName.MatchString(device) || !backupName.MatchString(name) || !slices.Contains(m.candidates(ctx), device) {
		return Manifest{}, ErrNotFound
	}
	m.work.Lock()
	defer m.work.Unlock()
	var manifest Manifest
	err := m.look(ctx, device, func(dir, label string) error {
		f, err := os.Open(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		defer f.Close()
		if info, err := f.Stat(); err != nil || info.Size() > maxArchive {
			return ErrTooLarge
		}
		manifest, err = m.restore(f, password)
		return err
	})
	return manifest, err
}

// Discover native Btrfs mirrors by filesystem UUID, scanning each mirror once.
func (m *Manager) candidates(ctx context.Context) []string {
	names := m.arrays()
	output, err := m.runner.Run(ctx, "", "blkid", "-t", "TYPE=btrfs", "-o", "export")
	if err != nil {
		return names
	}
	seen := map[string]bool{}
	for _, block := range strings.Split(output, "\n\n") {
		values := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			key, value, _ := strings.Cut(line, "=")
			values[key] = value
		}
		device := strings.TrimPrefix(values["DEVNAME"], "/dev/")
		if uuid := values["UUID"]; uuid != "" && !seen[uuid] && values["TYPE"] == "btrfs" && deviceName.MatchString(device) {
			names = append(names, device)
			seen[uuid] = true
		}
	}
	return names
}
