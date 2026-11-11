package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// wireAccess wires the copies of /config, SSH and the certificate of the web UI.
func (w *titand) wireAccess() error {
	ctx := w.ctx
	// Copies of /config on a volume: the stick is the only place where
	// the configuration lives, and sticks wear out.
	w.stickBackup = configbackup.NewManager(w.store, version.Version)
	w.stickBackup.Disabled = w.opts.dev
	w.stickBackup.DefaultVolume = w.systemData.Default
	if err := w.stickBackup.Load(); err != nil {
		slog.Warn("backup settings not readable from /config", "err", err)
	}
	// A failure is reported once, and again only after a backup worked.
	w.backupFailed = false
	w.stickBackup.OnResult = func(volume string, err error) {
		if err != nil && !w.backupFailed {
			w.notifier.Notify(notify.Warning, "config_backup_failed", volume)
		}
		w.backupFailed = err != nil
	}
	w.stickBackup.OnNoVolume = func(volume string) {
		w.notifier.Notify(notify.Warning, "config_backup_no_volume", volume)
	}
	w.background(func() { w.stickBackup.Run(ctx, w.volumes.Mounted) })

	// SSH is off unless switched on; then for administrators with a key.
	w.access = sshd.NewManager(w.store, shares.UsersGID)
	w.access.Disabled = w.opts.dev
	if err := w.access.Load(); err != nil {
		slog.Warn("SSH settings not readable from /config", "err", err)
	}
	w.background(func() { w.access.Run(ctx, w.users.List) })

	// The certificate of the web UI: the administrator's own or one made here.
	if !w.opts.dev {
		hostname, _ := os.Hostname()
		var err error
		w.certs, err = tlscert.NewManager(w.store, hostname)
		if w.certs == nil {
			return fmt.Errorf("TLS certificate: %w", err)
		}
		if err != nil {
			slog.Warn("using a temporary TLS certificate", "err", err)
		}
		// The domains of the app proxy known when the TitanOS authority is
		// made go into its name constraints.
		w.certs.CADomains = func() []string {
			domains := w.routes.Domains()
			if domain := w.routes.Settings().Domain; domain != "" {
				domains = append(domains, domain)
			}
			return domains
		}
		w.background(func() { watchCertificate(ctx, w.certs, w.notifier) })
	}
	return nil
}

// wireRemoteAccess wires certificates from Let's Encrypt, dynamic DNS, IPv64 and WireGuard.
func (w *titand) wireRemoteAccess() error {
	ctx := w.ctx
	// Certificates from Let's Encrypt, served next to the own or
	// self-signed one without a restart; renewed by themselves.
	w.acmeCerts = letsencrypt.New(w.store)
	w.acmeCerts.Disabled = w.opts.dev || w.certs == nil
	w.acmeCerts.HTTPPort = !w.opts.dev && w.opts.httpAddr != ""
	w.acmeCerts.AppDomain = func() string { return w.routes.Settings().Domain }
	w.acmeCerts.AppNames = w.routes.Domains
	// New full domains of app routes join the certificate soon.
	w.kickFolders = w.routes.OnChange
	w.routes.OnChange = func() {
		if w.kickFolders != nil {
			w.kickFolders()
		}
		w.acmeCerts.NamesChanged()
	}
	w.acmeCerts.Install = func(cert *tls.Certificate) {
		if w.certs != nil {
			w.certs.SetACME(cert)
		}
	}
	w.acmeCerts.OnEvent = func(severity, kind, subject string) { w.notifier.Notify(severity, kind, subject) }
	w.acmeCerts.Activity = func(action, target string, ok bool) {
		w.audit.Record(activity.Entry{Via: activity.ViaSystem, Action: action, Target: target, OK: ok})
	}
	if err := w.acmeCerts.Load(); err != nil {
		slog.Warn("Let's Encrypt settings not readable from /config", "err", err)
	}
	w.background(func() { w.acmeCerts.Run(ctx) })
	// The NAS's own name in the tailnet gets the certificate Tailscale hands
	// out once HTTPS is on in the tailnet (passkeys and web push need it).
	if w.certs != nil {
		w.background(func() { w.tailnet.RunCertificate(ctx, w.certs.SetTailnet) })
	}
	// Dynamic DNS: host names at the public addresses of the NAS.
	w.dynamicDNS = ddns.New(w.store)
	w.dynamicDNS.Disabled = w.opts.dev
	w.dynamicDNS.OnEvent = w.acmeCerts.OnEvent
	w.dynamicDNS.Activity = w.acmeCerts.Activity
	if err := w.dynamicDNS.Load(); err != nil {
		slog.Warn("dynamic DNS settings not readable from /config", "err", err)
	}
	w.background(func() { w.dynamicDNS.Run(ctx) })
	w.beforePowerOff = func() { w.dynamicDNS.PauseForShutdown(context.Background()) }
	// The IPv64.net account beyond dynamic DNS: notifications from polling,
	// the v64 blocklist for the firewall and the WireGuard tunnel into a
	// Cloud Router; all off until switched on.
	w.account64 = ipv64.NewManager(w.store)
	w.account64.Disabled = w.opts.dev
	w.account64.Fallback = func() (string, string) { return w.dynamicDNS.IPv64Key(), w.acmeCerts.IPv64Key() }
	w.account64.Notify = func(severity, kind, subject string) { w.notifier.Notify(severity, kind, subject) }
	w.account64.Firewall = w.packetFilter
	if err := w.account64.Load(ctx); err != nil {
		slog.Warn("IPv64 settings not readable from /config", "err", err)
	}
	w.background(func() { w.account64.Run(ctx) })
	// The WireGuard server of the NAS, off unless switched on.
	w.vpnServer = wgserver.New(w.store)
	w.vpnServer.Disabled = w.opts.dev
	w.vpnServer.HomeNetworks = sshd.HomeNetworks
	w.vpnServer.SuggestEndpoint = func() string {
		if names := w.dynamicDNS.Hostnames(); len(names) > 0 {
			return names[0]
		}
		return ""
	}
	if err := w.vpnServer.Load(); err != nil {
		slog.Warn("WireGuard settings not readable from /config", "err", err)
	} else if err := w.vpnServer.Apply(ctx); err != nil {
		slog.Warn("WireGuard server not up", "err", err)
	}
	return nil
}

// cachedTailscaleName returns the MagicDNS name of this NAS for the
// cluster, asking tailscale at most every five minutes.
func cachedTailscaleName(t *tailscale.Manager) func() string {
	var (
		mu   sync.Mutex
		name string
		at   time.Time
	)
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > 5*time.Minute {
			st := t.Status()
			name, at = "", time.Now()
			if st.State == "Running" {
				name = st.Name
			}
		}
		return name
	}
}
