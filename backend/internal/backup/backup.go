// Package backup copies shares to somewhere else: another volume, another
// server, or storage in the cloud.
//
// The work is done by restic. A backup is encrypted with a password that
// TitanOS makes for each job; whoever holds the place it goes to cannot
// read it. Without that password a backup is worthless, so the web UI
// tells its owner to write it down.
package backup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/retention"
)

const (
	jobsFile = "backup/jobs.json"
	keyFile  = "backup/ssh/id_ed25519"
	hostFile = "backup/ssh/known_hosts"
	// stateDir is where a job keeps what it knows about its runs, on the
	// volume of its first source: writing it to /config after every run
	// would wear the stick.
	stateDir = ".titanos/backup"
)

// Destination is where a backup goes.
type Destination struct {
	// Type is local, usb, smb (encrypted versions), local-copy, usb-copy,
	// smb-copy (readable copies), titanos, s3, sftp or rest.
	Type     string `json:"type"`
	DeviceID string `json:"deviceId,omitempty"`
	// DeviceName is the name the drive had when the job was saved, only
	// for showing; older versions ignore it.
	DeviceName string `json:"deviceName,omitempty"`
	TargetID   string `json:"targetId,omitempty"`
	// local: a folder on another volume.
	Volume string `json:"volume,omitempty"`
	Folder string `json:"folder,omitempty"`
	// s3: any storage that speaks the S3 protocol.
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
	// sftp: another machine reached over SSH, with the key of this NAS.
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	User string `json:"user,omitempty"`
	Path string `json:"path,omitempty"`
	// rest: a restic REST server.
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Keep says which versions stay, see internal/retention.
type Keep = retention.Keep

// Job is one backup: what, where to, when.
type Application struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Method string `json:"method"`
}

type CoordinatedSelection struct {
	Sources  []string            `json:"sources"`
	Extras   []string            `json:"extras"`
	Folders  map[string][]string `json:"folders,omitempty"`
	Skip     []string            `json:"skip,omitempty"`
	Patterns []string            `json:"patterns,omitempty"`
}

type Job struct {
	CoordinatedSelection *CoordinatedSelection `json:"coordinatedSelection,omitempty"`
	Applications         []Application         `json:"applications,omitempty"`
	AllowStop            bool                  `json:"allowStop,omitempty"`
	// ID is the name; it also names the job's files.
	ID string `json:"id"`
	// Sources are shares, by name; see selection.go for what else picks
	// out what is saved.
	Sources  []string            `json:"sources"`
	Folders  map[string][]string `json:"folders,omitempty"`
	Skip     []string            `json:"skip,omitempty"`
	Patterns []string            `json:"patterns,omitempty"`
	Extras   []string            `json:"extras,omitempty"`
	// Volumes are whole volumes and Places folders on a volume outside
	// the shares ("volume/folder"), both since 0.41.0. Older versions
	// ignore them; a job with nothing else fails there visibly.
	Volumes     []string    `json:"volumes,omitempty"`
	Places      []string    `json:"places,omitempty"`
	Destination Destination `json:"destination"`
	Enabled     bool        `json:"enabled"`
	// Every is hourly, daily, weekly or monthly; see schedule.go. Time,
	// Weekday (0 = Sunday) are the first time and the one weekday; the lists
	// carry more of them. WindowFrom and WindowTo limit when runs start.
	Every      string   `json:"every"`
	Interval   int      `json:"interval,omitempty"`
	Weekday    int      `json:"weekday"`
	Weekdays   []int    `json:"weekdays,omitempty"`
	MonthDay   int      `json:"monthDay,omitempty"`
	Time       string   `json:"time"`
	Times      []string `json:"times,omitempty"`
	WindowFrom string   `json:"windowFrom,omitempty"`
	WindowTo   string   `json:"windowTo,omitempty"`
	Keep       Keep     `json:"keep"`
	// Copy holds the options of readable copies; nil is a plain copy that
	// only adds and replaces. Older versions ignore it.
	Copy *CopyOptions `json:"copy,omitempty"`
	// Bandwidth limits how fast a run moves data; nil is no limit.
	Bandwidth *Bandwidth `json:"bandwidth,omitempty"`
	// Password encrypts the backup. It is made once and never changes.
	Password string `json:"password,omitempty"`
}

// State is what is known about the runs of a job.
type State struct {
	StatusError        string                    `json:"statusError,omitempty"`
	ApplicationRestore *ApplicationRestoreResult `json:"applicationRestore,omitempty"`
	Probe              *ProbeResult              `json:"probe,omitempty"`
	Running            bool                      `json:"running"`
	// Phase is backup, cleanup or restore while Running.
	Phase string `json:"phase"`
	// Queued is set while a started job waits for another one to end.
	Queued     bool    `json:"queued,omitempty"`
	Percent    float64 `json:"percent"`
	BytesDone  uint64  `json:"bytesDone"`
	BytesTotal uint64  `json:"bytesTotal"`
	// BandwidthLimit is the limit in MiB/s the running job keeps to now;
	// 0 while it runs without one and when it does not run.
	BandwidthLimit int `json:"bandwidthLimit,omitempty"`
	// LastRun is the last attempt, LastOK the last one that worked.
	LastRun   *time.Time `json:"lastRun"`
	LastOK    *time.Time `json:"lastOK"`
	LastError string     `json:"lastError"`
	// LastErrorCode names known causes of LastError, so that the page can
	// say it in the language of the viewer; empty for others.
	LastErrorCode string `json:"lastErrorCode,omitempty"`
	// FailingSince is the first failed attempt after the last backup that
	// worked; empty while backups work.
	FailingSince *time.Time `json:"failingSince,omitempty"`
	// Warning is set when a backup was made but could not read everything.
	Warning string `json:"warning"`
	// Skipped counts the entries a plain copy could not save in its last
	// run; Problems names them. Sockets, FIFOs and device files hold no
	// data and are not counted; symbolic links are kept as links.
	Skipped int64 `json:"skipped,omitempty"`
	// Problems says what a plain copy could not save in its last run, as
	// codes with the entries they hit, for the page to show in the
	// language of the viewer. Warning carries the same in English for
	// older pages and other readers.
	Problems []Problem `json:"problems,omitempty"`
	// Bytes is how much data the last backup covered.
	Bytes uint64 `json:"bytes"`
	// RestoredTo is the folder of the last restore; RestoreError why the
	// last one failed. A failed restore is not a failed backup.
	RestoredTo   string `json:"restoredTo"`
	RestoreError string `json:"restoreError,omitempty"`
	// LastCheck is when the repository was last checked; CheckError what
	// restic found then, empty if all was well.
	LastCheck  *time.Time `json:"lastCheck,omitempty"`
	CheckError string     `json:"checkError,omitempty"`
}

// Source is a share that can be backed up.
type Source struct {
	Name   string `json:"name"`
	Volume string `json:"volume"`
	Path   string `json:"-"`
}

// Snapshot is one backup in the place it went to.
type Snapshot struct {
	ID    string    `json:"id"`
	Time  time.Time `json:"time"`
	Paths []string  `json:"paths"`
	Bytes uint64    `json:"bytes"`
}

// Manager knows the jobs, runs them on time and restores from them.
type Manager struct {
	store      *config.Store
	smbTargets []SMBTarget
	// restic is the program; mountRoot where volumes are mounted; workDir
	// where the SSH key lies while a job runs.
	restic, mountRoot, workDir string
	// configDir is the folder of the settings; it is saved if a job asks.
	configDir string
	// mountsFile lists what is mounted; /proc/self/mounts.
	mountsFile string
	now        func() time.Time

	// Disabled runs nothing: in development mode titand runs on the
	// developer's own machine.
	Disabled bool
	// Sources returns the shares whose volume is mounted right now.
	Sources             func() []Source
	PrepareApplications func(context.Context, Job, string, []string) ([]string, func() error, error)
	// WithUSB pins a mounted filesystem for the complete copy operation.
	WithUSB func(context.Context, string, func(*os.Root) error) error
	// LiveFiles returns the files a running guest writes to all the time:
	// the image files of the disks of running VMs, as absolute paths. A
	// plain copy reads them from a snapshot where it can. Nil: none.
	LiveFiles func() []string
	// SnapshotSource makes a read-only snapshot of a mounted volume for
	// one run and returns its folder (the volume as it was at that moment)
	// and what removes it again. It fails for a volume that has no
	// snapshots (not Btrfs). Nil: no snapshots.
	SnapshotSource func(ctx context.Context, volume string) (dir string, release func(), err error)
	// OnResult is told of each backup that worked and of the first failure
	// of a series; OnRecovered when a job works again after failing.
	OnResult    func(job string, err error)
	OnRecovered func(job string)
	// OnFreed is told how many old versions a run removed because the
	// target was full.
	OnFreed func(job string, versions int)
	// ended is told of every run; tests wait for it.
	ended func(job string, err error)
	// OnCheck hears how a check of a repository ended.
	OnCheck func(job string, err error)
	OnProbe func(job string, err error)
	// OnOverdue is told once when a job has gone without a backup that
	// worked for too long (see overdue); again only after one worked.
	OnOverdue func(job string)
	// OnIncomplete is told when a backup was made without some of its
	// shares or places because they were not there.
	OnIncomplete   func(job string)
	ScheduledStart func(string) error
	// Timezone names the time zone of the NAS. Go keeps time.Local from the
	// start; schedules follow a change without a restart of titand.
	Timezone func() string
	overdue  map[string]bool
	// unavailable is since when a job could not run because none of its
	// shares was there; only in the RAM.
	unavailable map[string]time.Time
	zone        *time.Location

	// queue lets one job work at a time.
	queue sync.Mutex
	// runs counts the started jobs until their state is written; tests
	// wait for it before their temporary directory goes away.
	runs sync.WaitGroup
	// hostsMu keeps two calls from writing known_hosts at once.
	hostsMu sync.Mutex

	mu      sync.Mutex
	loaded  bool
	jobs    []Job
	states  map[string]*State
	cancels map[string]context.CancelFunc
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, restic: "restic", mountRoot: "/mnt", workDir: "/run/titanos-backup", configDir: "/config", mountsFile: "/proc/self/mounts", now: time.Now,
		states: map[string]*State{}, cancels: map[string]context.CancelFunc{}}
}

// Load reads the jobs from /config.
func (m *Manager) Load() error {
	var jobs []Job
	err := m.store.ReadJSON(jobsFile, &jobs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for i := range jobs {
		job := &jobs[i]
		if saved := job.CoordinatedSelection; saved != nil {
			if len(job.Applications) == 0 {
				return invalid("coordinated selection has no applications")
			}
			job.Sources = saved.Sources
			job.Extras = saved.Extras
			job.Folders = saved.Folders
			job.Skip = saved.Skip
			job.Patterns = saved.Patterns
			job.CoordinatedSelection = nil
		}
	}
	targets, err := m.loadSMBTargets()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.smbTargets = targets
	m.jobs, m.loaded = jobs, true
	return nil
}

func (m *Manager) save(jobs []Job) error {
	if !m.loaded {
		return ErrNotLoaded
	}
	if jobs == nil {
		jobs = []Job{}
	}
	stored := slices.Clone(jobs)
	for i := range stored {
		job := &stored[i]
		if len(job.Applications) > 0 {
			job.CoordinatedSelection = &CoordinatedSelection{job.Sources, job.Extras, job.Folders, job.Skip, job.Patterns}
			job.Sources = nil
			job.Extras = nil
			job.Folders = nil
			job.Skip = nil
			job.Patterns = nil
		}
	}
	if err := m.store.WriteJSON(jobsFile, stored); err != nil {
		return err
	}
	m.jobs = jobs
	return nil
}

// local returns a moment in the time zone of the NAS as it is set now.
func (m *Manager) local(at time.Time) time.Time {
	if m.Timezone == nil {
		return at
	}
	name := m.Timezone()
	m.mu.Lock()
	zone := m.zone
	m.mu.Unlock()
	if zone == nil || zone.String() != name {
		loaded, err := time.LoadLocation(name)
		if err != nil {
			return at
		}
		zone = loaded
		m.mu.Lock()
		m.zone = zone
		m.mu.Unlock()
	}
	return at.In(zone)
}

// --- the SSH key of this NAS ------------------------------------------------

// PlainCopy reports whether a kind of destination holds readable copies
// rather than restic versions.
func PlainCopy(kind string) bool { return plainCopy(kind) }

func plainCopy(kind string) bool {
	return kind == "usb-copy" || kind == "smb-copy" || kind == "local-copy"
}

// Busy reports whether a backup or a restore runs right now.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.states {
		if state.Running {
			return true
		}
	}
	return false
}

// CachedViews exposes only known RAM states for telemetry. A nil state is
// deliberately unknown; this method never opens an archive or wakes a disk.
func (m *Manager) CachedViews() []View {
	now := m.local(m.now())
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]View, 0, len(m.jobs))
	for _, job := range m.jobs {
		shown, set := withoutSecrets(job)
		v := View{Job: shown, SecretSet: set}
		if state := m.states[job.ID]; state != nil {
			v.State = *state
			v.StateKnown = state.StatusError == ""
		}
		if next, ok := job.NextRun(now); ok {
			v.Next = &next
		}
		out = append(out, v)
	}
	return out
}
