// Package backup copies shares to somewhere else: another volume, another
// server, or storage in the cloud.
//
// The work is done by restic. A backup is encrypted with a password that
// TitanOS makes for each job; whoever holds the place it goes to cannot
// read it. Without that password a backup is worthless, so the web UI
// tells its owner to write it down.
package backup

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
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

var (
	// ErrDisabled means titand runs in development mode.
	ErrDisabled = errors.New("backup: not available in development mode")
	// ErrNotLoaded means /config could not be read.
	ErrNotLoaded = errors.New("backup: jobs not loaded from /config")
	// ErrInvalid means a job is not acceptable; the message says why.
	ErrInvalid = errors.New("backup: invalid job")
	// ErrExists means there is a job of this name already.
	ErrExists = errors.New("backup: a job with this name exists")
	// ErrNotFound means there is no such job.
	ErrNotFound = errors.New("backup: no such job")
	// ErrRunning means the job is at work right now.
	ErrRunning = errors.New("backup: the job is running")
	// ErrSources means none of the shares of the job is there right now.
	ErrSources = errors.New("backup: none of the shares is available")
)

// Destination is where a backup goes.
type Destination struct {
	// Type is local, usb-copy, s3, sftp or rest.
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

// Keep says how many backups stay: the newest of each of that many days,
// weeks and months. All zero keeps everything.
type Keep struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

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
	Sources     []string            `json:"sources"`
	Folders     map[string][]string `json:"folders,omitempty"`
	Skip        []string            `json:"skip,omitempty"`
	Patterns    []string            `json:"patterns,omitempty"`
	Extras      []string            `json:"extras,omitempty"`
	Destination Destination         `json:"destination"`
	Enabled     bool                `json:"enabled"`
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
	// Skipped counts the links and special files (FIFOs, sockets, devices)
	// a plain copy left out in its last run; they are named in Warning.
	Skipped int64 `json:"skipped,omitempty"`
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
	// OnResult is told of each backup that worked and of the first failure
	// of a series; OnRecovered when a job works again after failing.
	OnResult    func(job string, err error)
	OnRecovered func(job string)
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

var (
	clockForm  = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	bucketForm = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	hostForm   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$|^\[[0-9a-fA-F:]+\]$`)
	userForm   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9._-]{0,63}$`)
	plainForm  = regexp.MustCompile(`^[\x21-\x7e]{1,500}$`)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func webAddress(address string) bool {
	parsed, err := url.Parse(address)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil && plainForm.MatchString(address)
}

// check tells whether a job makes sense. shares and volumes are what exists.
func check(job Job, shares []Source, volumes []string) error {
	if job.CoordinatedSelection != nil {
		return invalid("coordinatedSelection is reserved for persisted state")
	}
	if !naming.ValidBackup(job.ID) {
		return invalid("name")
	}
	if len(job.Applications) > 50 {
		return invalid("too many applications")
	}
	seenApps := map[string]bool{}
	for _, app := range job.Applications {
		if !slices.Contains([]string{"docker", "vm"}, app.Kind) || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`).MatchString(app.Name) || !slices.Contains([]string{"stop", "postgres-export"}, app.Method) || (app.Kind == "vm" && app.Method != "stop") || (!job.AllowStop && app.Method == "stop") || seenApps[app.Kind+":"+app.Name] || plainCopy(job.Destination.Type) {
			return invalid("application backup configuration or stop consent")
		}
		seenApps[app.Kind+":"+app.Name] = true
	}
	if err := checkSelection(job); err != nil {
		return err
	}
	sourceVolumes := []string{}
	for _, name := range job.Sources {
		at := slices.IndexFunc(shares, func(s Source) bool { return s.Name == name })
		if at < 0 {
			return invalid("share %q is not there", name)
		}
		sourceVolumes = append(sourceVolumes, shares[at].Volume)
	}
	if err := checkSchedule(job); err != nil {
		return err
	}
	if err := checkBandwidth(job.Bandwidth); err != nil {
		return err
	}
	for _, count := range []int{job.Keep.Daily, job.Keep.Weekly, job.Keep.Monthly} {
		if count < 0 || count > 999 {
			return invalid("how many to keep")
		}
	}
	d := job.Destination
	switch d.Type {
	case "usb-copy", "smb-copy":
		if (d.Type == "usb-copy" && !regexp.MustCompile(`^[A-Za-z0-9-]{4,80}$`).MatchString(d.DeviceID)) || (d.Type == "smb-copy" && !naming.ValidBackup(d.TargetID)) || !relative(d.Folder) || len(job.Extras) != 0 {
			return invalid("USB identity, folder or unsupported extra source")
		}
		if len(d.DeviceName) > 64 || strings.ContainsFunc(d.DeviceName, func(r rune) bool { return r < ' ' || r == 0x7f }) {
			return invalid("drive name")
		}
		for _, pattern := range job.Patterns {
			if _, err := path.Match(pattern, ""); err != nil || strings.Contains(pattern, "/") || strings.Contains(pattern, "**") {
				return invalid("USB copy patterns must match single file or folder names")
			}
		}
	case "local":
		if !slices.Contains(volumes, d.Volume) {
			return invalid("volume")
		}
		// A copy on the same disks does not survive what kills the original.
		if slices.Contains(sourceVolumes, d.Volume) {
			return invalid("the backup must go to another volume than the shares")
		}
		if d.Folder == "" || path.IsAbs(d.Folder) || path.Clean(d.Folder) != d.Folder || strings.HasPrefix(d.Folder, "..") || strings.ContainsAny(d.Folder, "\x00\n") {
			return invalid("folder")
		}
	case "s3":
		if !webAddress(d.Endpoint) || !bucketForm.MatchString(d.Bucket) || d.Prefix != "" && (!plainForm.MatchString(d.Prefix) || strings.Contains(d.Prefix, "..")) {
			return invalid("address or bucket")
		}
		if !plainForm.MatchString(d.AccessKey) || d.SecretKey == "" {
			return invalid("access key")
		}
	case "sftp":
		if !hostForm.MatchString(d.Host) || d.Port < 1 || d.Port > 65535 || !userForm.MatchString(d.User) {
			return invalid("server, port or user")
		}
		if !plainForm.MatchString(d.Path) || strings.HasPrefix(d.Path, "-") {
			return invalid("path on the server")
		}
	case "rest":
		if !webAddress(d.URL) || d.Username != "" && !userForm.MatchString(d.Username) {
			return invalid("address")
		}
	default:
		return invalid("kind of destination")
	}
	return nil
}

// withoutSecrets returns a job as the web UI may see it, and which of its
// secrets are set.
func withoutSecrets(job Job) (Job, map[string]bool) {
	set := map[string]bool{"secretKey": job.Destination.SecretKey != "", "password": job.Destination.Password != ""}
	job.Password, job.Destination.SecretKey, job.Destination.Password = "", "", ""
	return job, set
}

// View is a job with its state, for the web UI.
type View struct {
	StateKnown bool            `json:"stateKnown"`
	Job        Job             `json:"job"`
	SecretSet  map[string]bool `json:"secretSet"`
	State      State           `json:"state"`
	// Next is when the job runs next; empty if it is off.
	Next *time.Time `json:"next"`
}

// List returns the jobs with what is known about their runs.
func (m *Manager) List() []View {
	m.mu.Lock()
	jobs := slices.Clone(m.jobs)
	m.mu.Unlock()
	views := []View{}
	for _, job := range jobs {
		shown, set := withoutSecrets(job)
		state := m.state(job)
		view := View{StateKnown: state.StatusError == "", Job: shown, SecretSet: set, State: state}
		if next, ok := job.NextRun(m.local(m.now())); ok {
			view.Next = &next
		}
		views = append(views, view)
	}
	return views
}

// USBProgress returns only cached run states, without accessing disks or /config.
func (m *Manager) USBProgress() map[string]State {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]State)
	for _, job := range m.jobs {
		if job.Destination.Type == "usb-copy" {
			if state := m.states[job.ID]; state != nil {
				result[job.ID] = *state
			}
		}
	}
	return result
}

func (m *Manager) find(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if job.ID == id {
			return job, true
		}
	}
	return Job{}, false
}

func defaults(job *Job) {
	if job.Destination.Type == "sftp" && job.Destination.Port == 0 {
		job.Destination.Port = 22
	}
	if (job.Destination.Type == "local" || plainCopy(job.Destination.Type)) && job.Destination.Folder == "" {
		job.Destination.Folder = "backups/" + job.ID
	}
	if job.Every == "" {
		job.Every = "daily"
	}
	job.Bandwidth = normalBandwidth(job.Bandwidth)
}

// reservedNames are fixed routes next to /api/v1/backups/{id}.
var reservedNames = []string{"smb", "target", "progress", "folders", "ssh-key"}

// Create adds a job and makes the password its backups are encrypted with.
func (m *Manager) Create(job Job, volumes []string) error {
	defaults(&job)
	if err := check(job, m.sources(), volumes); err != nil {
		return err
	}
	if slices.ContainsFunc(reservedNames, func(name string) bool { return strings.EqualFold(name, job.ID) }) {
		return invalid("name is used by the backup pages")
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	job.Password = base64.RawURLEncoding.EncodeToString(secret)
	if plainCopy(job.Destination.Type) {
		job.Password = ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.ContainsFunc(m.jobs, func(j Job) bool { return strings.EqualFold(j.ID, job.ID) }) {
		return ErrExists
	}
	if job.Destination.Type == "smb-copy" && !slices.ContainsFunc(m.smbTargets, func(t SMBTarget) bool { return t.ID == job.Destination.TargetID }) {
		return invalid("SMB target")
	}
	return m.save(append(slices.Clone(m.jobs), job))
}

// Update changes a job. Its name and password stay; a secret that comes
// empty keeps its stored value.
func (m *Manager) Update(id string, job Job, volumes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	at := slices.IndexFunc(m.jobs, func(j Job) bool { return j.ID == id })
	if at < 0 {
		return ErrNotFound
	}
	if state := m.states[id]; state != nil && state.Running {
		return ErrRunning
	}
	old := m.jobs[at]
	job.ID, job.Password = old.ID, old.Password
	if (plainCopy(job.Destination.Type)) != (plainCopy(old.Destination.Type)) {
		return invalid("create a separate job to change the backup format")
	}
	// An empty secret keeps the stored one, but only for the same server and
	// account: restic sends the REST password to whatever address it is given.
	d, was := &job.Destination, old.Destination
	if d.SecretKey == "" && d.Type == was.Type && d.Endpoint == was.Endpoint && d.AccessKey == was.AccessKey {
		d.SecretKey = was.SecretKey
	}
	if d.Password == "" && d.Type == was.Type && d.URL == was.URL && d.Username == was.Username {
		d.Password = was.Password
	}
	defaults(&job)
	if err := check(job, m.sources(), volumes); err != nil {
		return err
	}
	if job.Destination.Type == "smb-copy" && !slices.ContainsFunc(m.smbTargets, func(t SMBTarget) bool { return t.ID == job.Destination.TargetID }) {
		return invalid("SMB target")
	}
	jobs := slices.Clone(m.jobs)
	jobs[at] = job
	return m.save(jobs)
}

// Delete forgets a job. What it backed up stays where it is.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	at := slices.IndexFunc(m.jobs, func(j Job) bool { return j.ID == id })
	if at < 0 {
		return ErrNotFound
	}
	if state := m.states[id]; state != nil && state.Running {
		return ErrRunning
	}
	delete(m.states, id)
	return m.save(slices.Delete(slices.Clone(m.jobs), at, at+1))
}

// Password returns the password a job's backups are encrypted with.
func (m *Manager) Password(id string) (string, error) {
	job, ok := m.find(id)
	if !ok {
		return "", ErrNotFound
	}
	return job.Password, nil
}

func (m *Manager) sources() []Source {
	if m.Sources == nil {
		return nil
	}
	return m.Sources()
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

// PublicKey returns the key this NAS shows to SFTP servers, making and
// storing it first if there is none. It goes into authorized_keys there.
func (m *Manager) PublicKey() (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return "", ErrNotLoaded
	}
	if data, err := m.store.Read(keyFile + ".pub"); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(private, "titanos-backup")
	if err != nil {
		return "", err
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " titanos-backup"
	if err := m.store.Write(keyFile, pem.EncodeToMemory(block)); err != nil {
		return "", err
	}
	if err := m.store.Write(keyFile+".pub", []byte(line+"\n")); err != nil {
		return "", err
	}
	return line, nil
}

// --- running restic ---------------------------------------------------------

// stateFile is where a job remembers its runs: on the volume of its first
// share that is there.
func (m *Manager) stateFile(job Job) string {
	if volume := m.homeVolume(job); volume != "" {
		return filepath.Join(m.mountRoot, volume, stateDir, job.ID+".json")
	}
	return ""
}

func (m *Manager) state(job Job) State {
	m.mu.Lock()
	if state := m.states[job.ID]; state != nil {
		defer m.mu.Unlock()
		return *state
	}
	m.mu.Unlock()
	var state State
	file := m.stateFile(job)
	if file == "" {
		// Its shares are not there, so neither is what it remembers.
		// Nothing is kept: the next look may find them.
		state.StatusError = "backup state volume unavailable"
		return state
	}
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		state.StatusError = "backup state could not be read"
		return state
	}
	if err == nil && json.Unmarshal(data, &state) != nil {
		state = State{StatusError: "backup state is invalid"}
		return state
	}

	state.Running, state.Phase, state.Percent, state.BandwidthLimit = false, "", 0, 0
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.states[job.ID]; existing != nil {
		return *existing
	}
	m.states[job.ID] = &state
	return state
}

func (m *Manager) update(job Job, change func(*State)) {
	m.state(job) // makes sure it is loaded
	m.mu.Lock()
	state := m.states[job.ID]
	if state == nil {
		state = &State{}
		m.states[job.ID] = state
	}
	change(state)
	copied := *state
	m.mu.Unlock()
	if copied.Running {
		return // written when the run ends
	}
	if file := m.stateFile(job); file != "" {
		if err := writeState(file, copied); err != nil {
			slog.Warn("backup state not saved", "job", job.ID, "err", err)
		}
	}
}

// writeState replaces the state file of a job durably: the new content is
// synced before it takes the old name, and the rename itself afterwards.
func writeState(file string, state State) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// rememberHosts adds the lines ssh learnt to the stored known_hosts. Two
// calls may have learnt something at the same time; neither loses the other's.
func (m *Manager) rememberHosts(learnt []byte) {
	m.hostsMu.Lock()
	defer m.hostsMu.Unlock()
	stored, err := m.store.Read(hostFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("backup: key of the server not remembered", "err", err)
		return
	}
	merged := strings.TrimRight(string(stored), "\n")
	known := map[string]bool{}
	for _, line := range strings.Split(merged, "\n") {
		known[line] = true
	}
	added := false
	for _, line := range strings.Split(string(learnt), "\n") {
		if line = strings.TrimSpace(line); line != "" && !known[line] {
			known[line] = true
			if merged != "" {
				merged += "\n"
			}
			merged += line
			added = true
		}
	}
	if !added {
		return
	}
	if err := m.store.Write(hostFile, []byte(merged+"\n")); err != nil {
		slog.Warn("backup: key of the server not remembered", "err", err)
	}
}

// repository returns the address restic takes, extra arguments and the
// environment for a job. cleanup removes what was put in place for it.
func (m *Manager) repository(job Job) (repo string, args, env []string, cleanup func(), err error) {
	cleanup = func() {}
	d := job.Destination
	if plainCopy(d.Type) {
		return "", nil, nil, cleanup, invalid("plain copies have no restic snapshots; restore files with the file manager")
	}
	cache := ""
	if volume := m.homeVolume(job); volume != "" {
		// restic's cache is large; it never goes into the RAM.
		cache = filepath.Join(m.mountRoot, volume, stateDir, "cache")
	}
	env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "RESTIC_PASSWORD=" + job.Password, "RESTIC_PROGRESS_FPS=1"}
	if cache != "" {
		env = append(env, "RESTIC_CACHE_DIR="+cache)
	} else {
		args = append(args, "--no-cache")
	}
	switch d.Type {
	case "local":
		if !slices.ContainsFunc(m.mounted(), func(volume string) bool { return volume == d.Volume }) {
			return "", nil, nil, cleanup, fmt.Errorf("the volume %s of the backup is not mounted", d.Volume)
		}
		repo = filepath.Join(m.mountRoot, d.Volume, d.Folder)
	case "s3":
		repo = "s3:" + strings.TrimRight(d.Endpoint, "/") + "/" + d.Bucket
		if d.Prefix != "" {
			repo += "/" + strings.Trim(d.Prefix, "/")
		}
		env = append(env, "AWS_ACCESS_KEY_ID="+d.AccessKey, "AWS_SECRET_ACCESS_KEY="+d.SecretKey)
	case "rest":
		repo = "rest:" + d.URL
		env = append(env, "RESTIC_REST_USERNAME="+d.Username, "RESTIC_REST_PASSWORD="+d.Password)
	case "sftp":
		if _, err := m.PublicKey(); err != nil {
			return "", nil, nil, cleanup, err
		}
		// ssh wants its key in a file only root can read; /config is
		// asleep most of the time. Both lie in the RAM while the job runs.
		// Each call has a folder of its own: a look at the snapshots while
		// the job runs must not take the key away from the backup.
		if err := os.MkdirAll(m.workDir, 0o700); err != nil {
			return "", nil, nil, cleanup, err
		}
		dir, err := os.MkdirTemp(m.workDir, job.ID+"-")
		if err != nil {
			return "", nil, nil, cleanup, err
		}
		cleanup = func() { os.RemoveAll(dir) }
		key, err := m.store.Read(keyFile)
		if err != nil {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		// A known_hosts that cannot be read is not an empty one: ssh would
		// take any server under the name.
		hosts, err := m.store.Read(hostFile)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "key"), key, 0o600); err != nil {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "known_hosts"), hosts, 0o600); err != nil {
			cleanup()
			return "", nil, nil, func() {}, err
		}
		cleanup = func() {
			// A server seen for the first time is remembered: from then
			// on only that one is accepted under its name.
			if now, err := os.ReadFile(filepath.Join(dir, "known_hosts")); err == nil && string(now) != string(hosts) && len(now) > 0 {
				m.rememberHosts(now)
			}
			os.RemoveAll(dir)
		}
		repo = fmt.Sprintf("sftp:%s@%s:%s", d.User, d.Host, d.Path)
		command := fmt.Sprintf("ssh -i %s -o UserKnownHostsFile=%s -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ServerAliveInterval=30 -p %d %s@%s -s sftp",
			filepath.Join(dir, "key"), filepath.Join(dir, "known_hosts"), d.Port, d.User, d.Host)
		args = append(args, "-o", "sftp.command="+command)
	}
	env = append(env, "RESTIC_REPOSITORY="+repo)
	return repo, args, env, cleanup, nil
}

func (m *Manager) mounted() []string {
	var volumes []string
	for _, source := range m.sources() {
		if !slices.Contains(volumes, source.Volume) {
			volumes = append(volumes, source.Volume)
		}
	}
	// A volume without shares can be the place a backup goes to.
	entries, _ := os.ReadDir(m.mountRoot)
	mounts, _ := os.ReadFile(m.mountsFile)
	for _, entry := range entries {
		if strings.Contains(string(mounts), " "+filepath.Join(m.mountRoot, entry.Name())+" ") && !slices.Contains(volumes, entry.Name()) {
			volumes = append(volumes, entry.Name())
		}
	}
	return volumes
}

// restic runs the program and hands each line it prints to line. It
// returns what it wrote as errors.
func (m *Manager) run(ctx context.Context, env, args []string, line func(string)) (string, error) {
	return m.runFiles(ctx, env, args, line, nil)
}
func (m *Manager) runFiles(ctx context.Context, env, args []string, line func(string), files []*os.File) (string, error) {
	cmd := exec.CommandContext(ctx, m.restic, args...)
	cmd.Env = env
	cmd.ExtraFiles = files
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var problems strings.Builder
	cmd.Stderr = &limited{w: &problems, left: 8000}
	// A cancelled job ends restic and what it started (ssh, for SFTP):
	// they get a group of their own, and the signal goes to all of it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = 20 * time.Second
	if err := cmd.Start(); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		if line != nil {
			line(scanner.Text())
		}
	}
	io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	return strings.TrimSpace(problems.String()), err
}

// limited keeps the first bytes of what is written to it.
type limited struct {
	w    io.Writer
	left int
}

func (l *limited) Write(p []byte) (int, error) {
	if take := min(len(p), l.left); take > 0 {
		l.w.Write(p[:take])
		l.left -= take
	}
	return len(p), nil
}

// lastLines returns the end of what restic said, without the password
// should it ever echo its environment.
func lastLines(text, secret string) string {
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "…")
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

// Start begins a backup now. It returns at once.
func (m *Manager) Start(id string) error {
	if m.Disabled {
		return ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return ErrNotFound
	}
	return m.begin(job, "backup", func(ctx context.Context) error { return m.backup(ctx, job) })
}

// begin runs work for a job in the background, one job at a time.
func (m *Manager) begin(job Job, phase string, work func(context.Context) error) error {
	m.state(job)
	m.mu.Lock()
	state := m.states[job.ID]
	if state == nil {
		state = &State{}
		m.states[job.ID] = state
	}
	if state.Running {
		m.mu.Unlock()
		return ErrRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.Running, state.Queued, state.Phase, state.Percent, state.BytesDone, state.BytesTotal, state.BandwidthLimit = true, true, phase, 0, 0, 0, 0
	m.cancels[job.ID] = cancel
	m.mu.Unlock()
	go func() {
		m.queue.Lock()
		m.update(job, func(s *State) { s.Queued = false })
		err := work(ctx)
		m.queue.Unlock()
		cancelled := ctx.Err() != nil
		var firstFailure, recovered bool
		cancel()
		now := m.now()
		m.mu.Lock()
		delete(m.cancels, job.ID)
		m.mu.Unlock()
		m.update(job, func(s *State) {
			s.Running, s.Queued, s.Phase, s.Percent, s.BandwidthLimit = false, false, "", 0, 0
			if phase != "backup" {
				// A failed check is kept as CheckError, a failed restore as
				// RestoreError: neither is a failed backup.
				if phase == "restore" {
					switch {
					case err == nil:
						s.RestoreError = ""
					case !cancelled:
						s.RestoreError = err.Error()
					}
				}
				return
			}
			s.LastRun, s.LastError, s.LastErrorCode = &now, "", ""
			switch {
			case cancelled:
				s.LastError = errCancelled
			case err != nil:
				s.LastError, s.LastErrorCode = err.Error(), errorCode(err)
				if s.FailingSince == nil {
					s.FailingSince, firstFailure = &now, true
				}
			default:
				recovered = s.FailingSince != nil
				s.LastOK, s.FailingSince = &now, nil
			}
		})
		if err != nil && !cancelled {
			slog.Warn("backup job failed", "job", job.ID, "phase", phase, "err", err)
		}
		// One message when a job starts failing and one when it works again,
		// not one per attempt: an hourly job with its drive unplugged
		// would otherwise report every hour.
		if phase == "backup" && !cancelled && m.OnResult != nil && (err == nil || firstFailure) {
			m.OnResult(job.ID, err)
		}
		if recovered && m.OnRecovered != nil {
			m.OnRecovered(job.ID)
		}
		if phase == "backup" && !cancelled && m.ended != nil {
			m.ended(job.ID, err)
		}
	}()
	return nil
}

// Stop cancels what a job is doing.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.cancels[id]
	if !ok {
		return ErrNotFound
	}
	cancel()
	return nil
}

func (m *Manager) backup(ctx context.Context, job Job) (result error) {
	if plainCopy(job.Destination.Type) {
		if job.Destination.Type == "smb-copy" {
			target, err := m.smbTarget(job.Destination.TargetID)
			if err != nil {
				return err
			}
			return m.withSMB(ctx, target, func(root *os.Root) error { return m.copyTo(ctx, job, root) })
		}
		return m.copyUSB(ctx, job)
	}
	sel := m.selection(job)
	if len(sel.paths) == 0 && len(job.Applications) == 0 {
		return ErrSources
	}
	if len(job.Applications) > 0 && m.PrepareApplications == nil {
		return invalid("application backups unavailable")
	}
	_, base, env, cleanup, err := m.repository(job)
	if err != nil {
		return err
	}
	defer cleanup()
	// restic cannot change its limit while it runs: the one that holds at
	// the start holds for the whole run, also past a window boundary.
	limit := job.Bandwidth.at(m.local(m.now()))
	base = append(base, resticLimits(limit)...)
	m.update(job, func(s *State) { s.BandwidthLimit = limit })

	// A repository that is not there yet is made; one that is, is used. This
	// comes before applications are stopped: a target that cannot be reached
	// must not stop them on every retry.
	if looked, err := m.run(ctx, env, append(slices.Clone(base), "cat", "config", "--quiet"), nil); err != nil {
		if made, err := m.run(ctx, env, append(slices.Clone(base), "init"), nil); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Both answers: the first says why it could not be opened.
			return fmt.Errorf("the place for the backup cannot be reached or set up:\n%s", lastLines(looked+"\n"+made, job.Password))
		}
	}
	// What a run that was cut short left locked.
	m.run(ctx, env, append(slices.Clone(base), "unlock"), nil)

	if len(job.Applications) > 0 {
		extra, finish, err := m.PrepareApplications(ctx, job, m.homeVolume(job), sel.paths)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, finish()) }()
		sel.paths = append(sel.paths, extra...)
	}
	paths := sel.paths
	if len(paths) == 0 {
		return ErrSources
	}
	slog.Info("backup started", "job", job.ID, "shares", len(paths))

	args := append(slices.Clone(base), "backup", "--json", "--tag", "titanos", "--exclude", ".recycle", "--exclude", "*.titanos-part")
	for _, exclude := range sel.excludes {
		args = append(args, "--exclude", exclude)
	}
	args = append(args, paths...)
	var total uint64
	out, err := m.run(ctx, env, args, func(line string) {
		var message struct {
			Type    string  `json:"message_type"`
			Percent float64 `json:"percent_done"`
			Done    uint64  `json:"bytes_done"`
			Total   uint64  `json:"total_bytes"`
			Size    uint64  `json:"total_bytes_processed"`
		}
		if json.Unmarshal([]byte(line), &message) != nil {
			return
		}
		switch message.Type {
		case "status":
			m.update(job, func(s *State) {
				s.Percent, s.BytesDone, s.BytesTotal = 100*message.Percent, message.Done, message.Total
			})
		case "summary":
			total = message.Size
		}
	})
	warning := ""
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitCode() == 3:
		// A backup was made, but some files could not be read.
		warning = lastLines(out, job.Password)
	case err != nil:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New(lastLines(out, job.Password))
	}
	if len(sel.notes) > 0 {
		missing := "Not found, not saved: " + strings.Join(sel.notes, ", ")
		warning = strings.TrimSpace(warning + "\n" + missing)
	}
	// Old backups are the only copy of what is missing now: "keep the
	// newest of N days" would forget them after N such runs.
	if len(sel.missing) > 0 {
		missing := "Not available, not saved: " + strings.Join(sel.missing, ", ") + "\nOlder backups are all kept until it is back."
		warning = strings.TrimSpace(warning + "\n" + missing)
	}
	m.update(job, func(s *State) { s.Bytes, s.Warning, s.Phase, s.Percent = total, warning, "cleanup", 100 })
	if len(sel.missing) > 0 && m.OnIncomplete != nil {
		m.OnIncomplete(job.ID)
	}

	if job.Keep.Daily+job.Keep.Weekly+job.Keep.Monthly > 0 && len(sel.missing) == 0 {
		args := append(slices.Clone(base), "forget", "--tag", "titanos", "--group-by", "tags", "--prune")
		for flag, count := range map[string]int{"--keep-daily": job.Keep.Daily, "--keep-weekly": job.Keep.Weekly, "--keep-monthly": job.Keep.Monthly} {
			if count > 0 {
				args = append(args, flag, fmt.Sprint(count))
			}
		}
		if out, err := m.run(ctx, env, args, nil); err != nil && ctx.Err() == nil {
			// The backup itself is there; say so instead of calling it failed.
			m.update(job, func(s *State) { s.Warning = strings.TrimSpace(s.Warning + "\n" + lastLines(out, job.Password)) })
		}
	}
	slog.Info("backup finished", "job", job.ID, "bytes", total)
	return nil
}

// Snapshots lists the backups of a job, the newest first.
func (m *Manager) Snapshots(ctx context.Context, id string) ([]Snapshot, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return nil, ErrNotFound
	}
	_, base, env, cleanup, err := m.repository(job)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var output strings.Builder
	out, err := m.run(ctx, env, append(base, "snapshots", "--json", "--no-lock"), func(line string) { output.WriteString(line) })
	if err != nil {
		return nil, errors.New(lastLines(out, job.Password))
	}
	var raw []struct {
		ID      string    `json:"short_id"`
		Time    time.Time `json:"time"`
		Paths   []string  `json:"paths"`
		Summary *struct {
			Bytes uint64 `json:"total_bytes_processed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output.String()), &raw); err != nil {
		return nil, err
	}
	list := []Snapshot{}
	for _, entry := range raw {
		snapshot := Snapshot{ID: entry.ID, Time: entry.Time, Paths: entry.Paths}
		if entry.Summary != nil {
			snapshot.Bytes = entry.Summary.Bytes
		}
		list = append(list, snapshot)
	}
	slices.Reverse(list)
	return list, nil
}

var snapshotForm = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

// Restore copies a backup back: into a new folder on a volume, never over
// what is there. It returns the folder at once; State tells how far it is.
func (m *Manager) Restore(id, snapshot, volume string) (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return "", ErrNotFound
	}
	if !snapshotForm.MatchString(snapshot) || !slices.Contains(m.mounted(), volume) {
		return "", invalid("backup snapshot or volume")
	}
	return m.restoreVerifiedCopy(job, snapshot, volume, nil, false)
}

// Run starts the jobs when their time has come, until ctx is done. A NAS
// that was off at that time catches up after the start.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for minute := 0; ; minute++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// The first minutes belong to the start: volumes and network.
		if minute < 3 {
			continue
		}
		m.mu.Lock()
		jobs := slices.Clone(m.jobs)
		m.mu.Unlock()
		for _, job := range jobs {
			// A job whose shares are not there has no state to tell
			// whether it ran; it waits, and says so when that lasts.
			if m.stateFile(job) == "" {
				m.checkUnavailable(job)
				continue
			}
			m.mu.Lock()
			delete(m.unavailable, job.ID)
			m.mu.Unlock()
			state := m.state(job)
			if due(job, state, m.local(m.now())) {
				start := m.Start
				if m.ScheduledStart != nil {
					start = m.ScheduledStart
				}
				if err := start(job.ID); err != nil && !errors.Is(err, ErrRunning) {
					slog.Warn("backup not started", "job", job.ID, "err", err)
				}
			}
			m.checkOverdue(job, state)
		}
	}
}

// Plugged starts the USB copies to a drive that was just plugged in whose
// last attempt failed, for example because the drive was not there. Jobs
// run by hand only are left alone.
func (m *Manager) Plugged(deviceID string) {
	if m.Disabled {
		return
	}
	m.mu.Lock()
	jobs := slices.Clone(m.jobs)
	m.mu.Unlock()
	for _, job := range jobs {
		if job.Destination.Type != "usb-copy" || job.Destination.DeviceID != deviceID || !job.Enabled || m.stateFile(job) == "" {
			continue
		}
		if state := m.state(job); state.Running || state.FailingSince == nil {
			continue
		}
		slog.Info("backup drive plugged in, catching up", "job", job.ID)
		if err := m.Start(job.ID); err != nil && !errors.Is(err, ErrRunning) {
			slog.Warn("backup not started", "job", job.ID, "err", err)
		}
	}
}

// checkOverdue tells OnOverdue when a job became overdue.
func (m *Manager) checkOverdue(job Job, state State) {
	m.tellOverdue(job, state.Running, overdue(job, state, m.local(m.now())))
}

// checkUnavailable treats a job that cannot run because none of its shares
// is there like one whose backups fail: overdue after two of its moments and
// overdueAfter.
func (m *Manager) checkUnavailable(job Job) {
	now := m.local(m.now())
	m.mu.Lock()
	if m.unavailable == nil {
		m.unavailable = map[string]time.Time{}
	}
	since, ok := m.unavailable[job.ID]
	if !ok {
		since = now
		m.unavailable[job.ID] = now
	}
	m.mu.Unlock()
	m.tellOverdue(job, false, overdue(job, State{FailingSince: &since, LastRun: &since}, now))
}

func (m *Manager) tellOverdue(job Job, running, late bool) {
	m.mu.Lock()
	if m.overdue == nil {
		m.overdue = map[string]bool{}
	}
	// A running job keeps what was said: it may still fail.
	changed := !running && late != m.overdue[job.ID]
	if changed {
		m.overdue[job.ID] = late
	}
	m.mu.Unlock()
	if changed && late && m.OnOverdue != nil {
		slog.Warn("backup overdue", "job", job.ID)
		m.OnOverdue(job.ID)
	}
}

func plainCopy(kind string) bool { return kind == "usb-copy" || kind == "smb-copy" }

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
