// Package davsync serves CalDAV and CardDAV to iPhones and Macs (TECHNIK
// 1.4, 1.5, 4.1): discovery, PROPFIND, GET/HEAD and the multiget reports;
// writing (step 7) and sync-collection/queries (step 8) slot into the
// tables of methods (methods.go), reports (report.go) and properties
// (props.go). Only the standard library and encoding/xml: the bytes
// stored by pim/store are served as they came, go-ical and go-vcard stay
// in pim.
//
// Every request but OPTIONS and /.well-known signs in with a device
// password (pim/devices); the path must name the account signed in, else
// 404. Without the system data volume every request that needs the data
// answers 503 with Retry-After, never an empty collection (TECHNIK 5.1):
// iOS would delete its copy.
//
// The journal never names titles, addresses or passwords: only account,
// device, collection and an error code.
package davsync

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/devices"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The paths (TECHNIK 4.1): /caldav/<account>/ is the principal,
// …/calendars/ the home set, …/calendars/<collection>/<entry>.ics an
// entry; CardDAV likewise below /carddav/<account>/addressbooks/.
const (
	CalDAVPrefix     = "/caldav"
	CardDAVPrefix    = "/carddav"
	WellKnownCalDAV  = "/.well-known/caldav"
	WellKnownCardDAV = "/.well-known/carddav"
)

// Patterns are the patterns of the API mux (without method, like /dav/),
// registered before the catch-all of the web UI.
var Patterns = []string{CalDAVPrefix, CalDAVPrefix + "/", CardDAVPrefix, CardDAVPrefix + "/", WellKnownCalDAV, WellKnownCardDAV}

// Limits (TECHNIK 5.3).
const (
	// MaxBody bounds the body of a PROPFIND or REPORT.
	MaxBody = 256 << 10
	// BodyTimeout is the time to send it (as readJSONBody).
	BodyTimeout = 60 * time.Second
	// MaxHrefs bounds the hrefs of a multiget.
	MaxHrefs = 1000
	// RetryAfter is the Retry-After of a 503 (seconds).
	RetryAfter = 300
)

// Path reports whether a request for p is served by davsync: /caldav,
// /caldav/…, /carddav, /carddav/… and /.well-known/caldav|carddav, with
// segment boundaries, and only for a canonical path (no "..", ".", "//";
// a trailing slash is fine). The handler chain lets a device password
// through only there (bearerAccess) and keeps the cluster out
// (clusterAccess); a path the mux would clean or send elsewhere is never
// exempt.
func Path(p string) bool {
	if p == WellKnownCalDAV || p == WellKnownCardDAV {
		return true
	}
	if p != CalDAVPrefix && p != CardDAVPrefix && !strings.HasPrefix(p, CalDAVPrefix+"/") && !strings.HasPrefix(p, CardDAVPrefix+"/") {
		return false
	}
	clean := path.Clean(p)
	return clean == p || clean+"/" == p
}

// Devices checks device passwords and notes "last synced" (pim/devices
// *Manager).
type Devices interface {
	Authenticate(r *http.Request) (devices.Identity, error)
	Synced(account, device, kind, addr string, volumeOpen bool)
}

// Handler serves /caldav, /carddav and /.well-known/caldav|carddav.
type Handler struct {
	// Store holds the data; nil (no PIM, titand -dev) answers 503 where
	// data is needed.
	Store *store.Store
	// Devices signs requests in.
	Devices Devices
	// RemoteIP is the address of the client for "last synced"; nil takes
	// the host of RemoteAddr.
	RemoteIP func(*http.Request) string
	// Redirect is the status of /.well-known (0 = 301; a test also follows
	// 308, TECHNIK 4.1.7).
	Redirect int
	// bodyTimeout is BodyTimeout (tests shorten it).
	bodyTimeout time.Duration

	methods map[string]method
	reports map[reportKey]report
	props   *propTable
}

// New returns the handler over the store and the device checker.
func New(st *store.Store, d Devices) *Handler {
	h := &Handler{Store: st, Devices: d, bodyTimeout: BodyTimeout}
	h.methods = readMethods()
	h.reports = readReports()
	h.props = newPropTable(readProps())
	return h
}

// request is one request after sign-in and path check.
type request struct {
	h  *Handler
	w  *statusWriter
	r  *http.Request
	t  target
	id devices.Identity
	// volumeOpen: the request read the volume anyway (GET, multiget), so
	// Synced may note it there.
	volumeOpen bool
}

// davHeader is the DAV header of a kind (TECHNIK 1.4).
func davHeader(kind store.Kind) string {
	if kind == store.Contacts {
		return "1, 3, access-control, addressbook, extended-mkcol"
	}
	return "1, 3, access-control, calendar-access, extended-mkcol"
}

// protocol is the name of a kind in the journal and for Synced.
func protocol(kind store.Kind) string {
	if kind == store.Contacts {
		return devices.CardDAV
	}
	return devices.CalDAV
}

func label(kind store.Kind) string {
	if kind == store.Contacts {
		return "CardDAV"
	}
	return "CalDAV"
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	switch r.URL.Path {
	case WellKnownCalDAV, WellKnownCardDAV:
		// RFC 6764, before any sign-in and for any method: the root
		// answers current-user-principal once signed in.
		to := CalDAVPrefix + "/"
		if r.URL.Path == WellKnownCardDAV {
			to = CardDAVPrefix + "/"
		}
		status := h.Redirect
		if status == 0 {
			status = http.StatusMovedPermanently
		}
		rw.Header().Set("Location", to)
		rw.WriteHeader(status)
		return
	}
	t, ok := parsePath(r.URL.EscapedPath())
	if !ok {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("DAV", davHeader(t.kind))
	if r.Method == http.MethodOptions {
		// From the shape of the path only: no sign-in, no store, nothing
		// about which accounts exist; no CORS headers either, so a
		// browser's preflight fails.
		rw.Header().Set("Allow", h.allow(t.level))
		rw.Header().Set("Content-Length", "0")
		rw.WriteHeader(http.StatusOK)
		return
	}
	id, err := h.Devices.Authenticate(r)
	if err != nil {
		devices.WriteChallenge(rw, err)
		return
	}
	if t.level != levelRoot && t.account != id.User.Name {
		// Never 403: nothing about other accounts (TECHNIK 3.4.5).
		http.NotFound(rw, r)
		return
	}
	ctx, cancel := h.accountContext(r.Context(), id.User.Name)
	defer cancel()
	rq := &request{h: h, w: &statusWriter{ResponseWriter: rw}, r: r.WithContext(ctx), t: t, id: id}
	if t.level == levelRoot {
		rq.t.account = id.User.Name
	}
	h.dispatch(rq)
	if s := rq.w.status; s >= 200 && s < 300 {
		h.Devices.Synced(id.User.Name, id.Device, protocol(t.kind), h.remoteIP(r), rq.volumeOpen)
	}
}

// accountContext ends with the request or when the account is forgotten
// (store.ForgetUser): a deleted account's requests stop.
func (h *Handler) accountContext(parent context.Context, account string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if h.Store == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(h.Store.Context(account), cancel)
	return ctx, func() { stop(); cancel() }
}

func (h *Handler) remoteIP(r *http.Request) string {
	if h.RemoteIP != nil {
		return h.RemoteIP(r)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = strings.Trim(host[:i], "[]")
	}
	return host
}

// statusWriter remembers the status for Synced.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection (read
// deadline, flush).
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// unavailable answers 503: no volume, the data on another volume, a move
// or clean-up holding the folder, or no store at all.
func (rq *request) unavailable() {
	rq.w.Header().Set("Retry-After", strconv.Itoa(RetryAfter))
	http.Error(rq.w, "calendar data not available, try again later", http.StatusServiceUnavailable)
}

// store returns the store, or answers 503 without one or once the
// account was forgotten.
func (rq *request) store() (*store.Store, bool) {
	if rq.h.Store == nil || rq.r.Context().Err() != nil {
		rq.unavailable()
		return nil, false
	}
	return rq.h.Store, true
}

// fail answers an error of the store. Only "not found" and invalid names
// are 404: a 404 for a known collection makes iOS delete its copy, so
// anything unknown is 500 and every kind of "not available" 503.
func (rq *request) fail(err error) {
	switch {
	case unavailable(err):
		rq.unavailable()
	case notFound(err):
		http.Error(rq.w, "not found", http.StatusNotFound)
	default:
		rq.journal("request failed", err)
		http.Error(rq.w, "internal error", http.StatusInternalServerError)
	}
}

func unavailable(err error) bool {
	return errors.Is(err, store.ErrUnavailable) || errors.Is(err, volumedata.ErrHeld)
}

func notFound(err error) bool {
	return errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidName) ||
		errors.Is(err, store.ErrInvalidCollection) || errors.Is(err, store.ErrInvalidAccount) || errors.Is(err, store.ErrNoAccount)
}

// journal writes a line with account, device, collection and the code of
// the error: never the error text (it may hold an entry's file name),
// titles or addresses.
func (rq *request) journal(msg string, err error) {
	slog.Warn(label(rq.t.kind)+": "+msg, "account", rq.t.account, "device", rq.id.Name,
		"collection", rq.t.collection, "method", rq.r.Method, "error", errorCode(err))
}

// errorCode is a short name of an error for the journal.
func errorCode(err error) string {
	for _, c := range []struct {
		err  error
		code string
	}{
		{store.ErrUnavailable, "unavailable"}, {volumedata.ErrHeld, "held"}, {store.ErrNotFound, "not_found"},
		{store.ErrInvalidName, "invalid_name"}, {store.ErrInvalidCollection, "invalid_collection"},
		{store.ErrInvalidAccount, "invalid_account"}, {store.ErrNoAccount, "no_account"},
		{store.ErrTooLarge, "too_large"}, {store.ErrInvalidData, "invalid_data"},
		{store.ErrPrecondition, "precondition"}, {store.ErrInvalidSyncToken, "invalid_sync_token"},
		{context.Canceled, "canceled"},
	} {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	return "io"
}
