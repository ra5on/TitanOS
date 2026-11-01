package docker

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

// An app is a Compose project whose compose file TitanOS keeps in
// /config/apps/<name>/compose.yaml.

var appNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// maxComposeBytes bounds a compose file; it is stored on the stick.
const maxComposeBytes = 256 * 1024

func composeFile(app string) string {
	return appsDir + "/" + app + "/compose.yaml"
}

// envFile holds the secrets of an app. Its values never leave the NAS: the
// compose file refers to them as ${NAME}.
func envFile(app string) string {
	return appsDir + "/" + app + "/.env"
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// renderEnv writes variables in the form `docker compose` reads. Values are
// put in single quotes, which Compose takes literally.
func renderEnv(env map[string]string) (string, error) {
	names := make([]string, 0, len(env))
	for name, value := range env {
		if !envNameRE.MatchString(name) || strings.ContainsAny(value, "'\n\r\x00") || len(value) > 4096 {
			return "", ErrInvalidEnv
		}
		names = append(names, name)
	}
	if len(names) > 200 {
		return "", ErrInvalidEnv
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name + "='" + env[name] + "'\n")
	}
	return b.String(), nil
}

// knownApps lists the apps in /config. The caller holds the lock.
//
// The list is read from the stick once and then kept: the Docker page
// polls, and polling must not keep the stick awake.
func (m *Manager) knownApps() []string {
	if m.appList == nil {
		names, err := m.store.List(appsDir)
		if err != nil {
			return []string{}
		}
		m.appList = []string{}
		for _, name := range names {
			if appNameRE.MatchString(name) {
				m.appList = append(m.appList, name)
			}
		}
		sort.Strings(m.appList)
	}
	return slices.Clone(m.appList)
}

// compose builds a `docker compose` command line for an app.
func (m *Manager) compose(volume, app string, args ...string) []string {
	line := []string{
		"compose", "--project-name", app,
		"--file", filepath.Join(m.configDir, filepath.FromSlash(composeFile(app))),
		// Relative paths in the compose file refer to the app's folder on
		// the volume, never to the stick the file itself is on.
		"--project-directory", m.appData(volume, app),
	}
	// With another project directory Compose would not find the secrets.
	if env := filepath.Join(m.configDir, filepath.FromSlash(envFile(app))); exists(env) {
		line = append(line, "--env-file", env)
	}
	return append(line, args...)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Validate with the same project directory and env values as the later start.
// New secrets stay in a private RAM temporary file until validation succeeds.
func (m *Manager) validateCompose(ctx context.Context, volume, name, compose, env string) error {
	file, err := os.CreateTemp("", "titanos-compose-env-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(env); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	out, err := run.Combined(ctx, m.runner, compose, "docker", "compose", "--project-name", name,
		"--file", "-", "--project-directory", m.appData(volume, name), "--env-file", file.Name(), "config", "--quiet")
	if err != nil {
		return &InvalidComposeError{Detail: strings.TrimSpace(out)}
	}
	return nil
}

// CreateApp stores a compose file and brings the app up in the background:
// images are pulled and containers created. Progress shows up as a job.
//
// env holds secrets the compose file refers to as ${NAME}. They go into a
// file of their own next to the compose file.
func (m *Manager) CreateApp(ctx context.Context, name, compose string, env map[string]string) error {
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	m.lanUsers.RLock()
	defer m.lanUsers.RUnlock()
	if m.Disabled {
		return ErrDisabled
	}
	if !appNameRE.MatchString(name) {
		return ErrInvalidName
	}
	if strings.TrimSpace(compose) == "" || len(compose) > maxComposeBytes {
		return ErrInvalid
	}
	secrets, err := renderEnv(env)
	if err != nil {
		return err
	}
	settings := m.current()
	m.mu.Lock()
	running := m.applied != ""
	known := slices.Contains(m.knownApps(), name)
	backup := m.backups > 0
	m.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	if known {
		return ErrExists
	}
	if backup {
		return errBackupRunning
	}

	// Let Docker check the file before anything is kept.
	if err := m.validateLANCompose(compose); err != nil {
		return err
	}
	if err := m.validateCompose(ctx, settings.Volume, name, compose, secrets); err != nil {
		return err
	}
	// Register the job before anything is written: an app backup refuses
	// to start while it runs, and a refusal here leaves no files behind.
	m.mu.Lock()
	if m.backups > 0 {
		m.mu.Unlock()
		return errBackupRunning
	}
	if job := m.jobs[name]; job != nil && job.State == "running" {
		m.mu.Unlock()
		return ErrExists
	}
	job := &Job{State: "running"}
	m.jobs[name] = job
	m.mu.Unlock()
	abandon := func(err error) error {
		_ = m.store.RemoveAll(appsDir + "/" + name) // cleanup, best effort: nothing more can be done if it fails
		m.mu.Lock()
		delete(m.jobs, name)
		m.appList = nil
		m.mu.Unlock()
		return err
	}
	if err := os.MkdirAll(m.appData(settings.Volume, name), 0o770); err != nil {
		return abandon(err)
	}
	if secrets != "" {
		if err := m.store.Write(envFile(name), []byte(secrets)); err != nil {
			return abandon(err)
		}
	}
	if err := m.store.Write(composeFile(name), []byte(compose)); err != nil {
		return abandon(err)
	}
	m.mu.Lock()
	m.appList = nil
	m.mu.Unlock()

	m.startJob(ctx, settings.Volume, name, job, false)
	return nil
}

// startJob brings an app up in the background, pulling newer images first
// if pull is set. Pulling can take minutes; it must not end with the request.
//
// The output of the job grows while it runs, so the page can show what is
// downloaded. Without pull, images that are still missing are pulled first
// with progress; that is what "up" would do anyway, only quietly.
func (m *Manager) startJob(ctx context.Context, volume, name string, job *Job, pull bool) {
	ctx = context.WithoutCancel(ctx)
	go func() {
		var log jobLog
		progress := func(line string) {
			m.mu.Lock()
			log.add(line)
			job.Output = log.String()
			m.mu.Unlock()
		}
		var err error
		if pull {
			_, err = run.CombinedLines(ctx, m.runner, progress, "docker", m.compose(volume, name, "pull", "--quiet")...)
		} else {
			args := m.compose(volume, name, "pull", "--policy", "missing", "--ignore-buildable", "--ignore-pull-failures")
			args = slices.Insert(args, 1, "--progress", "plain")
			// Best effort: "up" pulls what is left and says why it cannot.
			_, _ = run.CombinedLines(ctx, m.runner, progress, "docker", args...)
		}
		if err == nil {
			// Resolve hardware against the leases under hardwareMu, but run
			// compose up (pulls, builds: minutes) without it. While composing
			// is recorded, BeginVM refuses to lease the devices it may use,
			// so they stay free until the containers run.
			var path string
			var cleanup func()
			path, cleanup, err = m.beginCompose(ctx, name)
			if err == nil {
				args := m.compose(volume, name, "up", "--detach", "--remove-orphans", "--quiet-pull")
				args[4] = path
				_, err = run.CombinedLines(ctx, m.runner, progress, "docker", args...)
				cleanup()
				m.endCompose(name)
			}
		}
		if pull && err == nil {
			// The hints are stale now; ask again what is current.
			_ = m.CheckUpdates(ctx) // only refreshes the update hints; the pull itself succeeded
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		job.Output = log.String()
		if err != nil {
			job.State = "failed"
			if job.Output == "" {
				job.Output = err.Error()
			}
			slog.Error("app setup failed", "app", name, "err", err)
		} else {
			job.State = "done"
			slog.Info("app up", "app", name, "pulled", pull)
		}
	}()
}

// jobOutputMax is how much of a job's output the page gets.
const jobOutputMax = 8000

// progressLine is a line of Compose's plain progress: an image or a layer,
// then its state. A later line of the same one takes its place.
var progressLine = regexp.MustCompile(`^(\S+) (Pulling|Pulled|Waiting|Downloading|Download complete|Verifying Checksum|Extracting|Pull complete|Already exists|Skipped|Interrupted|Error)\b`)

// jobLog is the output of a job the way a terminal shows Compose's
// progress: one line per image and layer, updated in place.
type jobLog struct {
	lines []string
	at    map[string]int
}

func (l *jobLog) add(line string) {
	if match := progressLine.FindStringSubmatch(line); match != nil {
		if i, ok := l.at[match[1]]; ok {
			l.lines[i] = line
			return
		}
		if l.at == nil {
			l.at = map[string]int{}
		}
		l.at[match[1]] = len(l.lines)
	}
	l.lines = append(l.lines, line)
}

func (l *jobLog) String() string {
	return tail(strings.Join(l.lines, "\n"), jobOutputMax)
}

func (m *Manager) beginCompose(ctx context.Context, name string) (string, func(), error) {
	m.hardwareMu.Lock()
	defer m.hardwareMu.Unlock()
	path, cleanup, err := m.hardwareCompose(ctx, name)
	if err != nil {
		return "", nil, err
	}
	text, err := m.Compose(name)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	var devices []hardware.ContainerDevice
	if m.HardwareDevices != nil {
		devices = m.HardwareDevices(ctx)
	}
	m.composing[name] = composeHardware(text, devices)
	return path, cleanup, nil
}

func (m *Manager) endCompose(name string) {
	m.hardwareMu.Lock()
	delete(m.composing, name)
	m.hardwareMu.Unlock()
}

// begin registers a job for a known app, unless one is running.
func (m *Manager) begin(name string) (*Job, string, error) {
	if m.Disabled {
		return nil, "", ErrDisabled
	}
	settings := m.current()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.applied == "" {
		return nil, "", ErrNotRunning
	}
	if !slices.Contains(m.knownApps(), name) {
		return nil, "", ErrNotFound
	}
	if job := m.jobs[name]; job != nil && job.State == "running" {
		return nil, "", ErrBusy
	}
	if m.backups > 0 {
		return nil, "", errBackupRunning
	}
	job := &Job{State: "running"}
	m.jobs[name] = job
	return job, settings.Volume, nil
}

// DismissJob forgets a failed setup, so its message goes away. A running
// setup stays; a finished one is forgotten anyway.
func (m *Manager) DismissJob(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[name]
	if job == nil {
		return ErrNotFound
	}
	if job.State == "running" {
		return ErrBusy
	}
	delete(m.jobs, name)
	return nil
}

// UpdateApp pulls the newest images of an app and recreates its containers.
func (m *Manager) UpdateApp(ctx context.Context, name string) error {
	job, volume, err := m.begin(name)
	if err != nil {
		return err
	}
	m.startJob(ctx, volume, name, job, true)
	return nil
}

// SetCompose replaces the compose file of an app and recreates its
// containers. Data in mounted folders stays.
func (m *Manager) SetCompose(ctx context.Context, name, compose string) error {
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	m.lanUsers.RLock()
	defer m.lanUsers.RUnlock()
	if err := m.validateLANCompose(compose); err != nil {
		return err
	}
	if strings.TrimSpace(compose) == "" || len(compose) > maxComposeBytes {
		return ErrInvalid
	}
	if m.Disabled {
		return ErrDisabled
	}
	if !appNameRE.MatchString(name) {
		return ErrNotFound
	}
	env, err := m.store.Read(envFile(name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.validateCompose(ctx, m.current().Volume, name, compose, string(env)); err != nil {
		return err
	}
	job, volume, err := m.begin(name)
	if err != nil {
		return err
	}
	if err := m.store.Write(composeFile(name), []byte(compose)); err != nil {
		m.mu.Lock()
		job.State, job.Output = "failed", err.Error()
		m.mu.Unlock()
		return err
	}
	m.startJob(ctx, volume, name, job, false)
	return nil
}

// InvalidComposeError carries what `docker compose config` said.
type InvalidComposeError struct{ Detail string }

func (e *InvalidComposeError) Error() string { return "docker: invalid compose file: " + e.Detail }
func (e *InvalidComposeError) Unwrap() error { return ErrInvalid }

func tail(text string, n int) string {
	if len(text) > n {
		return "…" + text[len(text)-n:]
	}
	return text
}

// Compose returns the compose file of an app.
func (m *Manager) Compose(name string) (string, error) {
	if !appNameRE.MatchString(name) {
		return "", ErrNotFound
	}
	data, err := m.store.Read(composeFile(name))
	if err != nil {
		return "", ErrNotFound
	}
	return string(data), nil
}

// RemoveApp stops and removes the containers of an app and forgets its
// compose file. The app's data folder on the volume stays, unless purge is
// set: then its folder, its named volumes and, where no other container
// uses them, its images go too, so that installing it again starts afresh.
func (m *Manager) RemoveApp(ctx context.Context, name string, purge bool) error {
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	m.mu.Lock()
	known := slices.Contains(m.knownApps(), name)
	job := m.jobs[name]
	running := m.applied != ""
	backup := m.backups > 0
	m.mu.Unlock()
	if !known {
		return ErrNotFound
	}
	if job != nil && job.State == "running" {
		return ErrBusy
	}
	if backup {
		return errBackupRunning
	}
	if running {
		down := []string{"down", "--remove-orphans"}
		if purge {
			down = append(down, "--volumes")
		}
		if out, err := run.Combined(ctx, m.runner, "", "docker", m.compose(settings.Volume, name, down...)...); err != nil {
			slog.Error("app removal failed", "app", name, "output", tail(out, 2000))
			return err
		}
		if purge {
			// Separately and without failing: an image another app uses
			// stays, and Compose would give up on the first one.
			if out, err := run.Combined(ctx, m.runner, "", "docker", m.compose(settings.Volume, name, "down", "--rmi", "all")...); err != nil {
				slog.Warn("images of a removed app kept", "app", name, "output", tail(out, 2000))
			}
		}
	}
	if purge && settings.Volume != "" {
		if err := os.RemoveAll(m.appData(settings.Volume, name)); err != nil {
			return err
		}
	}
	if err := m.store.RemoveAll(appsDir + "/" + name); err != nil {
		return err
	}
	m.mu.Lock()
	m.appList = nil
	delete(m.jobs, name)
	m.mu.Unlock()
	return nil
}
