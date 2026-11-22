package api

// PLAN 116, docs/ANDROID-APP.md "VPN": the WireGuard device of a paired
// Android app. The app makes its key pair on the phone and sends only the
// public key; the NAS stores the device like one of Settings › WireGuard
// (with the owner account and app device), answers the server's side and
// the preshared key once, and takes the device away when the app ends.
//
// Only for app sessions (appOnly), this node only, not in publicRoutes and
// not in the tool catalog. Making a device needs an owner or
// administrator, like every WireGuard device: it reaches the NAS and the
// home network. Reading and removing one's own device needs only the app.

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/appdevices"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// appVPNRecheck is how often the devices of apps are checked in RAM (an
// app that ran out by "stay logged in" leaves the tunnel).
const appVPNRecheck = time.Hour

type appVPNArea struct {
	wg           *wgserver.Manager
	apps         *appdevices.Manager
	guard        appSessionGuard
	record       recordFunc
	remoteIPFunc func(*http.Request) string
	httpsPort    int
	certificate  func(host string) (*tls.Certificate, error)
	reach        func() []reachAddress
	lan          func() string
}

func (s *Server) appVPNArea() appVPNArea {
	port := s.opts.HTTPSPort
	if port <= 0 {
		port = 443
	}
	return appVPNArea{wg: s.opts.WireGuard, apps: s.appDevices, guard: s.appSessionGuard(), record: s.recordActivity,
		remoteIPFunc: remoteIP, httpsPort: port, certificate: s.appsArea().certificate, reach: s.reachAddresses, lan: appproxy.PrimaryIPv4}
}

func (s *Server) registerAppVPN(mux *routeMux) {
	a, g := s.appVPNArea(), s.guards()
	app := func(h http.HandlerFunc) http.HandlerFunc {
		return g.user(g.clusterLocal(g.browserOnly(a.guard.appOnly(h))))
	}
	mux.HandleFunc("GET /api/v1/app/vpn", app(a.handleGet))
	mux.HandleFunc("POST /api/v1/app/vpn", app(a.handleSet))
	mux.HandleFunc("DELETE /api/v1/app/vpn", app(a.handleRemove))
}

// Reasons an app cannot have a device now; GET answers them in reason,
// POST as the error code.
const (
	vpnOff        = "wireguard_off"
	vpnNoEndpoint = "wireguard_no_endpoint"
	vpnAdminOnly  = "wireguard_admin_only"
)

// appVPNPeer is the app's device as the app sees it: never the preshared
// key (only the answer of POST has it).
type appVPNPeer struct {
	Name          string     `json:"name"`
	Address       string     `json:"address"`
	PublicKey     string     `json:"publicKey"`
	Created       time.Time  `json:"created"`
	Active        bool       `json:"active"`
	LastHandshake *time.Time `json:"lastHandshake,omitempty"`
	Received      int64      `json:"received"`
	Sent          int64      `json:"sent"`
}

// appVPNServer is the server's side: wgserver.AppServer, every address
// the phone may dial (the one of the page first), the NAS's own address in
// the home network, and the NAS inside the tunnel as the app connects to
// it (address with HTTPS port and the SHA-256 of the certificate's key,
// like an address of the pairing).
type appVPNServer struct {
	wgserver.AppServer
	Endpoints    []string `json:"endpoints"`
	NASAddresses []string `json:"nasAddresses"`
	NAS          struct {
		Server string `json:"server"`
		SPKI   string `json:"spki"`
	} `json:"nas"`
}

type appVPNAnswer struct {
	Available bool          `json:"available"`
	Reason    string        `json:"reason,omitempty"`
	MayCreate bool          `json:"mayCreate"`
	Peer      *appVPNPeer   `json:"peer"`
	Server    *appVPNServer `json:"server"`
	// PresharedKey only in the answer of POST.
	PresharedKey string `json:"presharedKey,omitempty"`
}

func (a appVPNArea) server() (*appVPNServer, string) {
	base, ok := a.wg.AppServer()
	if !ok {
		return nil, vpnOff
	}
	out := &appVPNServer{AppServer: base, Endpoints: []string{}, NASAddresses: []string{}}
	add := func(host string) {
		if host != "" && !slices.Contains(out.Endpoints, host) {
			out.Endpoints = append(out.Endpoints, host)
		}
	}
	add(base.Endpoint)
	for _, r := range a.reach() {
		if r.Kind == "ddns" || r.Kind == "domain" {
			add(r.Host)
		}
	}
	if lan := a.lan(); lan != "" {
		if ip, err := netip.ParseAddr(lan); err == nil && ip.Is4() {
			out.NASAddresses = append(out.NASAddresses, ip.String())
		}
	}
	out.NAS.Server = base.Address
	if a.httpsPort != 443 {
		out.NAS.Server = net.JoinHostPort(base.Address, strconv.Itoa(a.httpsPort))
	}
	if a.certificate != nil {
		// An IP address: the certificate of every name without its own.
		if cert, err := a.certificate(""); err == nil {
			out.NAS.SPKI = spkiOf(cert)
		}
	}
	if len(out.Endpoints) == 0 {
		return out, vpnNoEndpoint
	}
	return out, ""
}

func (a appVPNArea) answer(user auth.User, device string) appVPNAnswer {
	out := appVPNAnswer{MayCreate: user.Role.CanAdminister()}
	server, reason := a.server()
	peer, hasPeer := a.wg.AppPeer(user.Name, device)
	// The server's key, home networks and endpoints only for whom may add a
	// device or has one already (security review 22.11.2026).
	if out.MayCreate || hasPeer {
		out.Server = server
	}
	if hasPeer {
		p := appVPNPeer{Name: peer.Name, Address: peer.Address, PublicKey: peer.PublicKey, Created: peer.Created}
		if st, ok := a.wg.PeerStatus(peer.PublicKey); ok {
			p.Active, p.LastHandshake, p.Received, p.Sent = st.Active, st.LastHandshake, st.Received, st.Sent
		}
		out.Peer = &p
	}
	if reason == "" && !out.MayCreate {
		reason = vpnAdminOnly
	}
	out.Available, out.Reason = reason == "", reason
	return out
}

func (a appVPNArea) note(r *http.Request, user, action, target string, ok bool) {
	a.record(activity.Entry{At: time.Now(), User: user, Via: activity.ViaWeb, IP: a.remoteIPFunc(r), Action: action, Target: target, OK: ok})
}

func (a appVPNArea) handleGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, a.answer(currentUser(r), a.guard.device(r)))
}

func (a appVPNArea) handleSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicKey string `json:"publicKey"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, id := currentUser(r), a.guard.device(r)
	device, err := a.apps.Device(user, id)
	if errors.Is(err, appdevices.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "app_unknown", "this app is not paired (any more)")
		return
	}
	if err != nil {
		writeAppsError(w, err)
		return
	}
	if device.App == appdevices.WatchApp {
		// A WireGuard access only for a phone app, never for a watch.
		writeError(w, http.StatusForbidden, "app_phone_required", "only a phone app may add a WireGuard device")
		return
	}
	if !user.Role.CanAdminister() {
		a.note(r, user.Name, "app.vpn", device.Name, false)
		writeError(w, http.StatusForbidden, vpnAdminOnly, "only owners and administrators may add WireGuard devices")
		return
	}
	if _, reason := a.server(); reason != "" {
		writeAppVPNReason(w, reason)
		return
	}
	peer, err := a.wg.SetAppPeer(r.Context(), user.Name, id, device.Name, req.PublicKey)
	if err != nil {
		a.note(r, user.Name, "app.vpn", device.Name, false)
		switch {
		case errors.Is(err, wgserver.ErrOff):
			writeAppVPNReason(w, vpnOff)
		case errors.Is(err, wgserver.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_key", "not a WireGuard public key")
		case errors.Is(err, wgserver.ErrKeyUsed):
			writeError(w, http.StatusConflict, "wireguard_key_used", "another device has this key")
		default:
			wireGuardError(w, err)
		}
		return
	}
	a.note(r, user.Name, "app.vpn", peer.Name, true)
	out := a.answer(user, id)
	out.PresharedKey = peer.Preshared
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (a appVPNArea) handleRemove(w http.ResponseWriter, r *http.Request) {
	user, id := currentUser(r), a.guard.device(r)
	peer, had := a.wg.AppPeer(user.Name, id)
	removed, err := a.wg.RemoveAppPeer(r.Context(), user.Name, id)
	if err != nil {
		slog.Warn("WireGuard device of an app not removed", "user", user.Name, "err", err)
		a.note(r, user.Name, "app.vpn.remove", peer.Name, false)
		writeError(w, http.StatusInternalServerError, "wireguard_failed", "the device could not be removed")
		return
	}
	if had {
		a.note(r, user.Name, "app.vpn.remove", peer.Name, true)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

func writeAppVPNReason(w http.ResponseWriter, reason string) {
	switch reason {
	case vpnNoEndpoint:
		writeError(w, http.StatusConflict, vpnNoEndpoint, "WireGuard has no address from the internet (Settings › WireGuard or dynamic DNS)")
	default:
		writeError(w, http.StatusConflict, vpnOff, "the WireGuard server of the NAS is off (Settings › WireGuard)")
	}
}

// ---------- devices that end with their app ----------

// appVPNLive says whether the device of an app may use the tunnel: the
// account exists, is not locked, is an owner or administrator, and the
// app is paired and has not run out. Not known while the apps of the
// account cannot be read (the stick): the device then stays.
func (s *Server) appVPNLive(account, device string) (bool, bool) {
	user, ok := s.appVPNAccount(account)
	if !ok {
		return false, true
	}
	if _, err := s.appDevices.Device(user, device); err != nil {
		if errors.Is(err, appdevices.ErrUnavailable) {
			return false, false
		}
		return false, true
	}
	return true, true
}

// appVPNCached is appVPNLive from RAM only (for the timer).
func (s *Server) appVPNCached(account, device string) (bool, bool) {
	user, ok := s.appVPNAccount(account)
	if !ok {
		return false, true
	}
	return s.appDevices.Cached(user, device)
}

func (s *Server) appVPNAccount(account string) (auth.User, bool) {
	if s.opts.Users == nil {
		return auth.User{}, false
	}
	user, ok := s.opts.Users.Get(account)
	if !ok || user.Disabled || !user.Role.CanAdminister() {
		return auth.User{}, false
	}
	return user, true
}

// appVPNPrune removes the devices of apps that ended (one write, none if
// nothing ended). Called when an app ends, an account is locked, deleted,
// changes its role or its credentials, and once at start.
func (s *Server) appVPNPrune() {
	if s.opts.WireGuard == nil || s.appDevices == nil {
		return
	}
	if n, err := s.opts.WireGuard.PruneAppPeers(context.Background()); err != nil {
		slog.Warn("WireGuard devices of ended apps not removed", "err", err)
	} else if n > 0 {
		slog.Info("WireGuard devices of ended apps removed", "count", n)
	}
}

// startAppVPN links the WireGuard server to the apps and checks once.
func (s *Server) startAppVPN() {
	wg := s.opts.WireGuard
	if wg == nil || s.appDevices == nil {
		return
	}
	s.appVPNPrune()
	// Not a goJob: it runs as long as the server, and jobs are what Close
	// and the tests wait for. The root context ends it.
	go wg.Watch(s.ctx, appVPNRecheck)
}
