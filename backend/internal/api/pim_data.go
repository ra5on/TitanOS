package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/files"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// pimDataArea is what the pages of T-Kalender and Kontakte read (TECHNIK
// 7.1/7.2). Step 11 has only what the frames need: whether the calendars
// can be read, and the address books; step 14 adds the rest here. Like the
// device passwords: a web session of this node only (no token, no MCP, no
// agent, not through the cluster), never in publicRoutes.
type pimDataArea struct {
	store *pimstore.Store
	// prefs holds the hidden calendars (preference kind "calendar").
	prefs *preferences
	// reserve takes RAM of TitanOS's budget for an import
	// (system.MemoryPolicy.TryReserve); nil counts nothing.
	reserve func(bytes int64) (release func(), ok bool)
	// clock is the time of DTSTAMP and of "today"; nil = time.Now.
	clock func() time.Time
	// mail is T-Mail: its learned addresses are the suggestions of
	// Kontakte, with the account's grant only (nil: none).
	mail *tmail.Manager
	// files and roots open a file of the NAS with the rights of the
	// account (a picture of a contact from the files).
	files *files.Manager
	roots func(r *http.Request) []files.Root
}

func (s *Server) pimDataArea() pimDataArea {
	a := pimDataArea{store: s.opts.PIM, prefs: s.prefs, mail: s.opts.Mail, files: s.opts.Files, roots: s.filesArea().requestRoots}
	if s.opts.MemoryPolicy != nil {
		a.reserve = s.opts.MemoryPolicy.TryReserve
	}
	return a
}

func (a pimDataArea) register(mux *routeMux, g guards) {
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("GET /api/v1/calendar/access", web(a.handleCalendarAccess))
	mux.HandleFunc("GET /api/v1/contacts/books", web(a.handleBooks))
	// T-Kalender (calendar.go, calendar_events.go, calendar_ics.go).
	mux.HandleFunc("GET /api/v1/calendar/calendars", web(a.handleCalendars))
	mux.HandleFunc("POST /api/v1/calendar/calendars", web(a.handleCreateCalendar))
	mux.HandleFunc("PUT /api/v1/calendar/calendars/{id}", web(a.handleUpdateCalendar))
	mux.HandleFunc("DELETE /api/v1/calendar/calendars/{id}", web(a.handleDeleteCalendar))
	mux.HandleFunc("POST /api/v1/calendar/calendars/{id}/import", web(a.handleImportCalendar))
	mux.HandleFunc("GET /api/v1/calendar/calendars/{id}/export", web(a.handleExportCalendar))
	mux.HandleFunc("GET /api/v1/calendar/events", web(a.handleEvents))
	mux.HandleFunc("POST /api/v1/calendar/events", web(a.handleCreateEvent))
	mux.HandleFunc("GET /api/v1/calendar/events/{cal}/{obj}", web(a.handleEvent))
	mux.HandleFunc("PUT /api/v1/calendar/events/{cal}/{obj}", web(a.handleUpdateEvent))
	mux.HandleFunc("DELETE /api/v1/calendar/events/{cal}/{obj}", web(a.handleDeleteEvent))
	mux.HandleFunc("POST /api/v1/calendar/preview", web(a.handlePreview))
	// Reminders, 1.13 (calendar_tasks.go).
	a.registerTasks(mux, web)
	// Kontakte (contacts.go, contacts_photo.go, contacts_suggest.go, contacts_vcf.go).
	mux.HandleFunc("GET /api/v1/contacts", web(a.handleContacts))
	mux.HandleFunc("POST /api/v1/contacts", web(a.handleCreateContact))
	mux.HandleFunc("GET /api/v1/contacts/suggestions", web(a.handleContactSuggestions))
	mux.HandleFunc("POST /api/v1/contacts/from-suggestion", web(a.handleContactFromSuggestion))
	mux.HandleFunc("POST /api/v1/contacts/books/{id}/import", web(a.handleImportContacts))
	mux.HandleFunc("GET /api/v1/contacts/books/{id}/export", web(a.handleExportContacts))
	mux.HandleFunc("GET /api/v1/contacts/{book}/{id}", web(a.handleContact))
	mux.HandleFunc("PUT /api/v1/contacts/{book}/{id}", web(a.handleUpdateContact))
	mux.HandleFunc("DELETE /api/v1/contacts/{book}/{id}", web(a.handleDeleteContact))
	mux.HandleFunc("GET /api/v1/contacts/{book}/{id}/photo", web(a.handleContactPhoto))
	mux.HandleFunc("PUT /api/v1/contacts/{book}/{id}/photo", web(a.handlePutContactPhoto))
	mux.HandleFunc("DELETE /api/v1/contacts/{book}/{id}/photo", web(a.handleDeleteContactPhoto))
}

type calendarAccess struct {
	Enabled bool `json:"enabled"`
	Volume  bool `json:"volume"`
	// Timezone is the IANA name of the NAS zone (Settings › Time zone), in
	// which the pages show times and create events (TECHNIK 2.2 point 5) –
	// a plain account cannot read /network. Missing when titand has no
	// named zone (the page then uses the browser's).
	Timezone string `json:"timezone,omitempty"`
}

// handleCalendarAccess says whether the account has T-Kalender and whether
// its data can be read now (the volume of the system data is there), and
// the zone of the NAS. From RAM only: the page asks on every start, the
// volume may sleep.
func (a pimDataArea) handleCalendarAccess(w http.ResponseWriter, r *http.Request) {
	out := calendarAccess{Enabled: true}
	if a.store != nil {
		out.Volume = a.store.Current == nil || a.store.Current(string(pimstore.Calendar)) != ""
		out.Timezone = zoneName(a.store.Zone())
	}
	writeJSON(w, http.StatusOK, out)
}

// zoneName is the IANA name of a zone; "" for time.Local without a name
// (its String() is "Local", which no browser knows).
func zoneName(loc *time.Location) string {
	if loc == nil || loc == time.Local {
		return ""
	}
	name := loc.String()
	if name == "" || name == "Local" {
		return ""
	}
	return name
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
