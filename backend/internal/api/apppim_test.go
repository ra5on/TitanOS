package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// appSync sends a request of the app with headers.
func appSync(t *testing.T, ts *testServer, phone *http.Client, method, path string, body []byte, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, ts.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := phone.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

const appSyncEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//iPhone//EN\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nEND:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\nUID:serie-1\r\nDTSTAMP:20261101T000000Z\r\nDTSTART;TZID=Europe/Berlin:20261103T090000\r\nDTEND;TZID=Europe/Berlin:20261103T100000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=TU;COUNT=10\r\nEXDATE;TZID=Europe/Berlin:20261110T090000\r\nSUMMARY:Woche\r\nX-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-P1D\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:serie-1\r\nDTSTAMP:20261101T000000Z\r\nRECURRENCE-ID;TZID=Europe/Berlin:20261117T090000\r\nDTSTART;TZID=Europe/Berlin:20261117T110000\r\n" +
	"DTEND;TZID=Europe/Berlin:20261117T120000\r\nSUMMARY:Woche (später)\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestAppPIMSync(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel 9 von Alex")
	phone, _, _ := ts.appSession(app, user, device)

	// The browser and anonymous requests never reach the sync.
	if res, body := ts.request("GET", "/api/v1/app/pim/calendar", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "app_session_required" {
		t.Fatalf("browser: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(ts.newClient(), "GET", "/api/v1/app/pim/calendar", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	if res, body := appSync(t, ts, phone, "GET", "/api/v1/app/pim/notes", nil, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("kind: %d %s", res.StatusCode, body)
	}

	// Collections: the virtual default before the first entry.
	var cols struct{ Collections []appPIMCollection }
	res, body := appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar", nil, nil)
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &cols) != nil || len(cols.Collections) != 1 {
		t.Fatalf("collections: %d %s", res.StatusCode, body)
	}
	cal := cols.Collections[0].ID

	// Create only with If-None-Match: *, the bytes stay as they came.
	path := "/api/v1/app/pim/calendar/" + cal + "/objects/serie-1.ics"
	ics := map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"}
	res, body = appSync(t, ts, phone, "PUT", path, []byte(appSyncEvent), ics)
	var put appPIMWritten
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &put) != nil || !put.Created || put.ETag == "" || res.Header.Get("ETag") != `"`+put.ETag+`"` {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "PUT", path, []byte(appSyncEvent), ics); res.StatusCode != http.StatusPreconditionFailed || errorCode(t, body) != "pim_conflict" {
		t.Fatalf("create twice: %d %s", res.StatusCode, body)
	}
	obj, err := pim.Get(pimstore.Calendar, "alex", cal, "serie-1.ics")
	if err != nil || string(obj.Data) != appSyncEvent {
		t.Fatalf("stored: %v %q", err, obj.Data)
	}

	// Changes without token: every entry; with the token: nothing more.
	var ch appPIMChanges
	res, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar/"+cal+"/changes", nil, nil)
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &ch) != nil || len(ch.Changes) != 1 || ch.Changes[0].Name != "serie-1.ics" || ch.Changes[0].ETag != put.ETag || ch.Token == "" {
		t.Fatalf("changes: %d %s", res.StatusCode, body)
	}
	token := ch.Token
	res, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar/"+cal+"/changes?token="+token, nil, nil)
	if json.Unmarshal(body, &ch) != nil || len(ch.Changes) != 0 {
		t.Fatalf("changes since: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar/"+cal+"/changes?token=nonsense", nil, nil); res.StatusCode != http.StatusGone || errorCode(t, body) != "pim_sync_token" {
		t.Fatalf("bad token: %d %s", res.StatusCode, body)
	}

	// Multiget gives RRULE, EXDATE, the exception and both alarms unchanged.
	var objs appPIMObjects
	res, body = appSync(t, ts, phone, "POST", "/api/v1/app/pim/calendar/"+cal+"/objects", []byte(`{"names":["serie-1.ics","gone.ics"]}`), nil)
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &objs) != nil || len(objs.Objects) != 1 || objs.Objects[0].Data != appSyncEvent ||
		objs.Objects[0].ETag != put.ETag || len(objs.Missing) != 1 || objs.Missing[0] != "gone.ics" {
		t.Fatalf("objects: %d %s", res.StatusCode, body)
	}

	// Replace: only with the current ETag; a stale one is 412.
	changed := strings.Replace(appSyncEvent, "SUMMARY:Woche\r\n", "SUMMARY:Woche neu\r\n", 1)
	if res, body = appSync(t, ts, phone, "PUT", path, []byte(changed), map[string]string{"Content-Type": "text/calendar", "If-Match": `"stale"`}); res.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "PUT", path, []byte(changed), map[string]string{"Content-Type": "text/calendar", "If-Match": "W/\"" + put.ETag + "\""}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("weak: %d %s", res.StatusCode, body)
	}
	res, body = appSync(t, ts, phone, "PUT", path, []byte(changed), map[string]string{"Content-Type": "text/calendar", "If-Match": `"` + put.ETag + `"`})
	var again appPIMWritten
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &again) != nil || again.Created || again.ETag == put.ETag {
		t.Fatalf("replace: %d %s", res.StatusCode, body)
	}
	res, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar/"+cal+"/changes?token="+token, nil, nil)
	if json.Unmarshal(body, &ch) != nil || len(ch.Changes) != 1 || ch.Changes[0].ETag != again.ETag {
		t.Fatalf("changes after replace: %s", body)
	}
	token = ch.Token

	// Wrong data and wrong type are refused, nothing stored.
	if res, body = appSync(t, ts, phone, "PUT", "/api/v1/app/pim/calendar/"+cal+"/objects/x.ics", []byte("BEGIN:VCARD\r\nEND:VCARD\r\n"), map[string]string{"Content-Type": "text/calendar"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "PUT", "/api/v1/app/pim/calendar/"+cal+"/objects/x.ics", []byte(appSyncEvent), map[string]string{"Content-Type": "text/vcard"}); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("type: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "PUT", "/api/v1/app/pim/calendar/nope/objects/x.ics", []byte(appSyncEvent), map[string]string{"Content-Type": "text/calendar"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("no collection: %d %s", res.StatusCode, body)
	}

	// Delete: a stale ETag keeps the entry, the current one removes it.
	if res, body = appSync(t, ts, phone, "DELETE", path, nil, map[string]string{"If-Match": `"` + put.ETag + `"`}); res.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("delete stale: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "DELETE", path, nil, map[string]string{"If-Match": `"` + again.ETag + `"`}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	res, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar/"+cal+"/changes?token="+token, nil, nil)
	if json.Unmarshal(body, &ch) != nil || len(ch.Changes) != 1 || !ch.Changes[0].Deleted {
		t.Fatalf("changes after delete: %s", body)
	}

	// Contacts: a card from the phone, then the same one again is a UID conflict under another name.
	var books struct{ Collections []appPIMCollection }
	_, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/contacts", nil, nil)
	if json.Unmarshal(body, &books) != nil || len(books.Collections) != 1 {
		t.Fatalf("books: %s", body)
	}
	book := books.Collections[0].ID
	vcf := map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-None-Match": "*"}
	if res, body = appSync(t, ts, phone, "PUT", "/api/v1/app/pim/contacts/"+book+"/objects/sam.vcf", pimCard("sam"), vcf); res.StatusCode != http.StatusCreated {
		t.Fatalf("card: %d %s", res.StatusCode, body)
	}
	if res, body = appSync(t, ts, phone, "PUT", "/api/v1/app/pim/contacts/"+book+"/objects/sam2.vcf", pimCard("sam"), vcf); res.StatusCode != http.StatusConflict || errorCode(t, body) != "pim_uid_conflict" || res.Header.Get("X-Conflict-Name") != "sam.vcf" {
		t.Fatalf("uid conflict: %d %s", res.StatusCode, body)
	}
	if _, err := pim.Get(pimstore.Contacts, "alex", book, "sam.vcf"); err != nil {
		t.Fatalf("card stored: %v", err)
	}

	// Reminders are a kind of their own.
	if res, body = appSync(t, ts, phone, "GET", "/api/v1/app/pim/tasks", nil, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("tasks: %d %s", res.StatusCode, body)
	}
}

// TestAppPIMOtherAccount: the app sees the collections of its own account only.
func TestAppPIMOtherAccount(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	if _, err := ts.users.Create("bob", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pim.CreateCollection(pimstore.Calendar, "bob", "bobs-kalender", pimstore.Meta{Name: "Bob"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pim.Put(pimstore.Calendar, "bob", "bobs-kalender", "b.ics", pimEvent("b"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel")
	phone, _, _ := ts.appSession(app, user, device)
	_, body := appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar", nil, nil)
	if strings.Contains(string(body), "bobs-kalender") {
		t.Fatalf("other account listed: %s", body)
	}
	if res, body := appSync(t, ts, phone, "POST", "/api/v1/app/pim/calendar/bobs-kalender/objects", []byte(`{"names":["b.ics"]}`), nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("other account read: %d %s", res.StatusCode, body)
	}
	if res, body := appSync(t, ts, phone, "GET", "/api/v1/app/pim/calendar/bobs-kalender/changes", nil, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("other account changes: %d %s", res.StatusCode, body)
	}
}
