package backup

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// copyUSB makes plain, directly readable copies. It never removes a destination
// absent from the source, follows no source symlinks, and replaces a file only
// after its complete replacement has been synced on the destination filesystem.
func (m *Manager) copyUSB(ctx context.Context, job Job) error {
	if m.WithUSB == nil {
		return fmt.Errorf("USB copy target is unavailable")
	}
	sources := map[string]Source{}
	for _, source := range m.sources() {
		sources[source.Name] = source
	}
	for _, name := range job.Sources {
		if _, ok := sources[name]; !ok {
			return fmt.Errorf("source share %s is not mounted", name)
		}
	}
	var total uint64
	err := m.WithUSB(ctx, job.Destination.DeviceID, func(target *os.Root) error {
		for _, name := range job.Sources {
			source := sources[name]
			root, err := os.OpenRoot(source.Path)
			if err != nil {
				return err
			}
			err = func() error {
				defer root.Close()
				folders := job.Folders[name]
				if len(folders) == 0 {
					folders = []string{"."}
				}
				for _, folder := range folders {
					if info, err := root.Lstat(folder); err != nil || !info.IsDir() {
						return fmt.Errorf("source folder %s/%s is unavailable", name, folder)
					}
					err := fs.WalkDir(root.FS(), folder, func(rel string, entry fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if err := ctx.Err(); err != nil {
							return err
						}
						excluded := entry.Name() == ".recycle" || strings.HasSuffix(entry.Name(), ".titanos-part")
						for _, skip := range job.Skip {
							full := name + "/" + rel
							excluded = excluded || full == skip || strings.HasPrefix(full, skip+"/")
						}
						for _, pattern := range job.Patterns {
							match, _ := path.Match(pattern, entry.Name())
							excluded = excluded || match
						}
						if excluded {
							if entry.IsDir() {
								return fs.SkipDir
							}
							return nil
						}
						info, err := entry.Info()
						if err != nil {
							return err
						}
						dst := path.Join(job.Destination.Folder, name, rel)
						if entry.IsDir() {
							return copyDirectories(target, dst)
						}
						if !info.Mode().IsRegular() {
							return fmt.Errorf("unsupported source file %s/%s (links and special files are not copied)", name, rel)
						}
						if err := copyDirectories(target, path.Dir(dst)); err != nil {
							return err
						}
						n, err := copyFile(ctx, root, rel, target, dst)
						if err != nil {
							return err
						}
						total += uint64(n)
						m.update(job, func(s *State) { s.BytesDone = total })
						return nil
					})
					if err != nil {
						return err
					}
				}
				return nil
			}()
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		m.update(job, func(s *State) { s.Bytes, s.BytesDone, s.Percent = total, total, 100 })
	}
	return err
}

// Refuse destination links even when they point somewhere within the same disk.
func copyDirectories(root *os.Root, dir string) error {
	parts := strings.Split(filepath.ToSlash(dir), "/")
	current := ""
	for _, part := range parts {
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if err = root.Mkdir(current, 0700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("copy destination %s is not a directory", current)
		}
	}
	return nil
}

func copyFile(ctx context.Context, source *os.Root, from string, target *os.Root, to string) (int64, error) {
	info, err := target.Lstat(to)
	if err == nil && !info.Mode().IsRegular() {
		return 0, fmt.Errorf("copy destination %s is not a regular file", to)
	}
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	// Root confinement also protects against source directory renames and links.
	input, err := source.OpenFile(from, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		return 0, err
	}
	if !before.Mode().IsRegular() {
		return 0, fmt.Errorf("source changed type")
	}
	temp := path.Join(path.Dir(to), ".titanos-copy-"+rand.Text())
	output, err := target.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	defer target.Remove(temp)
	defer output.Close()
	buf := make([]byte, 256<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := input.Read(buf)
		if n > 0 {
			written, err := output.Write(buf[:n])
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return total, readErr
		}
	}
	after, err := input.Stat()
	if err != nil {
		return total, err
	}
	if total != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return total, fmt.Errorf("source changed during copying: %s", from)
	}
	if err := output.Sync(); err != nil {
		return total, err
	}
	if err := output.Close(); err != nil {
		return total, err
	}
	if err := target.Rename(temp, to); err != nil {
		return total, err
	}
	return total, nil
}
