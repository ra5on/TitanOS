package api

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

// Old Craft apps are replaced by our own builds without asking (owner
// 9.11.2026, PLAN 75-20): an installed web app of an original (WordCraft …)
// gives way to its successor (T-Word …), a Docker version of a Craft app
// is removed with its Docker data once the T-app is installed. Files in
// the shares are never touched. It runs shortly after the start and then
// every hour, since Docker and the app volume may come up late.

// craftReplaceDelay and craftReplaceEvery: tests shorten them.
var (
	craftReplaceDelay = 2 * time.Minute
	craftReplaceEvery = time.Hour
)

// RunCraftReplacement replaces old Craft apps until ctx ends.
func (s *Server) RunCraftReplacement(ctx context.Context) {
	if s.opts.Webapps == nil {
		return
	}
	wait := craftReplaceDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = craftReplaceEvery
		s.replaceCraftOnce(ctx)
	}
}

// replaceCraftOnce does one round: web originals first, then Docker apps.
func (s *Server) replaceCraftOnce(ctx context.Context) {
	if s.opts.Webapps.Volume() == "" || s.opts.Webapps.NeedsVolume() {
		return // no place for web apps yet, or the owner has yet to choose it
	}
	s.replaceCraftWebapps(ctx)
	s.replaceCraftDocker(ctx)
}

// replaceCraftWebapps installs the successor of each installed original
// and then removes the original.
func (s *Server) replaceCraftWebapps(ctx context.Context) {
	list := s.opts.Webapps.List()
	for _, st := range list {
		if !st.Installed || st.Successor == "" || st.State == "downloading" {
			continue
		}
		next := st.Successor
		if !slices.ContainsFunc(list, func(o webapps.Status) bool { return o.ID == next && o.Installed && o.State == "ready" }) {
			if err := s.opts.Webapps.Install(next); err != nil {
				slog.Warn("successor not installed", "app", st.ID, "successor", next, "err", err)
				continue
			}
		}
		if err := s.awaitWebapp(ctx, next); err != nil {
			slog.Warn("original kept: its successor did not install", "app", st.ID, "successor", next, "err", err)
			continue
		}
		if err := s.opts.Webapps.Uninstall(st.ID); err != nil {
			slog.Warn("original not removed", "app", st.ID, "err", err)
			continue
		}
		s.moveFileDefaults(st.ID, next)
		slog.Info("web app replaced by our own build", "app", st.ID, "successor", next)
	}
}

// replaceCraftDocker removes the Docker versions of Craft apps, with their
// Docker data, after their T-app installed. It shares the state of the
// migration dialog, so the App-Center shows the progress.
func (s *Server) replaceCraftDocker(ctx context.Context) {
	if s.migration.detect == nil && (s.opts.Docker == nil || s.opts.Docker.Disabled) {
		return
	}
	found := s.webappsArea().dockerCraftApps(context.WithValue(ctx, freshDetection{}, true))
	if len(found) == 0 {
		return
	}
	var items []MigrationItem
	var install []string
	for _, d := range found {
		if slices.ContainsFunc(items, func(it MigrationItem) bool { return it.App == d.App }) {
			continue
		}
		items = append(items, MigrationItem{App: d.App, Webapp: d.Webapp, State: "waiting"})
		if !slices.Contains(install, d.Webapp) {
			install = append(install, d.Webapp)
		}
	}
	s.migration.mu.Lock()
	if s.migration.last != nil && s.migration.last.Running {
		s.migration.mu.Unlock()
		return // a migration of the dialog runs; next round
	}
	s.migration.last = &MigrationStatus{Running: true, Started: time.Now(), Install: install, Items: items}
	s.migration.mu.Unlock()
	ready := map[string]bool{}
	for _, st := range s.opts.Webapps.List() {
		ready[st.ID] = st.Installed && st.State == "ready"
	}
	for _, id := range install {
		if ready[id] {
			continue
		}
		if err := s.opts.Webapps.Install(id); err != nil {
			slog.Warn("web app install not started", "app", id, "err", err)
		}
	}
	slog.Info("Docker versions of Craft apps are replaced", "apps", len(items))
	s.webappsArea().runMigration(ctx, "TitanOS", true)
}

// awaitWebapp waits at most two hours for an install.
func (s *Server) awaitWebapp(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	return s.opts.Webapps.Await(ctx, id)
}

// moveFileDefaults points the "open with" choices of the original at its
// successor ({"docx":"webapp:wordcraft"} → "webapp:t-word").
func (s *Server) moveFileDefaults(from, to string) {
	if s.prefs == nil {
		return
	}
	if err := s.prefs.replaceOpenWith("webapp:"+from, "webapp:"+to); err != nil {
		slog.Warn("open-with choices not moved", "app", from, "err", err)
	}
}
