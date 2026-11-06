// titand is the TitanOS daemon. It serves the web UI and the API and manages
// storage, shares, containers and VMs.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/app"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/update"
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
	checkBoot := flag.Bool("check-boot", false, "wait until the running titand is ready, for the boot check after an update")
	checkApp := flag.Bool("check-app", false, "wait until a new app update is ready, and go back if it does not get there")
	recovery := flag.Bool("network-recovery", false, "authenticated local-console network recovery")
	flag.Parse()
	if *recovery {
		if err := networkRecovery(opts.configDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if !opts.dev {
		// The journal keeps the time itself.
		log.SetFlags(0)
		log.SetOutput(journalWriter{os.Stderr})
	}

	if *checkApp {
		if slot := system.BootSlot(); slot != "" {
			if err := checkAppUpdate(app.New("/efi", slot), opts.httpsAddr, 5*time.Minute); err != nil {
				fmt.Fprintln(os.Stderr, "app update:", err)
				os.Exit(1)
			}
		}
		return
	}

	if *checkBoot {
		// Only a system the boot loader started on trial has to prove itself.
		trial := update.NewManager(config.Open(opts.configDir), version.Version, system.BootSlot())
		if !trial.OnTrial() {
			return
		}
		// Generous: slow or many disks, a late HBA or a Btrfs mirror waiting
		// for its members must not send a good update back.
		if err := waitReady(opts.httpsAddr, 15*time.Minute); err != nil {
			fmt.Fprintln(os.Stderr, "titand is not ready, this system stays on trial:", err)
			os.Exit(1)
		}
		if err := trial.Bless(); err != nil {
			fmt.Fprintln(os.Stderr, "could not mark this system good:", err)
			os.Exit(1)
		}
		return
	}

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	handOver(opts)
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

// handOver starts the app update of the running slot in place of this
// titand, if there is one that passes the checks. It returns only when the
// titand of the system image itself is to run.
func handOver(opts options) {
	if opts.dev || os.Getenv(app.Env) != "" {
		return
	}
	slot := system.BootSlot()
	if slot == "" {
		return
	}
	stick := app.New("/efi", slot)
	if stick.Base == "" {
		return
	}
	path, appVersion, err := stick.Launch()
	if err != nil {
		slog.Error("app update not started, running the titand of the system image", "err", err)
		return
	}
	if path == "" {
		return
	}
	slog.Info("handing over to the app update", "version", appVersion, "base", stick.Base)
	err = syscall.Exec(path, os.Args, append(os.Environ(), app.Env+"="+appVersion))
	slog.Error("app update could not be started, running the titand of the system image", "err", err)
}

// checkAppUpdate settles an app update on trial: once titand runs it and
// is ready, it is kept; if not within five minutes, the app before it (or
// the titand of the system image) runs again.
func checkAppUpdate(stick *app.Stick, httpsAddr string, patience time.Duration) error {
	state, err := stick.ReadState()
	if err != nil || !state.Trial {
		return err
	}
	err = waitApp(stick, state.Current, httpsAddr, patience)
	// The base may have given up on the app in the meantime.
	now, readErr := stick.ReadState()
	if readErr != nil {
		return readErr
	}
	if !now.Trial || now.Current != state.Current {
		return nil
	}
	if err == nil {
		_, err = stick.Confirm(true)
		slog.Info("app update is ready and stays", "version", state.Current)
		return err
	}
	slog.Error("app update did not become ready, going back", "version", state.Current, "err", err)
	if _, confirmErr := stick.Confirm(false); confirmErr != nil {
		return confirmErr
	}
	if restartErr := restartTitand(); restartErr != nil {
		return restartErr
	}
	return err
}

// restartTitand starts titand anew, with whatever app is current now.
var restartTitand = func() error {
	_, err := (command.Exec{}).Run(context.Background(), "", "systemctl", "--no-block", "restart", "titand.service")
	return err
}

// waitApp waits until titand runs the app and says it is ready.
func waitApp(stick *app.Stick, appVersion, httpsAddr string, patience time.Duration) error {
	deadline := time.Now().Add(patience)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return errors.New("not ready in time")
		}
		if stick.Running() == appVersion {
			if err := waitReady(httpsAddr, min(left, 20*time.Second)); err == nil && stick.Running() == appVersion {
				return nil
			}
		} else {
			// Started over and fell back, or still starting.
			if state, err := stick.ReadState(); err == nil && (!state.Trial || state.Current != appVersion) {
				return errors.New("the system image gave up on it")
			}
			time.Sleep(3 * time.Second)
		}
	}
}

func run(opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := &titand{ctx: ctx, opts: opts}
	if err := w.wire(); err != nil {
		return err
	}
	defer w.mediaImports.Close()

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

	// Requests for the HTTPS name of an app go to the app, all others to
	// the web UI; see package appproxy.
	site := w.routes.Handler(w.handler)
	if opts.dev {
		serve(newServer(opts.devAddr, site), false)
		slog.Info("titand started in development mode", "version", version.Version, "listen", opts.devAddr, "config", opts.configDir, "webui", webui.Embedded)
	} else {
		https := newServer(opts.httpsAddr, site)
		https.TLSConfig = &tls.Config{GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Other w.nodes of the cluster ask for its own certificate.
			if cert := w.nodes.Certificate(hello); cert != nil {
				return cert, nil
			}
			if names := w.routes.CertificateNames(hello.ServerName); names != nil {
				return w.certs.ForNames(hello.ServerName, names)
			}
			return w.certs.GetCertificate(hello)
		}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			// Other nodes show their own certificate (cluster.Verify).
			return w.nodes.ServerConfig(hello), nil
		}, MinVersion: tls.VersionTLS12}
		serve(https, true)
		if opts.httpAddr != "" {
			// Port 80 also answers the HTTP challenge of Let's Encrypt.
			serve(newServer(opts.httpAddr, w.acmeCerts.HTTPHandler(redirectToHTTPS(opts.httpsAddr))), false)
		}
		slog.Info("titand started", "version", version.Version, "https", opts.httpsAddr, "http", opts.httpAddr, "config", opts.configDir, "webui", webui.Embedded)
	}

	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	stop()
	w.sessions.Flush()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil && err == nil {
			err = shutdownErr
		}
	}
	// The background services end with the context; a service that hangs
	// must not keep the daemon from stopping.
	w.waitServices(shutdownCtx)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// waitReady asks the running titand until it says it is ready. A system
// that was just updated is only marked good after that.
func waitReady(httpsAddr string, patience time.Duration) error {
	_, port, err := net.SplitHostPort(httpsAddr)
	if err != nil {
		return err
	}
	// The certificate is the NAS's own, and this never leaves the machine.
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	deadline := time.Now().Add(patience)
	last := errors.New("no answer")
	reported := time.Now()
	for time.Now().Before(deadline) {
		// The journal says what the check is waiting for.
		if time.Since(reported) >= time.Minute {
			fmt.Fprintln(os.Stderr, "still waiting for titand:", last)
			reported = time.Now()
		}
		res, err := client.Get("https://" + net.JoinHostPort("127.0.0.1", port) + "/api/v1/health")
		if err == nil {
			var health struct{ Ready bool }
			err = json.NewDecoder(res.Body).Decode(&health)
			res.Body.Close()
			if err == nil && health.Ready {
				return nil
			}
			if err == nil {
				err = errors.New("the configuration or a volume of before is not there yet")
			}
		}
		last = err
		time.Sleep(3 * time.Second)
	}
	return last
}

// watchCertificate warns once when an own certificate is about to run
// out: unlike the one TitanOS makes, nobody renews it by itself.
func watchCertificate(ctx context.Context, certs *tlscert.Manager, notifier *notify.Manager) {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	warned := ""
	for {
		info := certs.Info()
		if info.Kind == tlscert.KindCustom && time.Until(info.NotAfter) < 14*24*time.Hour && warned != info.Fingerprint {
			warned = info.Fingerprint
			notifier.Notify(notify.Warning, "certificate_expiring", info.NotAfter.Format(time.DateOnly))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// proxyPorts returns the HTTPS port for links to apps (0 for the usual
// 443) and the ports of titand an app route must not point at.
func proxyPorts(opts options) (int, []int) {
	var https int
	var reserved []int
	addrs := []string{opts.httpsAddr, opts.httpAddr}
	if opts.dev {
		addrs = []string{opts.devAddr}
	}
	for _, addr := range addrs {
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		if port, err := strconv.Atoi(p); err == nil {
			reserved = append(reserved, port)
			if addr == opts.httpsAddr && port != 443 {
				https = port
			}
		}
	}
	return https, reserved
}

// reservedPorts are the ports of titand that are never published.
func reservedPorts(opts options) []int {
	_, reserved := proxyPorts(opts)
	return reserved
}

// webPorts are the ports of the web UI for the firewall.
func webPorts(opts options) firewall.WebPorts {
	ports := firewall.WebPorts{HTTP: -1}
	if _, p, err := net.SplitHostPort(opts.httpsAddr); err == nil {
		ports.HTTPS, _ = strconv.Atoi(p)
	}
	if opts.httpAddr != "" {
		if _, p, err := net.SplitHostPort(opts.httpAddr); err == nil {
			ports.HTTP, _ = strconv.Atoi(p)
		}
	}
	return ports
}

// firewallNetworks lists the networks of the NAS for own firewall zones.
func firewallNetworks(connection *network.Manager, engine []docker.EngineNetwork) []firewall.Network {
	out := []firewall.Network{}
	for _, n := range connection.Connections() {
		dev, ok := connection.Device(n.ID, false)
		if !ok {
			continue
		}
		name := n.Name
		if name == "" {
			name = n.ID
		}
		out = append(out, firewall.Network{ID: "net:" + n.ID, Kind: firewall.KindNetwork, Name: name, Device: dev, Subnets: interfaceSubnets(dev)})
	}
	for _, n := range engine {
		kind := firewall.KindDocker
		if n.Device == "" {
			kind = firewall.KindDockerLAN
		}
		out = append(out, firewall.Network{ID: "docker:" + n.Name, Kind: kind, Name: n.Name, Device: n.Device, Parent: n.Parent, Subnets: n.Subnets})
	}
	// NAT bridges of libvirt or LXC, where someone set them up by hand:
	// TitanOS's own VMs use the QEMU user network or a bridge of the LAN.
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if strings.HasPrefix(iface.Name, "virbr") || strings.HasPrefix(iface.Name, "lxcbr") {
				out = append(out, firewall.Network{ID: "vm:" + iface.Name, Kind: firewall.KindVM, Name: iface.Name, Device: iface.Name, Subnets: interfaceSubnets(iface.Name)})
			}
		}
	}
	return out
}

// interfaceSubnets are the networks of the addresses of an interface,
// without link-local ones.
func interfaceSubnets(name string) []string {
	out := []string{}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		masked := (&net.IPNet{IP: ipnet.IP.Mask(ipnet.Mask), Mask: ipnet.Mask}).String()
		if !slices.Contains(out, masked) {
			out = append(out, masked)
		}
	}
	return out
}

// firewallApps groups the published ports of containers by app: the
// Compose project, or the container itself.
func firewallApps(containers []docker.Container) []firewall.App {
	byKey := map[string]*firewall.App{}
	var keys []string
	for _, c := range containers {
		key, name := c.App, c.App
		if key == "" {
			key, name = "container:"+c.Name, c.Name
		}
		app := byKey[key]
		if app == nil {
			app = &firewall.App{Key: key, Name: name, Ports: []firewall.AppPort{}}
			byKey[key] = app
			keys = append(keys, key)
		}
		app.HostNetwork = app.HostNetwork || c.HostNetwork
		for _, p := range c.Ports {
			port := firewall.AppPort{Port: p.Host, Protocol: p.Protocol}
			if !slices.Contains(app.Ports, port) {
				app.Ports = append(app.Ports, port)
			}
		}
	}
	sort.Strings(keys)
	out := make([]firewall.App, 0, len(keys))
	for _, key := range keys {
		app := byKey[key]
		sort.Slice(app.Ports, func(i, j int) bool {
			if app.Ports[i].Port != app.Ports[j].Port {
				return app.Ports[i].Port < app.Ports[j].Port
			}
			return app.Ports[i].Protocol < app.Ports[j].Protocol
		})
		out = append(out, *app)
	}
	return out
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          newServerLog(),
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

// announcedPower runs before first, for planned shutdowns and restarts.
type announcedPower struct {
	system.Power
	before func()
}

func (p announcedPower) Reboot() error {
	p.before()
	return p.Power.Reboot()
}

func (p announcedPower) Poweroff() error {
	p.before()
	return p.Power.Poweroff()
}

// portOf returns the port of a listen address, fallback without one.
func portOf(address string, fallback int) int {
	if _, port, err := net.SplitHostPort(address); err == nil {
		if n, err := strconv.Atoi(port); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
