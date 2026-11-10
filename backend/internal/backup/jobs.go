package backup

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/retention"
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
	// Whole volumes and folders outside shares (0.41.0).
	for _, volume := range job.Volumes {
		if !slices.Contains(volumes, volume) {
			return invalid("volume %q is not there", volume)
		}
		sourceVolumes = append(sourceVolumes, volume)
	}
	for _, place := range job.Places {
		volume, _, _ := strings.Cut(place, "/")
		if !slices.Contains(volumes, volume) {
			return invalid("volume %q is not there", volume)
		}
		sourceVolumes = append(sourceVolumes, volume)
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
	if err := retention.Check(job.Keep); err != nil {
		return invalid("%s", strings.TrimPrefix(err.Error(), "retention: invalid: "))
	}
	if c := job.Copy; c != nil && (!plainCopy(job.Destination.Type) || c.MirrorDays < 0 || c.MirrorDays > 3650) {
		return invalid("options of the readable copy")
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
	case "usb", "smb":
		// Encrypted versions on a drive or an SMB share (0.41.0).
		if (d.Type == "usb" && !regexp.MustCompile(`^[A-Za-z0-9-]{4,80}$`).MatchString(d.DeviceID)) || (d.Type == "smb" && !naming.ValidBackup(d.TargetID)) || !relative(d.Folder) {
			return invalid("USB identity, SMB target or folder")
		}
		if len(d.DeviceName) > 64 || strings.ContainsFunc(d.DeviceName, func(r rune) bool { return r < ' ' || r == 0x7f }) {
			return invalid("drive name")
		}
	case "local", "local-copy":
		if d.Type == "local-copy" && len(job.Extras) != 0 {
			return invalid("unsupported extra source")
		}
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

// sameCopyPlace reports whether two readable copies write into the same
// folder of the same place, or one into a folder of the other. Each job
// removes old versions in its folder: two there delete each other's.
func sameCopyPlace(a, b Destination) bool {
	if !plainCopy(a.Type) || a.Type != b.Type || a.DeviceID != b.DeviceID || a.TargetID != b.TargetID || a.Volume != b.Volume {
		return false
	}
	within := func(folder string) string {
		return strings.TrimSuffix(strings.ToLower(path.Clean("/"+folder)), "/") + "/"
	}
	x, y := within(a.Folder), within(b.Folder)
	return strings.HasPrefix(x, y) || strings.HasPrefix(y, x)
}

// copyPlaceTaken names the other job whose folder this one would share.
func copyPlaceTaken(jobs []Job, job Job) error {
	for _, other := range jobs {
		if other.ID != job.ID && sameCopyPlace(other.Destination, job.Destination) {
			return invalid("the job %q already copies into this folder; choose a folder of its own", other.ID)
		}
	}
	return nil
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
	if err := copyPlaceTaken(m.jobs, job); err != nil {
		return err
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
	// A job that shared its folder before this rule can still be edited;
	// moving a job into another one's folder cannot.
	if !sameCopyPlace(old.Destination, job.Destination) || old.Destination.Folder != job.Destination.Folder {
		if err := copyPlaceTaken(m.jobs, job); err != nil {
			return err
		}
	}
	jobs := slices.Clone(m.jobs)
	jobs[at] = job
	return m.save(jobs)
}

// Delete forgets a job. What it backed up stays where it is.
func (m *Manager) Delete(id string) error {
	// Where the job kept its state is asked before the lock is taken.
	file := ""
	if job, ok := m.find(id); ok {
		file = m.stateFile(job)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	at := slices.IndexFunc(m.jobs, func(j Job) bool { return j.ID == id })
	if at < 0 {
		return ErrNotFound
	}
	if state := m.states[id]; state != nil && state.Running {
		return ErrRunning
	}
	if err := m.save(slices.Delete(slices.Clone(m.jobs), at, at+1)); err != nil {
		return err
	}
	// The state goes with the job: a new job of the same name must not
	// start out with this one's "last success".
	delete(m.states, id)
	if file != "" {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("state of the deleted backup job not removed", "job", id, "err", err)
		}
	}
	return nil
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
