package docker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Automatic updates (docs/APP-AUTO-UPDATE.md): apps chosen for it get newer
// images in a time window. After an update TitanOS watches the containers
// for a few minutes; if one does not run or reports itself unhealthy, the
// previous images are tagged back and the app comes up with them again.
// Off for every app unless switched on.

const autoUpdateFile = "docker/auto-update.json"

var hourMinute = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// AutoUpdateSettings are kept in /config/docker/auto-update.json.
type AutoUpdateSettings struct {
	// Apps lists the apps that update by themselves.
	Apps []string `json:"apps"`
	// Weekdays (0 = Sunday) and Time are the window; empty Weekdays means
	// every day.
	Weekdays []int  `json:"weekdays"`
	Time     string `json:"time"`
}

// AutoUpdateResult is what the last automatic update of an app did.
type AutoUpdateResult struct {
	At time.Time `json:"at"`
	// Outcome: "updated", "rolledBack", "failed" or "current".
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

// AutoUpdateStatus is what the page shows.
type AutoUpdateStatus struct {
	Settings AutoUpdateSettings          `json:"settings"`
	Results  map[string]AutoUpdateResult `json:"results"`
	Next     *time.Time                  `json:"next,omitempty"`
}

// imageOf is one container of an app before the update.
type imageOf struct {
	service, ref, id string
}

// updater updates one app and rolls back on failure. Its steps are
// functions so that the order can be tested without an engine.
type updater struct {
	snapshot func(ctx context.Context, app string) ([]imageOf, error)
	pullUp   func(ctx context.Context, app string, pull bool) error
	healthy  func(ctx context.Context, app string) (bool, string)
	tag      func(ctx context.Context, id, ref string) error
	watch    time.Duration
	step     time.Duration
}

// run updates app and gives back what happened.
func (u updater) run(ctx context.Context, app string) AutoUpdateResult {
	now := time.Now()
	before, err := u.snapshot(ctx, app)
	if err != nil {
		return AutoUpdateResult{At: now, Outcome: "failed", Detail: "containers not readable: " + err.Error()}
	}
	if err := u.pullUp(ctx, app, true); err != nil {
		result := AutoUpdateResult{At: now, Outcome: "failed", Detail: tail(err.Error(), 300)}
		if back := u.rollback(ctx, app, before); back != "" {
			result.Detail += "; " + back
		} else {
			result.Outcome = "rolledBack"
		}
		return result
	}
	after, _ := u.snapshot(ctx, app)
	if sameImages(before, after) {
		return AutoUpdateResult{At: now, Outcome: "current"}
	}
	// Watch the containers: all must run and none may say unhealthy.
	deadline := time.Now().Add(u.watch)
	reason := ""
	for {
		ok, why := u.healthy(ctx, app)
		reason = why
		if !ok && why != "starting" {
			break
		}
		if time.Now().After(deadline) {
			if ok {
				return AutoUpdateResult{At: now, Outcome: "updated", Detail: changed(before, after)}
			}
			break
		}
		select {
		case <-ctx.Done():
			return AutoUpdateResult{At: now, Outcome: "failed", Detail: "stopped"}
		case <-time.After(u.step):
		}
	}
	result := AutoUpdateResult{At: now, Outcome: "rolledBack", Detail: reason}
	if back := u.rollback(ctx, app, before); back != "" {
		result.Outcome, result.Detail = "failed", reason+"; "+back
	}
	return result
}

// rollback tags the previous images back and brings the app up with them;
// it returns what went wrong, or "".
func (u updater) rollback(ctx context.Context, app string, before []imageOf) string {
	for _, c := range before {
		if c.id == "" || c.ref == "" || strings.HasPrefix(c.ref, "sha256:") {
			continue
		}
		if err := u.tag(ctx, c.id, c.ref); err != nil {
			return "previous image not tagged back: " + err.Error()
		}
	}
	if err := u.pullUp(ctx, app, false); err != nil {
		return "previous images not started: " + tail(err.Error(), 300)
	}
	return ""
}

func sameImages(a, b []imageOf) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.ContainsFunc(b, func(y imageOf) bool { return y.service == x.service && y.id == x.id }) {
			return false
		}
	}
	return true
}

func changed(before, after []imageOf) string {
	var names []string
	for _, x := range after {
		if !slices.ContainsFunc(before, func(y imageOf) bool { return y.service == x.service && y.id == x.id }) {
			names = append(names, x.service)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// autoUpdates is the state of the automatic updates in the manager.
type autoUpdates struct {
	mu       sync.Mutex
	loaded   bool
	settings AutoUpdateSettings
	results  map[string]AutoUpdateResult
	lastRun  time.Time
}

func (m *Manager) autoState() *autoUpdates {
	a := &m.auto
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		s := AutoUpdateSettings{Time: "04:00"}
		if err := m.store.ReadJSON(autoUpdateFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("auto update settings not readable", "err", err)
		}
		a.settings, a.loaded, a.results = s, true, map[string]AutoUpdateResult{}
	}
	return a
}

// AutoUpdates returns settings, results and the next window.
func (m *Manager) AutoUpdates() AutoUpdateStatus {
	a := m.autoState()
	a.mu.Lock()
	defer a.mu.Unlock()
	st := AutoUpdateStatus{Settings: a.settings, Results: map[string]AutoUpdateResult{}}
	for k, v := range a.results {
		st.Results[k] = v
	}
	if len(a.settings.Apps) > 0 {
		next := nextWindow(a.settings, m.localNow())
		st.Next = &next
	}
	return st
}

// SetAutoUpdates stores the settings.
func (m *Manager) SetAutoUpdates(s AutoUpdateSettings) error {
	if !hourMinute.MatchString(s.Time) {
		return fmt.Errorf("%w: time", ErrInvalid)
	}
	for _, d := range s.Weekdays {
		if d < 0 || d > 6 {
			return fmt.Errorf("%w: weekday", ErrInvalid)
		}
	}
	slices.Sort(s.Weekdays)
	s.Weekdays = slices.Compact(s.Weekdays)
	for _, app := range s.Apps {
		if !appNameRE.MatchString(app) {
			return fmt.Errorf("%w: app", ErrInvalid)
		}
	}
	slices.Sort(s.Apps)
	s.Apps = slices.Compact(s.Apps)
	if s.Apps == nil {
		s.Apps = []string{}
	}
	a := m.autoState()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := m.store.WriteJSON(autoUpdateFile, s); err != nil {
		return err
	}
	a.settings = s
	return nil
}

func (m *Manager) localNow() time.Time {
	now := time.Now()
	if m.Timezone != nil {
		if zone, err := time.LoadLocation(m.Timezone()); err == nil {
			return now.In(zone)
		}
	}
	return now
}

func windowDay(s AutoUpdateSettings, day time.Weekday) bool {
	return len(s.Weekdays) == 0 || slices.Contains(s.Weekdays, int(day))
}

// nextWindow is the next start of the window after now.
func nextWindow(s AutoUpdateSettings, now time.Time) time.Time {
	var hour, minute int
	_, _ = fmt.Sscanf(s.Time, "%d:%d", &hour, &minute) // a value that cannot be read leaves the zero value, which the code handles
	for i := range 8 {
		day := now.AddDate(0, 0, i)
		at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
		if at.After(now) && windowDay(s, at.Weekday()) {
			return at
		}
	}
	return now.Add(24 * time.Hour)
}

// dueWindow tells whether the window opened in the last hour and has not
// been used yet.
func dueWindow(s AutoUpdateSettings, now, last time.Time) bool {
	var hour, minute int
	_, _ = fmt.Sscanf(s.Time, "%d:%d", &hour, &minute) // a value that cannot be read leaves the zero value, which the code handles
	at := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	return windowDay(s, now.Weekday()) && !now.Before(at) && now.Sub(at) < time.Hour && last.Before(at)
}

// RunAutoUpdates checks every minute whether the window is open and then
// updates the chosen apps that have newer images, one after the other.
func (m *Manager) RunAutoUpdates(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		a := m.autoState()
		a.mu.Lock()
		s, last := a.settings, a.lastRun
		a.mu.Unlock()
		now := m.localNow()
		if len(s.Apps) == 0 || m.Disabled || !dueWindow(s, now, last) {
			continue
		}
		if m.Busy() || m.backupActive() || (m.ExternalBusy != nil && m.ExternalBusy()) {
			continue // try again next minute while the window is open
		}
		a.mu.Lock()
		a.lastRun = now
		a.mu.Unlock()
		m.autoUpdateNow(ctx, s.Apps)
	}
}

// autoUpdateNow updates the given apps that have newer images.
func (m *Manager) autoUpdateNow(ctx context.Context, apps []string) {
	if err := m.CheckUpdates(ctx); err != nil {
		slog.Warn("auto update: checking for updates failed", "err", err)
		return
	}
	pending := m.AppsWithUpdates(ctx)
	u := m.realUpdater()
	for _, app := range apps {
		if !slices.Contains(pending, app) {
			continue
		}
		job, _, err := m.begin(app)
		if err != nil {
			continue
		}
		result := u.run(ctx, app)
		m.mu.Lock()
		job.State = "done"
		if result.Outcome == "failed" {
			job.State, job.Output = "failed", result.Detail
		}
		m.mu.Unlock()
		a := m.autoState()
		a.mu.Lock()
		a.results[app] = result
		a.mu.Unlock()
		slog.Info("auto update", "app", app, "outcome", result.Outcome, "detail", result.Detail)
		if m.OnEvent != nil {
			switch result.Outcome {
			case "updated":
				m.OnEvent("info", "docker_auto_updated", app)
			case "rolledBack":
				m.OnEvent("warning", "docker_update_rolled_back", app)
			case "failed":
				m.OnEvent("error", "docker_update_failed", app)
			}
		}
	}
	_ = m.CheckUpdates(ctx) // only refreshes the update hints; the next round tries again
}

// realUpdater wires the steps to the engine and docker compose.
func (m *Manager) realUpdater() updater {
	volume := m.current().Volume
	return updater{
		watch: 3 * time.Minute, step: 10 * time.Second,
		snapshot: func(ctx context.Context, app string) ([]imageOf, error) {
			containers, err := m.api.containers(ctx)
			if err != nil {
				return nil, err
			}
			var out []imageOf
			for _, c := range containers {
				if c.Labels["com.docker.compose.project"] == app {
					out = append(out, imageOf{service: c.Labels["com.docker.compose.service"], ref: c.Image, id: c.ImageID})
				}
			}
			return out, nil
		},
		pullUp: func(ctx context.Context, app string, pull bool) error {
			if pull {
				if out, err := run.Combined(ctx, m.runner, "", "docker", m.compose(volume, app, "pull", "--quiet")...); err != nil {
					return fmt.Errorf("%w: %s", err, tail(out, 300))
				}
			}
			path, cleanup, err := m.beginCompose(ctx, app)
			if err != nil {
				return err
			}
			defer func() { cleanup(); m.endCompose(app) }()
			args := m.compose(volume, app, "up", "--detach", "--remove-orphans", "--pull", "never")
			args[4] = path
			if out, err := run.Combined(ctx, m.runner, "", "docker", args...); err != nil {
				return fmt.Errorf("%w: %s", err, tail(out, 300))
			}
			return nil
		},
		healthy: func(ctx context.Context, app string) (bool, string) {
			containers, err := m.api.containers(ctx)
			if err != nil {
				return false, "engine not answering"
			}
			found := false
			for _, c := range containers {
				if c.Labels["com.docker.compose.project"] != app {
					continue
				}
				found = true
				var info struct {
					State struct {
						Running bool
						Health  *struct{ Status string }
					}
				}
				if err := m.api.getJSON(ctx, "/containers/"+url.PathEscape(c.ID)+"/json", nil, &info); err != nil {
					return false, "container not readable"
				}
				name := c.Labels["com.docker.compose.service"]
				if !info.State.Running {
					return false, name + " is not running"
				}
				if info.State.Health != nil {
					switch info.State.Health.Status {
					case "unhealthy":
						return false, name + " reports itself unhealthy"
					case "starting":
						return false, "starting"
					}
				}
			}
			if !found {
				return false, "no container of the app"
			}
			return true, ""
		},
		tag: func(ctx context.Context, id, ref string) error {
			_, err := m.runner.Run(ctx, "", "docker", "image", "tag", id, ref)
			return err
		},
	}
}
