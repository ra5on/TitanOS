package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// maxEventBody bounds the JSON of one event (notes up to 32 KiB, escaped).
const maxEventBody = 256 << 10

func (a pimDataArea) now() time.Time {
	if a.clock != nil {
		return a.clock()
	}
	return time.Now()
}

// parseInstant reads from/to of the event list: RFC 3339, or a date or
// wall clock in the zone of the NAS.
func parseInstant(s string, local *time.Location) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// viewStart places a view on the timeline for sorting.
func viewStart(v calweb.View, local *time.Location) time.Time {
	t, _ := parseInstant(v.Start, local)
	return t
}

type calendarEventList struct {
	Events    []calweb.View `json:"events"`
	Truncated bool          `json:"truncated"`
	// BirthdaysUnavailable: the contacts could not be read now; the
	// birthdays are missing from the list (the events are complete).
	BirthdaysUnavailable bool `json:"birthdaysUnavailable,omitempty"`
}

// handleEvents lists the occurrences of [from, to) (at most 400 days) of
// the calendars named, or of all that are not hidden (the birthdays
// included unless hidden), sorted by start.
func (a pimDataArea) handleEvents(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	local := store.Zone()
	from, okFrom := parseInstant(r.URL.Query().Get("from"), local)
	to, okTo := parseInstant(r.URL.Query().Get("to"), local)
	if !okFrom || !okTo || !to.After(from) || to.After(from.AddDate(0, 0, recur.MaxWindowDays)) {
		writeError(w, http.StatusBadRequest, "calendar_window", calendarMessages["calendar_window"])
		return
	}
	user := currentUser(r).Name
	cols, err := store.Collections(pimstore.Calendar, user)
	if err != nil {
		calendarFail(w, r, "", err, "calendar_not_found")
		return
	}
	var ids []string
	birthdays, explicit := false, false
	if q := r.URL.Query().Get("calendars"); q != "" {
		explicit = true
		for _, id := range strings.Split(q, ",") {
			id = strings.TrimSpace(id)
			switch {
			case id == calweb.BirthdaysID:
				birthdays = true
			case slices.ContainsFunc(cols, func(c pimstore.Collection) bool { return c.ID == id }):
				if !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			default:
				writeError(w, http.StatusNotFound, "calendar_not_found", calendarMessages["calendar_not_found"])
				return
			}
		}
	} else {
		hidden := a.hiddenCalendars(user)
		for _, c := range cols {
			if !slices.Contains(hidden, c.ID) {
				ids = append(ids, c.ID)
			}
		}
		birthdays = !slices.Contains(hidden, calweb.BirthdaysID)
	}
	out := calendarEventList{Events: []calweb.View{}}
	for _, id := range ids {
		occs, cut, err := store.Query(user, id, from, to, pimstore.MaxQueryOccurrences)
		if err != nil {
			calendarFail(w, r, id, err, "calendar_not_found")
			return
		}
		out.Truncated = out.Truncated || cut
		for _, o := range occs {
			out.Truncated = out.Truncated || o.Truncated
		}
		out.Events = append(out.Events, calweb.Views(id, occs, local)...)
		if len(out.Events) > pimstore.MaxQueryOccurrences {
			// The earliest MaxQueryOccurrences of all calendars (S-20).
			out.Events, _ = earliestViews(out.Events, pimstore.MaxQueryOccurrences, local)
			out.Truncated = true
		}
	}
	if birthdays {
		contacts, err := store.Objects(pimstore.Contacts, user, pimstore.DefaultBook)
		switch {
		case err == nil:
			out.Events = append(out.Events, calweb.Birthdays(contacts, from, to, local)...)
		case explicit:
			calendarFail(w, r, calweb.BirthdaysID, err, "calendar_not_found")
			return
		default:
			out.BirthdaysUnavailable = true
		}
	}
	var cut bool
	out.Events, cut = earliestViews(out.Events, pimstore.MaxQueryOccurrences, local)
	out.Truncated = out.Truncated || cut
	writeJSON(w, http.StatusOK, out)
}

// earliestViews sorts views by start and keeps the first limit; true when
// it cut.
func earliestViews(views []calweb.View, limit int, local *time.Location) ([]calweb.View, bool) {
	type keyed struct {
		at time.Time
		v  calweb.View
	}
	ks := make([]keyed, len(views))
	for i, v := range views {
		ks[i] = keyed{viewStart(v, local), v}
	}
	slices.SortStableFunc(ks, func(x, y keyed) int { return x.at.Compare(y.at) })
	for i := range ks {
		views[i] = ks[i].v
	}
	if len(views) <= limit {
		return views, false
	}
	clear(views[limit:])
	return views[:limit], true
}

// birthdayEvent is one birthday of a contact: the occurrence named
// (YYYYMMDD) or the next one from today.
func (a pimDataArea) birthdayEvent(store *pimstore.Store, user, name, occurrence string) (calweb.View, error) {
	info, err := store.Stat(pimstore.Contacts, user, pimstore.DefaultBook, name)
	if err != nil {
		return calweb.View{}, err
	}
	local := store.Zone()
	from := a.now().In(local)
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, local)
	to := from.AddDate(1, 0, 1)
	if occurrence != "" {
		day, err := time.ParseInLocation("20060102", occurrence, local)
		if err != nil {
			return calweb.View{}, calweb.ErrNoOccurrence
		}
		from, to = day, day.AddDate(0, 0, 1)
	}
	views := calweb.Birthdays([]pimstore.ObjectInfo{info}, from, to, local)
	if len(views) == 0 {
		return calweb.View{}, calweb.ErrNoOccurrence
	}
	return views[0], nil
}

// handleEvent returns one event with its notes and etag.
func (a pimDataArea) handleEvent(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	cal, name, occurrence := r.PathValue("cal"), r.PathValue("obj"), r.URL.Query().Get("occurrence")
	user := currentUser(r).Name
	if cal == calweb.BirthdaysID {
		v, err := a.birthdayEvent(store, user, name, occurrence)
		if err != nil {
			calendarFail(w, r, cal, err, "event_not_found")
			return
		}
		writeJSON(w, http.StatusOK, v)
		return
	}
	v, _, err := a.openEvent(store, user, cal, name, occurrence)
	if err != nil {
		eventFail(w, r, cal, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// openEvent reads an event of an own calendar: the view of the occurrence
// and the stored object. A missing calendar is calendar_not_found.
func (a pimDataArea) openEvent(store *pimstore.Store, user, cal, name, occurrence string) (calweb.View, pimstore.Object, error) {
	if _, err := store.GetCollection(pimstore.Calendar, user, cal); err != nil {
		if errors.Is(err, pimstore.ErrNotFound) || errors.Is(err, pimstore.ErrInvalidCollection) {
			return calweb.View{}, pimstore.Object{}, errCalendarMissing
		}
		return calweb.View{}, pimstore.Object{}, err
	}
	obj, err := store.Get(pimstore.Calendar, user, cal, name)
	if err != nil {
		return calweb.View{}, pimstore.Object{}, err
	}
	v, err := calweb.Open(obj.Data, occurrence, store.Zone())
	if err != nil {
		return calweb.View{}, obj, err
	}
	v.ID, v.Calendar, v.ETag = name, cal, obj.ETag
	return v, obj, nil
}

// errCalendarMissing turns into 404 calendar_not_found.
var errCalendarMissing = errors.New("calendar missing")

func eventFail(w http.ResponseWriter, r *http.Request, cal string, err error) {
	if errors.Is(err, errCalendarMissing) {
		writeError(w, http.StatusNotFound, "calendar_not_found", calendarMessages["calendar_not_found"])
		return
	}
	calendarFail(w, r, cal, err, "event_not_found")
}

func readOnlyCalendar(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "calendar_read_only", "the birthdays come from the contacts")
}

type createEventRequest struct {
	Calendar string       `json:"calendar"`
	Event    calweb.Input `json:"event"`
}

type eventWritten struct {
	ID       string `json:"id"`
	ETag     string `json:"etag"`
	Calendar string `json:"calendar"`
	UID      string `json:"uid,omitempty"`
	// Scope applied: "this", "following" or "all" (a single event knows
	// only "all"; "following" at the first occurrence is "all").
	Scope             string `json:"scope,omitempty"`
	DroppedExceptions int    `json:"droppedExceptions"`
	// NewID/NewETag: the new series "this and following" made.
	NewID   string `json:"newId,omitempty"`
	NewETag string `json:"newEtag,omitempty"`
}

func (a pimDataArea) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	var q createEventRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	if q.Calendar == calweb.BirthdaysID {
		readOnlyCalendar(w)
		return
	}
	user := currentUser(r).Name
	if _, err := store.GetCollection(pimstore.Calendar, user, q.Calendar); err != nil {
		calendarFail(w, r, q.Calendar, err, "calendar_not_found")
		return
	}
	uid := calweb.NewUID()
	data, err := calweb.Build(q.Event, uid, a.now(), store.Zone())
	if err != nil {
		calendarFail(w, r, q.Calendar, err, "calendar_not_found")
		return
	}
	name := uid + ".ics"
	res, err := store.Put(pimstore.Calendar, user, q.Calendar, name, data, pimstore.PutOptions{IfNoneMatch: true})
	if err != nil {
		calendarFail(w, r, q.Calendar, err, "calendar_not_found")
		return
	}
	writeJSON(w, http.StatusCreated, eventWritten{ID: name, ETag: res.ETag, Calendar: q.Calendar, UID: uid, Scope: "all"})
}

func parseScope(s string) (recur.Scope, bool) {
	switch s {
	case "", "all":
		return recur.ScopeAll, true
	case "this":
		return recur.ScopeThis, true
	case "following":
		return recur.ScopeFollowing, true
	}
	return 0, false
}

func scopeName(s recur.Scope) string {
	switch s {
	case recur.ScopeThis:
		return "this"
	case recur.ScopeFollowing:
		return "following"
	}
	return "all"
}

func sameETag(given, current string) bool {
	return given != "" && strings.Trim(strings.TrimSpace(given), `"`) == current
}

// conflict answers 409 calendar_conflict with the event as it is now
// (null when it is gone), so the UI can show it and ask again.
func (a pimDataArea) conflict(w http.ResponseWriter, store *pimstore.Store, user, cal, name, occurrence string) {
	body := map[string]any{"error": apiError{Code: "calendar_conflict", Message: calendarMessages["calendar_conflict"]}, "current": nil}
	if v, _, err := a.openEvent(store, user, cal, name, occurrence); err == nil {
		body["current"] = v
	} else if v, _, err := a.openEvent(store, user, cal, name, ""); err == nil {
		body["current"] = v
	}
	writeJSON(w, http.StatusConflict, body)
}

type updateEventRequest struct {
	ETag       string       `json:"etag"`
	Scope      string       `json:"scope"`
	Occurrence string       `json:"occurrence"`
	Event      calweb.Input `json:"event"`
	// Calendar, when another own calendar: move the event there (the
	// whole series only).
	Calendar string `json:"calendar"`
}

// handleUpdateEvent changes an event (this / following / all, TECHNIK
// 2.4) on its component tree, or moves it into another calendar: a new
// file there, the old one removed (both in the change logs).
func (a pimDataArea) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	cal, name := r.PathValue("cal"), r.PathValue("obj")
	if cal == calweb.BirthdaysID {
		readOnlyCalendar(w)
		return
	}
	var q updateEventRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	scope, ok := parseScope(q.Scope)
	if !ok || q.ETag == "" {
		writeError(w, http.StatusBadRequest, "calendar_invalid", "etag and a scope this, following or all are required")
		return
	}
	user := currentUser(r).Name
	target := cal
	if q.Calendar != "" && q.Calendar != cal {
		if q.Calendar == calweb.BirthdaysID {
			readOnlyCalendar(w)
			return
		}
		if _, err := store.GetCollection(pimstore.Calendar, user, q.Calendar); err != nil {
			calendarFail(w, r, q.Calendar, err, "calendar_not_found")
			return
		}
		target = q.Calendar
	}
	opened, obj, err := a.openEvent(store, user, cal, name, q.Occurrence)
	if err != nil && obj.ETag == "" {
		eventFail(w, r, cal, err)
		return
	}
	if !sameETag(q.ETag, obj.ETag) {
		a.conflict(w, store, user, cal, name, q.Occurrence)
		return
	}
	if err != nil {
		eventFail(w, r, cal, err)
		return
	}
	if target != cal && opened.Recurring && scope != recur.ScopeAll {
		writeError(w, http.StatusBadRequest, "calendar_move_series", "a series moves to another calendar only as a whole")
		return
	}
	res, changed, err := calweb.Edit(obj.Data, q.Occurrence, scope, q.Event, calweb.EditOptions{Local: store.Zone(), Now: a.now(), NewUID: calweb.NewUID()})
	if err != nil {
		eventFail(w, r, cal, err)
		return
	}
	out := eventWritten{ID: name, ETag: obj.ETag, Calendar: cal, Scope: scopeName(scope), DroppedExceptions: res.DroppedExceptions}
	if changed {
		out.Scope = scopeName(res.Scope)
	}
	if target != cal {
		a.moveEvent(w, r, store, user, cal, target, obj, res.Data, changed, q.Occurrence)
		return
	}
	if !changed {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if res.New != nil {
		newName := res.NewUID + ".ics"
		put, err := store.Put(pimstore.Calendar, user, cal, newName, res.New, pimstore.PutOptions{IfNoneMatch: true})
		if err != nil {
			eventFail(w, r, cal, err)
			return
		}
		out.NewID, out.NewETag = newName, put.ETag
	}
	put, err := store.Put(pimstore.Calendar, user, cal, name, res.Data, pimstore.PutOptions{IfMatch: obj.ETag})
	if err != nil {
		if out.NewID != "" {
			_ = store.DeleteObject(pimstore.Calendar, user, cal, out.NewID, "")
		}
		if errors.Is(err, pimstore.ErrPrecondition) {
			a.conflict(w, store, user, cal, name, q.Occurrence)
			return
		}
		eventFail(w, r, cal, err)
		return
	}
	out.ETag = put.ETag
	writeJSON(w, http.StatusOK, out)
}

// moveEvent writes the (edited) event into target under its name (or a
// new one when that is taken), then removes it from cal with its etag; a
// change meanwhile removes the copy again.
func (a pimDataArea) moveEvent(w http.ResponseWriter, r *http.Request, store *pimstore.Store, user, cal, target string, obj pimstore.Object, data []byte, changed bool, occurrence string) {
	if !changed {
		data = obj.Data
	}
	newName := obj.Name
	if _, err := store.Stat(pimstore.Calendar, user, target, newName); err == nil {
		newName = calweb.NewUID() + ".ics"
	}
	put, err := store.Put(pimstore.Calendar, user, target, newName, data, pimstore.PutOptions{IfNoneMatch: true})
	if err != nil {
		eventFail(w, r, target, err)
		return
	}
	if err := store.DeleteObject(pimstore.Calendar, user, cal, obj.Name, obj.ETag); err != nil {
		_ = store.DeleteObject(pimstore.Calendar, user, target, newName, put.ETag)
		if errors.Is(err, pimstore.ErrPrecondition) {
			a.conflict(w, store, user, cal, obj.Name, occurrence)
			return
		}
		eventFail(w, r, cal, err)
		return
	}
	writeJSON(w, http.StatusOK, eventWritten{ID: newName, ETag: put.ETag, Calendar: target, Scope: "all"})
}

type eventDeleted struct {
	// Removed: the file went (the whole event); else ETag is the new one.
	Removed bool   `json:"removed"`
	ETag    string `json:"etag,omitempty"`
	Scope   string `json:"scope"`
}

// handleDeleteEvent deletes an occurrence, it and the following, or the
// whole event (a calendar is handleDeleteCalendar).
func (a pimDataArea) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	cal, name := r.PathValue("cal"), r.PathValue("obj")
	if cal == calweb.BirthdaysID {
		readOnlyCalendar(w)
		return
	}
	q := r.URL.Query()
	scope, ok := parseScope(q.Get("scope"))
	if !ok || q.Get("etag") == "" {
		writeError(w, http.StatusBadRequest, "calendar_invalid", "etag and a scope this, following or all are required")
		return
	}
	user, occurrence := currentUser(r).Name, q.Get("occurrence")
	if _, err := recur.ParseOccurrenceID(occurrence); occurrence != "" && err != nil {
		writeError(w, http.StatusNotFound, "event_not_found", calendarMessages["event_not_found"])
		return
	}
	_, obj, err := a.openEvent(store, user, cal, name, "")
	if err != nil && obj.ETag == "" {
		eventFail(w, r, cal, err)
		return
	}
	if !sameETag(q.Get("etag"), obj.ETag) {
		a.conflict(w, store, user, cal, name, occurrence)
		return
	}
	res, err := recur.Delete(obj.Data, "", occurrence, scope, recur.EditOptions{Local: store.Zone(), Now: a.now(), ProdID: calweb.ProdID})
	if err != nil {
		eventFail(w, r, cal, err)
		return
	}
	out := eventDeleted{Removed: res.Delete, Scope: scopeName(res.Scope)}
	if res.Delete {
		err = store.DeleteObject(pimstore.Calendar, user, cal, name, obj.ETag)
	} else {
		var put pimstore.PutResult
		put, err = store.Put(pimstore.Calendar, user, cal, name, res.Data, pimstore.PutOptions{IfMatch: obj.ETag})
		out.ETag = put.ETag
	}
	if errors.Is(err, pimstore.ErrPrecondition) {
		a.conflict(w, store, user, cal, name, occurrence)
		return
	}
	if err != nil {
		eventFail(w, r, cal, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePreview resolves the rule of the window "Neuer Termin": the next
// five occurrences and the rule in words (one resolution, on the server).
func (a pimDataArea) handlePreview(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	var in calweb.PreviewInput
	if !readJSON(w, r, &in) {
		return
	}
	res, err := calweb.Preview(in, 5, store.Zone())
	if err != nil {
		calendarFail(w, r, "", err, "event_not_found")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
