package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/usb"
)

// copyUSB makes plain, directly readable copies. It never removes a destination
// absent from the source, follows no source symlinks, and replaces a file only
// after its complete replacement has been synced on the destination filesystem.
func (m *Manager) copyUSB(ctx context.Context, job Job) error {
	if m.WithUSB == nil {
		return fmt.Errorf("USB copy target is unavailable")
	}
	return m.WithUSB(ctx, job.Destination.DeviceID, func(target *os.Root) error { return m.copyTo(ctx, job, target) })
}

// copyItem is one file a run wants to copy.
type copyItem struct {
	share, rel, dst string
	// prev is the same file in the dated run before, if any.
	prev string
	size int64
}

// copyProblems collects what a run left out, by reason, for the warning.
type copyProblems struct {
	count    map[string]int64
	examples map[string][]string
	order    []string
}

func (p *copyProblems) add(reason, what string) {
	if p.count == nil {
		p.count, p.examples = map[string]int64{}, map[string][]string{}
	}
	if p.count[reason] == 0 {
		p.order = append(p.order, reason)
	}
	p.count[reason]++
	if len(p.examples[reason]) < 5 {
		p.examples[reason] = append(p.examples[reason], what)
	}
}

func (p *copyProblems) total() int64 {
	var n int64
	for _, c := range p.count {
		n += c
	}
	return n
}

// String is the warning of the run: per reason the count and examples.
func (p *copyProblems) String() string {
	var parts []string
	for _, reason := range p.order {
		line := fmt.Sprintf("%d %s: %s", p.count[reason], reason, strings.Join(p.examples[reason], ", "))
		if p.count[reason] > int64(len(p.examples[reason])) {
			line += ", …"
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "; ")
}

// fatalTarget reports errors that mean the destination itself fails: full,
// read-only, gone. Those end the run; anything else skips one file.
func fatalTarget(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.EIO) ||
		errors.Is(err, syscall.ENODEV) || errors.Is(err, syscall.ENOTCONN) || errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// problemOf names why one file could not be copied, in words the page shows.
func problemOf(err error) string {
	switch {
	case errors.Is(err, syscall.EFBIG):
		return "files too large for the drive's file system (FAT32: 4 GB)"
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.ENAMETOOLONG), errors.Is(err, syscall.EILSEQ):
		return "names the drive's file system does not allow"
	case errors.Is(err, errSourceChanged):
		return "files that changed while being copied"
	case errors.Is(err, fs.ErrPermission):
		return "files that could not be read"
	}
	return "files with other errors"
}

var errSourceChanged = errors.New("source changed")

// caseInsensitive finds out whether the destination folds the case of
// names (FAT, exFAT, NTFS through some drivers).
func caseInsensitive(target *os.Root, dir string) bool {
	probe := path.Join(dir, ".titanos-Case-"+rand.Text())
	f, err := target.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return false
	}
	f.Close()
	defer target.Remove(probe)
	_, err = target.Lstat(strings.ToLower(probe))
	return err == nil
}

func (m *Manager) copyTo(ctx context.Context, job Job, target *os.Root) error {
	sources, names, folderOf, missing := m.copyEntries(job)
	var problems copyProblems
	for _, name := range missing {
		problems.add("shares not available", name)
	}
	if len(sources) == 0 {
		return fmt.Errorf("none of the shares of this job is available")
	}
	if err := copyDirectories(target, job.Destination.Folder); err != nil {
		return err
	}
	folding := caseInsensitive(target, job.Destination.Folder)
	// With dated folders each run gets a folder of its own; unchanged
	// files are hard links to the run before, where the drive allows it.
	dated := job.Copy != nil && job.Copy.Dated
	base, prev := job.Destination.Folder, ""
	links := false
	var runs []datedRun
	if dated {
		runs = datedRuns(target, job.Destination.Folder, m.local(m.now()).Location())
		if len(runs) > 0 {
			prev = path.Join(job.Destination.Folder, runs[len(runs)-1].name)
		}
		base = path.Join(job.Destination.Folder, newRunName(m.local(m.now()), runs))
		links = canLink(target, job.Destination.Folder)
	}
	seen := map[string]string{}

	// First the list: what there is, how much, and what is new. It gives the
	// page a percentage and the run a check of the free space.
	var items []copyItem
	var planned, needed uint64
	for _, name := range names {
		source, ok := sources[name]
		if !ok {
			continue
		}
		root, err := os.OpenRoot(source.Path)
		if err != nil {
			problems.add("shares not available", name)
			continue
		}
		folders := folderOf[name]
		if len(folders) == 0 {
			folders = []string{"."}
		}
		for _, folder := range folders {
			if info, err := root.Lstat(folder); err != nil || !info.IsDir() {
				problems.add("folders not available", name+"/"+folder)
				continue
			}
			err := fs.WalkDir(root.FS(), folder, func(rel string, entry fs.DirEntry, err error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err != nil {
					problems.add("files that could not be read", name+"/"+rel)
					if entry != nil && entry.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				excluded := entry.Name() == ".recycle" || entry.Name() == ".titanos" || entry.Name() == ".snapshots" || strings.HasSuffix(entry.Name(), ".titanos-part")
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
				dst := path.Join(base, name, rel)
				if folding {
					// Foto.jpg and foto.jpg are one file there: keep the first.
					key := strings.ToLower(dst)
					if other, ok := seen[key]; ok && other != dst {
						problems.add("names that differ only in upper/lower case (the drive treats them as one)", name+"/"+rel)
						if entry.IsDir() {
							return fs.SkipDir
						}
						return nil
					}
					seen[key] = dst
				}
				if entry.IsDir() {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					problems.add("files that could not be read", name+"/"+rel)
					return nil
				}
				if !info.Mode().IsRegular() {
					problems.add("links or special files", name+"/"+rel)
					return nil
				}
				item := copyItem{share: name, rel: rel, dst: dst, size: info.Size()}
				compare := dst
				if dated {
					compare = ""
					if prev != "" {
						item.prev = path.Join(prev, name, rel)
						if links {
							compare = item.prev
						}
					}
				}
				items = append(items, item)
				planned += uint64(info.Size())
				if old, err := target.Lstat(compare); compare == "" || err != nil || old.Size() != info.Size() || !sameTime(old.ModTime(), info.ModTime()) {
					needed += uint64(info.Size())
				}
				return nil
			})
			if err != nil {
				root.Close()
				return err
			}
		}
		root.Close()
	}
	// Not enough room: with the job's leave, old dated runs go first,
	// oldest first, never the newest.
	var freed []string
	for {
		free, ok := freeSpace(target)
		if !ok || needed <= free {
			break
		}
		if !dated || !job.Keep.DropsOldest() || len(runs) < 2 {
			return fmt.Errorf("%w: %d MB needed, %d MB free on the drive", errNoSpace, needed>>20, free>>20)
		}
		if err := target.RemoveAll(path.Join(job.Destination.Folder, runs[0].name)); err != nil {
			return err
		}
		freed = append(freed, runs[0].name)
		runs = runs[1:]
	}
	m.update(job, func(s *State) { s.BytesTotal = planned })

	// Unlike restic, a copy follows the limit as the clock passes a window
	// boundary; the page shows the limit that holds right now.
	var pace *limiter
	if job.Bandwidth != nil {
		pace = newLimiter(func() int64 { return int64(job.Bandwidth.at(m.local(m.now()))) << 20 }, func(rate int64) {
			m.update(job, func(s *State) { s.BandwidthLimit = int(rate >> 20) })
		})
	}
	var total uint64
	roots := map[string]*os.Root{}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()
	progress := func(done uint64) {
		m.mu.Lock()
		if state := m.states[job.ID]; state != nil {
			state.BytesDone = done
			if planned > 0 {
				state.Percent = min(100, float64(done)*100/float64(planned))
			}
		}
		m.mu.Unlock()
	}
	for _, item := range items {
		root := roots[item.share]
		if root == nil {
			var err error
			if root, err = os.OpenRoot(sources[item.share].Path); err != nil {
				problems.add("shares not available", item.share)
				continue
			}
			roots[item.share] = root
		}
		if err := copyDirectories(target, path.Dir(item.dst)); err != nil {
			if fatalTarget(err) {
				return err
			}
			problems.add(problemOf(err), item.share+"/"+item.rel)
			continue
		}
		offset := total
		var err error
		if item.prev != "" && links {
			_, err = copyOrLink(ctx, root, item.rel, target, item.dst, item.prev, pace, func(done int64) { progress(offset + uint64(done)) })
		} else {
			_, err = copyFilePaced(ctx, root, item.rel, target, item.dst, pace, func(done int64) { progress(offset + uint64(done)) })
		}
		if err != nil {
			if fatalTarget(err) {
				return err
			}
			problems.add(problemOf(err), item.share+"/"+item.rel)
		}
		total += uint64(item.size)
		progress(total)
	}
	if job.Copy != nil && job.Copy.Mirror && !dated {
		if err := m.mirror(ctx, job, target, sources, names, folderOf, &problems); err != nil {
			return err
		}
	}
	if dated {
		if err := m.keepRuns(job, target, datedRuns(target, job.Destination.Folder, m.local(m.now()).Location())); err != nil {
			return err
		}
	}
	// Everything on the drive, also the renames, before it counts as done:
	// whoever pulls the drive then loses nothing.
	if err := syncTarget(target); err != nil {
		return err
	}
	m.update(job, func(s *State) {
		s.Bytes, s.BytesDone, s.BytesTotal, s.Percent, s.Warning, s.Skipped = total, total, planned, 100, problems.String(), problems.total()
	})
	if len(freed) > 0 && m.OnFreed != nil && job.Keep.DropsOldest() {
		m.OnFreed(job.ID, len(freed))
	}
	return nil
}

var errNoSpace = errors.New("not enough space on the drive")

// errorCode names the causes of a failed copy people can do something
// about; the page has a text for each (error.<code>).
func errorCode(err error) string {
	switch {
	case errors.Is(err, usb.ErrNotFound):
		return "usb_not_found"
	case errors.Is(err, usb.ErrReadOnly), errors.Is(err, syscall.EROFS):
		return "usb_read_only"
	case errors.Is(err, usb.ErrUnsupported):
		return "usb_unsupported"
	case errors.Is(err, usb.ErrMountFailed):
		return "drive_mount_failed"
	case errors.Is(err, usb.ErrNotMounted), errors.Is(err, syscall.EIO), errors.Is(err, syscall.ENODEV), errors.Is(err, syscall.ENOTCONN):
		return "backup_drive_lost"
	case errors.Is(err, errNoSpace), errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "backup_no_space"
	}
	return ""
}

// sameTime compares modification times as file systems keep them: FAT
// rounds to two seconds.
func sameTime(a, b time.Time) bool {
	d := a.Sub(b)
	return d > -2*time.Second && d < 2*time.Second
}

// freeSpace is what the destination has left; a variable for tests.
var freeSpace = statfsFree

func statfsFree(target *os.Root) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(target.Name(), &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// syncTarget writes everything of the destination file system to the drive.
func syncTarget(target *os.Root) error {
	dir, err := target.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.Syncfs(int(dir.Fd()))
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
	return copyFilePaced(ctx, source, from, target, to, nil, nil)
}

// copyFilePaced copies one file; pace (nil for none) limits what is written
// and what is read back from the destination to compare.
func copyFilePaced(ctx context.Context, source *os.Root, from string, target *os.Root, to string, pace *limiter, progress func(int64)) (int64, error) {
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
	// Compare content even when timestamps match, without rewriting unchanged files.
	if info != nil && info.Size() == before.Size() {
		same, err := sameCopy(ctx, input, target, to, pace)
		if err != nil {
			return 0, err
		}
		after, err := input.Stat()
		if err != nil {
			return 0, err
		}
		if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return 0, fmt.Errorf("%w during comparison: %s", errSourceChanged, from)
		}
		if same {
			return before.Size(), nil
		}
		if _, err := input.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
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
			if err == nil {
				err = pace.wait(ctx, written)
			}
			if progress != nil {
				progress(total)
			}
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
		return total, fmt.Errorf("%w during copying: %s", errSourceChanged, from)
	}
	if err := target.Chtimes(temp, before.ModTime(), before.ModTime()); err != nil {
		return total, err
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

func sameCopy(ctx context.Context, input *os.File, target *os.Root, to string, pace *limiter) (bool, error) {
	other, err := target.OpenFile(to, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false, err
	}
	defer other.Close()
	info, err := other.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("destination changed type")
	}
	a, b := make([]byte, 256<<10), make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, e1 := io.ReadFull(input, a)
		k, e2 := io.ReadFull(other, b)
		if err := pace.wait(ctx, k); err != nil {
			return false, err
		}
		if e1 != nil && e1 != io.EOF && e1 != io.ErrUnexpectedEOF {
			return false, e1
		}
		if e2 != nil && e2 != io.EOF && e2 != io.ErrUnexpectedEOF {
			return false, e2
		}
		if n != k || !bytes.Equal(a[:n], b[:k]) {
			return false, nil
		}
		if e1 != nil || e2 != nil {
			return e1 == e2, nil
		}
	}
}

// copyEntries are the top folders of a readable copy: each share under its
// name, each volume (whole, or the folders chosen on it) as
// "Volume-<name>". It returns those that are there, in order, the folders
// chosen within them, and the names of those that are not.
func (m *Manager) copyEntries(job Job) (map[string]Source, []string, map[string][]string, []string) {
	shares := map[string]Source{}
	for _, source := range m.sources() {
		shares[source.Name] = source
	}
	there := map[string]Source{}
	folders := map[string][]string{}
	var names, missing []string
	for _, name := range job.Sources {
		if source, ok := shares[name]; ok {
			there[name] = source
			names = append(names, name)
			folders[name] = job.Folders[name]
		} else {
			missing = append(missing, name)
		}
	}
	mounted := m.mounted()
	volume := func(name string) (string, bool) {
		key := "Volume-" + name
		if !slices.Contains(mounted, name) {
			return key, false
		}
		if _, ok := there[key]; !ok {
			there[key] = Source{Name: key, Volume: name, Path: filepath.Join(m.mountRoot, name)}
			names = append(names, key)
		}
		return key, true
	}
	for _, name := range job.Volumes {
		if _, ok := volume(name); !ok {
			missing = append(missing, name)
		}
	}
	for _, place := range job.Places {
		name, folder, _ := strings.Cut(place, "/")
		key, ok := volume(name)
		if !ok {
			missing = append(missing, place)
			continue
		}
		folders[key] = append(folders[key], folder)
	}
	return there, names, folders, missing
}

// copyLocal makes a readable copy in a folder of another volume.
func (m *Manager) copyLocal(ctx context.Context, job Job) error {
	if !slices.Contains(m.mounted(), job.Destination.Volume) {
		return fmt.Errorf("the volume %s of the backup is not mounted", job.Destination.Volume)
	}
	root, err := os.OpenRoot(filepath.Join(m.mountRoot, job.Destination.Volume))
	if err != nil {
		return err
	}
	defer root.Close()
	return m.copyTo(ctx, job, root)
}

// holdTarget mounts the drive or SMB share of an encrypted job and keeps
// it, pinned and marked in use, until release is called: restic works on
// it as a folder for as long as the caller needs.
func (m *Manager) holdTarget(job Job) (dir string, release func(), err error) {
	type held struct {
		dir string
		err error
	}
	ready := make(chan held, 1)
	done := make(chan struct{})
	free := make(chan struct{})
	hold := func(root *os.Root) error {
		ready <- held{dir: root.Name()}
		<-free
		return nil
	}
	ctx := context.Background()
	go func() {
		defer close(done)
		var err error
		switch job.Destination.Type {
		case "usb":
			if m.WithUSB == nil {
				err = fmt.Errorf("USB target is unavailable")
				break
			}
			err = m.WithUSB(ctx, job.Destination.DeviceID, hold)
		case "smb":
			var target SMBTarget
			if target, err = m.smbTarget(job.Destination.TargetID); err == nil {
				err = m.withSMB(ctx, target, hold)
			}
		default:
			err = fmt.Errorf("nothing to mount for %s", job.Destination.Type)
		}
		if err != nil {
			ready <- held{err: err}
		}
	}()
	got := <-ready
	if got.err != nil {
		<-done
		return "", nil, got.err
	}
	var once sync.Once
	return got.dir, func() {
		once.Do(func() {
			close(free)
			<-done
		})
	}, nil
}
