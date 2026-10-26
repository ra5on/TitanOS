// Package media imports VM images using private, pinned data-volume staging.
package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

const ChunkBytes = 8 << 20
const VirtIOURL = "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"

var ErrInvalid = errors.New("invalid media import")
var ErrBusy = errors.New("media import is active")
var ErrUnavailable = volumedata.ErrUnavailable
var idRE = regexp.MustCompile(`^[a-f0-9]{24}$`)
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,119}$`)

type Request struct {
	Volume      string `json:"volume"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	UploadName  string `json:"uploadName"`
	UploadSize  int64  `json:"uploadSize"`
	SHA256      string `json:"sha256"`
	Compression string `json:"compression"`
}
type Job struct {
	ID             string    `json:"id"`
	Volume         string    `json:"volume"`
	Name           string    `json:"name"`
	URL            string    `json:"url,omitempty"`
	UploadName     string    `json:"uploadName,omitempty"`
	UploadSize     int64     `json:"uploadSize,omitempty"`
	ExpectedSHA256 string    `json:"expectedSHA256,omitempty"`
	Compression    string    `json:"compression"`
	Phase          string    `json:"phase"`
	Bytes          int64     `json:"bytes"`
	Total          int64     `json:"total"`
	Written        int64     `json:"written"`
	OutputBytes    int64     `json:"outputBytes"`
	Error          string    `json:"error,omitempty"`
	Output         string    `json:"output,omitempty"`
	SHA256         string    `json:"sha256,omitempty"`
	SourceSHA256   string    `json:"sourceSHA256,omitempty"`
	ETag           string    `json:"etag,omitempty"`
	LastModified   string    `json:"lastModified,omitempty"`
	SourceReady    bool      `json:"sourceReady,omitempty"`
	SourceInode    uint64    `json:"sourceInode,omitempty"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
}
type entry struct {
	job          Job
	op           sync.Mutex
	checkpoint   sync.Mutex
	running      bool
	uploadActive bool
	verified     int64
	cancel       context.CancelFunc
	desired      string
	done         chan struct{}
	mountLost    bool
}
type Manager struct {
	Context   context.Context
	Mounted   func() []string
	MountRoot string
	Disabled  bool
	GID       int
	Client    *http.Client
	mu        sync.Mutex
	scan      sync.Mutex
	virtioMu  sync.Mutex
	jobs      map[string]*entry
	roots     map[string]*os.Root
	wg        sync.WaitGroup
	closed    bool
}

func New(ctx context.Context, mounted func() []string, gid int) *Manager {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.ResponseHeaderTimeout = 30 * time.Second
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return ErrInvalid
		}
		if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return ErrInvalid
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return ErrInvalid
		}
		return nil
	}}
	return &Manager{Context: ctx, Mounted: mounted, MountRoot: "/mnt", GID: gid, Client: client, jobs: map[string]*entry{}, roots: map[string]*os.Root{}}
}
func validName(name string) bool { return nameRE.MatchString(name) && supported(name) }
func supported(name string) bool {
	return slices.Contains([]string{".iso", ".qcow2", ".raw", ".img", ".vmdk", ".vhdx", ".vdi"}, strings.ToLower(filepath.Ext(name)))
}
func validateRequest(r Request) error {
	if !validName(r.Name) || !slices.Contains([]string{"", "auto", "none", "gz", "xz", "zip"}, r.Compression) || r.UploadSize < 0 {
		return ErrInvalid
	}
	if r.SHA256 != "" {
		b, e := hex.DecodeString(r.SHA256)
		if e != nil || len(b) != 32 {
			return ErrInvalid
		}
	}
	if r.URL != "" {
		u, e := url.Parse(r.URL)
		if len(r.URL) > 8192 || e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || !(u.Scheme == "http" || u.Scheme == "https") {
			return ErrInvalid
		}
	} else if r.UploadSize <= 0 || r.UploadName == "" || len(r.UploadName) > 255 || filepath.Base(r.UploadName) != r.UploadName {
		return ErrInvalid
	}
	return nil
}
func (m *Manager) private(volume string) (*os.Root, error) {
	if m.Disabled {
		return nil, ErrUnavailable
	}
	return volumedata.Open(m.MountRoot, volume, "media", m.Mounted())
}
func secureJob(root *os.Root, id string) (*os.Root, error) {
	if !idRE.MatchString(id) {
		return nil, ErrInvalid
	}
	st, err := root.Lstat(id)
	if err != nil {
		return nil, err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, ErrInvalid
	}
	job, err := root.OpenRoot(id)
	if err == nil {
		err = volumedata.SameMount(root, job)
		if err != nil {
			job.Close()
		}
	}
	return job, err
}
func secureFile(root *os.Root, name string, flags int) (*os.File, error) {
	if st, e := root.Lstat(name); e == nil {
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !st.Mode().IsRegular() || !ok || owner.Uid != uint32(os.Geteuid()) || st.Mode().Perm()&0022 != 0 {
			return nil, ErrInvalid
		}
	} else if !errors.Is(e, fs.ErrNotExist) {
		return nil, e
	}
	return root.OpenFile(name, flags|unix.O_NOFOLLOW, 0600)
}
func (m *Manager) restore() {
	if m.Disabled {
		return
	}
	m.scan.Lock()
	defer m.scan.Unlock()
	mounted := m.Mounted()
	m.mu.Lock()
	for volume, root := range m.roots {
		if !slices.Contains(mounted, volume) {
			root.Close()
			delete(m.roots, volume)
			for _, e := range m.jobs {
				if e.job.Volume == volume {
					e.mountLost = true
					e.desired = "paused"
					if e.cancel != nil {
						e.cancel()
					}
				}
			}
		}
	}
	m.mu.Unlock()
	for _, volume := range mounted {
		if _, err := os.Lstat(filepath.Join(m.MountRoot, volume, ".titanos/media")); err != nil {
			continue
		}
		root, err := m.private(volume)
		if err != nil {
			continue
		}
		m.mu.Lock()
		old := m.roots[volume]
		same := old != nil && volumedata.SameMount(old, root) == nil
		wait := false
		if !same {
			for _, e := range m.jobs {
				if e.job.Volume == volume && e.running && (old != nil || e.mountLost) {
					e.mountLost = true
					e.desired = "paused"
					if e.cancel != nil {
						e.cancel()
					}
					wait = true
				}
			}
		}
		m.mu.Unlock()
		if same || wait {
			root.Close()
			continue
		}
		f, err := root.Open(".")
		if err != nil {
			root.Close()
			continue
		}
		dirs, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			root.Close()
			continue
		}
		discovered := map[string]*entry{}
		for _, dir := range dirs {
			if !idRE.MatchString(dir.Name()) {
				continue
			}
			jobRoot, e := secureJob(root, dir.Name())
			if e != nil {
				continue
			}
			var job Job
			e = volumedata.ReadJSON(jobRoot, "job.json", &job, 64<<10)
			jobRoot.Close()
			if e != nil || job.ID != dir.Name() || job.Volume != volume || !validName(job.Name) {
				job = Job{ID: dir.Name(), Volume: volume, Name: dir.Name(), Phase: "review", Error: "Import journal is unreadable; staging was retained"}
			} else if slices.Contains([]string{"downloading", "extracting", "verifying", "pausing"}, job.Phase) {
				job.Phase = "paused"
				job.Error = "Interrupted by service or NAS restart; resume explicitly"
			} else if job.Phase == "publishing" {
				job.Phase = "review"
				job.Error = "Publication interrupted; inspect the output before starting a new import"
			}
			discovered[job.ID] = &entry{job: job}
		}
		m.mu.Lock()
		if old != nil {
			old.Close()
		}
		m.roots[volume] = root
		for id, e := range discovered {
			if previous := m.jobs[id]; previous != nil {
				// A first scan may have read an older journal while the live
				// worker was advancing. RAM remains authoritative on this mount.
				if old == nil && !previous.mountLost {
					continue
				}
				if previous.running {
					previous.desired = "paused"
					if previous.cancel != nil {
						previous.cancel()
					}
					continue
				}
			}
			m.jobs[id] = e
		}
		m.mu.Unlock()
	}
}
func (m *Manager) List() []Job {
	m.restore()
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := []Job{}
	for _, e := range m.jobs {
		j := e.job
		// Do not offer retry/remove until final checkpoint and staging cleanup
		// have released the worker. A visible terminal phase is actionable.
		if e.running {
			if j.Phase == "done" || j.Phase == "review" {
				j.Phase = "publishing"
			}
			if slices.Contains([]string{"failed", "paused", "cancelled"}, j.Phase) {
				j.Phase = "pausing"
			}
		}
		if j.URL != "" {
			if u, err := url.Parse(j.URL); err == nil {
				u.RawQuery = ""
				j.URL = u.String()
			}
		}
		jobs = append(jobs, j)
	}
	slices.SortFunc(jobs, func(a, b Job) int { return b.Created.Compare(a.Created) })
	return jobs
}
func (m *Manager) get(id string) (*entry, error) {
	if !idRE.MatchString(id) {
		return nil, ErrInvalid
	}
	m.restore()
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.jobs[id]
	if e == nil {
		return nil, os.ErrNotExist
	}
	return e, nil
}
func (m *Manager) snapshot(e *entry) Job { m.mu.Lock(); defer m.mu.Unlock(); return e.job }
func (m *Manager) save(e *entry, root *os.Root) error {
	e.checkpoint.Lock()
	defer e.checkpoint.Unlock()
	j := m.snapshot(e)
	return volumedata.WriteJSON(root, "job.json", j)
}
func (m *Manager) update(e *entry, fn func(*Job)) {
	m.mu.Lock()
	fn(&e.job)
	e.job.Updated = time.Now().UTC()
	m.mu.Unlock()
}
func (m *Manager) Create(r Request) (Job, error) {
	if err := validateRequest(r); err != nil {
		return Job{}, err
	}
	m.restore()
	private, err := m.private(r.Volume)
	if err != nil {
		return Job{}, err
	}
	defer private.Close()
	var b [12]byte
	if _, err = rand.Read(b[:]); err != nil {
		return Job{}, err
	}
	id := hex.EncodeToString(b[:])
	if err = private.Mkdir(id, 0700); err != nil {
		return Job{}, err
	}
	root, err := secureJob(private, id)
	if err != nil {
		return Job{}, err
	}
	defer root.Close()
	compression := r.Compression
	if compression == "" || compression == "auto" {
		source := r.UploadName
		if r.URL != "" {
			u, _ := url.Parse(r.URL)
			source = u.Path
		}
		compression = strings.TrimPrefix(strings.ToLower(filepath.Ext(source)), ".")
		if !slices.Contains([]string{"gz", "xz", "zip"}, compression) {
			compression = "none"
		}
	}
	phase := "uploading"
	if r.URL != "" {
		phase = "paused"
	}
	now := time.Now().UTC()
	j := Job{ID: id, Volume: r.Volume, Name: r.Name, URL: r.URL, UploadName: r.UploadName, UploadSize: r.UploadSize, ExpectedSHA256: strings.ToLower(r.SHA256), Compression: compression, Phase: phase, Created: now, Updated: now, Total: r.UploadSize}
	e := &entry{job: j}
	if err = m.save(e, root); err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	m.jobs[id] = e
	m.mu.Unlock()
	if r.URL != "" {
		if err = m.start(e); err != nil {
			return m.snapshot(e), err
		}
	}
	return m.snapshot(e), nil
}
func (m *Manager) start(e *entry) error {
	m.mu.Lock()
	if m.closed || m.Context.Err() != nil {
		m.mu.Unlock()
		return ErrUnavailable
	}
	if e.running {
		m.mu.Unlock()
		return ErrBusy
	}
	if slices.Contains([]string{"done", "review", "cancelled"}, e.job.Phase) {
		m.mu.Unlock()
		return ErrInvalid
	}
	active := 0
	for _, v := range m.jobs {
		if v.running {
			active++
		}
	}
	if active >= 4 {
		m.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(m.Context)
	e.cancel = cancel
	e.running = true
	e.desired = ""
	e.done = make(chan struct{})
	e.job.Phase = "downloading"
	e.job.Error = ""
	m.wg.Add(1)
	m.mu.Unlock()
	go m.work(ctx, e)
	return nil
}
func (m *Manager) Control(id, action string) error {
	e, err := m.get(id)
	if err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	switch action {
	case "review":
		j := m.snapshot(e)
		if j.Phase != "review" {
			return ErrInvalid
		}
		if err := m.verifyPublished(j); err != nil {
			return err
		}
		private, err := m.private(j.Volume)
		if err != nil {
			return err
		}
		defer private.Close()
		root, err := secureJob(private, id)
		if err != nil {
			return err
		}
		defer root.Close()
		m.update(e, func(j *Job) {
			j.Phase = "done"
			j.Error = ""
			j.Output = filepath.Join(m.MountRoot, j.Volume, "iso", j.Name)
		})
		return m.save(e, root)
	case "resume", "retry":
		m.mu.Lock()
		running := e.running
		m.mu.Unlock()
		if running {
			return ErrBusy
		}
		j := m.snapshot(e)
		if action == "resume" && j.URL == "" && !j.SourceReady && slices.Contains([]string{"paused", "failed"}, j.Phase) {
			if e.verified < j.Bytes {
				return ErrInvalid
			}
			private, err := m.private(j.Volume)
			if err != nil {
				return err
			}
			defer private.Close()
			root, err := secureJob(private, id)
			if err != nil {
				return err
			}
			defer root.Close()
			m.update(e, func(j *Job) { j.Phase = "uploading"; j.Error = "" })
			return m.save(e, root)
		}
		if j.Phase == "uploading" || j.URL == "" && !j.SourceReady {
			return ErrInvalid
		}
		if action == "retry" && j.URL != "" {
			m.update(e, func(j *Job) { j.SourceReady = false; j.Bytes = 0; j.ETag = ""; j.LastModified = "" })
		}
		return m.start(e)
	case "pause", "cancel":
		m.mu.Lock()
		if e.job.Phase == "done" || e.job.Phase == "review" || e.job.Phase == "publishing" {
			m.mu.Unlock()
			return ErrInvalid
		}
		phase := "paused"
		if action == "cancel" {
			phase = "cancelled"
		}
		e.desired = phase
		if e.cancel != nil && e.running {
			e.job.Phase = "pausing"
			e.cancel()
			m.mu.Unlock()
			return nil
		}
		e.job.Phase = phase
		m.mu.Unlock()
		private, err := m.private(e.job.Volume)
		if err != nil {
			return err
		}
		defer private.Close()
		root, err := secureJob(private, id)
		if err != nil {
			return err
		}
		defer root.Close()
		if phase == "cancelled" {
			root.Remove("source.part")
			root.Remove("image.part")
		}
		return m.save(e, root)
	case "remove":
		m.mu.Lock()
		running := e.running
		m.mu.Unlock()
		if running {
			return ErrBusy
		}
		private, err := m.private(e.job.Volume)
		if err != nil {
			return err
		}
		defer private.Close()
		root, err := secureJob(private, id)
		if err != nil {
			return err
		}
		root.Close()
		if err = private.RemoveAll(id); err != nil {
			return err
		}
		m.mu.Lock()
		delete(m.jobs, id)
		m.mu.Unlock()
		return nil
	default:
		return ErrInvalid
	}
}
func (m *Manager) Upload(ctx context.Context, id string, offset int64, data []byte) error {
	if len(data) > ChunkBytes || offset < 0 {
		return ErrInvalid
	}
	e, err := m.get(id)
	if err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	m.mu.Lock()
	e.uploadActive = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); e.uploadActive = false; m.mu.Unlock() }()
	j := m.snapshot(e)
	m.mu.Lock()
	running := e.running
	m.mu.Unlock()
	if running || j.URL != "" || j.Phase != "uploading" || offset+int64(len(data)) > j.UploadSize {
		return ErrInvalid
	}
	private, err := m.private(j.Volume)
	if err != nil {
		return err
	}
	defer private.Close()
	root, err := secureJob(private, id)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := secureFile(root, "source.part", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return err
	}
	if offset != st.Size() {
		return fmt.Errorf("%w: upload offset is %d", ErrInvalid, st.Size())
	}
	if offset > e.verified {
		return fmt.Errorf("%w: verify the original uploaded prefix before resuming", ErrInvalid)
	}
	if j.SourceInode != 0 && j.SourceInode != st.Sys().(*syscall.Stat_t).Ino {
		return fmt.Errorf("upload staging file identity changed")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = freeSpace(file, int64(len(data))); err != nil {
		return err
	}
	if _, err = file.WriteAt(data, offset); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	sys := st.Sys().(*syscall.Stat_t)
	m.update(e, func(j *Job) { j.Phase = "uploading"; j.Bytes = offset + int64(len(data)); j.SourceInode = sys.Ino })
	e.verified = offset + int64(len(data))
	return m.save(e, root)
}
func (m *Manager) FinishUpload(id string) error {
	e, err := m.get(id)
	if err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	j := m.snapshot(e)
	if j.URL != "" || j.Bytes != j.UploadSize || j.Phase != "uploading" {
		return ErrInvalid
	}
	return m.start(e)
}
func (m *Manager) Working() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.jobs {
		if e.running || e.uploadActive {
			return true
		}
	}
	return false
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, e := range m.jobs {
		if e.cancel != nil {
			e.desired = "paused"
			e.cancel()
		}
	}
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	for _, root := range m.roots {
		root.Close()
	}
	m.roots = map[string]*os.Root{}
	m.mu.Unlock()
}
func freeSpace(file *os.File, needed int64) error {
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &st); err != nil {
		return err
	}
	if needed < 0 || st.Bavail*uint64(st.Bsize) < uint64(needed)+(64<<20) {
		return fmt.Errorf("insufficient free space on data volume")
	}
	return nil
}

// VirtIO reuses only a completed import from the fixed official source. Its
// pinned output digest is checked again before a new VM receives the ISO.
func (m *Manager) VirtIO(volume string) (Job, error) {
	m.virtioMu.Lock()
	defer m.virtioMu.Unlock()
	m.restore()
	m.mu.Lock()
	var matches []*entry
	for _, e := range m.jobs {
		if e.job.Volume == volume && e.job.URL == VirtIOURL && e.job.Phase != "cancelled" {
			matches = append(matches, e)
		}
	}
	m.mu.Unlock()
	for _, e := range matches {
		j := m.snapshot(e)
		if j.Phase == "done" {
			if err := m.verifyPublished(j); err == nil {
				return j, nil
			}
			continue
		}
		if j.Phase == "review" || j.Phase == "failed" {
			continue
		}
		if j.Phase == "paused" {
			if err := m.start(e); err != nil && !errors.Is(err, ErrBusy) {
				return Job{}, err
			}
		}
		return m.snapshot(e), nil
	}
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Job{}, err
	}
	return m.Create(Request{Volume: volume, Name: "virtio-win-" + hex.EncodeToString(random[:]) + ".iso", URL: VirtIOURL, Compression: "none"})
}
func (m *Manager) verifyPublished(j Job) error {
	if !validName(j.Name) || len(j.SHA256) != 64 || j.OutputBytes <= 0 {
		return ErrInvalid
	}
	private, err := m.private(j.Volume)
	if err != nil {
		return err
	}
	defer private.Close()
	root, err := os.OpenRoot(filepath.Join(m.MountRoot, j.Volume))
	if err != nil {
		return err
	}
	defer root.Close()
	if err = volumedata.SameMount(private, root); err != nil {
		return err
	}
	st, err := root.Lstat("iso")
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	iso, err := root.OpenRoot("iso")
	if err != nil {
		return err
	}
	defer iso.Close()
	if err = volumedata.SameMount(private, iso); err != nil {
		return err
	}
	file, err := iso.OpenFile(j.Name, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	st, err = file.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() != j.OutputBytes {
		return ErrInvalid
	}
	hash, err := hashFile(m.Context, file)
	if err != nil || hash != j.SHA256 {
		return ErrInvalid
	}
	return nil
}
