package api

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/pim/devices"
	"github.com/ra5on/TitanOS/backend/internal/pim/holidays"
	"github.com/ra5on/TitanOS/backend/internal/pim/profile"
)

// The iPhone profile (1.13, decision of the owner of 18 Nov 2026): one
// installation sets up calendars and contacts. The web session asks for a
// one-time link with the account password (and code), like a device
// password; the iPhone, not logged in, fetches the profile once over the
// link (QR code), and only then is the device password made and put into
// the profile. An unused link leaves nothing behind: no device, nothing on
// the stick, nothing to revoke. "Download" in the browser fetches the same
// link itself.
//
// Routes:
//   - GET/POST /api/v1/account/devices/profile, GET/DELETE …/profile/{id}:
//     the web session of this node (user + clusterLocal + browserOnly, the
//     same as every device route; TestDeviceRouteRights).
//   - GET /api/v1/iphone-profile/{token}: no session, only the token; the
//     limiter key "profile:" counts unknown tokens. Not in publicRoutes, so
//     a token of the API is refused (bearerAccess); not over the cluster.
//   - GET /api/v1/holidays/{file}: the holidays as iCalendar, public data
//     only (no account, no volume).

// profilePath is where the iPhone fetches a profile.
const profilePath = "/api/v1/iphone-profile/"

// holidaysPath is where the iPhone subscribes to the holidays.
const holidaysPath = "/api/v1/holidays/"

// profileArea is the profile of the iPhone; an area with narrow
// dependencies (PLAN 8a).
type profileArea struct {
	users        *auth.Users
	devices      *devices.Manager
	links        *profile.Links
	sessions     func() *auth.Sessions
	sessionToken func(r *http.Request) string
	webSession   webSessionFunc
	reconfirm    reconfirmFunc
	limiter      limiterFunc
	record       recordFunc
	setup        func(account string) deviceSetup
	// signer is the certificate the profile is signed with, nil for none
	// (then iOS shows it as not verified).
	signer func() *tls.Certificate
	now    func() time.Time
}

func (s *Server) profileArea() profileArea {
	signer := func() *tls.Certificate {
		if s.profileSigner != nil {
			return s.profileSigner()
		}
		return s.trustedSigner()
	}
	return profileArea{users: s.opts.Users, devices: s.devices, links: &s.profileLinks, sessions: s.sessionStore,
		sessionToken: s.authArea().sessionToken, webSession: s.requireWebSession, reconfirm: s.authArea().reconfirm,
		limiter: s.rateLimiter, record: s.recordActivity, setup: s.deviceSetup, signer: signer, now: time.Now}
}

func (a profileArea) register(mux *routeMux, g guards) {
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("GET /api/v1/account/devices/profile", web(a.handleInfo))
	mux.HandleFunc("POST /api/v1/account/devices/profile", web(a.handleNew))
	mux.HandleFunc("GET /api/v1/account/devices/profile/{id}", web(a.handleStatus))
	mux.HandleFunc("DELETE /api/v1/account/devices/profile/{id}", web(a.handleCancel))
	mux.HandleFunc("GET "+profilePath+"{token}", a.handleFetch)
	mux.HandleFunc("GET "+holidaysPath+"{file}", handleHolidays)
}

// handleInfo tells the page whether the profile will be signed and which
// regions the holidays have.
func (a profileArea) handleInfo(w http.ResponseWriter, r *http.Request) {
	signed, signer := false, ""
	if cert := a.signer(); cert != nil {
		signed = true
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil && len(leaf.DNSNames) > 0 {
			signer = leaf.DNSNames[0]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"signed": signed, "signer": signer, "ttl": int(profile.TTL / time.Second),
		"states": holidays.States})
}

// splitServer takes a server of the setup page apart: host (without
// brackets) and port (443 when none is named).
func splitServer(server string) (string, int, bool) {
	if host, port, err := net.SplitHostPort(server); err == nil {
		n, err := strconv.Atoi(port)
		if err != nil || n <= 0 || n > 65535 || host == "" {
			return "", 0, false
		}
		return host, n, true
	}
	host := strings.TrimSuffix(strings.TrimPrefix(server, "["), "]")
	if host == "" || strings.ContainsAny(host, "/?#@ \\") {
		return "", 0, false
	}
	return host, 443, true
}

// handleNew makes a one-time link: the device name and the choices of the
// profile, after the account password (and code). The device itself is
// made when the link is fetched.
func (a profileArea) handleNew(w http.ResponseWriter, r *http.Request) {
	if !a.webSession(w, r) {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Code     string `json:"code"`
		// Server is one of the servers of the setup page ("" for the first).
		Server string `json:"server"`
		// Holidays is a region of the holidays ("de", "de-by"), "" for none.
		Holidays string `json:"holidays"`
		Lang     string `json:"lang"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	noteTarget(r, strings.TrimSpace(req.Name))
	if req.Lang != "en" {
		req.Lang = "de"
	}
	if req.Holidays != "" {
		if _, ok := holidays.ParseRegion(req.Holidays); !ok {
			writeError(w, http.StatusBadRequest, "invalid_holidays", "no such region of holidays")
			return
		}
	}
	// The server must be one the setup page offers; with none known, the
	// one this page came over.
	servers := []string{}
	for _, s := range a.setup(user.Name).Addresses {
		servers = append(servers, s.Server)
	}
	if len(servers) == 0 {
		servers = append(servers, r.Host)
	}
	server := req.Server
	if server == "" {
		server = servers[0]
	}
	host, port, ok := splitServer(server)
	if !slices.Contains(servers, server) || !ok {
		writeError(w, http.StatusBadRequest, "invalid_server", "the server is not one of this NAS")
		return
	}
	// A name that cannot be used, or a 21st device, costs no attempt of
	// the password.
	if err := a.devices.Available(user, req.Name); err != nil {
		writeDeviceError(w, err)
		return
	}
	if !a.reconfirm(w, user, req.Password, req.Code) {
		return
	}
	// As the account is now (the second factor may have been used).
	if fresh, ok := a.users.Get(user.Name); ok {
		user = fresh
	}
	link, token, err := a.links.New(profile.Request{
		Account: user.Name, Created: user.Created, Creds: devices.Fingerprint(user),
		Session: auth.SessionID(a.sessionToken(r)), Device: strings.TrimSpace(req.Name),
		Host: host, Port: port, Lang: req.Lang, Holidays: req.Holidays,
	})
	if errors.Is(err, profile.ErrFull) {
		writeError(w, http.StatusServiceUnavailable, "profile_links_full", "too many profiles wait to be fetched; try again in a few minutes")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "no link could be made")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"link": link, "url": "https://" + server + profilePath + token,
		"path": profilePath + token, "signed": a.signer() != nil})
}

func (a profileArea) handleStatus(w http.ResponseWriter, r *http.Request) {
	link, err := a.links.Status(currentUser(r).Name, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "profile_link_not_found", "no such link")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, link)
}

func (a profileArea) handleCancel(w http.ResponseWriter, r *http.Request) {
	if err := a.links.Cancel(currentUser(r).Name, r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "profile_link_not_found", "no such link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// prefetch reports a request the browser makes ahead of a click (link
// previews and speculative loading must not use the link up).
func prefetch(r *http.Request) bool {
	for _, h := range []string{"Sec-Purpose", "Purpose", "X-Purpose", "X-Moz"} {
		if strings.Contains(strings.ToLower(r.Header.Get(h)), "prefetch") || strings.Contains(strings.ToLower(r.Header.Get(h)), "preview") {
			return true
		}
	}
	return false
}

// handleFetch hands out the profile of a link, once. Unknown, used and
// expired tokens get the same answer and count against the address.
func (a profileArea) handleFetch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		// HEAD (link checks) does not use the link up.
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET")
		return
	}
	if prefetch(r) {
		writeError(w, http.StatusForbidden, "prefetch_refused", "open the link to install the profile")
		return
	}
	if viaCluster(r) {
		writeError(w, http.StatusForbidden, "cluster_local_only", "the profile is fetched from the node itself")
		return
	}
	key := "profile:" + passkeySource(r)
	if wait := a.limiter().RetryAfter(key); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	req, id, ok := a.links.Take(r.PathValue("token"))
	if !ok {
		a.limiter().Fail(key)
		writeError(w, http.StatusNotFound, "profile_link_invalid", "this link is unknown, used or expired; make a new one on the NAS")
		return
	}
	fail := func(status int, code, message string) {
		a.links.Done(id, code)
		a.record(activity.Entry{User: req.Account, Via: activity.ViaWeb, IP: remoteIP(r), Action: "iphone_profile", Target: req.Device, Status: status, OK: false})
		writeError(w, status, code, message)
	}
	user, ok := a.users.Get(req.Account)
	if !ok || user.Disabled || !user.Created.Equal(req.Created) || devices.Fingerprint(user) != req.Creds {
		fail(http.StatusGone, "profile_link_void", "the account changed since the link was made; make a new one")
		return
	}
	if !a.sessions().Usable(req.Session, req.Account) {
		fail(http.StatusGone, "profile_link_void", "the login that made the link has ended; make a new one")
		return
	}
	created, err := a.devices.Create(user, req.Device)
	if err != nil {
		code := "config_unavailable"
		switch {
		case errors.Is(err, devices.ErrNameTaken):
			code = "device_name_taken"
		case errors.Is(err, devices.ErrTooMany):
			code = "device_limit"
		case errors.Is(err, devices.ErrInvalidName):
			code = "invalid_device_name"
		}
		a.links.Done(id, code)
		a.record(activity.Entry{User: req.Account, Via: activity.ViaWeb, IP: remoteIP(r), Action: "iphone_profile", Target: req.Device, Status: http.StatusConflict, OK: false})
		writeDeviceError(w, err)
		return
	}
	params := profile.Params{Account: user.Name, Created: user.Created, Device: created.Name, Password: created.Password,
		Host: req.Host, Port: req.Port, Lang: req.Lang}
	if req.Holidays != "" {
		state, _ := holidays.ParseRegion(req.Holidays)
		url := "https://" + hostPortOf(req.Host, req.Port) + holidaysPath + holidays.FileName(state)
		if req.Lang == "en" {
			url += "?lang=en"
		}
		params.Holidays = &profile.Subscription{URL: url, Description: holidays.Title(state, req.Lang)}
	}
	body := profile.Build(params)
	signed := false
	if cert := a.signer(); cert != nil {
		if out, err := profile.Sign(body, cert, a.now()); err == nil {
			body, signed = out, true
		} else {
			slog.Warn("Calendar: profile not signed", "err", err)
		}
	}
	a.links.Done(id, "")
	a.record(activity.Entry{User: user.Name, Via: activity.ViaWeb, IP: remoteIP(r), Action: "iphone_profile", Target: created.Name, Status: http.StatusOK, OK: true})
	slog.Info("Calendar: iPhone profile fetched", "user", user.Name, "device", created.ID, "signed", signed)
	w.Header().Set("Content-Type", profile.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+profile.FileName+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

// hostPortOf is host[:port] for a URL (IPv6 in brackets, no port for 443).
func hostPortOf(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != 443 {
		host += ":" + strconv.Itoa(port)
	}
	return host
}

// handleHolidays is the subscription of the holidays: computed, the same
// for everyone, no account.
func handleHolidays(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	region, ok := strings.CutSuffix(file, ".ics")
	state, valid := holidays.ParseRegion(region)
	if !ok || !valid {
		writeError(w, http.StatusNotFound, "not_found", "no such calendar of holidays")
		return
	}
	lang := "de"
	if r.URL.Query().Get("lang") == "en" {
		lang = "en"
	}
	first, last := holidays.Years(time.Now())
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(holidays.ICS(state, lang, first, last))
}

// trustedSigner is the certificate of the web UI that iOS trusts, to sign
// the profile with: the one served for a name the setup page marks as
// valid (Let's Encrypt, Tailscale), if its chain verifies against the
// system's roots for that name now. nil for none (self-signed, the
// TitanOS authority, an own certificate): the profile goes out unsigned.
func (s *Server) trustedSigner() *tls.Certificate {
	manager := s.core().certificate
	if manager == nil {
		return nil
	}
	var hosts []string
	for _, a := range s.deviceSetup("").Addresses {
		if a.Certificate == certValid {
			if host, _, ok := splitServer(a.Server); ok {
				hosts = append(hosts, host)
			}
		}
	}
	return trustedCert(hosts, func(host string) (*tls.Certificate, error) {
		return manager.GetCertificate(&tls.ClientHelloInfo{ServerName: host})
	}, nil, time.Now())
}

// trustedCert returns the first certificate served for one of the hosts
// whose chain verifies for that host against roots (nil: the system's),
// with a key that can sign.
func trustedCert(hosts []string, get func(string) (*tls.Certificate, error), roots *x509.CertPool, now time.Time) *tls.Certificate {
	for _, host := range hosts {
		cert, err := get(host)
		if err != nil || cert == nil || len(cert.Certificate) == 0 || cert.PrivateKey == nil {
			continue
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			continue
		}
		pool := x509.NewCertPool()
		for _, der := range cert.Certificate[1:] {
			if c, err := x509.ParseCertificate(der); err == nil {
				pool.AddCert(c)
			}
		}
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: pool, Roots: roots, CurrentTime: now}); err != nil {
			continue
		}
		return cert
	}
	return nil
}
