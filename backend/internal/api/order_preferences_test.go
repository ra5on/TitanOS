package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// 1.13: the order of the side bars of Mail, Kontakte and Kalender is kept
// per account (kind "order"); a PUT replaces only the keys it sends.
func TestOrderPreferencesMergePerKey(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	put := func(value any) (int, string) {
		res, body := ts.request("PUT", "/api/v1/preferences/order", value)
		return res.StatusCode, string(body)
	}
	stored := func() map[string]json.RawMessage {
		t.Helper()
		res, body := ts.request("GET", "/api/v1/preferences", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("get: %d %s", res.StatusCode, body)
		}
		var all map[string]json.RawMessage
		if err := json.Unmarshal(body, &all); err != nil {
			t.Fatal(err)
		}
		var order map[string]json.RawMessage
		if string(all["order"]) != "null" {
			if err := json.Unmarshal(all["order"], &order); err != nil {
				t.Fatalf("order: %s %v", all["order"], err)
			}
		}
		return order
	}

	if got := stored(); got != nil {
		t.Fatalf("nothing stored yet, got %v", got)
	}
	mail := map[string]any{"mail": map[string][]string{
		"acc-privat": {"INBOX", "Rechnungen", "Rechnungen/2026", "Entw&APw-rfe", "Ärger"},
		"acc-arbeit": {},
	}}
	if code, body := put(mail); code != http.StatusNoContent {
		t.Fatalf("mail: %d %s", code, body)
	}
	if code, body := put(map[string]any{"calendar": []string{"arbeit", "geburtstage"}}); code != http.StatusNoContent {
		t.Fatalf("calendar: %d %s", code, body)
	}
	got := stored()
	if !strings.Contains(string(got["mail"]), `"Rechnungen/2026"`) || string(got["calendar"]) != `["arbeit","geburtstage"]` {
		t.Fatalf("both kept: %v", got)
	}
	// The contacts' order does not touch the others; null drops one.
	if code, body := put(map[string]any{"contacts": []string{"kontakte"}, "calendar": nil}); code != http.StatusNoContent {
		t.Fatalf("contacts: %d %s", code, body)
	}
	got = stored()
	if _, ok := got["calendar"]; ok || string(got["contacts"]) != `["kontakte"]` || got["mail"] == nil {
		t.Fatalf("merged: %v", got)
	}
	// Writing the same again leaves the stick alone (the cache compares).
	if code, _ := put(map[string]any{"contacts": []string{"kontakte"}}); code != http.StatusNoContent {
		t.Fatalf("again: %d", code)
	}
}

func TestOrderPreferencesBounds(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	many := func(n int, name func(int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = name(i)
		}
		return out
	}
	folder := func(i int) string {
		return "F" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + "-" + strconv.Itoa(i)
	}
	boxes := map[string][]string{}
	for i := 0; i < 21; i++ {
		boxes["acc-"+strconv.Itoa(i)] = []string{"INBOX"}
	}
	for name, value := range map[string]any{
		"a list instead of an object": []string{"INBOX"},
		"an unknown key":              map[string]any{"notes": []string{"a"}},
		"a mailbox id with a slash":   map[string]any{"mail": map[string][]string{"../x": {"INBOX"}}},
		"a mailbox id in capitals":    map[string]any{"mail": map[string][]string{"Privat": {"INBOX"}}},
		"21 mailboxes":                map[string]any{"mail": boxes},
		"501 folders":                 map[string]any{"mail": map[string][]string{"a": many(501, folder)}},
		"a folder twice":              map[string]any{"mail": map[string][]string{"a": {"INBOX", "INBOX"}}},
		"an empty folder":             map[string]any{"mail": map[string][]string{"a": {""}}},
		"a folder with a newline":     map[string]any{"mail": map[string][]string{"a": {"A\nB"}}},
		"a folder of 1025 bytes":      map[string]any{"mail": map[string][]string{"a": {strings.Repeat("a", 1025)}}},
		"folders not a list":          map[string]any{"mail": map[string]any{"a": "INBOX"}},
		"mail a list":                 map[string]any{"mail": []string{"INBOX"}},
		"a calendar id with a dot":    map[string]any{"calendar": []string{"../x"}},
		"101 calendars":               map[string]any{"calendar": many(101, func(i int) string { return "k" + strconv.Itoa(i) })},
		"a contacts book twice":       map[string]any{"contacts": []string{"kontakte", "kontakte"}},
		"contacts an object":          map[string]any{"contacts": map[string]any{"a": 1}},
		"a mailbox twice in order":    map[string]any{"mailAccounts": []string{"a", "a"}},
		"21 mailboxes in order":       map[string]any{"mailAccounts": many(21, func(i int) string { return "acc-" + strconv.Itoa(i) })},
		"mailbox order an object":     map[string]any{"mailAccounts": map[string]any{"a": 1}},
		"a hidden folder of a bad id": map[string]any{"mailHidden": map[string][]string{"../x": {"Spam"}}},
		"a hidden folder empty":       map[string]any{"mailHidden": map[string][]string{"a": {""}}},
	} {
		if res, body := ts.request("PUT", "/api/v1/preferences/order", value); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
	// The bounds themselves pass.
	boxes = map[string][]string{}
	for i := 0; i < 20; i++ {
		boxes["acc-"+strconv.Itoa(i)] = []string{"INBOX"}
	}
	boxes["acc-0"] = many(500, func(i int) string { return "f" + strconv.Itoa(i) })
	for name, value := range map[string]any{
		"20 mailboxes, 500 folders": map[string]any{"mail": boxes},
		"100 calendars":             map[string]any{"calendar": many(100, func(i int) string { return "k" + strconv.Itoa(i) })},
		"a folder of 1024 bytes":    map[string]any{"mail": map[string][]string{"a": {strings.Repeat("ä", 512)}}},
		"nothing":                   map[string]any{},
		"mailbox order and hidden":  map[string]any{"mailAccounts": []string{"acc-2", "acc-0"}, "mailHidden": map[string][]string{"acc-0": {"Spam", "Archiv"}}},
	} {
		if res, body := ts.request("PUT", "/api/v1/preferences/order", value); res.StatusCode != http.StatusNoContent {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
}

// A damaged or hand-edited file: unknown keys go with the next change, a
// file that is no object counts as empty.
func TestOrderPreferencesMergeKeepsOnlyKnownKeys(t *testing.T) {
	merged, err := mergeOrderPrefs(json.RawMessage(`{"mail":{"a":["INBOX"]},"old":1}`), json.RawMessage(`{"contacts":["k"]}`))
	if err != nil || string(merged) != `{"contacts":["k"],"mail":{"a":["INBOX"]}}` {
		t.Fatalf("merged: %s %v", merged, err)
	}
	merged, err = mergeOrderPrefs(json.RawMessage(`[1,2]`), json.RawMessage(`{"calendar":["k"]}`))
	if err != nil || string(merged) != `{"calendar":["k"]}` {
		t.Fatalf("damaged: %s %v", merged, err)
	}
}
