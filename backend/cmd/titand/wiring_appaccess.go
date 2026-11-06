package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appaccess"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
)

// autoAppsInterval is how often the apps with a WebUI are looked at for
// their automatic HTTPS names (Docker only, never /config).
const autoAppsInterval = 30 * time.Second

// wireAppAccess wires the rights of accounts on Docker apps, the browser
// gate and the automatic HTTPS names of apps (PLAN 61).
func (w *titand) wireAppAccess() error {
	ctx := w.ctx
	w.appRights = appaccess.New(w.store)
	w.appRights.Lookup = func(name string) (appaccess.Account, bool) {
		user, ok := w.users.Get(name)
		if !ok {
			return appaccess.Account{}, false
		}
		return appaccess.Account{Name: user.Name, Admin: user.Role.CanAdminister(), Disabled: user.Disabled}, true
	}
	if err := w.appRights.Load(); err != nil {
		slog.Warn("app rights not readable from /config", "err", err)
	}
	w.routes.Access = w.appRights
	// The names the web UI is reached under besides the NAS's own name and
	// addresses: they may frame apps.
	tailnetName := cachedTailscaleName(w.tailnet)
	w.routes.UIHosts = func() []string {
		var hosts []string
		if w.acmeCerts != nil {
			if c := w.acmeCerts.Status().Certificate; c != nil {
				hosts = append(hosts, c.Names...)
			}
		}
		if w.certs != nil {
			if info := w.certs.Info(); info.Kind == tlscert.KindCustom {
				hosts = append(hosts, info.Names...)
			}
		}
		if w.dynamicDNS != nil {
			hosts = append(hosts, w.dynamicDNS.Hostnames()...)
		}
		if name := tailnetName(); name != "" {
			hosts = append(hosts, name)
		}
		out := hosts[:0]
		for _, host := range hosts {
			// Wildcards name apps, not the web UI.
			if !strings.HasPrefix(host, "*.") {
				out = append(out, host)
			}
		}
		return out
	}
	w.background(func() { w.feedAutoApps(ctx) })
	return nil
}

// feedAutoApps tells the app proxy which apps have a WebUI, as long as
// Docker answers; while it does not, the names stay as they were.
func (w *titand) feedAutoApps(ctx context.Context) {
	ticker := time.NewTicker(autoAppsInterval)
	defer ticker.Stop()
	for {
		if status := w.containers.Status(ctx); status.Running {
			w.routes.SetAutoApps(autoApps(status.Containers))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// autoApps are the apps (Compose projects) with a WebUI label that names a
// port of the NAS, with that port. Containers without a project and
// WebUIs elsewhere get no automatic name.
func autoApps(containers []docker.Container) map[string]int {
	out := map[string]int{}
	for _, c := range containers {
		if c.App == "" {
			continue
		}
		if port := appproxy.WebUIPort(c.WebUI); port > 0 {
			if _, seen := out[c.App]; !seen {
				out[c.App] = port
			}
		}
	}
	return out
}
