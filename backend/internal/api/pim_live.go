package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// The live updates of Kontakte and Kalender (1.13): GET
// /api/v1/calendar/stream is a stream of Server-Sent Events like
// /mail/events, but for every account (T-Mail's stream needs T-Mail's
// grant). Each change of one of the account's own calendars or address
// books – from the web page, an iPhone over CalDAV/CardDAV, the agent or
// an import (pim/store OnChanged) – becomes an event "pim":
//
//	{"account":"alex","kind":"contacts","collection":"kontakte","generation":"3f2a…-412"}
//
// generation is the collection's ctag after the change ("" when the
// collection was deleted). Changes are gathered for pimFlush and sent once
// per collection (an iPhone that uploads 2000 contacts makes a handful of
// events, not 2000). The stream begins with "ready" {account,
// generations: {"<kind>/<collection>": ctag}} and says "resync" when it
// had to leave events out: read everything again. Only the account's own
// changes go into its stream; it ends with the login (whileLoginHolds).

// pimFlush is how long changes are gathered before they go out.
const pimFlush = 250 * time.Millisecond

// maxPIMStreams is how many streams an account may have open; a new one
// ends the oldest (as T-Mail's).
const maxPIMStreams = 8

// pimChanged is the data of an event "pim".
type pimChanged struct {
	Account    string `json:"account"`
	Kind       string `json:"kind"`
	Collection string `json:"collection"`
	Generation string `json:"generation"`
}

type pimSub struct {
	ch chan pimChanged
	// lost: an event did not fit; the stream says "resync".
	lost atomic.Bool
}

// pimHub hands the changes of an account to its open streams.
type pimHub struct {
	mu   sync.Mutex
	subs map[string][]*pimSub
}

func newPIMHub() *pimHub { return &pimHub{subs: map[string][]*pimSub{}} }

// subscribe returns the changes of an account until cancel; the channel
// closes when the account opens more than maxPIMStreams or is forgotten.
func (h *pimHub) subscribe(user string) (*pimSub, func()) {
	sub := &pimSub{ch: make(chan pimChanged, 64)}
	h.mu.Lock()
	if list := h.subs[user]; len(list) >= maxPIMStreams {
		close(list[0].ch)
		h.subs[user] = list[1:]
	}
	h.subs[user] = append(h.subs[user], sub)
	h.mu.Unlock()
	var once sync.Once
	return sub, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			list := h.subs[user]
			if i := slices.Index(list, sub); i >= 0 {
				close(sub.ch)
				h.subs[user] = slices.Delete(slices.Clone(list), i, i+1)
				if len(h.subs[user]) == 0 {
					delete(h.subs, user)
				}
			}
		})
	}
}

// publish is the store's OnChanged: never blocks; an event that does not
// fit marks the stream for a resync.
func (h *pimHub) publish(c pimstore.Changed) {
	if h == nil {
		return
	}
	ev := pimChanged{Account: c.Account, Kind: string(c.Kind), Collection: c.Collection, Generation: c.CTag}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs[c.Account] {
		select {
		case sub.ch <- ev:
		default:
			sub.lost.Store(true)
		}
	}
}

// forget ends the streams of an account (deleted or made anew).
func (h *pimHub) forget(user string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs[user] {
		close(sub.ch)
	}
	delete(h.subs, user)
}

// pimLiveArea is the stream; whileLoginHolds ends it with its login.
type pimLiveArea struct {
	store           *pimstore.Store
	hub             *pimHub
	whileLoginHolds func(r *http.Request, user auth.User) (context.Context, context.CancelFunc)
}

func (s *Server) pimLiveArea() pimLiveArea {
	return pimLiveArea{store: s.opts.PIM, hub: s.pimLive, whileLoginHolds: s.whileLoginHolds}
}

func (a pimLiveArea) register(mux *routeMux, g guards) {
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("GET /api/v1/calendar/stream", web(a.handleStream))
}

// generations are the ctags of the account's collections now (RAM; a
// kind that cannot be read is left out).
func (a pimLiveArea) generations(user string) map[string]string {
	out := map[string]string{}
	for _, kind := range []pimstore.Kind{pimstore.Calendar, pimstore.Contacts} {
		cols, err := a.store.Collections(kind, user)
		if err != nil {
			continue
		}
		for _, c := range cols {
			out[string(kind)+"/"+c.ID] = c.CTag
		}
	}
	return out
}

func (a pimLiveArea) handleStream(w http.ResponseWriter, r *http.Request) {
	if a.store == nil || a.hub == nil {
		writePIMUnavailable(w, pimstore.Calendar)
		return
	}
	user := currentUser(r)
	sub, cancel := a.hub.subscribe(user.Name)
	defer cancel()
	holds, stop := a.whileLoginHolds(r, user)
	defer stop()
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, v any) {
		data, err := json.Marshal(v)
		if err == nil {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		}
	}
	send("ready", map[string]any{"account": user.Name, "generations": a.generations(user.Name)})
	_ = rc.Flush() // a writer without flushing still gets the events at the end
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	pending := map[string]pimChanged{}
	var order []string
	var flush <-chan time.Time
	for {
		select {
		case <-r.Context().Done():
			return
		case <-holds.Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
		case ev, ok := <-sub.ch:
			if !ok {
				return
			}
			key := ev.Kind + "/" + ev.Collection
			if _, seen := pending[key]; !seen {
				order = append(order, key)
			}
			pending[key] = ev
			if flush == nil {
				flush = time.After(pimFlush)
			}
			continue
		case <-flush:
			flush = nil
			if sub.lost.Swap(false) {
				send("resync", map[string]string{"account": user.Name})
			}
			for _, key := range order {
				send("pim", pending[key])
			}
			clear(pending)
			order = order[:0]
		}
		_ = rc.Flush() // see above
	}
}
