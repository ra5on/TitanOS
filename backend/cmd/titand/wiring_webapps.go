package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

// wireWebapps wires "Web-Apps direkt" (PLAN 66). The archives come from the
// signed manifest of this titand's own release, like the image of the
// remote view. In development mode they come from -webapps-dir and are
// unpacked into a temporary folder, never onto a data volume.
func (w *titand) wireWebapps() error {
	w.webApps = webapps.New(w.store)
	w.webApps.Release = w.extensions
	w.webApps.Version = strings.TrimPrefix(version.Version, "v")
	w.webApps.Dev = w.opts.dev
	w.webApps.DevDir = w.opts.webappsDir
	w.webApps.Mounted = w.volumes.Mounted
	w.webApps.DockerVolume = func() string { return w.containers.Current().Volume }
	w.webApps.Ready = func() bool { return w.users.Count() > 0 }
	w.webApps.Notify = func(kind, subject string) { w.notifier.Notify(notify.Info, kind, subject) }
	w.webApps.Docker = func() []webapps.DockerApp {
		ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
		defer cancel()
		return webapps.DetectDockerStatus(w.containers.Status(ctx))
	}
	if err := w.webApps.Load(); err != nil {
		slog.Warn("web apps not readable from /config", "err", err)
	}
	ctx := w.ctx
	w.background(func() { w.webApps.Run(ctx) })
	return nil
}
