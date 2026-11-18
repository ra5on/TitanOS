package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/pim/tasks"
)

const iPhoneReminder = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\nBEGIN:VTODO\r\n" +
	"CREATED:20261118T081500Z\r\nDTSTAMP:20261118T081512Z\r\nSUMMARY:Arzt anrufen\r\nUID:9B1F0D3E\r\n" +
	"DUE;TZID=Europe/Berlin:20261120T090000\r\nDTSTART;TZID=Europe/Berlin:20261120T090000\r\nX-APPLE-SORT-ORDER:785139312\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261120T080000Z\r\nX-WR-ALARMUID:keep-me\r\nEND:VALARM\r\n" +
	"END:VTODO\r\nEND:VCALENDAR\r\n"

func (e *calEnv) list(query string) []tasks.Item {
	e.t.Helper()
	var out struct {
		Tasks []tasks.Item `json:"tasks"`
	}
	e.do("GET", "/api/v1/calendar/tasks"+query, nil, http.StatusOK, &out)
	return out.Tasks
}

func TestTasksAPI(t *testing.T) {
	e := newCalEnv(t)
	var lists []tasks.List
	e.do("GET", "/api/v1/calendar/tasklists?lang=en", nil, http.StatusOK, &lists)
	if len(lists) != 1 || lists[0].ID != pimstore.DefaultTaskList || lists[0].Name != "Reminders" || !lists[0].Virtual {
		t.Fatalf("lists: %+v", lists)
	}
	// The calendars never show a list, the events never a reminder.
	var cals []calendarView
	e.do("GET", "/api/v1/calendar/calendars", nil, http.StatusOK, &cals)
	if len(cals) != 2 {
		t.Fatalf("calendars: %+v", cals)
	}
	var created tasks.Task
	e.do("POST", "/api/v1/calendar/tasks", map[string]any{"title": "Milch kaufen", "due": "2026-11-20", "priority": 1, "lang": "de"}, http.StatusCreated, &created)
	if created.List != pimstore.DefaultTaskList || created.Due != "2026-11-20" || !created.DueAllDay || created.ETag == "" {
		t.Fatalf("created: %+v", created)
	}
	if ev := e.events("from=2026-11-01T00:00:00Z&to=2026-12-01T00:00:00Z"); len(ev.Events) != 0 {
		t.Fatalf("a reminder among the events: %+v", ev.Events)
	}
	col, _ := e.pim.GetCollection(pimstore.Tasks, "alex", pimstore.DefaultTaskList)
	ctag := col.CTag
	// A reminder the iPhone wrote, with what TitanOS does not know.
	if _, err := e.pim.Put(pimstore.Tasks, "alex", pimstore.DefaultTaskList, "9B1F0D3E.ics", []byte(iPhoneReminder), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	open := e.list("")
	// By due: the date (00:00) before the time on that day.
	if len(open) != 2 || open[1].Title != "Arzt anrufen" || open[1].Due != "2026-11-20T09:00:00+01:00" || !open[1].Alarm || open[0].Title != "Milch kaufen" {
		t.Fatalf("open: %+v", open)
	}
	var due struct {
		Tasks []tasks.Item `json:"tasks"`
	}
	e.do("GET", "/api/v1/calendar/tasks/due?from=2026-11-20&to=2026-11-21", nil, http.StatusOK, &due)
	if len(due.Tasks) != 2 {
		t.Fatalf("due: %+v", due.Tasks)
	}
	e.do("GET", "/api/v1/calendar/tasks/due?from=2026-11-20&to=2028-11-21", nil, http.StatusBadRequest, nil)
	e.do("GET", "/api/v1/calendar/tasks?dueBefore=2026-11-20T09:00:00%2B01:00", nil, http.StatusOK, nil)
	if got := e.list("?dueBefore=2026-11-20T09:30:00%2B01:00"); len(got) != 2 {
		t.Fatalf("due before: %+v", got)
	}

	// Change the iPhone's reminder: the X-APPLE property and the alarm's
	// own property stay; an old ETag is a conflict with the current state.
	var one tasks.Task
	path := "/api/v1/calendar/tasks/" + pimstore.DefaultTaskList + "/9B1F0D3E.ics"
	e.do("GET", path, nil, http.StatusOK, &one)
	if len(one.Alarms) != 1 || one.Alarms[0] != "2026-11-20T09:00:00+01:00" || one.Timezone != "Europe/Berlin" {
		t.Fatalf("one: %+v", one)
	}
	var edited tasks.Task
	e.do("PUT", path, map[string]any{"etag": one.ETag, "title": "Arzt anrufen (Rezept)", "notes": "Überweisung"}, http.StatusOK, &edited)
	obj, _ := e.pim.Get(pimstore.Tasks, "alex", pimstore.DefaultTaskList, "9B1F0D3E.ics")
	raw := strings.ReplaceAll(string(obj.Data), "\r\n ", "")
	mustHave(t, "edited", raw, "SUMMARY:Arzt anrufen (Rezept)", "DESCRIPTION:Überweisung", "X-APPLE-SORT-ORDER:785139312", "X-WR-ALARMUID:keep-me", "SEQUENCE:1")
	body := e.do("PUT", path, map[string]any{"etag": one.ETag, "title": "x"}, http.StatusConflict, nil)
	var conflict struct {
		Error   apiError   `json:"error"`
		Current tasks.Task `json:"current"`
	}
	if json.Unmarshal(body, &conflict); conflict.Error.Code != "task_conflict" || conflict.Current.ETag != edited.ETag {
		t.Fatalf("conflict: %s", body)
	}
	e.do("PUT", path, map[string]any{"title": "x"}, http.StatusBadRequest, nil)
	var same tasks.Task
	e.do("PUT", path, map[string]any{"etag": edited.ETag, "title": "Arzt anrufen (Rezept)"}, http.StatusOK, &same)
	if same.ETag != edited.ETag {
		t.Fatal("an unchanged input wrote")
	}
	// Ticked off and opened again.
	var done tasks.Done
	e.do("POST", path+"/done", map[string]any{"etag": edited.ETag, "done": true}, http.StatusOK, &done)
	if !done.Task.Done || done.Copy != nil {
		t.Fatalf("done: %+v", done)
	}
	if got := e.list("?status=done"); len(got) != 1 || got[0].ID != "9B1F0D3E.ics" {
		t.Fatalf("done list: %+v", got)
	}
	e.do("POST", path+"/done", map[string]any{"etag": edited.ETag, "done": false}, http.StatusConflict, nil)
	e.do("POST", path+"/done", map[string]any{"etag": done.Task.ETag}, http.StatusBadRequest, nil)
	var reopened tasks.Done // done:false is left out of the answer
	e.do("POST", path+"/done", map[string]any{"etag": done.Task.ETag, "done": false}, http.StatusOK, &reopened)
	if reopened.Task.Done || reopened.Task.ETag == done.Task.ETag {
		t.Fatal("not opened again")
	}
	// Every write moved the list's ctag (the iPhone sees it).
	col, _ = e.pim.GetCollection(pimstore.Tasks, "alex", pimstore.DefaultTaskList)
	if col.CTag == ctag {
		t.Fatal("ctag did not move")
	}
	// Delete: the ETag is required.
	e.do("DELETE", path, nil, http.StatusBadRequest, nil)
	e.do("DELETE", path+"?etag="+url.QueryEscape(reopened.Task.ETag), nil, http.StatusNoContent, nil)
	e.do("GET", path, nil, http.StatusNotFound, nil)
}

func TestTaskListsAPI(t *testing.T) {
	e := newCalEnv(t)
	var l tasks.List
	e.do("POST", "/api/v1/calendar/tasklists", map[string]any{"name": "Einkauf"}, http.StatusCreated, &l)
	if l.ID != "einkauf" || l.Color != pimstore.DefaultTaskColor {
		t.Fatalf("list: %+v", l)
	}
	// A calendar of the same name gets the next id (one id: a calendar or a list).
	var c calendarView
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Einkauf"}, http.StatusCreated, &c)
	if c.ID != "einkauf-2" {
		t.Fatalf("calendar: %+v", c)
	}
	e.do("PUT", "/api/v1/calendar/tasklists/einkauf", map[string]any{"name": "Wocheneinkauf", "color": "#2563eb"}, http.StatusOK, &l)
	if l.Name != "Wocheneinkauf" || l.Color != "#2563eb" {
		t.Fatalf("renamed: %+v", l)
	}
	e.do("PUT", "/api/v1/calendar/tasklists/einkauf", map[string]any{"color": "red"}, http.StatusBadRequest, nil)
	e.do("PUT", "/api/v1/calendar/tasklists/privat", map[string]any{"name": "x"}, http.StatusNotFound, nil)
	var lists []tasks.List
	e.do("GET", "/api/v1/calendar/tasklists", nil, http.StatusOK, &lists)
	if len(lists) != 2 || lists[0].Name != "Erinnerungen" || lists[0].Virtual || lists[1].ID != "einkauf" {
		t.Fatalf("lists: %+v", lists)
	}
	var task tasks.Task
	e.do("POST", "/api/v1/calendar/tasks", map[string]any{"list": "einkauf", "title": "Brot"}, http.StatusCreated, &task)
	e.do("POST", "/api/v1/calendar/tasks", map[string]any{"list": "nichts", "title": "Brot"}, http.StatusNotFound, nil)
	e.do("POST", "/api/v1/calendar/tasks", map[string]any{"list": "einkauf"}, http.StatusBadRequest, nil)
	e.do("DELETE", "/api/v1/calendar/tasklists/einkauf", nil, http.StatusNoContent, nil)
	body := e.do("DELETE", "/api/v1/calendar/tasklists/"+pimstore.DefaultTaskList, nil, http.StatusConflict, nil)
	if errorCode(t, body) != "tasklist_last" {
		t.Fatalf("last: %s", body)
	}
	// Another account's list is not there for alex.
	if _, err := e.pim.Put(pimstore.Tasks, "bea", pimstore.DefaultTaskList, "b.ics", []byte(iPhoneReminder), pimstore.PutOptions{}); err == nil {
		body := e.do("GET", "/api/v1/calendar/tasks/"+pimstore.DefaultTaskList+"/b.ics", nil, http.StatusNotFound, nil)
		if errorCode(t, body) != "task_not_found" {
			t.Fatalf("foreign: %s", body)
		}
	}
	// Without the volume: 503, never an empty list.
	e.setVolume("")
	body = e.do("GET", "/api/v1/calendar/tasklists", nil, http.StatusServiceUnavailable, nil)
	if errorCode(t, body) != "calendar_unavailable" {
		t.Fatalf("no volume: %s", body)
	}
}
