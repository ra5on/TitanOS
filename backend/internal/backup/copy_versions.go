package backup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/retention"
)

// Readable copies (decided 31.10.2026): besides adding and replacing,
// a copy can mirror deletions or keep each run in a dated folder.
//
//   - Mirror: what is gone from a share is moved on the target to
//     <folder>/_gelöscht/<date>/… and removed for good after MirrorDays
//     (default 30). Only files whose source is really gone move; a share or
//     volume that is not there leaves its copy alone.
//   - Dated: each run writes <folder>/<YYYY-MM-DD_HHMM>/<share>/…; files
//     that did not change since the run before are hard links to it where
//     the file system allows (not on FAT32/exFAT). The runs are versions:
//     Keep picks which stay, and with its leave the oldest go when the drive
//     is full.

// CopyOptions are the options of a readable copy.
type CopyOptions struct {
	Dated      bool `json:"dated,omitempty"`
	Mirror     bool `json:"mirror,omitempty"`
	MirrorDays int  `json:"mirrorDays,omitempty"`
}

// deletedFolder holds what a mirror took away, by date.
const deletedFolder = "_gelöscht"

var runName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}_\d{4})(-\d+)?$`)

type datedRun struct {
	name string
	at   time.Time
}

// datedRuns lists the dated runs in folder, oldest first.
func datedRuns(target *os.Root, folder string, zone *time.Location) []datedRun {
	dir, err := target.Open(folder)
	if err != nil {
		return nil
	}
	defer dir.Close()
	entries, _ := dir.ReadDir(-1)
	var runs []datedRun
	for _, e := range entries {
		match := runName.FindStringSubmatch(e.Name())
		if !e.IsDir() || match == nil {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02_1504", match[1], zone)
		if err != nil {
			continue
		}
		runs = append(runs, datedRun{name: e.Name(), at: at})
	}
	slices.SortFunc(runs, func(a, b datedRun) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	return runs
}

// newRunName names the folder of a run starting at now.
func newRunName(now time.Time, runs []datedRun) string {
	name := now.Format("2006-01-02_1504")
	taken := func(n string) bool { return slices.ContainsFunc(runs, func(r datedRun) bool { return r.name == n }) }
	for i := 2; taken(name); i++ {
		name = now.Format("2006-01-02_1504") + "-" + itoa(i)
	}
	return name
}

func itoa(i int) string { return strconv.Itoa(i) }

// canLink tries a hard link in folder: FAT32 and exFAT have none.
func canLink(target *os.Root, folder string) bool {
	probe := path.Join(folder, ".titanos-link-"+itoa(os.Getpid()))
	f, err := target.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	f.Close()
	defer target.Remove(probe)
	defer target.Remove(probe + "-2")
	return target.Link(probe, probe+"-2") == nil
}

// copyOrLink links to the same file of the run before when its content is
// unchanged, and copies it otherwise.
func copyOrLink(ctx context.Context, source *os.Root, from string, target *os.Root, to, prev string, pace *limiter, progress func(int64)) (int64, error) {
	old, err := target.Lstat(prev)
	if err == nil && old.Mode().IsRegular() {
		input, err := source.OpenFile(from, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return 0, err
		}
		before, err := input.Stat()
		if err == nil && before.Mode().IsRegular() && before.Size() == old.Size() {
			same, err := sameCopy(ctx, input, target, prev, pace)
			after, statErr := input.Stat()
			input.Close()
			if err != nil {
				return 0, err
			}
			if statErr == nil && same && after.Size() == before.Size() && after.ModTime().Equal(before.ModTime()) {
				if err := target.Link(prev, to); err == nil {
					return before.Size(), nil
				}
			}
		} else {
			input.Close()
		}
	}
	return copyFilePaced(ctx, source, from, target, to, pace, progress)
}

// keepRuns removes the dated runs Keep does not keep.
func (m *Manager) keepRuns(job Job, target *os.Root, runs []datedRun) error {
	versions := make([]retention.Version, len(runs))
	for i, r := range runs {
		versions[i] = retention.Version{ID: r.name, Time: r.at}
	}
	keep := retention.Select(job.Keep, versions, m.local(m.now()))
	for _, r := range runs {
		if !keep[r.name] {
			if err := target.RemoveAll(path.Join(job.Destination.Folder, r.name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// mirror moves what is gone from the shares into the deleted folder and
// empties days of it older than MirrorDays.
func (m *Manager) mirror(ctx context.Context, job Job, target *os.Root, sources map[string]Source, names []string, folderOf map[string][]string, problems *copyProblems) error {
	days := job.Copy.MirrorDays
	if days == 0 {
		days = 30
	}
	now := m.local(m.now())
	bin := path.Join(job.Destination.Folder, deletedFolder, now.Format("2006-01-02"))
	for _, name := range names {
		source, ok := sources[name]
		if !ok {
			continue // not there: its copy stays as it is
		}
		root, err := os.OpenRoot(source.Path)
		if err != nil {
			continue
		}
		folders := folderOf[name]
		if len(folders) == 0 {
			folders = []string{"."}
		}
		for _, folder := range folders {
			copyRoot := path.Join(job.Destination.Folder, name, folder)
			err := fs.WalkDir(target.FS(), copyRoot, func(at string, entry fs.DirEntry, err error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err != nil || at == copyRoot {
					return nil
				}
				rel := strings.TrimPrefix(at, path.Join(job.Destination.Folder, name)+"/")
				if strings.HasPrefix(entry.Name(), ".titanos-") {
					return nil
				}
				if _, err := root.Lstat(rel); !errors.Is(err, fs.ErrNotExist) {
					return nil // still there, or cannot tell: keep
				}
				to := path.Join(bin, name, rel)
				if err := copyDirectories(target, path.Dir(to)); err != nil {
					return err
				}
				if err := target.Rename(at, to); err != nil {
					if fatalTarget(err) {
						return err
					}
					problems.add("deleted files that could not be moved aside", name+"/"+rel)
					return nil
				}
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			})
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				root.Close()
				return err
			}
		}
		root.Close()
	}
	// Days older than the limit go for good.
	dir, err := target.Open(path.Join(job.Destination.Folder, deletedFolder))
	if err != nil {
		return nil
	}
	entries, _ := dir.ReadDir(-1)
	dir.Close()
	for _, e := range entries {
		day, err := time.ParseInLocation("2006-01-02", e.Name(), now.Location())
		if err == nil && day.Before(now.AddDate(0, 0, -days)) {
			if err := target.RemoveAll(path.Join(job.Destination.Folder, deletedFolder, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
