package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// calEnv is a test server with a store of calendars in Europe/Berlin and
// the owner alex signed in.
type calEnv struct {
	t         *testing.T
	ts        *testServer
	pim       *pimstore.Store
	setVolume func(string)
	client    *http.Client
}

var calBerlin = func() *time.Location { l, _ := time.LoadLocation("Europe/Berlin"); return l }()

func newCalEnv(t *testing.T) *calEnv {
	t.Helper()
	ts, pim, _, setVolume := pimServer(t)
	pim.Local = func() *time.Location { return calBerlin }
	return &calEnv{t: t, ts: ts, pim: pim, setVolume: setVolume, client: ts.client}
}

// do sends a request as alex; v (if not nil) gets the JSON answer when the
// status is want.
func (e *calEnv) do(method, path string, body any, want int, v any) []byte {
	e.t.Helper()
	res, data := e.ts.do(e.client, method, path, body)
	if res.StatusCode != want {
		e.t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
	}
	if v != nil {
		if err := json.Unmarshal(data, v); err != nil {
			e.t.Fatalf("%s %s: %v %s", method, path, err, data)
		}
	}
	return data
}

func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(strings.TrimPrefix(s, "\n"), "\n", "\r\n"))
}

// seed stores a file as a device would.
func (e *calEnv) seed(cal, name, ics string) string {
	e.t.Helper()
	if cal != pimstore.DefaultCalendar {
		if _, err := e.pim.GetCollection(pimstore.Calendar, "alex", cal); errors.Is(err, pimstore.ErrNotFound) {
			if _, err := e.pim.CreateCollection(pimstore.Calendar, "alex", cal, pimstore.Meta{Name: cal}); err != nil {
				e.t.Fatal(err)
			}
		}
	}
	res, err := e.pim.Put(pimstore.Calendar, "alex", cal, name, crlf(ics), pimstore.PutOptions{})
	if err != nil {
		e.t.Fatalf("seed %s: %v", name, err)
	}
	return res.ETag
}

// raw is the stored file, as a CalDAV GET hands it out, unfolded.
func (e *calEnv) raw(cal, name string) string {
	e.t.Helper()
	obj, err := e.pim.Get(pimstore.Calendar, "alex", cal, name)
	if err != nil {
		e.t.Fatalf("get %s/%s: %v", cal, name, err)
	}
	return strings.ReplaceAll(string(obj.Data), "\r\n ", "")
}

func (e *calEnv) events(query string) calendarEventList {
	e.t.Helper()
	var out calendarEventList
	e.do("GET", "/api/v1/calendar/events?"+query, nil, http.StatusOK, &out)
	return out
}

func mustHave(t *testing.T, what, s string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("%s: missing %q in\n%s", what, w, s)
		}
	}
}

func mustLack(t *testing.T, what, s string, not ...string) {
	t.Helper()
	for _, w := range not {
		if strings.Contains(s, w) {
			t.Errorf("%s: unexpected %q in\n%s", what, w, s)
		}
	}
}

// calendarRoutes are the routes of T-Kalender with method.
func calendarRoutes(ts *testServer) [][2]string {
	var out [][2]string
	for _, rt := range pimDataRoutes(ts) {
		// The stream never ends by itself: TestPIMStreamRights has it.
		if strings.HasPrefix(rt[1], "/api/v1/calendar/") && rt[1] != "/api/v1/calendar/access" && rt[1] != "/api/v1/calendar/stream" {
			out = append(out, rt)
		}
	}
	return out
}

// TestCalendarRouteRights: every route of T-Kalender is for a web session
// of this node only – never in-process (MCP, agent), a token, another
// node, without a session or in publicRoutes – and the session reaches
// the handler (TECHNIK 8.3 questions 19/20).
func TestCalendarRouteRights(t *testing.T) {
	e := newCalEnv(t)
	ts := e.ts
	routes := calendarRoutes(ts)
	if len(routes) != 24 { // 12 of events, 11 of reminders, 1 of hints (1.13)
		t.Fatalf("routes: %v", routes)
	}
	for _, r := range publicRoutes {
		if strings.HasPrefix(r.Path, "/calendar") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}
	owner, _ := ts.users.Get("alex")
	token := ts.mcpToken("alex", "system:read", "system:write", "files:read", "files:write")
	for _, rt := range routes {
		path := strings.NewReplacer("{id}", "privat", "{cal}", "privat", "{obj}", "x.ics").Replace(rt[1])
		code, body := ts.server.internalAs(context.Background(), owner, rt[0], path, map[string]any{})
		if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
			t.Errorf("%s %s in-process: %d %s", rt[0], path, code, body)
		}
		req, _ := http.NewRequest(rt[0], ts.url+path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with a token: %d", rt[0], path, res.StatusCode)
		}
		creq := httptest.NewRequest(rt[0], path, strings.NewReader("{}"))
		creq = creq.WithContext(cluster.WithIdentity(creq.Context(), cluster.Identity{Node: "n2", User: "alex"}))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, creq)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_local_only") {
			t.Errorf("%s %s through the cluster: %d %s", rt[0], path, rec.Code, rec.Body)
		}
		if res, _ := ts.do(ts.newClient(), rt[0], path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", rt[0], path, res.StatusCode)
		}
		if res, body := ts.do(e.client, rt[0], path, map[string]any{}); strings.Contains(string(body), "browser_session_required") ||
			res.StatusCode == http.StatusNotFound && !strings.Contains(string(body), "_not_found") || res.StatusCode >= 500 {
			t.Errorf("%s %s from the web UI: %d %s", rt[0], path, res.StatusCode, body)
		}
	}
}

// Without the volume (or without a store) every route answers 503
// calendar_unavailable with Retry-After – never an empty list.
func TestCalendarWithoutVolume(t *testing.T) {
	e := newCalEnv(t)
	e.seed("privat", "a.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:a\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T080000Z\nEND:VEVENT\nEND:VCALENDAR\n")
	e.setVolume("")
	bare := newTestServer(t)
	bare.setup()
	for _, ts := range []*testServer{e.ts, bare} {
		for _, rt := range calendarRoutes(e.ts) {
			path := strings.NewReplacer("{id}", "privat", "{cal}", "privat", "{obj}", "a.ics").Replace(rt[1])
			res, body := ts.do(ts.client, rt[0], path, map[string]any{})
			if res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "calendar_unavailable" || res.Header.Get("Retry-After") == "" {
				t.Errorf("%s %s: %d %s", rt[0], path, res.StatusCode, body)
			}
		}
	}
}

func TestCalendarCalendars(t *testing.T) {
	e := newCalEnv(t)
	var cals []calendarView
	e.do("GET", "/api/v1/calendar/calendars", nil, http.StatusOK, &cals)
	if len(cals) != 2 || cals[0].ID != "privat" || cals[0].Kind != "own" || cals[1].ID != "geburtstage" || cals[1].Kind != "birthdays" || !cals[1].ReadOnly {
		t.Fatalf("fresh: %+v", cals)
	}
	var arbeit, again calendarView
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Arbeit", "color": "#0f766e"}, http.StatusCreated, &arbeit)
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "  Arbeit "}, http.StatusCreated, &again)
	if arbeit.ID != "arbeit" || arbeit.Color != "#0f766e" || again.ID != "arbeit-2" || again.Name != "Arbeit" || again.Color != pimstore.DefaultColor || again.Order <= arbeit.Order {
		t.Fatalf("created %+v %+v", arbeit, again)
	}
	var birthdayName calendarView
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Geburtstage"}, http.StatusCreated, &birthdayName)
	if birthdayName.ID != "geburtstage-2" {
		t.Fatalf("the id of the birthdays is taken: %+v", birthdayName)
	}
	for _, bad := range []map[string]any{{"name": ""}, {"name": "x", "color": "red"}, {"color": "#ffffff"}, {"name": "a\nb"}} {
		if res, body := e.ts.do(e.client, "POST", "/api/v1/calendar/calendars", bad); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "calendar_invalid" {
			t.Errorf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
	var renamed calendarView
	e.do("PUT", "/api/v1/calendar/calendars/arbeit-2", map[string]any{"name": "Büro", "color": "#2563eb", "order": 7}, http.StatusOK, &renamed)
	if renamed.Name != "Büro" || renamed.Color != "#2563eb" || renamed.Order != 7 {
		t.Fatalf("renamed %+v", renamed)
	}
	for _, rt := range [][2]string{{"PUT", "/api/v1/calendar/calendars/geburtstage"}, {"DELETE", "/api/v1/calendar/calendars/geburtstage"}} {
		if res, body := e.ts.do(e.client, rt[0], rt[1], map[string]any{"name": "x"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "calendar_read_only" {
			t.Errorf("%s birthdays: %d %s", rt[0], res.StatusCode, body)
		}
	}
	e.do("DELETE", "/api/v1/calendar/calendars/arbeit-2", nil, http.StatusNoContent, nil)
	e.do("DELETE", "/api/v1/calendar/calendars/geburtstage-2", nil, http.StatusNoContent, nil)
	e.do("DELETE", "/api/v1/calendar/calendars/arbeit", nil, http.StatusNoContent, nil)
	if res, body := e.ts.do(e.client, "DELETE", "/api/v1/calendar/calendars/privat", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "calendar_last" {
		t.Fatalf("last: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.do(e.client, "DELETE", "/api/v1/calendar/calendars/nope", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "calendar_not_found" {
		t.Fatalf("unknown: %d %s", res.StatusCode, body)
	}

	// Another account sees none of it: alex's calendar is no id of bea's.
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Arbeit"}, http.StatusCreated, nil)
	evID := e.seed("arbeit", "geheim.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:g\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T080000Z\nSUMMARY:Geheim\nEND:VEVENT\nEND:VCALENDAR\n")
	bea := e.ts.signIn("bea", auth.RoleUser)
	for _, rt := range [][2]string{{"GET", "/api/v1/calendar/events/arbeit/geheim.ics"}, {"PUT", "/api/v1/calendar/calendars/arbeit"},
		{"DELETE", "/api/v1/calendar/calendars/arbeit"}, {"GET", "/api/v1/calendar/calendars/arbeit/export"},
		{"GET", "/api/v1/calendar/events?from=2026-11-01&to=2026-12-01&calendars=arbeit"}, {"DELETE", "/api/v1/calendar/events/arbeit/geheim.ics?scope=all&etag=" + evID},
		{"PUT", "/api/v1/calendar/events/arbeit/geheim.ics"}} {
		body := map[string]any{"name": "x", "etag": evID, "scope": "all", "event": map[string]any{"title": "x"}}
		if res, data := e.ts.do(bea, rt[0], rt[1], body); res.StatusCode != http.StatusNotFound || strings.Contains(string(data), "Geheim") {
			t.Errorf("bea %s %s: %d %s", rt[0], rt[1], res.StatusCode, data)
		}
	}
	if !strings.Contains(e.raw("arbeit", "geheim.ics"), "SUMMARY:Geheim") {
		t.Fatal("bea changed alex's event")
	}
}

// The hidden calendars are a preference of the account (kind "calendar"):
// the list says hidden, the events leave them out unless named.
func TestCalendarHiddenPreference(t *testing.T) {
	e := newCalEnv(t)
	e.seed("arbeit", "a.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:a\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T080000Z\nSUMMARY:Arbeit\nEND:VEVENT\nEND:VCALENDAR\n")
	e.seed("privat", "p.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:p\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T090000Z\nSUMMARY:Privat\nEND:VEVENT\nEND:VCALENDAR\n")
	if _, err := e.pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, "anna.vcf", crlf("\nBEGIN:VCARD\nVERSION:3.0\nUID:anna\nFN:Anna\nBDAY:1991-11-12\nEND:VCARD\n"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.events("from=2026-11-01&to=2026-12-01").Events); n != 3 {
		t.Fatalf("all: %d", n)
	}
	e.do("PUT", "/api/v1/preferences/calendar", map[string]any{"hidden": []string{"arbeit", "geburtstage"}}, http.StatusNoContent, nil)
	var prefs map[string]json.RawMessage
	e.do("GET", "/api/v1/preferences", nil, http.StatusOK, &prefs)
	if string(prefs["calendar"]) != `{"hidden":["arbeit","geburtstage"]}` {
		t.Fatalf("round trip: %s", prefs["calendar"])
	}
	var cals []calendarView
	e.do("GET", "/api/v1/calendar/calendars", nil, http.StatusOK, &cals)
	for _, c := range cals {
		if c.Hidden != (c.ID == "arbeit" || c.ID == "geburtstage") {
			t.Errorf("%s hidden=%v", c.ID, c.Hidden)
		}
	}
	list := e.events("from=2026-11-01&to=2026-12-01")
	if len(list.Events) != 1 || list.Events[0].Title != "Privat" {
		t.Fatalf("hidden ones shown: %+v", list.Events)
	}
	if n := len(e.events("from=2026-11-01&to=2026-12-01&calendars=arbeit,geburtstage").Events); n != 2 {
		t.Fatalf("named: %d", n)
	}
	if res, _ := e.ts.request("PUT", "/api/v1/preferences/calendar", map[string]any{"hidden": []string{"../x"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad id stored: %d", res.StatusCode)
	}
	// Deleting a hidden calendar drops it from the list.
	e.do("DELETE", "/api/v1/calendar/calendars/arbeit", nil, http.StatusNoContent, nil)
	e.do("GET", "/api/v1/preferences", nil, http.StatusOK, &prefs)
	if string(prefs["calendar"]) != `{"hidden":["geburtstage"]}` {
		t.Fatalf("after delete: %s", prefs["calendar"])
	}
}

// Create, list, read, change with a conflict, delete; texts stay plain
// text.
func TestCalendarEvents(t *testing.T) {
	e := newCalEnv(t)
	title := `<img src=x onerror=alert(1)> & "Elternabend"`
	var made eventWritten
	e.do("POST", "/api/v1/calendar/events", map[string]any{"calendar": "privat", "event": map[string]any{
		"title": title, "start": "2026-11-17T19:00", "end": "2026-11-17T20:30", "timezone": "Europe/Berlin",
		"location": "Grundschule <script>", "notes": "Bastelsachen\nmitbringen", "alarms": []int{60, 30},
		"repeat": map[string]any{"freq": "monthly", "byDay": []string{"3TU"}, "count": 4},
	}}, http.StatusCreated, &made)
	if made.ID != made.UID+".ics" || made.ETag == "" {
		t.Fatalf("created %+v", made)
	}
	raw := e.raw("privat", made.ID)
	mustHave(t, "new file", raw, "PRODID:-//TitanOS//T-Kalender 1.11//DE", "BEGIN:VTIMEZONE", "TZID:Europe/Berlin", "UID:"+made.UID, "DTSTAMP:",
		"DTSTART;TZID=Europe/Berlin:20261117T190000", "DTEND;TZID=Europe/Berlin:20261117T203000", "RRULE:FREQ=MONTHLY;COUNT=4;BYDAY=3TU",
		"ACTION:DISPLAY", "TRIGGER:-PT1H", "TRIGGER:-PT30M", `LOCATION:Grundschule <script>`, `DESCRIPTION:Bastelsachen\nmitbringen`)
	list := e.events("from=2026-11-01&to=2027-03-01")
	if len(list.Events) != 4 || list.Truncated {
		t.Fatalf("list %+v", list)
	}
	first := list.Events[0]
	if first.Title != title || first.Location != "Grundschule <script>" || first.Start != "2026-11-17T19:00:00+01:00" || first.End != "2026-11-17T20:30:00+01:00" ||
		first.Timezone != "Europe/Berlin" || !first.Recurring || first.Rule == nil || first.Rule.Count != 4 || !strings.Contains(first.Rule.Text["de"], "dritten Dienstag") ||
		len(first.Alarms) != 2 || first.Alarms[0] != 30 || first.Notes != nil || first.ETag != made.ETag || first.Calendar != "privat" || first.Occurrence != "20261117T180000Z" {
		t.Fatalf("first %+v", first)
	}
	if list.Events[1].Start != "2026-12-15T19:00:00+01:00" {
		t.Fatalf("second %+v", list.Events[1])
	}
	var one calweb.View
	e.do("GET", "/api/v1/calendar/events/privat/"+url.PathEscape(made.ID)+"?occurrence=20261215T180000Z", nil, http.StatusOK, &one)
	if one.Notes == nil || *one.Notes != "Bastelsachen\nmitbringen" || one.Start != "2026-12-15T19:00:00+01:00" || one.ETag != made.ETag || one.Title != title {
		t.Fatalf("one %+v", one)
	}
	if res, body := e.ts.request("GET", "/api/v1/calendar/events/privat/"+made.ID+"?occurrence=20261216T180000Z", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "event_not_found" {
		t.Fatalf("no such occurrence: %d %s", res.StatusCode, body)
	}

	// A stale etag: 409 with the event as it is now.
	put := func(etag string, body map[string]any, want int, v any) []byte {
		body["etag"] = etag
		return e.do("PUT", "/api/v1/calendar/events/privat/"+made.ID, body, want, v)
	}
	var changed eventWritten
	put(made.ETag, map[string]any{"scope": "all", "occurrence": first.Occurrence, "event": map[string]any{"title": "Elternabend"}}, http.StatusOK, &changed)
	if changed.ETag == made.ETag || changed.Scope != "all" {
		t.Fatalf("changed %+v", changed)
	}
	var conflict struct {
		Error   apiError    `json:"error"`
		Current calweb.View `json:"current"`
	}
	put(made.ETag, map[string]any{"scope": "all", "event": map[string]any{"title": "Verloren"}}, http.StatusConflict, &conflict)
	if conflict.Error.Code != "calendar_conflict" || conflict.Current.ETag != changed.ETag || conflict.Current.Title != "Elternabend" {
		t.Fatalf("conflict %+v", conflict)
	}
	if res, body := e.ts.request("DELETE", "/api/v1/calendar/events/privat/"+made.ID+"?scope=all&etag="+made.ETag, nil); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), changed.ETag) {
		t.Fatalf("stale delete: %d %s", res.StatusCode, body)
	}
	if res, _ := e.ts.request("DELETE", "/api/v1/calendar/events/privat/"+made.ID+"?scope=all", nil); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete without etag: %d", res.StatusCode)
	}
	// The same state again writes nothing.
	var same eventWritten
	put(changed.ETag, map[string]any{"scope": "all", "event": map[string]any{"title": "Elternabend", "alarms": []int{30, 60}}}, http.StatusOK, &same)
	if same.ETag != changed.ETag {
		t.Fatalf("unchanged input wrote %+v", same)
	}
	if res, body := e.ts.request("POST", "/api/v1/calendar/events", map[string]any{"calendar": "geburtstage", "event": map[string]any{"start": "2026-11-17T19:00"}}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "calendar_read_only" {
		t.Fatalf("into the birthdays: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.request("POST", "/api/v1/calendar/events", map[string]any{"calendar": "privat", "event": map[string]any{"start": "morgen"}}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "calendar_invalid" {
		t.Fatalf("bad start: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.request("GET", "/api/v1/calendar/events?from=2026-01-01&to=2027-06-01", nil); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "calendar_window" {
		t.Fatalf("window: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.request("DELETE", "/api/v1/calendar/events/privat/"+made.ID+"?scope=this&occurrence=garbage&etag="+changed.ETag, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "event_not_found" {
		t.Fatalf("bad occurrence: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.request("DELETE", "/api/v1/calendar/events/privat/"+made.ID+"?scope=this&occurrence=20261118T180000Z&etag="+changed.ETag, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "event_not_found" {
		t.Fatalf("no such occurrence: %d %s", res.StatusCode, body)
	}
	// A title longer than the list keeps is marked; the event has it whole.
	long := strings.Repeat("Sehr langer Titel ", 30)
	var lw eventWritten
	e.do("POST", "/api/v1/calendar/events", map[string]any{"calendar": "privat", "event": map[string]any{"title": long, "start": "2026-11-20T10:00"}}, http.StatusCreated, &lw)
	for _, v := range e.events("from=2026-11-20&to=2026-11-21").Events {
		if v.ID == lw.ID && (!v.Clipped || len(v.Title) > 256) {
			t.Fatalf("long title in the list: %+v", v)
		}
	}
	var whole calweb.View
	e.do("GET", "/api/v1/calendar/events/privat/"+lw.ID, nil, http.StatusOK, &whole)
	if whole.Title != strings.TrimSpace(long) && whole.Title != long {
		t.Fatalf("whole title: %q", whole.Title)
	}
	var gone eventDeleted
	e.do("DELETE", "/api/v1/calendar/events/privat/"+made.ID+"?scope=all&etag="+changed.ETag, nil, http.StatusOK, &gone)
	if !gone.Removed {
		t.Fatalf("delete %+v", gone)
	}
	if _, err := e.pim.Stat(pimstore.Calendar, "alex", "privat", made.ID); !errors.Is(err, pimstore.ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
}

// series is one weekly series of six in the three forms of DTSTART.
type seriesKind struct {
	name, start, end string
	occ              func(day int) string // occurrence id of 2026-11-<day>
	form             func(day int) string // the value in the form of DTSTART, with its parameters
	until            func(day int) string // UNTIL for a cut before 2026-11-<day>
	moved            func(day int) string // the value after "all" moved by an hour (a day for dates)
	moveTo           map[string]any
}

var seriesKinds = []seriesKind{
	{name: "tzid", start: "DTSTART;TZID=Europe/Berlin:20261103T090000", end: "DTEND;TZID=Europe/Berlin:20261103T100000",
		occ:    func(d int) string { return fmt.Sprintf("202611%02dT080000Z", d) },
		form:   func(d int) string { return fmt.Sprintf(";TZID=Europe/Berlin:202611%02dT090000", d) },
		until:  func(d int) string { return fmt.Sprintf("UNTIL=202611%02dT075959Z", d) },
		moved:  func(d int) string { return fmt.Sprintf(";TZID=Europe/Berlin:202611%02dT100000", d) },
		moveTo: map[string]any{"start": "2026-11-03T10:00", "end": "2026-11-03T11:00", "timezone": "Europe/Berlin"}},
	{name: "utc", start: "DTSTART:20261103T080000Z", end: "DTEND:20261103T090000Z",
		occ:    func(d int) string { return fmt.Sprintf("202611%02dT080000Z", d) },
		form:   func(d int) string { return fmt.Sprintf(":202611%02dT080000Z", d) },
		until:  func(d int) string { return fmt.Sprintf("UNTIL=202611%02dT075959Z", d) },
		moved:  func(d int) string { return fmt.Sprintf(":202611%02dT090000Z", d) },
		moveTo: map[string]any{"start": "2026-11-03T09:00", "end": "2026-11-03T10:00", "timezone": "UTC"}},
	{name: "date", start: "DTSTART;VALUE=DATE:20261103", end: "DTEND;VALUE=DATE:20261104",
		occ:    func(d int) string { return fmt.Sprintf("202611%02d", d) },
		form:   func(d int) string { return fmt.Sprintf(";VALUE=DATE:202611%02d", d) },
		until:  func(d int) string { return fmt.Sprintf("UNTIL=202611%02d", d-1) },
		moved:  func(d int) string { return fmt.Sprintf(";VALUE=DATE:202611%02d", d+1) },
		moveTo: map[string]any{"start": "2026-11-04", "end": "2026-11-05", "allDay": true}},
}

// TestCalendarSeriesEditing checks every row of TECHNIK 2.4 in the bytes a
// CalDAV GET hands out, for a series with TZID, in UTC and of dates.
func TestCalendarSeriesEditing(t *testing.T) {
	for _, k := range seriesKinds {
		t.Run(k.name, func(t *testing.T) {
			e := newCalEnv(t)
			etag := e.seed("privat", "s.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//Apple Inc.//iOS 26//EN\nBEGIN:VEVENT\nUID:serie\nDTSTAMP:20261101T000000Z\n"+
				k.start+"\n"+k.end+"\nRRULE:FREQ=WEEKLY;COUNT=6\nSUMMARY:Serie\nX-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC\nEND:VEVENT\nEND:VCALENDAR\n")
			path := "/api/v1/calendar/events/privat/s.ics"
			var w eventWritten
			// Change only this one (10.11.): an override with RECURRENCE-ID in the form of DTSTART.
			e.do("PUT", path, map[string]any{"etag": etag, "scope": "this", "occurrence": k.occ(10), "event": map[string]any{"title": "Ausnahme"}}, http.StatusOK, &w)
			raw := e.raw("privat", "s.ics")
			mustHave(t, "this", raw, "RECURRENCE-ID"+k.form(10), "SUMMARY:Ausnahme", "SUMMARY:Serie", "RRULE:FREQ=WEEKLY;COUNT=6", "X-APPLE-TRAVEL-ADVISORY-BEHAVIOR")
			// Delete only this one (17.11.): EXDATE in the form of DTSTART.
			var d eventDeleted
			e.do("DELETE", path+"?scope=this&occurrence="+k.occ(17)+"&etag="+w.ETag, nil, http.StatusOK, &d)
			mustHave(t, "delete this", e.raw("privat", "s.ics"), "EXDATE"+k.form(17))
			// Deleting the override removes it and leaves an EXDATE.
			e.do("DELETE", path+"?scope=this&occurrence="+k.occ(10)+"&etag="+d.ETag, nil, http.StatusOK, &d)
			raw = e.raw("privat", "s.ics")
			mustHave(t, "delete override", raw, "EXDATE"+k.form(10))
			mustLack(t, "delete override", raw, "Ausnahme", "RECURRENCE-ID")
			// This and following (24.11.): the old series ends with UNTIL, a new file with a new UID and the rest of COUNT.
			e.do("PUT", path, map[string]any{"etag": d.ETag, "scope": "following", "occurrence": k.occ(24), "event": map[string]any{"title": "Neu"}}, http.StatusOK, &w)
			raw = e.raw("privat", "s.ics")
			mustHave(t, "following old", raw, k.until(24), "SUMMARY:Serie")
			mustLack(t, "following old", raw, "COUNT=")
			if w.NewID == "" || w.Scope != "following" {
				t.Fatalf("following %+v", w)
			}
			newName := w.NewID
			fresh := e.raw("privat", newName)
			mustHave(t, "following new", fresh, "DTSTART"+k.form(24), "RRULE:FREQ=WEEKLY;COUNT=3", "SUMMARY:Neu", "X-APPLE-TRAVEL-ADVISORY-BEHAVIOR")
			mustLack(t, "following new", fresh, "UID:serie\r")
			// The whole (old) series an hour later (dates: a day): EXDATE and
			// RECURRENCE-ID move along.
			e.do("PUT", path, map[string]any{"etag": w.ETag, "scope": "this", "occurrence": k.occ(3), "event": map[string]any{"title": "Erster"}}, http.StatusOK, &w)
			e.do("PUT", path, map[string]any{"etag": w.ETag, "scope": "all", "occurrence": k.occ(3), "event": k.moveTo}, http.StatusOK, &w)
			raw = e.raw("privat", "s.ics")
			mustHave(t, "all", raw, "DTSTART"+k.moved(3), "EXDATE"+k.moved(10), "EXDATE"+k.moved(17), "RECURRENCE-ID"+k.moved(3), "SUMMARY:Erster")
			if k.name == "date" {
				mustHave(t, "all", raw, k.until(24))
			} else {
				mustHave(t, "all", raw, strings.Replace(k.until(24), "T075959Z", "T085959Z", 1))
			}
			// The new series: delete this and following from 8.12. – dates
			// beyond November need their own form.
			newPath := "/api/v1/calendar/events/privat/" + newName
			obj, _ := e.pim.Stat(pimstore.Calendar, "alex", "privat", newName)
			occ8 := strings.Replace(k.occ(8), "202611", "202612", 1)
			e.do("DELETE", newPath+"?scope=following&occurrence="+occ8+"&etag="+obj.ETag, nil, http.StatusOK, &d)
			want := strings.Replace(k.until(8), "202611", "202612", 1)
			if k.name == "date" {
				want = "UNTIL=20261207"
			}
			mustHave(t, "delete following", e.raw("privat", newName), want)
			// The whole series: the file goes.
			e.do("DELETE", newPath+"?scope=all&etag="+d.ETag, nil, http.StatusOK, &d)
			if _, err := e.pim.Stat(pimstore.Calendar, "alex", "privat", newName); !d.Removed || !errors.Is(err, pimstore.ErrNotFound) {
				t.Fatalf("delete all: %+v %v", d, err)
			}
		})
	}
}

// The rows of 2.4 about exceptions after the cut, in the raw bytes: an
// override (1.12.) and an EXDATE (8.12.) after the cut on 24.11.
func TestCalendarSeriesExceptionsAfterTheCut(t *testing.T) {
	dec := func(s string) string { return strings.Replace(s, "202611", "202612", 1) }
	for _, k := range seriesKinds {
		t.Run(k.name, func(t *testing.T) {
			e := newCalEnv(t)
			seed := func() string {
				override := "BEGIN:VEVENT\nUID:serie\nDTSTAMP:20261101T000000Z\nRECURRENCE-ID" + dec(k.form(1)) + "\n" +
					strings.Replace(k.start, "20261103", "20261201", 1) + "\n" + strings.Replace(k.end, "20261104", "20261202", 1) + "\nSUMMARY:Ausnahme Dezember\nEND:VEVENT\n"
				if k.name != "date" {
					override = strings.Replace(override, strings.Replace(k.end, "20261104", "20261202", 1), strings.Replace(k.end, "20261103", "20261201", 1), 1)
				}
				_ = e.pim.DeleteObject(pimstore.Calendar, "alex", "privat", "x.ics", "")
				return e.seed("privat", "x.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:serie\nDTSTAMP:20261101T000000Z\n"+
					k.start+"\n"+k.end+"\nRRULE:FREQ=WEEKLY;COUNT=8\nEXDATE"+dec(k.form(8))+"\nSUMMARY:Serie\nEND:VEVENT\n"+override+"END:VCALENDAR\n")
			}
			path := "/api/v1/calendar/events/privat/x.ics"
			// Following with the title only: override and EXDATE go along
			// into the new file with the new UID, the old file loses them.
			var w eventWritten
			e.do("PUT", path, map[string]any{"etag": seed(), "scope": "following", "occurrence": k.occ(24), "event": map[string]any{"title": "Neu"}}, http.StatusOK, &w)
			old, fresh := e.raw("privat", "x.ics"), e.raw("privat", w.NewID)
			mustLack(t, "old", old, "RECURRENCE-ID", "EXDATE", "Ausnahme Dezember")
			mustHave(t, "old", old, k.until(24))
			mustHave(t, "new", fresh, "RECURRENCE-ID"+dec(k.form(1)), "EXDATE"+dec(k.form(8)), "Ausnahme Dezember", "RRULE:FREQ=WEEKLY;COUNT=5")
			if w.DroppedExceptions != 0 || strings.Count(fresh, "UID:serie\r") != 0 {
				t.Fatalf("carried %+v\n%s", w, fresh)
			}
			// Following with a new time: they no longer fit and are dropped (counted).
			moveTo := map[string]any{"start": "2026-11-24T10:00", "end": "2026-11-24T11:00", "timezone": "Europe/Berlin"}
			switch k.name {
			case "utc":
				moveTo = map[string]any{"start": "2026-11-24T09:00", "end": "2026-11-24T10:00", "timezone": "UTC"}
			case "date":
				moveTo = map[string]any{"start": "2026-11-25", "end": "2026-11-26", "allDay": true}
			}
			e.do("PUT", path, map[string]any{"etag": seed(), "scope": "following", "occurrence": k.occ(24), "event": moveTo}, http.StatusOK, &w)
			fresh = e.raw("privat", w.NewID)
			if w.DroppedExceptions != 2 {
				t.Fatalf("dropped %+v", w)
			}
			mustLack(t, "new moved", fresh, "RECURRENCE-ID", "EXDATE", "Ausnahme Dezember")
			// Delete following: override and EXDATE after the cut go.
			var d eventDeleted
			e.do("DELETE", path+"?scope=following&occurrence="+k.occ(24)+"&etag="+seed(), nil, http.StatusOK, &d)
			old = e.raw("privat", "x.ics")
			mustLack(t, "delete following", old, "RECURRENCE-ID", "EXDATE", "Ausnahme Dezember", "COUNT=")
			mustHave(t, "delete following", old, k.until(24))
		})
	}
}

// Moving into another calendar: a new file there, the old one gone, both
// in the change logs the devices sync with.
func TestCalendarMoveEvent(t *testing.T) {
	e := newCalEnv(t)
	etag := e.seed("privat", "m.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:m\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T080000Z\nSUMMARY:Umzug\nEND:VEVENT\nEND:VCALENDAR\n")
	e.seed("arbeit", "other.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:o\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T080000Z\nRRULE:FREQ=DAILY;COUNT=3\nEND:VEVENT\nEND:VCALENDAR\n")
	before := map[string]string{}
	for _, cal := range []string{"privat", "arbeit"} {
		cs, _ := e.pim.Changes(pimstore.Calendar, "alex", cal, "")
		before[cal] = cs.Token
	}
	var w eventWritten
	e.do("PUT", "/api/v1/calendar/events/privat/m.ics", map[string]any{"etag": etag, "scope": "all", "calendar": "arbeit", "event": map[string]any{"title": "Umgezogen"}}, http.StatusOK, &w)
	if w.Calendar != "arbeit" || w.ID != "m.ics" {
		t.Fatalf("moved %+v", w)
	}
	mustHave(t, "target", e.raw("arbeit", "m.ics"), "SUMMARY:Umgezogen", "UID:m")
	if _, err := e.pim.Stat(pimstore.Calendar, "alex", "privat", "m.ics"); !errors.Is(err, pimstore.ErrNotFound) {
		t.Fatalf("source kept: %v", err)
	}
	for cal, deleted := range map[string]bool{"privat": true, "arbeit": false} {
		cs, err := e.pim.Changes(pimstore.Calendar, "alex", cal, before[cal])
		if err != nil || len(cs.Changes) != 1 || cs.Changes[0].Name != "m.ics" || cs.Changes[0].Deleted != deleted {
			t.Fatalf("log of %s: %+v %v", cal, cs, err)
		}
	}
	// One occurrence of a series does not move alone.
	obj, _ := e.pim.Stat(pimstore.Calendar, "alex", "arbeit", "other.ics")
	if res, body := e.ts.request("PUT", "/api/v1/calendar/events/arbeit/other.ics", map[string]any{"etag": obj.ETag, "scope": "this", "occurrence": "20261111T080000Z", "calendar": "privat", "event": map[string]any{}}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "calendar_move_series" {
		t.Fatalf("move one occurrence: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.request("PUT", "/api/v1/calendar/events/arbeit/other.ics", map[string]any{"etag": obj.ETag, "scope": "all", "calendar": "geburtstage", "event": map[string]any{}}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("move into the birthdays: %d %s", res.StatusCode, body)
	}
}

// multipartICS is the body of an import.
func multipartICS(t *testing.T, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "kalender.ics")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	return &body, mw.FormDataContentType()
}

func (e *calEnv) importICS(cal, onDuplicate string, data []byte, want int) importResult {
	e.t.Helper()
	body, ctype := multipartICS(e.t, data)
	req, _ := http.NewRequest("POST", e.ts.url+"/api/v1/calendar/calendars/"+cal+"/import?onDuplicate="+onDuplicate, body)
	req.Header.Set("Content-Type", ctype)
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out importResult
	raw := new(bytes.Buffer)
	_, _ = raw.ReadFrom(res.Body)
	if res.StatusCode != want {
		e.t.Fatalf("import: %d %s", res.StatusCode, raw)
	}
	_ = json.Unmarshal(raw.Bytes(), &out)
	return out
}

// A single event of a series whose rule ends says how many occurrences
// it has ("Alle 12 Termine").
func TestCalendarEventOccurrences(t *testing.T) {
	e := newCalEnv(t)
	e.seed("privat", "r.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:r\nDTSTAMP:20261101T000000Z\n"+
		"DTSTART;TZID=Europe/Berlin:20261103T090000\nDURATION:PT30M\nRRULE:FREQ=WEEKLY;COUNT=12\nSUMMARY:Reihe\nEND:VEVENT\nEND:VCALENDAR\n")
	e.seed("privat", "o.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:o\nDTSTAMP:20261101T000000Z\n"+
		"DTSTART;TZID=Europe/Berlin:20261103T090000\nDURATION:PT30M\nRRULE:FREQ=WEEKLY\nSUMMARY:Offen\nEND:VEVENT\nEND:VCALENDAR\n")
	var v calweb.View
	e.do("GET", "/api/v1/calendar/events/privat/r.ics?occurrence=20261110T080000Z", nil, http.StatusOK, &v)
	if v.Occurrences == nil || *v.Occurrences != 12 {
		t.Fatalf("occurrences %v", v.Occurrences)
	}
	body := e.do("GET", "/api/v1/calendar/events/privat/o.ics", nil, http.StatusOK, nil)
	if strings.Contains(string(body), "occurrences") {
		t.Fatalf("no end: %s", body)
	}
}
