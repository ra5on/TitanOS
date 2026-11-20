package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/davsync"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The sync of the Android app (1.19, PLAN 116, docs/ANDROID-APP.md
// "Kontakte und Kalender"): the account "TitanOS" in Contacts and Calendar
// of the phone needs the entries as they are stored – RRULE, EXDATE,
// exceptions (RECURRENCE-ID), VTIMEZONE, every alarm and vCard property –
// and the changes since the last sync. The routes of the web pages give
// the occurrences of a window in a form of their own, so the app would
// lose what the iPhone wrote. These routes are a thin mirror of davsync
// (CalDAV/CardDAV) for the session of the paired app: the same store, the
// same checks before a write (davsync.PrepareEntry), ETags as If-Match.
// Only for app sessions (appOnly), this node only, not in publicRoutes and
// not in the tool catalog, like the photo backup.
type appPIMArea struct {
	appSessionGuard
	store *pimstore.Store
}

func (s *Server) appPIMArea() appPIMArea {
	return appPIMArea{appSessionGuard: s.appSessionGuard(), store: s.opts.PIM}
}

func (a appPIMArea) register(mux *routeMux, g guards) {
	app := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(a.appOnly(h)))) }
	mux.HandleFunc("GET /api/v1/app/pim/{kind}", app(a.handleCollections))
	mux.HandleFunc("GET /api/v1/app/pim/{kind}/{col}/changes", app(a.handleChanges))
	mux.HandleFunc("POST /api/v1/app/pim/{kind}/{col}/objects", app(a.handleObjects))
	mux.HandleFunc("PUT /api/v1/app/pim/{kind}/{col}/objects/{name}", app(a.handlePut))
	mux.HandleFunc("DELETE /api/v1/app/pim/{kind}/{col}/objects/{name}", app(a.handleDelete))
}

// appPIMMaxChanges and appPIMMaxObjects bound one answer; a cut list of
// changes is truncated, the app asks again with the token.
const (
	appPIMMaxChanges = 1000
	appPIMMaxObjects = 100
)

type appPIMCollection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Order       int    `json:"order"`
	Description string `json:"description,omitempty"`
	CTag        string `json:"ctag"`
	SyncToken   string `json:"syncToken"`
	// Count is the number of entries (group cards included).
	Count int `json:"count"`
	// ReadOnly: the app must not write here. Every collection of the store
	// is the account's own today; the field is there for shared ones.
	ReadOnly bool `json:"readOnly"`
	// Virtual is the default an account has before its first entry.
	Virtual bool `json:"virtual,omitempty"`
}

type appPIMChange struct {
	Name    string `json:"name"`
	ETag    string `json:"etag,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

type appPIMChanges struct {
	Token     string         `json:"token"`
	Truncated bool           `json:"truncated"`
	Changes   []appPIMChange `json:"changes"`
}

type appPIMObject struct {
	Name string `json:"name"`
	ETag string `json:"etag"`
	Data string `json:"data"`
}

type appPIMObjects struct {
	Objects []appPIMObject `json:"objects"`
	// Missing are the names asked for that are not there (deleted meanwhile).
	Missing []string `json:"missing"`
}

type appPIMWritten struct {
	ETag    string `json:"etag"`
	Created bool   `json:"created"`
	// Rewritten: the store keeps other bytes than were sent (the picture
	// of a card shrunk); the app fetches the entry again.
	Rewritten bool `json:"rewritten"`
}

// kind is the kind of the path, or answers 404.
func (a appPIMArea) kind(w http.ResponseWriter, r *http.Request) (pimstore.Kind, bool) {
	switch k := pimstore.Kind(r.PathValue("kind")); k {
	case pimstore.Calendar, pimstore.Contacts, pimstore.Tasks:
		if a.store == nil || (a.store.Current != nil && a.store.Current(string(k)) == "") {
			appPIMUnavailable(w, k)
			return "", false
		}
		return k, true
	}
	writeError(w, http.StatusNotFound, "pim_not_found", "no such kind: calendar, contacts or tasks")
	return "", false
}

func appPIMUnavailable(w http.ResponseWriter, kind pimstore.Kind) {
	if kind == pimstore.Tasks {
		kind = pimstore.Calendar
	}
	writePIMUnavailable(w, kind)
}

// appPIMFail answers an error of the store.
func appPIMFail(w http.ResponseWriter, r *http.Request, kind pimstore.Kind, err error) {
	var uc *pimstore.UIDConflictError
	switch {
	case errors.Is(err, pimstore.ErrUnavailable), errors.Is(err, volumedata.ErrHeld):
		appPIMUnavailable(w, kind)
	case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidCollection):
		writeError(w, http.StatusNotFound, "pim_not_found", "no such collection or entry")
	case errors.Is(err, pimstore.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "pim_invalid_name", "the name of the entry is not allowed")
	case errors.Is(err, pimstore.ErrPrecondition):
		writeError(w, http.StatusPreconditionFailed, "pim_conflict", "the entry was changed or removed meanwhile")
	case errors.As(err, &uc):
		// The entry that holds the UID, so the app can take it instead of a duplicate.
		w.Header().Set("X-Conflict-Name", uc.Name)
		writeError(w, http.StatusConflict, "pim_uid_conflict", "another entry of the collection has this UID")
	case errors.Is(err, pimstore.ErrInvalidSyncToken):
		writeError(w, http.StatusGone, "pim_sync_token", "the sync token is no longer valid: start again without one")
	case errors.Is(err, pimstore.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "pim_too_large", "the entry is too large")
	case errors.Is(err, pimstore.ErrCollectionFull), errors.Is(err, pimstore.ErrTooManyCollections):
		writeError(w, http.StatusInsufficientStorage, "pim_full", "the collection or the account is full")
	case errors.Is(err, pimstore.ErrUnsupportedComponent):
		writeError(w, http.StatusBadRequest, "pim_unsupported", "the entry holds no component of this kind")
	case errors.Is(err, pimstore.ErrInvalidData):
		writeError(w, http.StatusBadRequest, "pim_invalid", "the entry is no valid iCalendar resp. vCard")
	default:
		slog.Error("App sync: request failed", "account", currentUser(r).Name, "kind", kind, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "the entries could not be read or written")
	}
}

func (a appPIMArea) handleCollections(w http.ResponseWriter, r *http.Request) {
	kind, ok := a.kind(w, r)
	if !ok {
		return
	}
	cols, err := a.store.Collections(kind, currentUser(r).Name)
	if err != nil {
		appPIMFail(w, r, kind, err)
		return
	}
	out := make([]appPIMCollection, 0, len(cols))
	for _, c := range cols {
		out = append(out, appPIMCollection{ID: c.ID, Name: c.Name, Color: c.Color, Order: c.Order, Description: c.Description,
			CTag: c.CTag, SyncToken: c.SyncToken, Count: c.Count, Virtual: c.Virtual})
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": out})
}

// handleChanges is sync-collection: without token every entry, else what
// changed since; truncated answers go on with their token.
func (a appPIMArea) handleChanges(w http.ResponseWriter, r *http.Request) {
	kind, ok := a.kind(w, r)
	if !ok {
		return
	}
	limit := appPIMMaxChanges
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < limit {
		limit = v
	}
	set, err := a.store.ChangesLimit(kind, currentUser(r).Name, r.PathValue("col"), r.URL.Query().Get("token"), limit)
	if err != nil {
		appPIMFail(w, r, kind, err)
		return
	}
	out := appPIMChanges{Token: set.Token, Truncated: set.Truncated, Changes: make([]appPIMChange, 0, len(set.Changes))}
	for _, c := range set.Changes {
		out.Changes = append(out.Changes, appPIMChange{Name: c.Name, ETag: c.ETag, Deleted: c.Deleted})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// handleObjects is multiget: the stored bytes of up to 100 entries.
func (a appPIMArea) handleObjects(w http.ResponseWriter, r *http.Request) {
	kind, ok := a.kind(w, r)
	if !ok {
		return
	}
	var req struct {
		Names []string `json:"names"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Names) > appPIMMaxObjects {
		writeError(w, http.StatusBadRequest, "pim_too_many", "at most 100 entries per request")
		return
	}
	user, col := currentUser(r).Name, r.PathValue("col")
	if _, err := a.store.GetCollection(kind, user, col); err != nil {
		appPIMFail(w, r, kind, err)
		return
	}
	out := appPIMObjects{Objects: []appPIMObject{}, Missing: []string{}}
	for _, name := range req.Names {
		obj, err := a.store.Get(kind, user, col, name)
		switch {
		case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidName):
			out.Missing = append(out.Missing, name)
		case err != nil:
			appPIMFail(w, r, kind, err)
			return
		default:
			out.Objects = append(out.Objects, appPIMObject{Name: obj.Name, ETag: obj.ETag, Data: string(obj.Data)})
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// unquote takes the tag of an If-Match header: one tag, quotes optional,
// never weak (W/ is no match for a write).
func unquoteTag(h string) (string, bool) {
	h = strings.TrimSpace(h)
	if h == "" || h == "*" {
		return h, true
	}
	if strings.HasPrefix(h, "W/") || strings.Contains(h, ",") {
		return "", false
	}
	return strings.Trim(h, `"`), true
}

// handlePut stores an entry as the body brings it: If-None-Match: * only
// creates, If-Match: <etag> only replaces that state (412 otherwise).
func (a appPIMArea) handlePut(w http.ResponseWriter, r *http.Request) {
	kind, ok := a.kind(w, r)
	if !ok {
		return
	}
	if !davsync.AcceptedType(kind, r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "pim_type", "text/calendar resp. text/vcard in UTF-8")
		return
	}
	ifMatch, okTag := unquoteTag(r.Header.Get("If-Match"))
	if !okTag {
		writeError(w, http.StatusBadRequest, "pim_if_match", "If-Match takes one strong entity tag")
		return
	}
	opt := pimstore.PutOptions{IfMatch: ifMatch}
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" {
		if inm != "*" {
			writeError(w, http.StatusBadRequest, "pim_if_none_match", "If-None-Match takes only *")
			return
		}
		opt.IfNoneMatch = true
	}
	limit := davsync.MaxEntry(kind)
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "pim_too_large", "the entry is too large")
		return
	}
	user, col, name := currentUser(r).Name, r.PathValue("col"), r.PathValue("name")
	if _, err := a.store.GetCollection(kind, user, col); err != nil {
		appPIMFail(w, r, kind, err)
		return
	}
	data, rewritten := davsync.PrepareEntry(kind, data)
	res, err := a.store.Put(kind, user, col, name, data, opt)
	if err != nil {
		appPIMFail(w, r, kind, err)
		return
	}
	w.Header().Set("ETag", `"`+res.ETag+`"`)
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, appPIMWritten{ETag: res.ETag, Created: res.Created, Rewritten: rewritten})
}

// handleDelete removes an entry; If-Match keeps a change made meanwhile.
func (a appPIMArea) handleDelete(w http.ResponseWriter, r *http.Request) {
	kind, ok := a.kind(w, r)
	if !ok {
		return
	}
	ifMatch, okTag := unquoteTag(r.Header.Get("If-Match"))
	if !okTag {
		writeError(w, http.StatusBadRequest, "pim_if_match", "If-Match takes one strong entity tag")
		return
	}
	col, name := r.PathValue("col"), r.PathValue("name")
	if err := a.store.DeleteObject(kind, currentUser(r).Name, col, name, ifMatch); err != nil {
		appPIMFail(w, r, kind, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
