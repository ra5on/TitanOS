package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appnotify"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The messages of the Android app (PLAN 116, TECHNIK §5,
// docs/ANDROID-APP.md "Mitteilungen"): what Web Push sends to browsers,
// kept in RAM for the paired apps (package appnotify). Only the session of
// a paired app reaches these routes, like the photo backup's: they are
// neither in publicRoutes nor in the tool catalog.
//
//   - GET /api/v1/app/notifications?since=<marker>: the account's messages
//     after the marker, oldest first, {epoch, latest, reset, items}.
//   - GET /api/v1/app/notifications/stream?since=<marker>: Server-Sent
//     Events. "ready" {epoch, latest, reset}, then what came after since,
//     then each new message as "notification" (with id: <marker>); a
//     comment every appNotifyHeartbeat; "resync" when messages were left
//     out (read the list). It ends with its login (whileLoginHolds).

// appNotifyHeartbeat is how often an idle stream sends a comment (tests
// shorten it). Long enough to let the phone sleep, short enough for the
// NAT of a home router.
var appNotifyHeartbeat atomic.Int64

func init() { appNotifyHeartbeat.Store(int64(90 * time.Second)) }

type appNotifyArea struct {
	appSessionGuard
	hub             *appnotify.Hub
	whileLoginHolds func(r *http.Request, user auth.User) (context.Context, context.CancelFunc)
}

func (s *Server) appNotifyArea() appNotifyArea {
	return appNotifyArea{appSessionGuard: s.appSessionGuard(), hub: s.opts.AppNotify, whileLoginHolds: s.whileLoginHolds}
}

func (s *Server) registerAppNotify(mux *routeMux) {
	a, g := s.appNotifyArea(), s.guards()
	app := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(a.appOnly(h)))) }
	mux.HandleFunc("GET /api/v1/app/notifications", app(a.handleList))
	mux.HandleFunc("GET /api/v1/app/notifications/stream", app(a.handleStream))
}

type appNotifyHead struct {
	Epoch  string `json:"epoch"`
	Latest string `json:"latest"`
	// Reset: the marker was of an earlier start of titand (or unreadable);
	// the list is everything that is kept.
	Reset bool `json:"reset"`
}

func (a appNotifyArea) handleList(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	after, reset := a.hub.Since(r.URL.Query().Get("since"))
	items, latest := a.hub.List(user, after)
	writeJSON(w, http.StatusOK, struct {
		appNotifyHead
		Items []appnotify.Item `json:"items"`
	}{appNotifyHead{a.hub.Epoch(), latest, reset}, items})
}

func (a appNotifyArea) handleStream(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	// First the stream, then the backlog: nothing falls between them.
	sub, cancel := a.hub.Subscribe(user.Name)
	defer cancel()
	holds, stop := a.whileLoginHolds(r, user)
	defer stop()
	after, reset := a.hub.Since(r.URL.Query().Get("since"))
	backlog, latest := a.hub.List(user.Name, after)
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event, id string, v any) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}
		if id != "" {
			fmt.Fprintf(w, "id: %s\n", id)
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	}
	send("ready", "", appNotifyHead{a.hub.Epoch(), latest, reset})
	var last int64
	for _, it := range backlog {
		send("notification", it.ID, it)
		last = a.hub.Seq(it)
	}
	_ = rc.Flush() // a writer without flushing still gets the events at the end
	beat := time.NewTicker(time.Duration(appNotifyHeartbeat.Load()))
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-holds.Done():
			return
		case <-beat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
		case it, ok := <-sub.C:
			if !ok {
				return
			}
			// Already in the backlog.
			if a.hub.Seq(it) <= last {
				continue
			}
			last = a.hub.Seq(it)
			send("notification", it.ID, it)
		}
		if sub.Lost.Swap(false) {
			send("resync", "", map[string]string{"latest": a.hub.Latest()})
		}
		if rc.Flush() != nil {
			return
		}
	}
}
