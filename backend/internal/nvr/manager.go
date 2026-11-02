package nvr

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Manager ties the store, the recorder and the retention together.
type Manager struct {
	Store *Store
	Rec   *Recorder
	// VolumePath returns the mount path of a volume by name.
	VolumePath func(name string) (string, bool)
	Prober     *Prober

	mu     sync.Mutex
	ctx    context.Context
	pruned time.Time
}

func NewManager(cfg *config.Store, volumePath func(string) (string, bool)) *Manager {
	m := &Manager{Store: NewStore(cfg), VolumePath: volumePath, Prober: &Prober{}}
	m.Rec = &Recorder{Store: m.Store, Root: m.Root}
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
	for {
		select {
		case <-ctx.Done():
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
	m.Rec.Apply(ctx, m.Store.Settings().Enabled)
}

// prepareRoot creates the recordings folder. On Btrfs it is a subvolume of
// its own, so snapshots of the volume do not contain the recordings.
func prepareRoot(ctx context.Context, root string) {
	if root == "" {
		return
	}
	if _, err := os.Stat(root); err == nil {
		return
	}
	if err := exec.CommandContext(ctx, "btrfs", "subvolume", "create", root).Run(); err == nil {
		return
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		slog.Warn("nvr: recordings folder not created", "err", err)
	}
}

// Prune applies the retention once.
func (m *Manager) Prune(now time.Time) {
	root := m.Root()
	st := m.Store.Settings()
	if root == "" || !st.Enabled {
		return
	}
	files, bytes := Prune(root, m.Store.Cameras(), st.KeepDays, int64(st.ReserveGB)<<30, now, nil)
	if files > 0 {
		slog.Info("nvr: old recordings removed", "files", files, "bytes", bytes)
	}
}
