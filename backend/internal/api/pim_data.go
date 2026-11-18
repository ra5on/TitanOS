package api

import (
	"errors"
	"log/slog"
	"net/http"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// pimDataArea is what the pages of T-Kalender and Kontakte read (TECHNIK
// 7.1/7.2). Step 11 has only what the frames need: whether the calendars
// can be read, and the address books; step 14 adds the rest here. Like the
// device passwords: a web session of this node only (no token, no MCP, no
// agent, not through the cluster), never in publicRoutes.
type pimDataArea struct {
	store *pimstore.Store
}

func (s *Server) pimDataArea() pimDataArea { return pimDataArea{store: s.opts.PIM} }

func (a pimDataArea) register(mux *routeMux, g guards) {
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("GET /api/v1/calendar/access", web(a.handleCalendarAccess))
	mux.HandleFunc("GET /api/v1/contacts/books", web(a.handleBooks))
}

// handleCalendarAccess says whether the account has T-Kalender and whether
// its data can be read now (the volume of the system data is there). From
// RAM only: the page asks on every start, the volume may sleep.
func (a pimDataArea) handleCalendarAccess(w http.ResponseWriter, r *http.Request) {
	volume := a.store != nil && (a.store.Current == nil || a.store.Current(string(pimstore.Calendar)) != "")
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true, "volume": volume})
}

type bookView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Count is the number of contacts, groups not counted.
	Count int `json:"count"`
}

// handleBooks lists the account's address books (one, "Kontakte"). Without
// the volume 503 contacts_unavailable, never an empty list.
func (a pimDataArea) handleBooks(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if a.store == nil {
		writePIMUnavailable(w, pimstore.Contacts)
		return
	}
	books, err := a.store.Collections(pimstore.Contacts, user.Name)
	if err != nil {
		writePIMError(w, pimstore.Contacts, err)
		return
	}
	out := make([]bookView, 0, len(books))
	for _, b := range books {
		objects, err := a.store.Objects(pimstore.Contacts, user.Name, b.ID)
		if err != nil {
			writePIMError(w, pimstore.Contacts, err)
			return
		}
		view := bookView{ID: b.ID, Name: b.Name}
		for _, o := range objects {
			if !o.Entry.Group {
				view.Count++
			}
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
}

// writePIMUnavailable answers 503 calendar_unavailable or
// contacts_unavailable: the data cannot be read now (no volume, the
// account's folder on another volume, a removal running).
func writePIMUnavailable(w http.ResponseWriter, kind pimstore.Kind) {
	w.Header().Set("Retry-After", "300")
	if kind == pimstore.Contacts {
		writeError(w, http.StatusServiceUnavailable, "contacts_unavailable", "the contacts cannot be read now: the data volume is not mounted")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "calendar_unavailable", "the calendars cannot be read now: the data volume is not mounted")
}

func writePIMError(w http.ResponseWriter, kind pimstore.Kind, err error) {
	switch {
	case errors.Is(err, pimstore.ErrUnavailable), errors.Is(err, volumedata.ErrHeld):
		writePIMUnavailable(w, kind)
	default:
		slog.Error("Calendar: read failed", "kind", kind, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not read the data")
	}
}
