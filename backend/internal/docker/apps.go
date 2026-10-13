package docker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
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

// CreateApp stores a compose file and brings the app up in the background:
// images are pulled and containers created. Progress shows up as a job.
//
// env holds secrets the compose file refers to as ${NAME}. They go into a
// file of their own next to the compose file.
func (m *Manager) CreateApp(ctx context.Context, name, compose string, env map[string]string) error {
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
	m.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	if known {
		return ErrExists
	}

	// Let Docker check the file before anything is kept.
	if out, err := m.runner.Run(ctx, compose, "docker", "compose", "--project-name", name, "--file", "-", "config", "--quiet"); err != nil {
		return &InvalidComposeError{Detail: strings.TrimSpace(out)}
	}
	if err := os.MkdirAll(m.appData(settings.Volume, name), 0o770); err != nil {
		return err
	}
	if secrets != "" {
		if err := m.store.Write(envFile(name), []byte(secrets)); err != nil {
			return err
		}
	}
	if err := m.store.Write(composeFile(name), []byte(compose)); err != nil {
		return err
	}
	m.mu.Lock()
	m.appList = nil
	job := &Job{State: "running"}
	m.jobs[name] = job
	m.mu.Unlock()

	m.startJob(ctx, settings.Volume, name, job, false)
	return nil
}

// startJob brings an app up in the background, pulling newer images first
// if pull is set. Pulling can take minutes; it must not end with the request.
func (m *Manager) startJob(ctx context.Context, volume, name string, job *Job, pull bool) {
	ctx = context.WithoutCancel(ctx)
	go func() {
		var out string
		var err error
		if pull {
			out, err = m.runner.Run(ctx, "", "docker", m.compose(volume, name, "pull", "--quiet")...)
		}
		if err == nil {
			var up string
			up, err = m.runner.Run(ctx, "", "docker", m.compose(volume, name, "up", "--detach", "--remove-orphans", "--quiet-pull")...)
			out += up
		}
		if pull && err == nil {
			// The hints are stale now; ask again what is current.
			m.CheckUpdates(ctx)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		job.Output = tail(out, 4000)
		if err != nil {
			job.State = "failed"
			slog.Error("app setup failed", "app", name, "err", err)
		} else {
			job.State = "done"
			slog.Info("app up", "app", name, "pulled", pull)
		}
	}()
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
	job := &Job{State: "running"}
	m.jobs[name] = job
	return job, settings.Volume, nil
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
	if strings.TrimSpace(compose) == "" || len(compose) > maxComposeBytes {
		return ErrInvalid
	}
	if m.Disabled {
		return ErrDisabled
	}
	if !appNameRE.MatchString(name) {
		return ErrNotFound
	}
	if out, err := m.runner.Run(ctx, compose, "docker", "compose", "--project-name", name, "--file", "-", "config", "--quiet"); err != nil {
		return &InvalidComposeError{Detail: strings.TrimSpace(out)}
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
// compose file. The app's data folder on the volume stays.
func (m *Manager) RemoveApp(ctx context.Context, name string) error {
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	m.mu.Lock()
	known := slices.Contains(m.knownApps(), name)
	job := m.jobs[name]
	running := m.applied != ""
	m.mu.Unlock()
	if !known {
		return ErrNotFound
	}
	if job != nil && job.State == "running" {
		return ErrBusy
	}
	if running {
		if _, err := m.runner.Run(ctx, "", "docker", m.compose(settings.Volume, name, "down", "--remove-orphans")...); err != nil {
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
