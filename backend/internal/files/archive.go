package files

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
)

const archiveLimit = 20000
const archiveBytesLimit = uint64(1 << 40)

type ArchiveEntry struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
	Dir  bool   `json:"dir"`
}
type ArchiveListing struct {
	Entries []ArchiveEntry `json:"entries"`
	Bytes   uint64         `json:"bytes"`
	Version string         `json:"version"`
}

func (m *Manager) openArchive(root Root, p string) (*os.File, *zip.Reader, ArchiveListing, error) {
	listing := ArchiveListing{Entries: []ArchiveEntry{}}
	p, e := clean(p)
	if e != nil || root.secret(p) || m.protected(root, p) {
		return nil, nil, listing, ErrInvalid
	}
	r, e := os.OpenRoot(root.Dir)
	if e != nil {
		return nil, nil, listing, mapError(e)
	}
	f, e := r.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	r.Close()
	if e != nil {
		return nil, nil, listing, mapError(e)
	}
	info, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, nil, listing, mapError(e)
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, listing, ErrInvalid
	}
	zr, e := zip.NewReader(f, info.Size())
	if e != nil {
		f.Close()
		return nil, nil, listing, ErrInvalid
	}
	listing.Version = entryVersion(info)
	if len(zr.File) > archiveLimit {
		f.Close()
		return nil, nil, listing, ErrScanLimit
	}
	seen := map[string]bool{}
	types := map[string]bool{}
	for _, file := range zr.File {
		name := strings.TrimSuffix(file.Name, "/")
		cleaned, e := clean(name)
		if e != nil || cleaned != name || name == "." || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || seen[name] || file.Mode()&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) || file.Flags&1 != 0 {
			f.Close()
			return nil, nil, listing, ErrInvalid
		}
		seen[name] = true
		types[name] = file.FileInfo().IsDir()
		if file.UncompressedSize64 > archiveBytesLimit-listing.Bytes {
			f.Close()
			return nil, nil, listing, ErrNoSpace
		}
		listing.Bytes += file.UncompressedSize64
		listing.Entries = append(listing.Entries, ArchiveEntry{Path: name, Size: file.UncompressedSize64, Dir: file.FileInfo().IsDir()})
	}
	for name := range types {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if dir, exists := types[parent]; exists && !dir {
				f.Close()
				return nil, nil, listing, ErrInvalid
			}
		}
	}
	return f, zr, listing, nil
}
func (m *Manager) ArchiveList(root Root, p string) (ArchiveListing, error) {
	f, _, result, e := m.openArchive(root, p)
	if f != nil {
		f.Close()
	}
	return result, e
}

// Extract always publishes a new folder. Existing folders are a visible
// conflict, never an implicit merge or overwrite. Staging is on the target
// filesystem, and only complete, CRC-checked files become visible.
func (m *Manager) StartExtract(src Root, archive, version string, selected []string, dst Root, dir, name, user string, uid int) (Job, error) {
	if !dst.Writable || len(dst.Hide) > 0 {
		return Job{}, ErrReadOnly
	}
	if !validName(name) || len(selected) == 0 {
		return Job{}, ErrInvalid
	}
	dir, e := clean(dir)
	if e != nil {
		return Job{}, e
	}
	destination := path.Join(dir, name)
	if m.protected(dst, destination) {
		return Job{}, ErrProtected
	}
	f, zr, listing, e := m.openArchive(src, archive)
	if e != nil {
		return Job{}, e
	}
	fail := func(e error) (Job, error) { f.Close(); return Job{}, e }
	if version != listing.Version {
		return fail(ErrTextConflict)
	}
	wanted := map[string]bool{}
	known := map[string]bool{}
	for _, entry := range listing.Entries {
		known[entry.Path] = true
	}
	for _, p := range selected {
		if !known[p] {
			return fail(ErrInvalid)
		}
		wanted[p] = true
	}
	chosen := []*zip.File{}
	var count, size int64
	for _, entry := range zr.File {
		p := strings.TrimSuffix(entry.Name, "/")
		include := wanted[p]
		for parent := path.Dir(p); !include && parent != "."; parent = path.Dir(parent) {
			include = wanted[parent]
		}
		if include {
			chosen = append(chosen, entry)
			if !entry.FileInfo().IsDir() {
				count++
				size += int64(entry.UncompressedSize64)
			}
		}
	}
	_, free := Usage(dst)
	if uint64(size) > free {
		return fail(ErrNoSpace)
	}
	dr, e := os.OpenRoot(dst.Dir)
	if e != nil {
		return fail(mapError(e))
	}
	if info, e := dr.Stat(dir); e != nil || !info.IsDir() {
		dr.Close()
		return fail(ErrNotFound)
	}
	if _, e = dr.Lstat(destination); e == nil {
		dr.Close()
		return fail(ErrExists)
	} else if !os.IsNotExist(e) {
		dr.Close()
		return fail(mapError(e))
	}
	stage := path.Join(dir, ".titanos-extract-"+randomID()+partSuffix)
	if e = dr.Mkdir(stage, 0700); e != nil {
		dr.Close()
		return fail(mapError(e))
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: randomID(), Operation: "extract", State: JobRunning, From: src.ID, To: dst.ID, Dir: destination, Names: []string{archive}, Files: count, Bytes: size, Started: m.now(), user: user, cancel: cancel, c: &counters{}}
	m.jobs.mu.Lock()
	m.jobs.list = append(m.jobs.list, job)
	m.jobs.mu.Unlock()
	go func() {
		defer f.Close()
		defer dr.Close()
		defer dr.RemoveAll(stage)
		err := m.extractArchive(ctx, job, dr, stage, chosen, uid)
		if err == nil {
			info, e := f.Stat()
			if e != nil {
				err = e
			} else if entryVersion(info) != version {
				err = ErrTextConflict
			}
		}
		if err == nil {
			m.own(dr, stage, uid, true)
			err = publishNewFile(dr, stage, destination)
		}
		m.jobs.mu.Lock()
		defer m.jobs.mu.Unlock()
		job.Finished = m.now()
		switch {
		case ctx.Err() != nil:
			job.State = JobCancelled
		case err != nil:
			job.State = JobFailed
			job.Error = err.Error()
		default:
			job.State = JobDone
			job.Current = ""
		}
		m.pruneJobs()
	}()
	return m.snapshot(job), nil
}
func (m *Manager) extractArchive(ctx context.Context, job *Job, dr *os.Root, stage string, files []*zip.File, uid int) error {
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, entry := range files {
		if e := ctx.Err(); e != nil {
			return e
		}
		name := strings.TrimSuffix(entry.Name, "/")
		target := path.Join(stage, name)
		m.jobs.mu.Lock()
		job.Current = name
		m.jobs.mu.Unlock()
		if e := m.makeParents(dr, path.Dir(target), uid); e != nil {
			return e
		}
		if entry.FileInfo().IsDir() {
			if e := dr.Mkdir(target, 0770); e != nil && !os.IsExist(e) {
				return e
			}
			m.own(dr, target, uid, true)
			continue
		}
		input, e := entry.Open()
		if e != nil {
			return e
		}
		output, e := dr.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0660)
		if e != nil {
			input.Close()
			return e
		}
		buf := make([]byte, 1<<20)
		var written uint64
		for {
			if e = ctx.Err(); e != nil {
				break
			}
			var n int
			n, e = input.Read(buf)
			if n > 0 {
				written += uint64(n)
				if written > entry.UncompressedSize64 {
					e = ErrInvalid
					break
				}
				if _, err := output.Write(buf[:n]); err != nil {
					e = err
					break
				}
				job.c.copied.Add(int64(n))
			}
			if e != nil {
				break
			}
		}
		input.Close()
		if errors.Is(e, io.EOF) && written == entry.UncompressedSize64 {
			e = m.syncFile(output)
		}
		closeErr := output.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		m.own(dr, target, uid, false)
		if !entry.Modified.IsZero() {
			_ = dr.Chtimes(target, entry.Modified, entry.Modified) // permissions and times are a convenience; the data are there either way
		}
		job.c.count.Add(1)
	}
	return nil
}
