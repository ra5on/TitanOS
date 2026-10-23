package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeWS speaks the WebSocket API of Home Assistant: authentication,
// subscribe_trigger and subscribe_events, and sends what push gets.
type fakeWS struct {
	mu         sync.Mutex
	subscribed []string
	conns      []*websocket.Conn
	triggerIDs []int
	eventIDs   map[string]int
}

func (f *fakeWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/websocket" {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := r.Context()
	send := func(v any) { b, _ := json.Marshal(v); conn.Write(ctx, websocket.MessageText, b) }
	send(map[string]string{"type": "auth_required"})
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		return
	}
	json.Unmarshal(data, &auth)
	if auth.AccessToken != "good-token" {
		send(map[string]string{"type": "auth_invalid", "message": "bad token"})
		conn.Close(websocket.StatusPolicyViolation, "")
		return
	}
	send(map[string]string{"type": "auth_ok"})
	f.mu.Lock()
	f.conns = append(f.conns, conn)
	f.mu.Unlock()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg struct {
			ID        int            `json:"id"`
			Type      string         `json:"type"`
			EventType string         `json:"event_type"`
			Trigger   map[string]any `json:"trigger"`
		}
		json.Unmarshal(data, &msg)
		f.mu.Lock()
		switch msg.Type {
		case "subscribe_trigger":
			b, _ := json.Marshal(msg.Trigger["entity_id"])
			f.subscribed = append(f.subscribed, string(b))
			f.triggerIDs = append(f.triggerIDs, msg.ID)
		case "subscribe_events":
			if f.eventIDs == nil {
				f.eventIDs = map[string]int{}
			}
			f.eventIDs[msg.EventType] = msg.ID
		}
		f.mu.Unlock()
		send(map[string]any{"id": msg.ID, "type": "result", "success": true})
	}
}

func (f *fakeWS) last() (*websocket.Conn, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.conns) == 0 || len(f.triggerIDs) == 0 {
		return nil, 0
	}
	return f.conns[len(f.conns)-1], f.triggerIDs[len(f.triggerIDs)-1]
}

func (f *fakeWS) change(t *testing.T, entity, from, to string) {
	t.Helper()
	conn, id := f.last()
	event := map[string]any{"id": id, "type": "event", "event": map[string]any{"variables": map[string]any{"trigger": map[string]any{
		"entity_id": entity, "from_state": map[string]any{"state": from}, "to_state": map[string]any{"state": to}}}}}
	b, _ := json.Marshal(event)
	if err := conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 300 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

func TestWatcherRecordsTransitionsAndEvents(t *testing.T) {
	fake := &fakeWS{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	var got []string
	var mu sync.Mutex
	m.OnEvent("titanos_ask", func(data json.RawMessage) { mu.Lock(); got = append(got, string(data)); mu.Unlock() })
	m.SetWatched([]string{"person.alex", "bad entity"})
	eventually(t, "subscribed", func() bool { c, _ := fake.last(); return c != nil })
	fake.mu.Lock()
	if fake.subscribed[len(fake.subscribed)-1] != `["person.alex"]` {
		t.Fatalf("subscribed %v", fake.subscribed)
	}
	fake.mu.Unlock()
	if connected, _ := m.WatchStatus(); !connected {
		t.Fatal("not connected")
	}

	fake.change(t, "person.alex", "home", "not_home")
	fake.change(t, "person.alex", "not_home", "not_home") // attributes only
	fake.change(t, "person.alex", "not_home", "home")
	eventually(t, "transitions", func() bool { list, _ := m.Transitions("person.alex", ""); return len(list) == 2 })
	list, newest := m.Transitions("person.alex", "")
	if list[0].To != "not_home" || list[1].From != "not_home" || newest != list[1].Mark || !(list[0].Mark < list[1].Mark) {
		t.Fatalf("%+v %s", list, newest)
	}
	if since, _ := m.Transitions("person.alex", list[0].Mark); len(since) != 1 || since[0].To != "home" {
		t.Fatalf("since: %+v", since)
	}

	// Subscribed events reach their handlers.
	conn, _ := fake.last()
	fake.mu.Lock()
	askID := fake.eventIDs["titanos_ask"]
	fake.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"id": askID, "type": "event", "event": map[string]any{"event_type": "titanos_ask", "data": map[string]string{"text": "Wie voll ist das NAS?"}}})
	conn.Write(context.Background(), websocket.MessageText, b)
	eventually(t, "event", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })
	if !strings.Contains(got[0], "Wie voll") {
		t.Fatalf("event %v", got)
	}

	// A new entity list subscribes again.
	m.SetWatched([]string{"person.alex", "sensor.power"})
	eventually(t, "resubscribed", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.subscribed[len(fake.subscribed)-1] == `["person.alex","sensor.power"]`
	})
}

func TestWatcherReportsRefusedToken(t *testing.T) {
	fake := &fakeWS{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "bad-token"})
	err := m.session(context.Background(), []string{"light.a"}, nil, make(chan struct{}), make(chan struct{}))
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("bad token: %v", err)
	}
}

func TestMatch(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		tr           Transition
		from, to     string
		above, below *float64
		want         bool
	}{
		{Transition{From: "home", To: "not_home"}, "", "not_home", nil, nil, true},
		{Transition{From: "home", To: "not_home"}, "work", "not_home", nil, nil, false},
		{Transition{From: "19", To: "21"}, "", "", f(20), nil, true},  // crosses upwards
		{Transition{From: "21", To: "22"}, "", "", f(20), nil, false}, // already above
		{Transition{From: "unavailable", To: "25"}, "", "", f(20), nil, true},
		{Transition{From: "5", To: "3"}, "", "", nil, f(4), true},
		{Transition{From: "3", To: "on"}, "", "", nil, f(4), false},
	}
	for _, c := range cases {
		if got := Match(c.tr, c.from, c.to, c.above, c.below); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}
