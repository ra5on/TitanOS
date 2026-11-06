package main

import (
	"log/slog"
	"strings"

	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/viewer"
	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// wireViewer wires the remote view of Docker apps (PLAN 63). Its image is
// the one of this titand's own release (not the base: an app release
// brings a viewer that matches it). In development mode it uses the local
// image titanos-viewer:dev and a temporary folder for profiles, never a
// data volume, and downloads nothing.
func (w *titand) wireViewer() error {
	w.remoteView = viewer.New(w.store)
	w.remoteView.Engine = w.containers
	w.remoteView.Release = w.extensions
	w.remoteView.Version = strings.TrimPrefix(version.Version, "v")
	w.remoteView.Dev = w.opts.dev
	w.remoteView.Mounted = w.volumes.Mounted
	w.remoteView.DockerVolume = func() string { return w.containers.Current().Volume }
	w.remoteView.Devices = w.devices.ContainerDevices
	w.remoteView.Leased = w.containers.LeasedHardware
	// Chromium's policy (only the app's address) comes from titand, read
	// only in the container: nothing in it can widen it.
	w.remoteView.Policy = proto.ChromiumPolicy
	if !w.opts.dev {
		// The host drops new connections from viewer networks (nft).
		w.remoteView.HostGuard = viewer.HostGuard(command.Exec{})
	}
	w.remoteView.Route = func(app string) (int, string, bool) {
		route, ok := w.routes.Route(app)
		if !ok || route.Host != "" {
			return 0, "", false
		}
		return route.Port, route.Scheme, true
	}
	if err := w.remoteView.Load(); err != nil {
		slog.Warn("remote view settings not readable from /config", "err", err)
	}
	ctx := w.ctx
	w.background(func() { w.remoteView.Run(ctx) })
	return nil
}
