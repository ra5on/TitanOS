package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeCommands answers single commands over the WebSocket API with what
// answer returns: a result, or an error code and message.
type fakeCommands struct {
	mu     sync.Mutex
	seen   []map[string]any
	answer func(msg map[string]any) (result any, code, message string)
}

func (f *fakeCommands) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := r.Context()
	send := func(v any) { b, _ := json.Marshal(v); conn.Write(ctx, websocket.MessageText, b) }
	send(map[string]string{"type": "auth_required"})
	if _, _, err := conn.Read(ctx); err != nil {
		return
	}
	send(map[string]string{"type": "auth_ok"})
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		f.mu.Lock()
		f.seen = append(f.seen, msg)
		f.mu.Unlock()
		// Something unrelated first: the client waits for its own answer.
		send(map[string]any{"id": 99, "type": "event"})
		result, code, message := f.answer(msg)
		if code != "" {
			send(map[string]any{"id": msg["id"], "type": "result", "success": false, "error": map[string]string{"code": code, "message": message}})
			continue
		}
		send(map[string]any{"id": msg["id"], "type": "result", "success": true, "result": result})
	}
}

func commandManager(t *testing.T, fake *fakeCommands) *Manager {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token"}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMountsThroughTheSupervisor(t *testing.T) {
	fake := &fakeCommands{answer: func(msg map[string]any) (any, string, string) {
		if msg["type"] != "supervisor/api" {
			return nil, "unknown_command", "?"
		}
		switch msg["method"] {
		case "get":
			return map[string]any{"mounts": []map[string]any{{"name": "titanos_ha", "usage": "backup", "type": "cifs", "server": "192.168.1.5", "share": "ha", "state": "active"}}}, "", ""
		case "post":
			data := msg["data"].(map[string]any)
			if data["server"] == "10.0.0.9" {
				return nil, "home_assistant_error", "Could not mount titanos_ha: permission denied"
			}
			return map[string]any{}, "", ""
		}
		return nil, "unknown_error", "?"
	}}
	m := commandManager(t, fake)
	ctx := context.Background()
	mounts, err := m.Mounts(ctx)
	if err != nil || len(mounts) != 1 || mounts[0].Name != "titanos_ha" || mounts[0].State != "active" {
		t.Fatalf("mounts %+v %v", mounts, err)
	}
	req := MountRequest{Name: MountName("Home-Assistant"), Usage: "backup", Server: "192.168.1.5", Share: "Home-Assistant", Username: "homeassistant", Password: "pw"}
	if req.Name != "titanos_home_assistant" {
		t.Fatalf("name %q", req.Name)
	}
	if err := m.AddMount(ctx, req); err != nil {
		t.Fatal(err)
	}
	sent := fake.seen[len(fake.seen)-1]
	data := sent["data"].(map[string]any)
	if sent["endpoint"] != "/mounts" || data["type"] != "cifs" || data["password"] != "pw" || data["share"] != "Home-Assistant" || data["usage"] != "backup" {
		t.Fatalf("sent %v", sent)
	}
	// The Supervisor's reason comes back.
	req.Server = "10.0.0.9"
	var ce *CommandError
	if err := m.AddMount(ctx, req); !errors.As(err, &ce) || ce.Message == "" {
		t.Fatalf("refused mount: %v", err)
	}
	for _, bad := range []MountRequest{{Name: "a b", Usage: "backup", Server: "s", Share: "x", Username: "u", Password: "p"}, {Name: "ok", Usage: "music", Server: "s", Share: "x", Username: "u", Password: "p"}} {
		if err := m.AddMount(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestNoSupervisor(t *testing.T) {
	m := commandManager(t, &fakeCommands{answer: func(map[string]any) (any, string, string) {
		return nil, "unknown_command", "Unknown command."
	}})
	if _, err := m.Mounts(context.Background()); !errors.Is(err, ErrNoSupervisor) {
		t.Fatalf("container installation: %v", err)
	}
}

func TestBackupsOfBothFormats(t *testing.T) {
	day := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	m := commandManager(t, &fakeCommands{answer: func(msg map[string]any) (any, string, string) {
		return map[string]any{"backups": []map[string]any{
			{"backup_id": "a", "name": "alt", "date": day.Add(-48 * time.Hour), "agent_ids": []string{"hassio.titanos_ha", "hassio.local"}},
			{"backup_id": "b", "name": "neu", "date": day, "agents": map[string]any{"hassio.local": map[string]any{"size": 1}}},
			{"backup_id": "c", "name": "mitte", "date": day.Add(-24 * time.Hour), "agents": map[string]any{"hassio.titanos_ha": map[string]any{"size": 2}}},
		}}, "", ""
	}})
	backups, err := m.Backups(context.Background())
	if err != nil || len(backups) != 3 || backups[0].ID != "b" {
		t.Fatalf("%+v %v", backups, err)
	}
	latest, ok := LatestOn(backups, MountAgent("titanos_ha"))
	if !ok || latest.ID != "c" {
		t.Fatalf("latest on the NAS: %+v %v", latest, ok)
	}
	if _, ok := LatestOn(backups, MountAgent("other")); ok {
		t.Fatal("found a backup on storage that has none")
	}
}
