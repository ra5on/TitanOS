package api

import (
	"net/http"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/davsync"
)

// registerPIM registers T-Kalender and Kontakte (TECHNIK 3, 4.1): the
// device passwords for the web session of this node only (none in
// publicRoutes or the tool catalog), and /caldav, /carddav and
// /.well-known/caldav|carddav for the devices (davsync), before the
// catch-all of the web UI.
func (s *Server) registerPIM(mux *routeMux) {
	s.pimArea().register(mux, s.guards())
	s.pimDataArea().register(mux, s.guards())
	// 1.13: the profile of the iPhone (one-time link) and the holidays.
	s.profileArea().register(mux, s.guards())
	s.pimLiveArea().register(mux, s.guards())
	dav := davsync.New(s.opts.PIM, s.devices)
	dav.RemoteIP = remoteIP
	for _, pattern := range davsync.Patterns {
		mux.Handle(pattern, dav)
	}
}

// davsyncPath reports whether a request goes to davsync, so the chain may
// let its Authorization header through (bearerAccess) and keeps the
// cluster out (clusterAccess): only a canonical path (davsync.Path) and
// no encoded "/" in it – the mux matches the escaped segments, so
// /caldav%2Fx would reach the web UI, not davsync.
func davsyncPath(r *http.Request) bool {
	if strings.Contains(strings.ToLower(r.URL.RawPath), "%2f") {
		return false
	}
	return davsync.Path(r.URL.Path)
}
