package nvr

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Marker is the file that tells the backups this folder holds recordings.
const Marker = ".titanos-nvr"

// Manager ties the store, the recorder and the retention together.
type Manager struct {
	Store *Store
	Rec   *Recorder
	// VolumePath returns the mount path of a volume by name.
	VolumePath func(name string) (string, bool)
	Prober     *Prober
	Watch      *Watcher
	// Notify sends a notification; nil sends none.
	Notify func(severity, kind, subject string)

	// TimeAddr is the listen address of the time server; empty means ":123".
	TimeAddr string

	mu         sync.Mutex
	ctx        context.Context
	sntp       *SNTPServer
	sntpCancel context.CancelFunc
	notified   map[string]time.Time
	// shortNoticed and offline remember what was already reported.
	shortNoticed map[string]string
	offline      map[string]bool
	since        time.Time
}

func NewManager(cfg *config.Store, volumePath func(string) (string, bool)) *Manager {
	m := &Manager{Store: NewStore(cfg), VolumePath: volumePath, Prober: &Prober{}}
	m.Rec = &Recorder{Store: m.Store, Root: m.Root}
	m.Watch = &Watcher{Store: m.Store, Rec: m.Rec, Root: m.Root, OnEvent: m.onEvent}
	return m
}

// Root is the folder holding the recordings; empty while no volume is
// chosen or the volume is not mounted.
func (m *Manager) Root() string {
	v := m.Store.Settings().Volume
	if v == "" || m.VolumePath == nil {
		return ""
	}
	p, ok := m.VolumePath(v)
	if !ok {
		return ""
	}
	return filepath.Join(p, "nvr")
}

// Run applies the recordings and prunes every ten minutes until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	m.Reload()
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	health := time.NewTicker(30 * time.Second)
	defer health.Stop()
	for {
		select {
		case <-health.C:
			m.CheckOnline(time.Now())
		case <-ctx.Done():
			m.Watch.Apply(context.Background(), false)
			m.Rec.Stop()
			return
		case <-t.C:
			m.Prune(time.Now())
			m.Reload() // picks up a volume that came back
		}
	}
}

// Reload brings the recorder in line with the settings; call after changes.
func (m *Manager) Reload() {
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx == nil {
		return
	}
	if st := m.Store.Settings(); st.Enabled {
		prepareRoot(ctx, m.Root())
	}
	m.mu.Lock()
	m.since = time.Now() // cameras get a moment before they count as offline
	m.mu.Unlock()
	m.Rec.Apply(ctx, m.Store.Settings().Enabled)
	m.Watch.Apply(ctx, m.Store.Settings().Enabled)
	m.syncTimeServer(ctx)
}

// syncTimeServer runs the time server while the NVR is on and a camera
// takes its time from it, and stops it otherwise.
func (m *Manager) syncTimeServer(ctx context.Context) {
	want := false
	if m.Store.Settings().Enabled {
		for _, c := range m.Store.Cameras() {
			want = want || c.TimeSync
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !want {
		if m.sntpCancel != nil {
			m.sntpCancel()
			m.sntp.Close()
			m.sntp, m.sntpCancel = nil, nil
		}
		return
	}
	if m.sntp != nil {
		return
	}
	s := &SNTPServer{Addr: m.TimeAddr}
	if _, err := s.Listen(); err != nil {
		slog.Warn("nvr: time server not started", "err", err)
		return
	}
	sctx, cancel := context.WithCancel(ctx)
	m.sntp, m.sntpCancel = s, cancel
	go s.Serve(sctx)
}

// prepareRoot creates the recordings folder. On Btrfs it is a subvolume of
// its own, so snapshots of the volume do not contain the recordings.
func prepareRoot(ctx context.Context, root string) {
	if root == "" {
		return
	}
	if _, err := os.Stat(root); err != nil {
		if exec.CommandContext(ctx, "btrfs", "subvolume", "create", root).Run() != nil {
			if err := os.MkdirAll(root, 0o755); err != nil {
				slog.Warn("nvr: recordings folder not created", "err", err)
				return
			}
		}
	}
	// Backups skip a folder "nvr" with this marker.
	marker := filepath.Join(root, Marker)
	if _, err := os.Stat(marker); err != nil {
		_ = os.WriteFile(marker, []byte("NVR recordings: not part of backups\n"), 0o644)
	}
}

// Prune applies the retention once.
func (m *Manager) Prune(now time.Time) {
	root := m.Root()
	st := m.Store.Settings()
	if root == "" || !st.Enabled {
		return
	}
	cams := m.Store.Cameras()
	res := PruneAll(root, cams, st.KeepDays, int64(st.ReserveGB)<<30, now, nil)
	for _, c := range cams {
		pruneEvents(root, c.ID, c.EventKeep(st), now)
	}
	if res.Files > 0 {
		slog.Info("nvr: old recordings removed", "files", res.Files, "bytes", res.Bytes, "shortened", len(res.Shortened))
	}
	// Never shorten in silence: one notice per camera and day.
	for _, c := range cams {
		left, ok := res.Shortened[c.ID]
		if !ok || m.Notify == nil {
			continue
		}
		day := now.Format("2006-01-02")
		m.mu.Lock()
		if m.shortNoticed == nil {
			m.shortNoticed = map[string]string{}
		}
		already := m.shortNoticed[c.ID] == day
		m.shortNoticed[c.ID] = day
		m.mu.Unlock()
		if !already {
			slog.Warn("nvr: retention shortened, space ran short", "camera", c.ID, "days_left", left, "wanted", c.Keep(st))
			m.Notify("warning", "nvr_retention_short", fmt.Sprintf("%s: %d/%d", c.Name, left, c.Keep(st)))
		}
	}
}

// onEvent sends a notification when an event starts while the NVR is armed
// for that camera, at most once per quiet period.
func (m *Manager) onEvent(c Camera, e Event) {
	arming := m.Store.Settings().Arming
	if !e.End.IsZero() || m.Notify == nil || !arming.Active(time.Now()) || !arming.Watches(c.ID) {
		return
	}
	m.mu.Lock()
	if m.notified == nil {
		m.notified = map[string]time.Time{}
	}
	if last, ok := m.notified[c.ID]; ok && time.Since(last) < arming.quiet() {
		m.mu.Unlock()
		return
	}
	m.notified[c.ID] = time.Now()
	m.mu.Unlock()
	m.Notify("info", "nvr_motion", c.Name)
}

// offlineAfter is how long a recording camera may write nothing before it
// is reported offline.
const offlineAfter = 3 * time.Minute

// CheckOnline reports cameras that stopped delivering pictures, and their
// return. A camera counts as delivering while its newest recording file
// keeps growing.
func (m *Manager) CheckOnline(now time.Time) {
	root := m.Root()
	st := m.Store.Settings()
	if root == "" || !st.Enabled || m.Notify == nil {
		return
	}
	m.mu.Lock()
	since := m.since
	m.mu.Unlock()
	if now.Sub(since) < offlineAfter {
		return
	}
	for _, c := range m.Store.Cameras() {
		if c.Mode() == "off" {
			continue
		}
		latest := LatestWrite(root, c.ID)
		down := latest.IsZero() && now.Sub(since) >= offlineAfter || !latest.IsZero() && now.Sub(latest) > offlineAfter
		m.mu.Lock()
		if m.offline == nil {
			m.offline = map[string]bool{}
		}
		was := m.offline[c.ID]
		m.offline[c.ID] = down
		m.mu.Unlock()
		switch {
		case down && !was:
			slog.Warn("nvr: camera delivers no pictures", "camera", c.ID)
			m.Notify("warning", "nvr_camera_offline", c.Name)
		case !down && was:
			m.Notify("info", "nvr_camera_online", c.Name)
		}
	}
}
