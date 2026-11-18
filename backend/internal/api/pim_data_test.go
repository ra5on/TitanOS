package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// pimTestStore is a store of calendars on a temporary folder; setVolume("")
// takes the volume away (Current = "").
func pimTestStore(t *testing.T) (*pimstore.Store, string, func(string)) {
	t.Helper()
	dir := t.TempDir()
	volume := "disk1"
	pim := pimstore.New()
	pim.Current = func(string) string { return volume }
	pim.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		if volume == "" {
			return nil, nil, pimstore.ErrUnavailable
		}
		path := filepath.Join(dir, volume, ".titanos", feature)
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(path)
		return root, func() {}, err
	}
	return pim, dir, func(v string) { volume = v }
}

// pimDataRoutes are the routes the pages of T-Kalender and Kontakte read.
func pimDataRoutes(ts *testServer) [][2]string {
	var out [][2]string
	for _, pattern := range ts.server.patterns {
		method, path, ok := strings.Cut(pattern, " ")
		if ok && (strings.HasPrefix(path, "/api/v1/calendar/") || strings.HasPrefix(path, "/api/v1/contacts")) {
			out = append(out, [2]string{method, path})
		}
	}
	return out
}

// TestPIMDataRouteRights: what the frames of T-Kalender and Kontakte read is
// for a web session of this node only, like the device passwords – never
// in-process (MCP, the agent), never a token, never through another node,
// never in publicRoutes. Without the volume the address books are 503,
// never an empty list; the access says volume:false.
func TestPIMDataRouteRights(t *testing.T) {
	pim, _, setVolume := pimTestStore(t)
	ts := newTestServerWith(t, func(o *Options) {
		o.PIM = pim
		pim.Account = func(name string) (time.Time, bool) {
			u, ok := o.Users.Get(name)
			return u.Created, ok
		}
	})
	ts.setup()
	routes := pimDataRoutes(ts)
	if len(routes) != 27 {
		t.Fatalf("routes: %v", routes)
	}
	for _, r := range publicRoutes {
		if strings.HasPrefix(r.Path, "/calendar") || strings.HasPrefix(r.Path, "/contacts") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}
	owner, _ := ts.users.Get("alex")
	token := ts.mcpToken("alex", "system:read", "system:write", "files:read", "files:write")
	for _, rt := range routes {
		if rt[0] != "GET" || strings.Contains(rt[1], "{") {
			continue // TestCalendarRouteRights has them all
		}
		code, body := ts.server.internalAs(context.Background(), owner, rt[0], rt[1], nil)
		if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
			t.Errorf("%s %s in-process: %d %s", rt[0], rt[1], code, body)
		}
		req, _ := http.NewRequest(rt[0], ts.url+rt[1], nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with a token: %d", rt[0], rt[1], res.StatusCode)
		}
		creq := httptest.NewRequest(rt[0], rt[1], nil)
		creq = creq.WithContext(cluster.WithIdentity(creq.Context(), cluster.Identity{Node: "n2", User: "alex"}))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, creq)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_local_only") {
			t.Errorf("%s %s through the cluster: %d %s", rt[0], rt[1], rec.Code, rec.Body)
		}
		if res, _ := ts.newClient().Get(ts.url + rt[1]); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", rt[0], rt[1], res.StatusCode)
		}
	}

	// A plain account reads its own books; groups are not counted.
	bea := ts.signIn("bea", auth.RoleUser)
	card := func(uid, extra string) []byte {
		return []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + uid + "\r\n" + extra + "END:VCARD\r\n")
	}
	for name, data := range map[string][]byte{
		"a.vcf": card("a", ""), "b.vcf": card("b", ""),
		"g.vcf": card("g", "X-ADDRESSBOOKSERVER-KIND:group\r\n"),
	} {
		if _, err := pim.Put(pimstore.Contacts, "bea", pimstore.DefaultBook, name, data, pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	res, body := ts.do(bea, "GET", "/api/v1/contacts/books", nil)
	var books []bookView
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &books) != nil || len(books) != 1 || books[0] != (bookView{ID: "kontakte", Name: "Kontakte", Count: 2}) {
		t.Fatalf("books of bea: %d %s", res.StatusCode, body)
	}
	// The owner sees its own (virtual) book, not bea's contacts.
	if _, body := ts.request("GET", "/api/v1/contacts/books", nil); !strings.Contains(string(body), `"count":0`) {
		t.Fatalf("books of alex: %s", body)
	}
	if _, body := ts.do(bea, "GET", "/api/v1/calendar/access", nil); !strings.Contains(string(body), `"enabled":true`) || !strings.Contains(string(body), `"volume":true`) {
		t.Fatalf("access with the volume: %s", body)
	}
	// The NAS zone for the pages (a plain account cannot read /network);
	// time.Local without a name is left out (the page takes the browser's).
	if _, body := ts.do(bea, "GET", "/api/v1/calendar/access", nil); strings.Contains(string(body), `"timezone"`) {
		t.Fatalf("access names an unnamed zone: %s", body)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	pim.Local = func() *time.Location { return berlin }
	if _, body := ts.do(bea, "GET", "/api/v1/calendar/access", nil); !strings.Contains(string(body), `"timezone":"Europe/Berlin"`) {
		t.Fatalf("access without the NAS zone: %s", body)
	}

	// The volume gone: 503, never an empty list.
	setVolume("")
	res, body = ts.do(bea, "GET", "/api/v1/contacts/books", nil)
	if res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "contacts_unavailable" || res.Header.Get("Retry-After") == "" {
		t.Fatalf("books without the volume: %d %s", res.StatusCode, body)
	}
	if _, body := ts.do(bea, "GET", "/api/v1/calendar/access", nil); !strings.Contains(string(body), `"volume":false`) {
		t.Fatalf("access without the volume: %s", body)
	}
}

// Without a store (tests, titand -dev): the access says volume:false, the
// books 503 – the pages say "nicht verfügbar".
func TestPIMDataWithoutStore(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/calendar/access", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"volume":false`) {
		t.Fatalf("access: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/contacts/books", nil); res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "contacts_unavailable" {
		t.Fatalf("books: %d %s", res.StatusCode, body)
	}
}

// pimServer is a test server with a store of calendars as titand wires it:
// accounts by creation time, and the mark pim.json under users/<name> on
// the stick (cmd/titand/wiring_pim.go). setVolume("") takes the volume away.
func pimServer(t *testing.T) (*testServer, *pimstore.Store, string, func(string)) {
	t.Helper()
	pim, dir, setVolume := pimTestStore(t)
	ts := newTestServerWith(t, func(o *Options) {
		o.PIM = pim
		users, store := o.Users, o.Store
		pim.Account = func(name string) (time.Time, bool) {
			u, ok := users.Get(name)
			return u.Created, ok
		}
		pim.ReadMark = func(account string) ([]byte, error) { return store.Read("users/" + account + "/pim.json") }
		pim.WriteMark = func(account string, data []byte) error {
			return store.Write("users/"+account+"/pim.json", data)
		}
	})
	ts.setup()
	return ts, pim, dir, setVolume
}

// pimEvent is a small event; pimCard a contact.
func pimEvent(uid string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261110T080000Z\r\nDURATION:PT1H\r\nSUMMARY:" + uid + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
}

func pimCard(uid string) []byte {
	return []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + uid + "\r\nEND:VCARD\r\n")
}

// TestPIMDataCrossSite (Sicherheitsdurchsicht 1.11, question 19): another
// website cannot change calendars or contacts with the person's cookie –
// every route that writes answers 403 to a cross-site request
// (http.CrossOriginProtection around the whole mux, api.go) and changes
// nothing; the same request from the page itself goes through.
func TestPIMDataCrossSite(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	if _, err := pim.Put(pimstore.Calendar, "alex", "privat", "a.ics", pimEvent("a"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, "c.vcf", pimCard("c"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	writes := 0
	for _, rt := range pimDataRoutes(ts) {
		if rt[0] == http.MethodGet {
			continue
		}
		writes++
		names := strings.NewReplacer("{id}", "privat", "{cal}", "privat", "{obj}", "a.ics")
		switch {
		case strings.HasPrefix(rt[1], "/api/v1/contacts/books/"):
			names = strings.NewReplacer("{id}", pimstore.DefaultBook)
		case strings.HasPrefix(rt[1], "/api/v1/contacts/"):
			names = strings.NewReplacer("{book}", pimstore.DefaultBook, "{id}", "c.vcf")
		}
		path := names.Replace(rt[1])
		for _, h := range []map[string]string{{"Sec-Fetch-Site": "cross-site"}, {"Origin": "https://evil.example"}} {
			req, _ := http.NewRequest(rt[0], ts.url+path, strings.NewReader(`{"name":"x","event":{"title":"x","start":"2026-12-01"},"contact":{"first":"x"}}`))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range h {
				req.Header.Set(k, v)
			}
			res, err := ts.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s %v: %d, want 403", rt[0], path, h, res.StatusCode)
			}
		}
	}
	if writes != 15 {
		t.Fatalf("writing routes: %d", writes)
	}
	cals, _ := pim.Collections(pimstore.Calendar, "alex")
	events, _ := pim.Objects(pimstore.Calendar, "alex", "privat")
	cards, _ := pim.Objects(pimstore.Contacts, "alex", pimstore.DefaultBook)
	if len(cals) != 1 || len(events) != 1 || len(cards) != 1 {
		t.Fatalf("changed by a cross-site request: %d calendars, %d events, %d contacts", len(cals), len(events), len(cards))
	}
	// The same request from the page itself goes through.
	if res, body := ts.request("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Neu", "color": "#3B82F6"}); res.StatusCode >= 300 {
		t.Fatalf("new calendar from the page: %d %s", res.StatusCode, body)
	}
}
