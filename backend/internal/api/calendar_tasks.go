package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/pim/tasks"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Reminders (VTODO, 1.13): the reminder lists of the account and their
// reminders, under the routes of T-Kalender with the same rules – a web
// session of this node, the account's own data only (a foreign id is
// 404), without the volume 503 calendar_unavailable, never in
// publicRoutes or the tool catalog. The work is pim/tasks (Service), which
// the agent's tools will call as well; the writes go through pim/store,
// so ctag and sync token move and the iPhone sees them on its next sync.

func (a pimDataArea) registerTasks(mux *routeMux, web func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/calendar/tasklists", web(a.handleTaskLists))
	mux.HandleFunc("POST /api/v1/calendar/tasklists", web(a.handleCreateTaskList))
	mux.HandleFunc("PUT /api/v1/calendar/tasklists/{id}", web(a.handleUpdateTaskList))
	mux.HandleFunc("DELETE /api/v1/calendar/tasklists/{id}", web(a.handleDeleteTaskList))
	mux.HandleFunc("GET /api/v1/calendar/tasks", web(a.handleTasks))
	mux.HandleFunc("GET /api/v1/calendar/tasks/due", web(a.handleTasksDue))
	mux.HandleFunc("POST /api/v1/calendar/tasks", web(a.handleCreateTask))
	mux.HandleFunc("GET /api/v1/calendar/tasks/{cal}/{obj}", web(a.handleTask))
	mux.HandleFunc("PUT /api/v1/calendar/tasks/{cal}/{obj}", web(a.handleUpdateTask))
	mux.HandleFunc("POST /api/v1/calendar/tasks/{cal}/{obj}/done", web(a.handleTaskDone))
	mux.HandleFunc("DELETE /api/v1/calendar/tasks/{cal}/{obj}", web(a.handleDeleteTask))
}

// taskService is the service when the lists can be read now; else it
// answers 503 calendar_unavailable itself. From RAM only.
func (a pimDataArea) taskService(w http.ResponseWriter) (tasks.Service, bool) {
	if a.store == nil || (a.store.Current != nil && a.store.Current(string(pimstore.Tasks)) == "") {
		writePIMUnavailable(w, pimstore.Calendar)
		return tasks.Service{}, false
	}
	return tasks.Service{Store: a.store, Now: a.now}, true
}

var taskMessages = map[string]string{
	"tasklist_not_found":   "no such reminder list",
	"task_not_found":       "no such reminder",
	"task_conflict":        "the reminder was changed meanwhile",
	"tasklist_last":        "the last reminder list stays",
	"tasklist_exists":      "a list or calendar with this id exists",
	"task_uid_conflict":    "the target list holds this reminder already",
	"task_full":            "the list or the account is full",
	"task_too_large":       "the reminder is too large",
	"task_invalid":         "the reminder, its dates, repetition or the list are not valid",
	"calendar_window":      calendarMessages["calendar_window"],
	"calendar_unavailable": "the reminders cannot be read now",
	"internal":             "the reminders could not be read or written",
}

func taskCode(err error, notFound string) (int, string) {
	var uc *pimstore.UIDConflictError
	switch {
	case errors.Is(err, pimstore.ErrUnavailable), errors.Is(err, volumedata.ErrHeld):
		return http.StatusServiceUnavailable, "calendar_unavailable"
	case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidCollection), errors.Is(err, pimstore.ErrInvalidName):
		return http.StatusNotFound, notFound
	case errors.Is(err, pimstore.ErrPrecondition):
		return http.StatusConflict, "task_conflict"
	case errors.Is(err, pimstore.ErrLastCollection):
		return http.StatusConflict, "tasklist_last"
	case errors.Is(err, pimstore.ErrExists):
		return http.StatusConflict, "tasklist_exists"
	case errors.As(err, &uc):
		return http.StatusConflict, "task_uid_conflict"
	case errors.Is(err, pimstore.ErrCollectionFull), errors.Is(err, pimstore.ErrTooManyCollections):
		return http.StatusInsufficientStorage, "task_full"
	case errors.Is(err, pimstore.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "task_too_large"
	case errors.Is(err, recur.ErrWindow):
		return http.StatusBadRequest, "calendar_window"
	case errors.Is(err, tasks.ErrInvalid), errors.Is(err, pimstore.ErrInvalidMeta), errors.Is(err, pimstore.ErrInvalidData),
		errors.Is(err, pimstore.ErrUnsupportedComponent), errors.Is(err, recur.ErrInvalid), errors.Is(err, recur.ErrLimit):
		return http.StatusBadRequest, "task_invalid"
	}
	return http.StatusInternalServerError, "internal"
}

// taskFail answers an error; the journal gets account, list and code of a
// failure of the server only, never titles or the error text of a file.
func taskFail(w http.ResponseWriter, r *http.Request, list string, err error, notFound string) {
	status, code := taskCode(err, notFound)
	if status == http.StatusServiceUnavailable {
		writePIMUnavailable(w, pimstore.Calendar)
		return
	}
	if status == http.StatusInternalServerError {
		slog.Error("Reminders: request failed", "account", currentUser(r).Name, "list", list, "code", code, "err", err)
	}
	writeError(w, status, code, taskMessages[code])
}

func lang(r *http.Request) string {
	if r.URL.Query().Get("lang") == "en" {
		return "en"
	}
	return "de"
}

func (a pimDataArea) handleTaskLists(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	lists, err := svc.Lists(currentUser(r).Name, lang(r))
	if err != nil {
		taskFail(w, r, "", err, "tasklist_not_found")
		return
	}
	writeJSON(w, http.StatusOK, lists)
}

func (a pimDataArea) handleCreateTaskList(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	var q tasks.ListInput
	if !readJSON(w, r, &q) {
		return
	}
	l, err := svc.CreateList(currentUser(r).Name, q)
	if err != nil {
		taskFail(w, r, "", err, "tasklist_not_found")
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (a pimDataArea) handleUpdateTaskList(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	var q tasks.ListInput
	if !readJSON(w, r, &q) {
		return
	}
	id := r.PathValue("id")
	l, err := svc.UpdateList(currentUser(r).Name, id, q)
	if err != nil {
		taskFail(w, r, id, err, "tasklist_not_found")
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// handleDeleteTaskList deletes a whole list with its reminders (one
// reminder is handleDeleteTask). The last one stays: 409 tasklist_last.
func (a pimDataArea) handleDeleteTaskList(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.DeleteList(currentUser(r).Name, id); err != nil {
		taskFail(w, r, id, err, "tasklist_not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type taskList struct {
	Tasks []tasks.Item `json:"tasks"`
}

// parseTaskDay reads a date (00:00 in the NAS zone) or an RFC 3339 time.
func parseTaskDay(s string, loc *time.Location) (time.Time, bool) {
	if t, err := time.ParseInLocation(time.DateOnly, s, loc); err == nil {
		return t, true
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// handleTasks lists reminders from the index: ?list= one list (else all),
// ?status=open|done|all (open), ?dueBefore= a date or time (exclusive).
func (a pimDataArea) handleTasks(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := tasks.Filter{List: q.Get("list"), Status: q.Get("status")}
	if s := q.Get("dueBefore"); s != "" {
		if f.DueBefore, ok = parseTaskDay(s, a.store.Zone()); !ok {
			writeError(w, http.StatusBadRequest, "task_invalid", "dueBefore is a date or an RFC 3339 time")
			return
		}
	}
	items, err := svc.Tasks(currentUser(r).Name, f)
	if err != nil {
		taskFail(w, r, f.List, err, "tasklist_not_found")
		return
	}
	writeJSON(w, http.StatusOK, taskList{Tasks: items})
}

// handleTasksDue lists the open reminders of all lists due in [from, to)
// (dates or RFC 3339, at most 400 days), for the calendar's views.
func (a pimDataArea) handleTasksDue(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	from, ok1 := parseTaskDay(q.Get("from"), a.store.Zone())
	to, ok2 := parseTaskDay(q.Get("to"), a.store.Zone())
	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "calendar_window", taskMessages["calendar_window"])
		return
	}
	items, err := svc.Due(currentUser(r).Name, from, to)
	if err != nil {
		taskFail(w, r, "", err, "tasklist_not_found")
		return
	}
	writeJSON(w, http.StatusOK, taskList{Tasks: items})
}

func (a pimDataArea) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	var q tasks.Input
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	t, err := svc.Create(currentUser(r).Name, q)
	if err != nil {
		taskFail(w, r, q.List, err, "tasklist_not_found")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a pimDataArea) handleTask(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	list := r.PathValue("cal")
	t, err := svc.Get(currentUser(r).Name, list, r.PathValue("obj"))
	if err != nil {
		taskFail(w, r, list, err, "task_not_found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// taskConflict answers 409 task_conflict with the reminder as it is now
// (null when it is gone).
func taskConflict(w http.ResponseWriter, svc tasks.Service, user, list, id string) {
	body := map[string]any{"error": apiError{Code: "task_conflict", Message: taskMessages["task_conflict"]}, "current": nil}
	if t, err := svc.Get(user, list, id); err == nil {
		body["current"] = t
	}
	writeJSON(w, http.StatusConflict, body)
}

type updateTaskRequest struct {
	ETag string `json:"etag"`
	tasks.Input
}

// handleUpdateTask changes a reminder: etag (required) is the one read;
// fields left out stay; list moves it. An older state is 409
// task_conflict with current.
func (a pimDataArea) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	var q updateTaskRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	user, list, id := currentUser(r).Name, r.PathValue("cal"), r.PathValue("obj")
	if strings.TrimSpace(q.ETag) == "" {
		writeError(w, http.StatusBadRequest, "task_invalid", "etag missing")
		return
	}
	t, _, err := svc.Update(user, list, id, q.ETag, q.Input)
	if errors.Is(err, pimstore.ErrPrecondition) {
		taskConflict(w, svc, user, list, id)
		return
	}
	if err != nil {
		taskFail(w, r, list, err, "task_not_found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type taskDoneRequest struct {
	ETag string `json:"etag"`
	Done *bool  `json:"done"`
}

// handleTaskDone ticks a reminder off (done: true) or opens it again
// (false); a series moves to its next occurrence and keeps the done one
// as copy.
func (a pimDataArea) handleTaskDone(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	var q taskDoneRequest
	if !readJSON(w, r, &q) {
		return
	}
	user, list, id := currentUser(r).Name, r.PathValue("cal"), r.PathValue("obj")
	if strings.TrimSpace(q.ETag) == "" || q.Done == nil {
		writeError(w, http.StatusBadRequest, "task_invalid", "etag and done are required")
		return
	}
	d, err := svc.SetDone(user, list, id, q.ETag, *q.Done)
	if errors.Is(err, pimstore.ErrPrecondition) {
		taskConflict(w, svc, user, list, id)
		return
	}
	if err != nil {
		taskFail(w, r, list, err, "task_not_found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a pimDataArea) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	svc, ok := a.taskService(w)
	if !ok {
		return
	}
	user, list, id := currentUser(r).Name, r.PathValue("cal"), r.PathValue("obj")
	etag := r.URL.Query().Get("etag")
	if strings.TrimSpace(etag) == "" {
		writeError(w, http.StatusBadRequest, "task_invalid", "etag missing")
		return
	}
	err := svc.Delete(user, list, id, etag)
	if errors.Is(err, pimstore.ErrPrecondition) {
		taskConflict(w, svc, user, list, id)
		return
	}
	if err != nil {
		taskFail(w, r, list, err, "task_not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
