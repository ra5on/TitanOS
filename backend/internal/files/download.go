package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/httpfetch"
	"github.com/ra5on/TitanOS/backend/internal/netguard"
	"golang.org/x/sys/unix"
)

// Downloads from a link: titand fetches one HTTP(S) resource straight into a
// folder of a root the account may write to, as a background job in the same
// list as copies. Bytes go into a hidden part file next to the target, are
// written in windows of the shared writeback budget with fsync, optionally
// checked against a SHA-256 and published with RENAME_NOREPLACE: no half
// files, no overwriting, nothing buffered in RAM, the overlay or /tmp.
//
// Accounts with the role "user" only reach public addresses (the rule of
// notify.Manager.Confined); owners and administrators may also reach their
// home network. The address of every connection is checked when it is made,
// redirects included; https never falls back to http.
//
// A job lives in the memory of titand. A dropped connection is resumed with
// HTTP Range a few times by itself and later on request, but only while
// titand runs. After a restart the part file is an upload leftover: a note on
// the volume lets the hourly sweep remove it after 24 idle hours, and on
// roots without a volume (USB drives) opening the folder removes it.

const (
	// downloadPerAccount and downloadTotal cap concurrent downloads.
	downloadPerAccount = 3
	downloadTotal      = 8
	// downloadAttempts is how often a dropped connection is resumed by itself.
	downloadAttempts = 5
	// downloadStall ends an attempt that received nothing for this long.
	downloadStall = 2 * time.Minute
	// downloadReserve stays free on the target filesystem.
	downloadReserve = 64 << 20
	// downloadWindow is the write window when no memory budget is set.
	downloadWindow = 8 << 20
	// downloadPrefix names part files, followed by the 24-hex id.
	downloadPrefix = ".titanos-download-"
)

var (
	ErrDownloadLimit = errors.New("files: too many downloads at once")
	ErrBlocked       = errors.New("files: this address may not be reached")
	ErrHashMismatch  = errors.New("files: SHA-256 does not match")
	errStalled       = errors.New("download stalled: no data for two minutes")
	downloadPartRE   = regexp.MustCompile(`^` + regexp.QuoteMeta(downloadPrefix) + `[0-9a-f]{24}` + regexp.QuoteMeta(partSuffix) + `$`)
)

// DownloadRequest is what the dialog sends.
type DownloadRequest struct {
	URL, Dir, Name string
	// SHA256 is the optional expected digest (hex).
	SHA256 string
	// Conflict is "" (fail when the name exists) or "rename" (keep both).
	Conflict string
}

// DownloadProbe is what a link tells before downloading.
type DownloadProbe struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Type   string `json:"type,omitempty"`
	Exists bool   `json:"exists"`
	Free   uint64 `json:"free"`
}

// download is the state of a download job beyond Job.
type download struct {
	url              string
	root             Root
	dir, name, part  string
	sha256, conflict string
	uid              int
	confined         bool
	id               string
	device, inode    uint64
	partInode        uint64
	// resume holds the durable bytes of the part file and the validators of
	// the source they came from.
	resume   httpfetch.Resume
	complete bool
}

// downloadClients are built once: one for confined accounts, one for others.
var downloadClients = sync.OnceValue(func() [2]*http.Client {
	return [2]*http.Client{
		httpfetch.NewClient(httpfetch.Policy{Allow: func(a netip.AddrPort) bool { return netguard.LAN(a.Addr()) }}),
		httpfetch.NewClient(httpfetch.Policy{Allow: func(a netip.AddrPort) bool { return netguard.Public(a.Addr()) }}),
	}
})

func (m *Manager) downloadClient(confined bool) *http.Client {
	if m.DownloadClient != nil {
		return m.DownloadClient(confined)
	}
	if confined {
		return downloadClients()[1]
	}
	return downloadClients()[0]
}

func (m *Manager) downloadFreeBytes(f *os.File) (uint64, error) {
	if m.downloadFree != nil {
		return m.downloadFree(f)
	}
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// downloadError turns transport and policy errors into the file errors the
// API knows; everything else keeps its text for the job.
func downloadError(err error) error {
	switch {
	case errors.Is(err, httpfetch.ErrBlocked):
		return fmt.Errorf("%w: %v", ErrBlocked, err)
	case errors.Is(err, httpfetch.ErrRedirect):
		return fmt.Errorf("%w: %v", ErrBlocked, err)
	}
	return err
}

func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

// displayURL is what the job list shows: no query string, it may carry tokens.
func displayURL(raw string) string {
	u, ok := httpfetch.ValidURL(raw)
	if !ok {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}

// ProbeDownload asks the server about a link without writing anything: the
// suggested file name, size and type, and whether that name exists in dir.
func (m *Manager) ProbeDownload(ctx context.Context, root Root, dir, raw string, confined bool) (DownloadProbe, error) {
	if !root.Writable || len(root.Hide) > 0 {
		return DownloadProbe{}, ErrReadOnly
	}
	u, ok := httpfetch.ValidURL(raw)
	if !ok {
		return DownloadProbe{}, ErrInvalid
	}
	u.Fragment = ""
	dir, err := clean(dir)
	if err != nil {
		return DownloadProbe{}, err
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return DownloadProbe{}, mapError(err)
	}
	defer r.Close()
	if info, err := r.Stat(dir); err != nil || !info.IsDir() {
		return DownloadProbe{}, ErrNotFound
	}
	res, err := httpfetch.Open(ctx, m.downloadClient(confined), u.String(), downloadHeader(), httpfetch.Resume{})
	if err != nil {
		return DownloadProbe{}, downloadError(err)
	}
	res.Body.Close()
	probe := DownloadProbe{Name: res.Filename(), Size: res.Total, Type: res.Header.Get("Content-Type")}
	if !validName(probe.Name) {
		probe.Name = ""
	}
	if probe.Name != "" {
		_, err := r.Lstat(path.Join(dir, probe.Name))
		probe.Exists = err == nil
	}
	_, probe.Free = Usage(root)
	return probe, nil
}

func downloadHeader() http.Header {
	return http.Header{"User-Agent": {"TitanOS file download"}}
}

// runningDownloads counts running downloads; the caller holds jobs.mu.
func (m *Manager) runningDownloads(user string) (mine, all int) {
	for _, job := range m.jobs.list {
		if job.download != nil && job.State == JobRunning {
			all++
			if job.user == user {
				mine++
			}
		}
	}
	return mine, all
}

// StartDownload checks the request against the root and starts the job.
func (m *Manager) StartDownload(root Root, req DownloadRequest, user string, uid int, confined bool) (Job, error) {
	if !root.Writable || len(root.Hide) > 0 {
		return Job{}, ErrReadOnly
	}
	u, ok := httpfetch.ValidURL(req.URL)
	if !ok {
		return Job{}, ErrInvalid
	}
	u.Fragment = ""
	req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))
	if req.SHA256 != "" && !validDigest(req.SHA256) {
		return Job{}, ErrInvalid
	}
	dir, err := clean(req.Dir)
	if err != nil || !validName(req.Name) || (req.Conflict != ConflictFail && req.Conflict != ConflictRename) {
		return Job{}, ErrInvalid
	}
	if m.Available != nil && !m.Available(root) {
		return Job{}, ErrVolumeChanged
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return Job{}, mapError(err)
	}
	defer r.Close()
	if info, err := r.Stat(dir); err != nil || !info.IsDir() {
		return Job{}, ErrNotFound
	}
	device, inode, err := identity(r)
	if err != nil {
		return Job{}, err
	}
	name, err := m.downloadName(r, root, dir, req.Name, req.Conflict)
	if err != nil {
		return Job{}, err
	}
	m.jobs.mu.Lock()
	mine, all := m.runningDownloads(user)
	m.jobs.mu.Unlock()
	if mine >= downloadPerAccount || all >= downloadTotal {
		return Job{}, ErrDownloadLimit
	}
	d := &download{url: u.String(), root: root, dir: dir, name: name, sha256: req.SHA256, conflict: req.Conflict, uid: uid, confined: confined, device: device, inode: inode}
	if err := m.createDownloadPart(r, d); err != nil {
		return Job{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: randomID(), Operation: "download", State: JobRunning, From: displayURL(d.url), To: root.ID, Dir: dir, Names: []string{name}, Files: 1, Started: m.now(), user: user, cancel: cancel, c: &counters{}, download: d}
	m.jobs.mu.Lock()
	// Check the limit again under the lock that adds the job.
	if mine, all := m.runningDownloads(user); mine >= downloadPerAccount || all >= downloadTotal {
		m.jobs.mu.Unlock()
		cancel()
		m.discardDownload(d)
		return Job{}, ErrDownloadLimit
	}
	m.jobs.list = append(m.jobs.list, job)
	m.jobs.mu.Unlock()
	go m.runDownload(ctx, job)
	return m.snapshot(job), nil
}

// downloadName returns the target name, or a free "name (2).ext" when the
// person chose to keep both. A visible conflict is never overwritten.
func (m *Manager) downloadName(r *os.Root, root Root, dir, name, conflict string) (string, error) {
	target := path.Join(dir, name)
	if m.protected(root, target) {
		return "", ErrProtected
	}
	if _, err := r.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		return name, nil
	} else if err != nil {
		return "", mapError(err)
	}
	if conflict != ConflictRename {
		return "", ErrExists
	}
	ext := path.Ext(name)
	for n := 2; n < 10000; n++ {
		candidate := renameUpload(name, ext, n)
		if !validName(candidate) {
			return "", ErrNameTooLong
		}
		if _, err := r.Lstat(path.Join(dir, candidate)); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", mapError(err)
		}
	}
	return "", ErrExists
}

// createDownloadPart makes a new, empty part file only titand may write and
// leaves a note so that a restart does not leave it behind for good.
func (m *Manager) createDownloadPart(r *os.Root, d *download) error {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	id := hex.EncodeToString(raw)
	part := path.Join(d.dir, downloadPrefix+id+partSuffix)
	f, err := r.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return mapError(err)
	}
	info, err := f.Stat()
	if err == nil {
		err = m.syncFile(f)
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		var parent *os.File
		if parent, err = r.Open(d.dir); err == nil {
			err = errors.Join(parent.Sync(), parent.Close())
		}
	}
	if err != nil {
		r.Remove(part)
		return mapError(err)
	}
	m.jobs.mu.Lock()
	d.id, d.part, d.partInode = id, part, info.Sys().(*syscall.Stat_t).Ino
	d.resume, d.complete = httpfetch.Resume{}, false
	m.jobs.mu.Unlock()
	m.noteUpload(d.root, id, part)
	return nil
}

// openDownload reopens the root and checks it is still the same filesystem
// and directory the job started on.
func (m *Manager) openDownload(d *download) (*os.Root, error) {
	if m.Available != nil && !m.Available(d.root) {
		return nil, ErrVolumeChanged
	}
	r, err := os.OpenRoot(d.root.Dir)
	if err != nil {
		return nil, ErrVolumeChanged
	}
	device, inode, err := identity(r)
	if err != nil || device != d.device || inode != d.inode {
		r.Close()
		return nil, ErrVolumeChanged
	}
	return r, nil
}

// openDownloadPart opens the part file only if it is still the file titand
// created: same inode, owned by titand, mode 0600, one link.
func openDownloadPart(r *os.Root, d *download) (*os.File, error) {
	f, err := r.OpenFile(d.part, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, mapError(err)
	}
	info, err := f.Stat()
	if err != nil || !trustedPart(info) || info.Sys().(*syscall.Stat_t).Ino != d.partInode {
		f.Close()
		return nil, ErrMismatch
	}
	return f, nil
}

// discardDownload removes the part file and its note. Best effort: when the
// root is gone the note stays, so the sweep catches the file later. The
// caller holds jobs.mu or the job is not listed.
func (m *Manager) discardDownload(d *download) {
	if d == nil || d.part == "" {
		return
	}
	if r, err := m.openDownload(d); err == nil {
		if f, err := openDownloadPart(r, d); err == nil {
			f.Close()
			r.Remove(d.part)
		}
		r.Close()
		m.forgetUpload(d.root, d.id)
	}
	d.part = ""
}

// downloadKnown reports whether a note id belongs to a job titand still has.
func (m *Manager) downloadKnown(id string) bool {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	for _, job := range m.jobs.list {
		if job.download != nil && job.download.id == id && job.download.part != "" {
			return true
		}
	}
	return false
}

func (m *Manager) runDownload(ctx context.Context, job *Job) {
	err := m.fetchWithRetries(ctx, job)
	d := job.download
	if err == nil {
		err = m.publishDownload(job)
	}
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	job.Finished = m.now()
	job.Current = ""
	switch {
	case ctx.Err() != nil:
		job.State = JobCancelled
		// Cancelling removes what was downloaded; a retry starts over.
		m.discardDownload(d)
	case err != nil:
		job.State = JobFailed
		job.Error = err.Error()
		if errors.Is(err, ErrHashMismatch) || errors.Is(err, ErrVolumeChanged) || errors.Is(err, ErrMismatch) {
			m.discardDownload(d)
		}
	default:
		job.State = JobDone
		job.c.count.Store(1)
	}
	m.pruneJobs()
}

// permanent errors are not retried by the job itself.
func permanent(err error) bool {
	var status httpfetch.StatusError
	if errors.As(err, &status) {
		return status.Code < 500 && status.Code != http.StatusTooManyRequests && status.Code != http.StatusRequestTimeout
	}
	for _, known := range []error{ErrBlocked, ErrNoSpace, ErrHashMismatch, ErrInvalid, ErrVolumeChanged, ErrMismatch, ErrExists, ErrProtected, ErrReadOnly, ErrNotFound, httpfetch.ErrRange, context.Canceled} {
		if errors.Is(err, known) {
			return true
		}
	}
	var netErr net.Error
	return !errors.As(err, &netErr) && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, errStalled) && !errors.Is(err, httpfetch.ErrChanged) && !strings.HasPrefix(err.Error(), "download failed")
}

func (m *Manager) fetchWithRetries(ctx context.Context, job *Job) error {
	for attempt := 1; ; attempt++ {
		err := m.fetchDownload(ctx, job)
		if err == nil || ctx.Err() != nil {
			return err
		}
		if errors.Is(err, httpfetch.ErrChanged) {
			// The server has a different file now: start over from zero.
			m.jobs.mu.Lock()
			job.download.resume = httpfetch.Resume{}
			m.jobs.mu.Unlock()
		}
		if permanent(err) || attempt >= downloadAttempts {
			return err
		}
		m.jobs.mu.Lock()
		job.Current = fmt.Sprintf("retry %d/%d: %v", attempt, downloadAttempts-1, err)
		m.jobs.mu.Unlock()
		timer := time.NewTimer(m.downloadBackoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *Manager) downloadBackoff(attempt int) time.Duration {
	if m.downloadDelay != 0 {
		return m.downloadDelay
	}
	return time.Duration(1<<attempt) * time.Second
}

// stallReader cancels the request when nothing arrives for downloadStall.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
	limit time.Duration
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(s.limit)
	}
	return n, err
}

// fetchDownload makes one attempt: resume the part file where its durable
// bytes end, write in budget windows with fsync and check the digest.
func (m *Manager) fetchDownload(ctx context.Context, job *Job) error {
	m.jobs.mu.Lock()
	d := job.download
	resume, complete := d.resume, d.complete
	job.Current = ""
	m.jobs.mu.Unlock()
	r, err := m.openDownload(d)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := openDownloadPart(r, d)
	if err != nil {
		return err
	}
	defer f.Close()
	if complete {
		return m.checkDigest(f, d, nil, 0)
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	resume.Offset = min(resume.Offset, info.Size())
	fetchCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	limit := downloadStall
	if m.downloadStall != 0 {
		limit = m.downloadStall
	}
	timer := time.AfterFunc(limit, func() { cancel(errStalled) })
	defer timer.Stop()
	res, err := httpfetch.Open(fetchCtx, m.downloadClient(d.confined), d.url, downloadHeader(), resume)
	if err != nil {
		if cause := context.Cause(fetchCtx); errors.Is(cause, errStalled) {
			return errStalled
		}
		return downloadError(err)
	}
	defer res.Body.Close()
	offset, total := res.Offset, res.Total
	if total >= 0 {
		free, err := m.downloadFreeBytes(f)
		if err != nil {
			return err
		}
		if free < uint64(total-offset)+downloadReserve {
			return ErrNoSpace
		}
	}
	if err = f.Truncate(offset); err != nil {
		return mapError(err)
	}
	var digest hash.Hash
	if d.sha256 != "" {
		digest = sha256.New()
		if err = hashPrefix(f, offset, digest); err != nil {
			return err
		}
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	m.jobs.mu.Lock()
	d.resume = httpfetch.Resume{Offset: offset, ETag: res.ETag, LastModified: res.LastModified}
	job.Bytes = max(total, 0)
	job.c.copied.Store(offset)
	m.jobs.mu.Unlock()
	body := &stallReader{r: res.Body, timer: timer, limit: limit}
	buffer := make([]byte, 256<<10)
	written := offset
	for {
		window, release := int64(downloadWindow), func() {}
		if m.Acquire != nil {
			if window, release, err = m.Acquire(ctx); err != nil {
				return err
			}
		}
		need := window
		if total >= 0 {
			need = min(window, max(total-written, 0))
		}
		free, err := m.downloadFreeBytes(f)
		if err == nil && free < uint64(need)+downloadReserve {
			err = ErrNoSpace
		}
		if err != nil {
			release()
			return err
		}
		var out io.Writer = f
		if digest != nil {
			out = io.MultiWriter(f, digest)
		}
		n, copyErr := io.CopyBuffer(out, io.LimitReader(body, window), buffer)
		syncErr := m.syncFile(f)
		if syncErr == nil {
			// Only durable bytes count for a resume; drop them from the cache.
			page := int64(os.Getpagesize())
			if start, end := written/page*page, (written+n)/page*page; end > start {
				_ = unix.Fadvise(int(f.Fd()), start, end-start, unix.FADV_DONTNEED)
			}
			written += n
			m.jobs.mu.Lock()
			d.resume.Offset = written
			m.jobs.mu.Unlock()
			job.c.copied.Store(written)
		} else {
			_ = f.Truncate(written)
		}
		release()
		if copyErr != nil {
			if cause := context.Cause(fetchCtx); errors.Is(cause, errStalled) {
				return errStalled
			}
			return errors.Join(downloadError(copyErr), syncErr)
		}
		if syncErr != nil {
			return mapError(syncErr)
		}
		if total >= 0 && written > total {
			return ErrInvalid
		}
		if n < window {
			break
		}
	}
	if total >= 0 && written != total {
		return io.ErrUnexpectedEOF
	}
	m.jobs.mu.Lock()
	job.Bytes = written
	d.complete = true
	m.jobs.mu.Unlock()
	return m.checkDigest(f, d, digest, written)
}

// hashPrefix feeds the first n bytes of f to h, without keeping them cached.
func hashPrefix(f *os.File, n int64, h hash.Hash) error {
	if n <= 0 {
		return nil
	}
	defer unix.Fadvise(int(f.Fd()), 0, n, unix.FADV_DONTNEED)
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, n)); err != nil {
		return err
	}
	return nil
}

// checkDigest compares the expected SHA-256. A part file that was already
// complete is read again; a mismatch means the bytes must be fetched anew.
func (m *Manager) checkDigest(f *os.File, d *download, digest hash.Hash, size int64) error {
	if d.sha256 == "" {
		return nil
	}
	if digest == nil {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		digest = sha256.New()
		if err = hashPrefix(f, info.Size(), digest); err != nil {
			return err
		}
	}
	if hex.EncodeToString(digest.Sum(nil)) != d.sha256 {
		return ErrHashMismatch
	}
	return nil
}

// publishDownload gives the finished part file to the account and publishes
// it under its name without overwriting anything.
func (m *Manager) publishDownload(job *Job) error {
	m.jobs.mu.Lock()
	d := job.download
	m.jobs.mu.Unlock()
	r, err := m.openDownload(d)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := openDownloadPart(r, d)
	if err != nil {
		return err
	}
	f.Close()
	name := d.name
	if m.protected(d.root, path.Join(d.dir, name)) {
		return ErrProtected
	}
	if err = r.Lchown(d.part, d.uid, m.GID); err == nil {
		err = r.Chmod(d.part, 0o660)
	}
	for err == nil {
		err = publishNewFile(r, d.part, path.Join(d.dir, name))
		if !errors.Is(err, fs.ErrExist) || d.conflict != ConflictRename {
			break
		}
		// Someone took the name meanwhile: keep both, as chosen.
		if name, err = m.downloadName(r, d.root, d.dir, d.name, ConflictRename); err != nil {
			break
		}
	}
	if err != nil {
		// Keep the part file trusted for a retry.
		r.Lchown(d.part, os.Geteuid(), os.Getegid())
		r.Chmod(d.part, 0o600)
		return mapError(err)
	}
	if parent, err := r.Open(d.dir); err == nil {
		parent.Sync()
		parent.Close()
	}
	m.forgetUpload(d.root, d.id)
	m.jobs.mu.Lock()
	d.part = ""
	job.Names = []string{name}
	m.jobs.mu.Unlock()
	return nil
}

// RetryDownload continues a failed or cancelled download of the account. The
// root is resolved again with the account's current rights; a kept part file
// is resumed, otherwise the download starts over.
func (m *Manager) RetryDownload(user, id string, root Root) (Job, error) {
	if !root.Writable || len(root.Hide) > 0 {
		return Job{}, ErrReadOnly
	}
	m.jobs.mu.Lock()
	var job *Job
	for _, candidate := range m.jobs.list {
		if candidate.ID == id && candidate.user == user && candidate.download != nil {
			job = candidate
		}
	}
	if job == nil || (job.State != JobFailed && job.State != JobCancelled) || job.To != root.ID || job.download.root.Dir != root.Dir {
		m.jobs.mu.Unlock()
		return Job{}, ErrNoJob
	}
	if mine, all := m.runningDownloads(user); mine >= downloadPerAccount || all >= downloadTotal {
		m.jobs.mu.Unlock()
		return Job{}, ErrDownloadLimit
	}
	d := job.download
	d.root = root
	job.State = JobRunning
	job.Error, job.Current, job.Finished = "", "", time.Time{}
	m.jobs.mu.Unlock()
	fail := func(err error) (Job, error) {
		m.jobs.mu.Lock()
		job.State, job.Error, job.Finished = JobFailed, err.Error(), m.now()
		m.jobs.mu.Unlock()
		return Job{}, err
	}
	r, err := m.openDownload(d)
	if err != nil {
		return fail(err)
	}
	if d.part != "" {
		if f, err := openDownloadPart(r, d); err == nil {
			f.Close()
		} else {
			// Not our file any more: leave it alone and start over.
			m.forgetUpload(d.root, d.id)
			m.jobs.mu.Lock()
			d.part = ""
			m.jobs.mu.Unlock()
		}
	}
	if d.part == "" {
		if _, err := m.downloadName(r, root, d.dir, d.name, d.conflict); err != nil {
			r.Close()
			return fail(err)
		}
		if err := m.createDownloadPart(r, d); err != nil {
			r.Close()
			return fail(err)
		}
		job.c.copied.Store(0)
	}
	r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m.jobs.mu.Lock()
	job.cancel = cancel
	m.jobs.mu.Unlock()
	go m.runDownload(ctx, job)
	return m.snapshot(job), nil
}

// sweepDownloadPart removes a download part file of an earlier titand run
// found while listing a folder; used where no volume note exists (USB).
func (m *Manager) sweepDownloadPart(r *os.Root, dir, name string) {
	if !downloadPartRE.MatchString(name) {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, downloadPrefix), partSuffix)
	if m.downloadKnown(id) {
		return
	}
	p := path.Join(dir, name)
	if info, err := r.Lstat(p); err == nil && trustedPart(info) && m.now().Sub(info.ModTime()) > uploadIdle {
		r.Remove(p)
	}
}
