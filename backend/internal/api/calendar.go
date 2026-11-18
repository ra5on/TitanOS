package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The calendars of T-Kalender (TECHNIK 7.1): the account's own ones from
// pim/store and the virtual "geburtstage" from the contacts (5.6, only
// here, never under /caldav/). Every route: a web session of this node
// (pimDataArea.register), the account's own data only, foreign ids 404,
// without the volume 503 calendar_unavailable (never an empty list).

// birthdayColor is --icon-square-pink of web/src/app.css.
const birthdayColor = "#be185d"

// calendarPrefs is the preference kind "calendar": the calendars hidden in
// TitanOS (not on the devices). 1.10 ignores the kind.
type calendarPrefs struct {
	Hidden []string `json:"hidden"`
}

// maxHiddenCalendars bounds the list (50 calendars and the birthdays).
const maxHiddenCalendars = 100

func validCalendarPrefs(value json.RawMessage) bool {
	var p struct {
		Hidden *[]string `json:"hidden"`
	}
	if json.Unmarshal(value, &p) != nil {
		return false
	}
	if p.Hidden == nil {
		return true
	}
	if len(*p.Hidden) > maxHiddenCalendars {
		return false
	}
	for _, id := range *p.Hidden {
		if !pimstore.ValidCollection(id) {
			return false
		}
	}
	return true
}

type calendarView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Order    int    `json:"order"`
	ReadOnly bool   `json:"readOnly"`
	Kind     string `json:"kind"` // "own" or "birthdays"
	Count    int    `json:"count"`
	Hidden   bool   `json:"hidden"`
	// Unavailable: the birthdays only, when the contacts cannot be read
	// now (the calendars can).
	Unavailable bool `json:"unavailable,omitempty"`
}

func ownCalendar(c pimstore.Collection, hidden []string) calendarView {
	color := c.Color
	if color == "" {
		color = pimstore.DefaultColor
	}
	return calendarView{ID: c.ID, Name: c.Name, Color: color, Order: c.Order, Kind: "own", Count: c.Count, Hidden: slices.Contains(hidden, c.ID)}
}

// calendarStore is the store when the calendars can be read now; else it
// answers 503 calendar_unavailable itself. From RAM only.
func (a pimDataArea) calendarStore(w http.ResponseWriter) (*pimstore.Store, bool) {
	if a.store == nil || (a.store.Current != nil && a.store.Current(string(pimstore.Calendar)) == "") {
		writePIMUnavailable(w, pimstore.Calendar)
		return nil, false
	}
	return a.store, true
}

// hiddenCalendars reads the preference; a stick that cannot be read hides
// nothing (the calendar stays usable).
func (a pimDataArea) hiddenCalendars(user string) []string {
	if a.prefs == nil {
		return nil
	}
	raw, err := a.prefs.get(user, "calendar")
	if err != nil {
		slog.Warn("Calendar: hidden calendars not readable", "account", user, "err", err)
		return nil
	}
	var p calendarPrefs
	if raw != nil {
		_ = json.Unmarshal(raw, &p)
	}
	return p.Hidden
}

// unhide drops a deleted calendar from the hidden ones, so that a new
// calendar of the same name shows.
func (a pimDataArea) unhide(user, id string) {
	if a.prefs == nil {
		return
	}
	raw, err := a.prefs.get(user, "calendar")
	if err != nil || raw == nil {
		return
	}
	var all map[string]json.RawMessage
	var p calendarPrefs
	if json.Unmarshal(raw, &all) != nil || json.Unmarshal(raw, &p) != nil || !slices.Contains(p.Hidden, id) {
		return
	}
	hidden, _ := json.Marshal(slices.DeleteFunc(p.Hidden, func(x string) bool { return x == id }))
	all["hidden"] = hidden
	data, _ := json.Marshal(all)
	if err := a.prefs.set(user, "calendar", data); err != nil {
		slog.Warn("Calendar: hidden calendars not written", "account", user, "err", err)
	}
}

// calendarCode maps an error of the store or of calweb to status and code.
func calendarCode(err error, notFound string) (int, string) {
	var uc *pimstore.UIDConflictError
	switch {
	case errors.Is(err, pimstore.ErrUnavailable), errors.Is(err, volumedata.ErrHeld):
		return http.StatusServiceUnavailable, "calendar_unavailable"
	case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidCollection), errors.Is(err, pimstore.ErrInvalidName),
		errors.Is(err, calweb.ErrNoOccurrence), errors.Is(err, recur.ErrNoSeries):
		return http.StatusNotFound, notFound
	case errors.Is(err, pimstore.ErrPrecondition):
		return http.StatusConflict, "calendar_conflict"
	case errors.Is(err, pimstore.ErrLastCollection):
		return http.StatusConflict, "calendar_last"
	case errors.Is(err, pimstore.ErrExists):
		return http.StatusConflict, "calendar_exists"
	case errors.As(err, &uc):
		return http.StatusConflict, "calendar_uid_conflict"
	case errors.Is(err, pimstore.ErrCollectionFull), errors.Is(err, pimstore.ErrTooManyCollections):
		return http.StatusInsufficientStorage, "calendar_full"
	case errors.Is(err, pimstore.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "calendar_too_large"
	case errors.Is(err, recur.ErrWindow):
		return http.StatusBadRequest, "calendar_window"
	case errors.Is(err, calweb.ErrInvalid), errors.Is(err, pimstore.ErrInvalidMeta), errors.Is(err, pimstore.ErrInvalidData),
		errors.Is(err, pimstore.ErrUnsupportedComponent), errors.Is(err, recur.ErrInvalid), errors.Is(err, recur.ErrLimit):
		return http.StatusBadRequest, "calendar_invalid"
	}
	return http.StatusInternalServerError, "internal"
}

var calendarMessages = map[string]string{
	"calendar_not_found":    "no such calendar",
	"event_not_found":       "no such event or occurrence",
	"calendar_conflict":     "the event was changed meanwhile",
	"calendar_last":         "the last calendar stays",
	"calendar_exists":       "a calendar with this id exists",
	"calendar_uid_conflict": "the target calendar holds this event already",
	"calendar_full":         "the calendar or the account is full",
	"calendar_too_large":    "the event or file is too large",
	"calendar_window":       "from and to must span at most 400 days",
	"calendar_invalid":      "the event, rule or calendar is not valid",
	"internal":              "the calendar could not be read or written",
}

// calendarFail answers an error. The journal gets account, calendar and
// code, never titles, places or the error text of a file (it may quote
// content); only failures of the server itself (500) are logged.
func calendarFail(w http.ResponseWriter, r *http.Request, calendar string, err error, notFound string) {
	status, code := calendarCode(err, notFound)
	if status == http.StatusServiceUnavailable {
		writePIMUnavailable(w, pimstore.Calendar)
		return
	}
	if status == http.StatusInternalServerError {
		slog.Error("Calendar: request failed", "account", currentUser(r).Name, "calendar", calendar, "code", code, "err", err)
	}
	writeError(w, status, code, calendarMessages[code])
}

func (a pimDataArea) handleCalendars(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	user := currentUser(r).Name
	cols, err := store.Collections(pimstore.Calendar, user)
	if err != nil {
		calendarFail(w, r, "", err, "calendar_not_found")
		return
	}
	hidden := a.hiddenCalendars(user)
	out := make([]calendarView, 0, len(cols)+1)
	last := 0
	for _, c := range cols {
		out = append(out, ownCalendar(c, hidden))
		last = max(last, c.Order)
	}
	birthdays := calendarView{ID: calweb.BirthdaysID, Name: "Geburtstage", Color: birthdayColor, Order: last + 1, ReadOnly: true,
		Kind: "birthdays", Hidden: slices.Contains(hidden, calweb.BirthdaysID)}
	if ix, _, err := a.cache.book(store, user, pimstore.DefaultBook); err == nil {
		birthdays.Count = ix.Birthdays().Len()
	} else {
		birthdays.Unavailable = true
	}
	writeJSON(w, http.StatusOK, append(out, birthdays))
}

type calendarRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
	Order *int    `json:"order"`
}

func (q calendarRequest) name() (string, bool) {
	if q.Name == nil {
		return "", true
	}
	n := strings.TrimSpace(*q.Name)
	return n, n != ""
}

func (a pimDataArea) handleCreateCalendar(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	var q calendarRequest
	if !readJSON(w, r, &q) {
		return
	}
	name, ok := q.name()
	if !ok || q.Name == nil {
		writeError(w, http.StatusBadRequest, "calendar_invalid", "a calendar needs a name")
		return
	}
	user := currentUser(r).Name
	meta := pimstore.Meta{Name: name, Color: pimstore.DefaultColor}
	if q.Color != nil && *q.Color != "" {
		meta.Color = *q.Color
	}
	var created pimstore.Collection
	var err error
	for range 3 { // a second request of the same name meanwhile: next id
		var cols []pimstore.Collection
		if cols, err = store.Collections(pimstore.Calendar, user); err != nil {
			break
		}
		for _, c := range cols {
			meta.Order = max(meta.Order, c.Order+1)
		}
		id := calweb.CalendarID(name, func(id string) bool {
			return slices.ContainsFunc(cols, func(c pimstore.Collection) bool { return c.ID == id && !c.Virtual })
		})
		if created, err = store.CreateCollection(pimstore.Calendar, user, id, meta); !errors.Is(err, pimstore.ErrExists) {
			break
		}
	}
	if err != nil {
		calendarFail(w, r, "", err, "calendar_not_found")
		return
	}
	writeJSON(w, http.StatusCreated, ownCalendar(created, a.hiddenCalendars(user)))
}

func (a pimDataArea) handleUpdateCalendar(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == calweb.BirthdaysID {
		writeError(w, http.StatusForbidden, "calendar_read_only", "the birthdays come from the contacts")
		return
	}
	var q calendarRequest
	if !readJSON(w, r, &q) {
		return
	}
	name, ok := q.name()
	if !ok {
		writeError(w, http.StatusBadRequest, "calendar_invalid", "a calendar needs a name")
		return
	}
	patch := pimstore.MetaPatch{Color: q.Color, Order: q.Order}
	if q.Name != nil {
		patch.Name = &name
	}
	user := currentUser(r).Name
	c, err := store.UpdateCollection(pimstore.Calendar, user, id, patch)
	if err != nil {
		calendarFail(w, r, id, err, "calendar_not_found")
		return
	}
	writeJSON(w, http.StatusOK, ownCalendar(c, a.hiddenCalendars(user)))
}

// handleDeleteCalendar deletes a whole calendar with its events (an
// event is handleDeleteEvent). The last one stays: 409 calendar_last.
func (a pimDataArea) handleDeleteCalendar(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == calweb.BirthdaysID {
		writeError(w, http.StatusForbidden, "calendar_read_only", "the birthdays come from the contacts")
		return
	}
	user := currentUser(r).Name
	if err := store.DeleteCollection(pimstore.Calendar, user, id, ""); err != nil {
		calendarFail(w, r, id, err, "calendar_not_found")
		return
	}
	a.unhide(user, id)
	w.WriteHeader(http.StatusNoContent)
}
