package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// sse reads the events of a stream: event name and data.
type sse struct {
	res    *http.Response
	events chan [2]string
}

func openStream(t *testing.T, ts *testServer, client *http.Client) *sse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.url+"/api/v1/calendar/stream", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stream: %d %v", res.StatusCode, res.Header)
	}
	s := &sse{res: res, events: make(chan [2]string, 64)}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		var name, data string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && name != "":
				s.events <- [2]string{name, data}
				name, data = "", ""
			}
		}
		close(s.events)
	}()
	return s
}

// next waits for the next event (or none within d: ok false).
func (s *sse) next(d time.Duration) (name string, data string, ok bool) {
	select {
	case ev, open := <-s.events:
		return ev[0], ev[1], open
	case <-time.After(d):
		return "", "", false
	}
}

// TestPIMStreamRights: the stream is for a web session of this node
// only, like every route of Kontakte and Kalender.
func TestPIMStreamRights(t *testing.T) {
	ts, _, _, setVolume := pimServer(t)
	owner, _ := ts.users.Get("alex")
	const path = "/api/v1/calendar/stream"
	if code, body := ts.server.internalAs(context.Background(), owner, "GET", path, nil); code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
		t.Errorf("in-process: %d %s", code, body)
	}
	token := ts.mcpToken("alex", "system:read", "system:write", "files:read", "files:write")
	req, _ := http.NewRequest("GET", ts.url+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if res, err := http.DefaultClient.Do(req); err != nil || (res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized) {
		t.Errorf("token: %v %v", res, err)
	}
	creq := httptest.NewRequest("GET", path, nil)
	creq = creq.WithContext(cluster.WithIdentity(creq.Context(), cluster.Identity{Node: "n2", User: "alex"}))
	rec := httptest.NewRecorder()
	ts.server.ServeHTTP(rec, creq)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_local_only") {
		t.Errorf("cluster: %d %s", rec.Code, rec.Body)
	}
	if res, _ := ts.newClient().Get(ts.url + path); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("without a session: %d", res.StatusCode)
	}
	for _, r := range publicRoutes {
		if strings.HasPrefix(r.Path, "/calendar") {
			t.Errorf("public route %s", r.Path)
		}
	}
	// Without the store: 503 like every route of Kalender.
	bare := newTestServer(t)
	bare.setup()
	if res, body := bare.request("GET", path, nil); res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "calendar_unavailable" {
		t.Errorf("without a store: %d %s", res.StatusCode, body)
	}
	_ = setVolume
}

// TestPIMStream: a change from any side (here the store, as CardDAV and
// CalDAV write) reaches the account's own open pages, gathered per
// collection, with the ctag as generation – never another account's.
func TestPIMStream(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	bea := ts.signIn("bea", auth.RoleUser)
	alexStream := openStream(t, ts, ts.client)
	beaStream := openStream(t, ts, bea)
	var ready struct {
		Account     string            `json:"account"`
		Generations map[string]string `json:"generations"`
	}
	name, data, ok := alexStream.next(5 * time.Second)
	if !ok || name != "ready" || json.Unmarshal([]byte(data), &ready) != nil || ready.Account != "alex" ||
		ready.Generations["contacts/kontakte"] != "0-0" || ready.Generations["calendar/privat"] != "0-0" {
		t.Fatalf("ready: %s %s", name, data)
	}
	if name, _, ok := beaStream.next(5 * time.Second); !ok || name != "ready" {
		t.Fatal("bea's ready")
	}
	// Three cards at once (an iPhone): one event for the book.
	for _, uid := range []string{"a", "b", "c"} {
		if _, err := pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, uid+".vcf", pimCard(uid), pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	book, _ := pim.GetCollection(pimstore.Contacts, "alex", pimstore.DefaultBook)
	name, data, ok = alexStream.next(5 * time.Second)
	var ev pimChanged
	if !ok || name != "pim" || json.Unmarshal([]byte(data), &ev) != nil ||
		ev != (pimChanged{Account: "alex", Kind: "contacts", Collection: "kontakte", Generation: book.CTag}) {
		t.Fatalf("change: %s %s (ctag %s)", name, data, book.CTag)
	}
	if name, data, ok := alexStream.next(600 * time.Millisecond); ok {
		t.Fatalf("not gathered: %s %s", name, data)
	}
	// An event through the web API: the calendar.
	ts.do(ts.client, "POST", "/api/v1/calendar/events", map[string]any{"calendar": "privat",
		"event": map[string]any{"title": "Termin", "start": "2026-11-20T10:00", "end": "2026-11-20T11:00"}})
	cal, _ := pim.GetCollection(pimstore.Calendar, "alex", "privat")
	name, data, ok = alexStream.next(5 * time.Second)
	if !ok || name != "pim" || !strings.Contains(data, `"kind":"calendar"`) || !strings.Contains(data, `"generation":"`+cal.CTag+`"`) {
		t.Fatalf("calendar change: %s %s", name, data)
	}
	// bea heard nothing of alex.
	if name, data, ok := beaStream.next(500 * time.Millisecond); ok {
		t.Fatalf("bea heard %s %s", name, data)
	}
	// The account deleted: its streams end.
	ts.server.pimLive.forget("alex")
	if _, _, ok := alexStream.next(5 * time.Second); ok {
		t.Fatal("stream of a forgotten account goes on")
	}
}

// TestPIMStreamResync: a stream that cannot keep up says so.
func TestPIMStreamResync(t *testing.T) {
	h := newPIMHub()
	sub, cancel := h.subscribe("alex")
	defer cancel()
	for i := range 100 {
		h.publish(pimstore.Changed{Kind: pimstore.Calendar, Account: "alex", Collection: "privat", CTag: "e-" + string(rune('0'+i%10))})
	}
	if !sub.lost.Load() || len(sub.ch) != cap(sub.ch) {
		t.Fatalf("lost %v, %d queued", sub.lost.Load(), len(sub.ch))
	}
	// Beyond maxPIMStreams the oldest ends.
	var subs []*pimSub
	for range maxPIMStreams {
		s, c := h.subscribe("bea")
		defer c()
		subs = append(subs, s)
	}
	_, c := h.subscribe("bea")
	defer c()
	if _, open := <-subs[0].ch; open {
		t.Fatal("the oldest stream stays")
	}
}
