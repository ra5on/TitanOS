package api

import (
	"context"
	"io"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/activity"
)

// What the areas with narrow dependencies (PLAN 8a, see fansArea) take from
// the server besides their managers. An area that needs one of these holds
// a field of the type named here, so that every area has the same shape for
// the same thing; the server fills it with the method named in the comment.

// guards are the checks in front of a route. Small areas take the one or
// two they need as arguments of register; areas with many routes take all
// of them at once.
type guards struct {
	user, admin, appAdmin, heavyAdmin, heavyAppAdmin, appConsole func(http.HandlerFunc) http.HandlerFunc
	// clusterLocal keeps a route on this node; browserOnly refuses tokens
	// and in-process calls.
	clusterLocal, browserOnly func(http.HandlerFunc) http.HandlerFunc
}

// guards are the checks of this server.
func (s *Server) guards() guards {
	return guards{user: s.requireUser, admin: s.requireAdmin, appAdmin: s.requireAppAdmin, heavyAdmin: s.requireHeavyAdmin,
		heavyAppAdmin: s.requireHeavyAppAdmin, appConsole: s.requireAppConsole, clusterLocal: s.clusterLocal, browserOnly: s.browserOnly}
}

// admitFunc admits a job unless maintenance runs (Server.admitOperation).
// It answers the request itself when it refuses; leave ends the job.
type admitFunc func(w http.ResponseWriter) (leave func(), ok bool)

// consoleOpen opens what a console shows and says how to resize it.
type consoleOpen = func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error)

// consoleFunc connects a WebSocket to a shell with the rules of every
// console (Server.serveConsole).
type consoleFunc func(w http.ResponseWriter, r *http.Request, target string, open consoleOpen)

// streamFunc is consoleFunc for a stream with options of its own, like the
// screen of a VM (Server.serveStream).
type streamFunc func(w http.ResponseWriter, r *http.Request, opts consoleOptions, open consoleOpen)

// confirmedFunc says whether the password was entered again for this
// request (Server.confirmed).
type confirmedFunc func(r *http.Request) bool

// sessionConfirmed is a confirmedFunc that ignores the switch of the
// consoles: the web session of the request must have had the password
// entered again.
func (s *Server) sessionConfirmed(r *http.Request) bool {
	cookie, err := r.Cookie(s.cookieName())
	return err == nil && s.sessions.Confirmed(cookie.Value)
}

// webSessionFunc lets only a person at a web session through and answers
// the request itself otherwise (Server.requireWebSession).
type webSessionFunc func(w http.ResponseWriter, r *http.Request) bool

// recordFunc writes an entry of the activity log (Server.recordActivity).
// It reads the log when it is called, so an area never holds a log that
// was replaced.
type recordFunc func(entry activity.Entry)

// recordActivity writes an entry of the activity log, if there is one.
func (s *Server) recordActivity(entry activity.Entry) {
	if s.opts.Activity != nil {
		s.opts.Activity.Record(entry)
	}
}
