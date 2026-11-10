package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/app"
)

// installApp loads titand of a release next to the running system and
// restarts titand with it, on trial. The NAS keeps running.
func (m *Manager) installApp(ctx context.Context, manifest Manifest, raw, signature []byte) error {
	if m.Working != nil && m.Working() {
		return ErrWorking
	}
	if len(raw) == 0 || len(signature) == 0 {
		return ErrNothing
	}
	var room unix.Statfs_t
	if err := unix.Statfs(m.bootDir, &room); err != nil {
		return err
	}
	if free := int64(room.Bavail) * int64(room.Bsize); free < manifest.App.Size+(8<<20) {
		return fmt.Errorf("%w: %d MiB needed, %d MiB free", ErrNoSpace, manifest.App.Size>>20, free>>20)
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	m.setProgress(func(p *Progress) { *p = Progress{Phase: "download", File: "titand"} })
	go func() {
		defer m.work.Unlock()
		err := m.loadApp(context.WithoutCancel(ctx), manifest, raw, signature)
		if err != nil {
			slog.Error("app update not installed", "version", manifest.Version, "err", err)
			m.setProgress(func(p *Progress) { *p = Progress{Phase: "failed", Error: err.Error()} })
			return
		}
		m.setProgress(func(p *Progress) { *p = Progress{Phase: "restart", Percent: 100} })
		// The answer to the request and a look at the progress should get
		// out before titand goes.
		time.Sleep(m.restartDelay)
		// Work that began while titand was downloading (an app that is
		// being updated or removed, a copy, a backup) would be cut off
		// by the restart: it is waited for, though not for ever.
		for waited := time.Duration(0); m.Working != nil && m.Working() && waited < restartWait; waited += restartPoll {
			time.Sleep(restartPoll)
		}
		m.restart()
	}()
	return nil
}

// restartWait is how long a downloaded app update waits for running work
// before it restarts titand anyway; restartPoll is how often it looks.
var (
	restartWait = 15 * time.Minute
	restartPoll = 5 * time.Second
)

func (m *Manager) loadApp(ctx context.Context, manifest Manifest, raw, signature []byte) error {
	slog.Info("installing app update", "version", manifest.Version, "base", manifest.App.Base)
	file := File{Name: "titand", Asset: manifest.App.Asset, SHA256: manifest.App.SHA256, Size: manifest.App.Size}
	if !assetName.MatchString(file.Asset) || !hexSHA256.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxFile {
		return fmt.Errorf("%w: app", ErrManifest)
	}
	dest := m.Apps.PartPath(manifest.Version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	url := m.source() + "/releases/download/" + manifest.Tag + "/" + file.Asset
	if err := m.download(ctx, url, dest, file, func(n int64, speed uint64) {
		m.setProgress(func(p *Progress) {
			p.File, p.Percent, p.SpeedBytesPerSecond = "titand", 100*float64(n)/float64(file.Size), speed
		})
	}); err != nil {
		return err
	}
	if err := m.Apps.Install(manifest.Version, raw, signature, dest); err != nil {
		return err
	}
	m.settleApp()
	m.mu.Lock()
	m.available, m.availableRaw, m.availableSig = nil, nil, nil
	m.mu.Unlock()
	return nil
}

// restart restarts titand: the base starts the app that is now current.
func (m *Manager) restart() {
	if m.Restart == nil {
		return
	}
	slog.Info("restarting titand for the app update")
	if err := m.Restart(); err != nil {
		slog.Error("titand not restarted", "err", err)
		m.setProgress(func(p *Progress) { *p = Progress{Phase: "failed", Error: err.Error()} })
	}
}

// trialRestartAfter is how many minutes in a row an app may stand on trial
// on the stick while this titand is still another one, before titand is
// restarted again.
const trialRestartAfter = 10

// restartForTrial ends a trial that never began: the stick names a new app
// on trial, but the restart into it did not happen (the restart command
// failed, titand was busy past its patience and something went wrong
// after). Without it every installation says "busy" and the stick is read
// every minute until someone restarts the NAS. Called once a minute.
func (m *Manager) restartForTrial() {
	if m.Apps == nil {
		return
	}
	m.mu.Lock()
	cache, phase := m.appCache, m.progress.Phase
	m.mu.Unlock()
	waiting := cache != nil && cache.Trial && cache.Current != "" && m.Apps.Running() != cache.Current &&
		phase != "download" && (m.Working == nil || !m.Working())
	if !waiting {
		m.trialMinutes = 0
		return
	}
	if m.trialMinutes++; m.trialMinutes < trialRestartAfter {
		return
	}
	m.trialMinutes = 0
	slog.Warn("an app update is on trial but titand did not restart into it, restarting", "app", cache.Current)
	m.restart()
}

// AppRollback goes back to the app before, or to the titand of the system
// image, and restarts titand. Going back to an app is on trial too.
func (m *Manager) AppRollback() error {
	if m.Disabled || m.Apps == nil {
		return ErrDisabled
	}
	m.mu.Lock()
	pending, phase := m.pending, m.progress.Phase
	m.mu.Unlock()
	if pending != nil || phase == "download" || phase == "switch" || phase == "restart" {
		return ErrBusy
	}
	if m.Working != nil && m.Working() {
		return ErrWorking
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	defer m.work.Unlock()
	target, err := m.Apps.Rollback()
	switch {
	case errors.Is(err, app.ErrBusy):
		return ErrBusy
	case errors.Is(err, app.ErrNothing):
		return ErrNothing
	case err != nil:
		return err
	}
	if target == "" {
		slog.Info("going back to the titand of the system image", "base", m.Base, "running", m.version)
	} else {
		slog.Info("going back to the app before", "app", target, "running", m.version)
	}
	m.settleApp()
	m.setProgress(func(p *Progress) { *p = Progress{Phase: "restart", Percent: 100} })
	go func() {
		time.Sleep(m.restartDelay)
		m.restart()
	}()
	return nil
}

// appOnTrial says whether the app of this slot still has to prove itself.
func (m *Manager) appOnTrial() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appTrial
}
