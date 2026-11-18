package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// getTagged sends a GET with If-None-Match (when tag is set) as client.
func getTagged(t *testing.T, ts *testServer, client *http.Client, path, tag string) (*http.Response, []byte) {
	t.Helper()
	var header map[string]string
	if tag != "" {
		header = map[string]string{"If-None-Match": tag}
	}
	req, _ := http.NewRequest("GET", ts.url+path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

// The list of contacts: from the cache under the book's ctag, so a change
// over CardDAV (here the store) shows at once; its ETag answers 304
// without a body and changes with the book, q and the account (1.13).
func TestContactsListCacheAndETag(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	put := func(account, name, fn string) {
		data := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + name + "\r\nFN:" + fn + "\r\nN:" + fn + ";;;;\r\nBDAY:1990-01-02\r\nEND:VCARD\r\n")
		if _, err := pim.Put(pimstore.Contacts, account, pimstore.DefaultBook, name+".vcf", data, pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	put("alex", "a", "Anna")
	res, body := getTagged(t, ts, ts.client, "/api/v1/contacts", "")
	tag := res.Header.Get("ETag")
	var list []cardweb.Item
	if res.StatusCode != http.StatusOK || tag == "" || res.Header.Get("Cache-Control") != "no-store" || json.Unmarshal(body, &list) != nil || len(list) != 1 {
		t.Fatalf("list: %d %s %s", res.StatusCode, tag, body)
	}
	if res, body := getTagged(t, ts, ts.client, "/api/v1/contacts", tag); res.StatusCode != http.StatusNotModified || len(body) != 0 || res.Header.Get("ETag") != tag {
		t.Fatalf("unchanged: %d %q", res.StatusCode, body)
	}
	if res, _ := getTagged(t, ts, ts.client, "/api/v1/contacts?q=ann", tag); res.StatusCode != http.StatusOK || res.Header.Get("ETag") == tag {
		t.Fatalf("another q: %d", res.StatusCode)
	}
	// A card from the iPhone: the next list has it, the old tag is stale.
	put("alex", "b", "Bert")
	res, body = getTagged(t, ts, ts.client, "/api/v1/contacts", tag)
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || len(list) != 2 || res.Header.Get("ETag") == tag {
		t.Fatalf("after a change: %d %s", res.StatusCode, body)
	}
	if res, body := getTagged(t, ts, ts.client, "/api/v1/contacts/books", ""); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"count":2`) {
		t.Fatalf("books: %s", body)
	}
	// bea's list is hers, also with alex's tag.
	bea := ts.signIn("bea", auth.RoleUser)
	if res, body := getTagged(t, ts, bea, "/api/v1/contacts", res.Header.Get("ETag")); res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("bea: %d %s", res.StatusCode, body)
	}
}

// The events: their ETag follows the calendars and the address book
// (birthdays); full=1 gives clipped titles in whole (1.13).
func TestEventsETagAndFull(t *testing.T) {
	e := newCalEnv(t)
	long := strings.Repeat("Sehr langer Titel ", 20) + "Ende"
	e.seed("privat", "long.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:long\nDTSTAMP:20261101T000000Z\nDTSTART:20261110T080000Z\nDURATION:PT1H\nSUMMARY:"+long+"\nEND:VEVENT\nEND:VCALENDAR\n")
	e.seed("privat", "daily.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:daily\nDTSTAMP:20261101T000000Z\nDTSTART:20261109T080000Z\nDURATION:PT1H\nRRULE:FREQ=DAILY;COUNT=3\nSUMMARY:"+long+" täglich\nEND:VEVENT\nEND:VCALENDAR\n")
	const month = "/api/v1/calendar/events?from=2026-11-01&to=2026-12-01"
	var list calendarEventList
	res, body := getTagged(t, e.ts, e.client, month, "")
	if json.Unmarshal(body, &list) != nil {
		t.Fatalf("%s", body)
	}
	clipped := 0
	for _, v := range list.Events {
		if v.Clipped {
			clipped++
		}
	}
	if clipped != 4 {
		t.Fatalf("clipped without full: %d of %+v", clipped, list.Events)
	}
	tag := res.Header.Get("ETag")
	if res, body := getTagged(t, e.ts, e.client, month, tag); res.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Fatalf("unchanged: %d", res.StatusCode)
	}
	res, body = getTagged(t, e.ts, e.client, month+"&full=1", tag)
	list = calendarEventList{}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || len(list.Events) != 4 {
		t.Fatalf("full: %d %s", res.StatusCode, body)
	}
	for _, v := range list.Events {
		if v.Clipped || (v.Title != long && v.Title != long+" täglich") {
			t.Fatalf("full=1 left %q clipped=%v", v.Title, v.Clipped)
		}
	}
	// A birthday in the contacts changes the tag of a list with birthdays,
	// not of one without.
	only := month + "&calendars=privat"
	resOnly, _ := getTagged(t, e.ts, e.client, only, "")
	if _, err := e.pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, "c.vcf",
		[]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c\r\nFN:Carla\r\nBDAY:1990-11-20\r\nEND:VCARD\r\n"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if res, body := getTagged(t, e.ts, e.client, month, tag); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"title":"Carla"`) {
		t.Fatalf("birthday added: %d %s", res.StatusCode, body)
	}
	if res, _ := getTagged(t, e.ts, e.client, only, resOnly.Header.Get("ETag")); res.StatusCode != http.StatusNotModified {
		t.Fatalf("calendar without birthdays: %d", res.StatusCode)
	}
	// A change of the calendar: a new tag.
	e.seed("privat", "x.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:x\nDTSTAMP:20261101T000000Z\nDTSTART:20261111T080000Z\nDURATION:PT1H\nSUMMARY:X\nEND:VEVENT\nEND:VCALENDAR\n")
	if res, _ := getTagged(t, e.ts, e.client, only, resOnly.Header.Get("ETag")); res.StatusCode != http.StatusOK {
		t.Fatalf("calendar changed: %d", res.StatusCode)
	}
}

// Long lists go out compressed when the browser takes gzip (1.13).
func TestPIMListsGzip(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	var items []pimstore.Item
	for i := range 400 {
		uid := "k" + strings.Repeat("x", 3) + string(rune('a'+i%26)) + strings.Repeat("0", i%7) + strconv.Itoa(i)
		items = append(items, pimstore.Item{Name: uid + ".vcf", Data: pimCard(uid)})
	}
	if _, err := pim.Import(t.Context(), pimstore.Contacts, "alex", pimstore.DefaultBook, items); err != nil {
		t.Fatal(err)
	}
	get := func(enc string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", ts.url+"/api/v1/contacts", nil)
		if enc != "" {
			req.Header.Set("Accept-Encoding", enc) // set by hand: the client leaves it compressed
		}
		res, err := ts.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res, data
	}
	res, zipped := get("gzip, deflate, br")
	if res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("Cache-Control") != "no-store" || !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("headers: %v", res.Header)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	var list []cardweb.Item
	if json.Unmarshal(plain, &list) != nil || len(list) != 400 {
		t.Fatalf("decoded: %d bytes", len(plain))
	}
	if len(zipped)*3 > len(plain) {
		t.Fatalf("compressed %d of %d", len(zipped), len(plain))
	}
	for _, enc := range []string{"identity", "gzip;q=0"} {
		if res, body := get(enc); res.Header.Get("Content-Encoding") != "" || json.Unmarshal(body, &list) != nil {
			t.Fatalf("%s: %v", enc, res.Header)
		}
	}
}
