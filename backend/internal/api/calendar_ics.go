package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Import and export of a calendar as one .ics file (TECHNIK 7.1, 5.3).

// maxImport bounds an uploaded .ics file (20 MB).
const maxImport = 20 << 20

// The RAM an import holds, measured (TestCalendarImport5000/-Large): per
// byte of the upload the bytes, the event blocks and the files made of
// them; per event its index entry and the parse that checks it.
const (
	importFactor   = 3
	importPerEvent = 6 << 10
)

// maxImportFailures bounds the list of failed entries in the answer.
const maxImportFailures = 500

type importResult struct {
	Added   int              `json:"added"`
	Updated int              `json:"updated"`
	Skipped int              `json:"skipped"`
	Failed  []calweb.Failure `json:"failed"`
}

// safeUID is a UID that may name its file as it is.
var safeUID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,150}$`)

// importName is the file name of a new UID: <uid>.ics (.vcf) when that
// is a plain name and free, else a hash of the UID, else a random one.
func importName(kind pimstore.Kind, uid string, taken map[string]bool) string {
	ext := ".ics"
	if kind == pimstore.Contacts {
		ext = ".vcf"
	}
	if name := uid + ext; safeUID.MatchString(uid) && pimstore.ValidName(kind, name) && !taken[name] {
		return name
	}
	sum := sha256.Sum256([]byte(uid))
	if name := hex.EncodeToString(sum[:16]) + ext; !taken[name] {
		return name
	}
	return calweb.NewUID() + ext
}

// importReason is the code of a failed entry (never the error text: a
// parse error may quote content).
func importReason(err error) string {
	var uc *pimstore.UIDConflictError
	switch {
	case errors.Is(err, pimstore.ErrTooLarge):
		return "too_large"
	case errors.Is(err, pimstore.ErrUnsupportedComponent):
		return "unsupported_component"
	case errors.Is(err, pimstore.ErrInvalidData), errors.Is(err, pimstore.ErrInvalidName):
		return "invalid"
	case errors.As(err, &uc):
		return "uid_conflict"
	case errors.Is(err, pimstore.ErrCollectionFull):
		return "full"
	}
	return "write_failed"
}

// reserveImport takes RAM of the budget for the rest of the request; it
// answers 503 calendar_busy itself when there is no room now.
func (a pimDataArea) reserveImport(w http.ResponseWriter, bytes int64) (release func(), ok bool) {
	return a.reserveRAM(w, pimstore.Calendar, bytes)
}

// handleImportCalendar imports an .ics file (multipart field "file",
// at most 20 MB) into a calendar: one file per UID with the VTIMEZONEs it
// needs, all written with one state.json (store.Import). A UID the
// calendar has is skipped or, with onDuplicate=replace, replaced in its
// file.
func (a pimDataArea) handleImportCalendar(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == calweb.BirthdaysID {
		readOnlyCalendar(w)
		return
	}
	onDuplicate := r.URL.Query().Get("onDuplicate")
	if onDuplicate == "" {
		onDuplicate = "skip"
	}
	if onDuplicate != "skip" && onDuplicate != "replace" {
		writeError(w, http.StatusBadRequest, "calendar_invalid", "onDuplicate is skip or replace")
		return
	}
	user := currentUser(r).Name
	if _, err := store.GetCollection(pimstore.Calendar, user, id); err != nil {
		calendarFail(w, r, id, err, "calendar_not_found")
		return
	}
	size := int64(maxImport)
	if r.ContentLength > 0 && r.ContentLength < size {
		size = r.ContentLength
	}
	release, ok := a.reserveImport(w, importFactor*size)
	if !ok {
		return
	}
	defer release()
	r.Body = http.MaxBytesReader(w, r.Body, maxImport+(64<<10))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "calendar_import_invalid", "a multipart body with the field file is required")
		return
	}
	var data []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "calendar_too_large", "the file is larger than 20 MB")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "calendar_import_invalid", "the multipart body could not be read")
			return
		}
		if part.FormName() != "file" || data != nil {
			continue
		}
		data, err = io.ReadAll(io.LimitReader(part, maxImport+1))
		if errors.As(err, &maxErr) || len(data) > maxImport {
			writeError(w, http.StatusRequestEntityTooLarge, "calendar_too_large", "the file is larger than 20 MB")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "calendar_import_invalid", "the file could not be read")
			return
		}
	}
	if data == nil {
		writeError(w, http.StatusBadRequest, "calendar_import_invalid", "the field file is missing")
		return
	}
	sp, err := calweb.SplitICS(data, calweb.NewUID)
	data = nil
	if err != nil {
		writeError(w, http.StatusBadRequest, "calendar_import_invalid", "the file is no iCalendar file")
		return
	}
	releaseEvents, ok := a.reserveImport(w, importPerEvent*int64(len(sp.Groups)))
	if !ok {
		return
	}
	defer releaseEvents()
	existing, err := store.Objects(pimstore.Calendar, user, id)
	if err != nil {
		calendarFail(w, r, id, err, "calendar_not_found")
		return
	}
	byUID := map[string]pimstore.ObjectInfo{}
	taken := map[string]bool{}
	for _, o := range existing {
		taken[o.Name] = true
		if o.UID != "" {
			byUID[o.UID] = o
		}
	}
	out := importResult{Failed: append([]calweb.Failure{}, sp.Failed...)}
	var items []pimstore.Item
	var uids []string
	for _, g := range sp.Groups {
		file := sp.File(g)
		if old, ok := byUID[g.UID]; ok {
			if onDuplicate == "skip" || pimstore.ETag(file) == old.ETag {
				out.Skipped++
				continue
			}
			items = append(items, pimstore.Item{Name: old.Name, Data: file})
		} else {
			name := importName(pimstore.Calendar, g.UID, taken)
			taken[name] = true
			items = append(items, pimstore.Item{Name: name, Data: file})
		}
		uids = append(uids, g.UID)
	}
	sp = nil
	if len(items) > 0 {
		results, err := store.Import(r.Context(), pimstore.Calendar, user, id, items)
		if err != nil {
			calendarFail(w, r, id, err, "calendar_not_found")
			return
		}
		for i, res := range results {
			switch {
			case res.Err != nil:
				out.Failed = append(out.Failed, calweb.Failure{UID: uids[i], Reason: importReason(res.Err)})
			case res.Created:
				out.Added++
			default:
				out.Updated++
			}
		}
	}
	if len(out.Failed) > maxImportFailures {
		out.Failed = out.Failed[:maxImportFailures]
	}
	if len(out.Failed) > 0 {
		slog.Info("Calendar: import with failed entries", "account", user, "calendar", id, "failed", len(out.Failed))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleExportCalendar writes a calendar as one .ics download: each
// VTIMEZONE once, then every event as stored. Two passes over the files,
// so only the zones stay in RAM; never shown inline (nosniff, sandbox).
func (a pimDataArea) handleExportCalendar(w http.ResponseWriter, r *http.Request) {
	store, ok := a.calendarStore(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	user := currentUser(r).Name
	col, err := store.GetCollection(pimstore.Calendar, user, id)
	if err != nil {
		calendarFail(w, r, id, err, "calendar_not_found")
		return
	}
	objects, err := store.Objects(pimstore.Calendar, user, id)
	if err != nil {
		calendarFail(w, r, id, err, "calendar_not_found")
		return
	}
	exp := calweb.NewExport(w)
	for _, o := range objects {
		obj, err := store.Get(pimstore.Calendar, user, id, o.Name)
		if errors.Is(err, pimstore.ErrNotFound) {
			continue // deleted meanwhile
		}
		if err != nil {
			calendarFail(w, r, id, err, "calendar_not_found")
			return
		}
		exp.Zones(obj.Data)
	}
	h := w.Header()
	h.Set("Content-Type", "text/calendar; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+id+`.ics"`)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "no-store")
	if err := exp.Begin(col.Name); err != nil {
		return // the client went away
	}
	for _, o := range objects {
		obj, err := store.Get(pimstore.Calendar, user, id, o.Name)
		if errors.Is(err, pimstore.ErrNotFound) {
			continue
		}
		if err != nil {
			// The answer has begun: a cut file must not look complete.
			slog.Error("Calendar: export cut short", "account", user, "calendar", id, "code", "read_failed")
			panic(http.ErrAbortHandler)
		}
		if err := exp.Events(obj.Data); err != nil {
			return
		}
	}
	_ = exp.End()
}
