// titand is the TitanOS daemon. It serves the web UI and the API and manages
// storage, shares, containers and VMs.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/api"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/webui"
)

type options struct {
	configDir string
	httpsAddr string
	httpAddr  string
	dev       bool
	devAddr   string
}

func main() {
	var opts options
	flag.StringVar(&opts.configDir, "config", "/config", "directory with the persistent configuration")
	flag.StringVar(&opts.httpsAddr, "https", ":443", "address of the web UI (HTTPS)")
	flag.StringVar(&opts.httpAddr, "http", ":80", "address that redirects to HTTPS; empty to turn off")
	flag.BoolVar(&opts.dev, "dev", false, "development mode: plain HTTP on -dev-listen, no reboot or poweroff")
	flag.StringVar(&opts.devAddr, "dev-listen", "127.0.0.1:8080", "address in development mode")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	if err := run(opts); err != nil {
		slog.Error("titand stopped", "err", err)
		os.Exit(1)
	}
}

// journalWriter marks each log line with its level the way journald reads
// it from a service's output ("<3>" is an error), so that the log view
// can tell errors and warnings of titand from the rest.
type journalWriter struct{ out io.Writer }

func (w journalWriter) Write(line []byte) (int, error) {
	prefix := "<6>"
	switch {
	case bytes.HasPrefix(line, []byte("ERROR")):
		prefix = "<3>"
	case bytes.HasPrefix(line, []byte("WARN")):
		prefix = "<4>"
	case bytes.HasPrefix(line, []byte("DEBUG")):
		prefix = "<7>"
	}
	if _, err := w.out.Write(append([]byte(prefix), line...)); err != nil {
		return 0, err
	}
	return len(line), nil
}

func run(opts options) error {
	if !opts.dev {
		// The journal keeps the time itself.
		log.SetFlags(0)
		log.SetOutput(journalWriter{os.Stderr})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := config.Open(opts.configDir)
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		// Not fatal: the web UI says that /config is missing and titand
		// tries again when someone opens it.
		slog.Warn("accounts not readable, is the stick plugged in?", "config", opts.configDir, "err", err)
	}

	// Time zone and network first: everything after it relies on them.
	settings := system.NewSettings(store)
	settings.Disabled = opts.dev
	if err := settings.Load(); err != nil {
		slog.Warn("system settings not put in place", "err", err)
	}
	connection := network.NewManager(store)
	connection.Disabled = opts.dev
	if err := connection.Load(ctx); err != nil {
		slog.Warn("network configuration not put in place", "err", err)
	}

	volumes := storage.NewManager(store)
	volumes.ReadOnly = opts.dev
	if err := volumes.Load(); err != nil {
		slog.Warn("volumes not readable from /config", "err", err)
	}
	// Mounts the volumes as their arrays come up and reads the disks' health.
	go volumes.Run(ctx)

	// Keeps Linux and Samba accounts, smb.conf, the NFS exports and the
	// services in line with accounts, shares and mounted volumes.
	folders := shares.NewManager(store)
	folders.Disabled = opts.dev
	if err := folders.Load(); err != nil {
		slog.Warn("shares not readable from /config", "err", err)
	}
	go folders.Run(ctx, func() shares.State {
		return shares.State{Users: users.List(), Mounted: volumes.Mounted()}
	})

	// Runs the Docker engine while its volume is mounted.
	containers := docker.NewManager(store, opts.configDir)
	containers.Disabled = opts.dev
	if err := containers.Load(); err != nil {
		slog.Warn("docker settings not readable from /config", "err", err)
	}
	go containers.Run(ctx, volumes.Mounted)

	// Knows the apps that can be installed, from the built-in catalog and
	// from sources on the network.
	appCatalog := catalog.NewManager(store)
	if err := appCatalog.Load(); err != nil {
		slog.Warn("app sources not readable from /config", "err", err)
	}
	go appCatalog.Run(ctx)

	// Virtual machines; starts those marked for autostart once their volumes are there.
	machines := vm.NewManager(store, shares.UsersGID)
	machines.Disabled = opts.dev
	if err := machines.Load(); err != nil {
		slog.Warn("VMs not readable from /config", "err", err)
	}
	// Only disks that are in no volume may be handed to a VM as a whole.
	machines.FreeDisks = func() []string {
		status, err := volumes.Status(ctx)
		if err != nil {
			return nil
		}
		var free []string
		for _, disk := range status.Disks {
			if disk.Role == "free" {
				free = append(free, disk.Name)
			}
		}
		return free
	}
	go machines.Run(ctx, volumes.Mounted)

	// System extensions on a volume, laid over the system once it is mounted.
	extensions := plugins.NewManager(store)
	extensions.Disabled = opts.dev
	if err := extensions.Load(); err != nil {
		slog.Warn("plugin settings not readable from /config", "err", err)
	}
	go extensions.Run(ctx, volumes.Mounted)

	// The root shell of the terminal panel.
	shells := terminal.NewManager(store)
	shells.Disabled = opts.dev
	if err := shells.Load(); err != nil {
		slog.Warn("terminal settings not readable from /config", "err", err)
	}

	// Tells in the web UI, by mail and by push message when a volume or a
	// disk needs attention.
	notifier := notify.NewManager(store)
	if err := notifier.Load(); err != nil {
		slog.Warn("notification settings not readable from /config", "err", err)
	}
	go notify.NewWatcher(notifier).Run(ctx, volumes.Status)

	// Copies of /config on a volume: the stick is the only place where
	// the configuration lives, and sticks wear out.
	stickBackup := configbackup.NewManager(store, version.Version)
	stickBackup.Disabled = opts.dev
	if err := stickBackup.Load(); err != nil {
		slog.Warn("backup settings not readable from /config", "err", err)
	}
	// A failure is reported once, and again only after a backup worked.
	backupFailed := false
	stickBackup.OnResult = func(volume string, err error) {
		if err != nil && !backupFailed {
			notifier.Notify(notify.Warning, "config_backup_failed", volume)
		}
		backupFailed = err != nil
	}
	go stickBackup.Run(ctx, volumes.Mounted)

	var power system.Power = system.SystemdPower{}
	if opts.dev {
		power = system.NoPower{}
	}
	handler := api.New(api.Options{
		UI:            webui.FS(),
		Store:         store,
		Users:         users,
		Monitor:       system.NewMonitor(ctx),
		Storage:       volumes,
		Shares:        folders,
		Files:         files.NewManager(shares.UsersGID),
		Docker:        containers,
		Catalog:       appCatalog,
		Terminal:      shells,
		VMs:           machines,
		Hardware:      hardware.NewManager(),
		Plugins:       extensions,
		Network:       connection,
		Notify:        notifier,
		ConfigBackup:  stickBackup,
		Diagnose:      diagnose.NewManager(store, version.Version),
		Settings:      settings,
		Power:         power,
		Version:       version.Version,
		SecureCookies: !opts.dev,
	})

	var servers []*http.Server
	errc := make(chan error, 2)
	serve := func(srv *http.Server, useTLS bool) {
		servers = append(servers, srv)
		go func() {
			if useTLS {
				errc <- srv.ListenAndServeTLS("", "")
			} else {
				errc <- srv.ListenAndServe()
			}
		}()
	}

	if opts.dev {
		serve(newServer(opts.devAddr, handler), false)
		slog.Info("titand started in development mode", "version", version.Version, "listen", opts.devAddr, "config", opts.configDir, "webui", webui.Embedded)
	} else {
		hostname, _ := os.Hostname()
		cert, err := tlscert.LoadOrCreate(store, hostname)
		if len(cert.Certificate) == 0 {
			return fmt.Errorf("TLS certificate: %w", err)
		}
		if err != nil {
			slog.Warn("using a temporary TLS certificate", "err", err)
		}
		https := newServer(opts.httpsAddr, handler)
		https.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		serve(https, true)
		if opts.httpAddr != "" {
			serve(newServer(opts.httpAddr, redirectToHTTPS(opts.httpsAddr)), false)
		}
		slog.Info("titand started", "version", version.Version, "https", opts.httpsAddr, "http", opts.httpAddr, "config", opts.configDir, "webui", webui.Embedded)
	}

	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil && err == nil {
			err = shutdownErr
		}
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// redirectToHTTPS sends every plain HTTP request to the same path on the
// HTTPS address.
func redirectToHTTPS(httpsAddr string) http.Handler {
	_, port, _ := net.SplitHostPort(httpsAddr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if port != "" && port != "443" {
			host = net.JoinHostPort(host, port)
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}
