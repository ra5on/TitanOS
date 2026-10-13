// titand is the TitanOS daemon. It serves the web UI and the API and manages
// storage, shares, containers and VMs.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/api"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/version"
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

func run(opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := config.Open(opts.configDir)
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		// Not fatal: the web UI says that /config is missing and titand
		// tries again when someone opens it.
		slog.Warn("accounts not readable, is the stick plugged in?", "config", opts.configDir, "err", err)
	}

	var power system.Power = system.SystemdPower{}
	if opts.dev {
		power = system.NoPower{}
	}
	handler := api.New(api.Options{
		UI:            webui.FS(),
		Store:         store,
		Users:         users,
		Monitor:       system.NewMonitor(ctx),
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
