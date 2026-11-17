package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

// acceptHA answers the login of Home Assistant's WebSocket API and returns
// the connection, or nil.
func acceptHA(w http.ResponseWriter, r *http.Request) *websocket.Conn {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil
	}
	ctx := r.Context()
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_required"}`))
	if _, _, err := conn.Read(ctx); err != nil {
		return nil
	}
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_ok"}`))
	return conn
}

func connected(t *testing.T, handler http.Handler) *Manager {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token"}); err != nil {
		t.Fatal(err)
	}
	return m
}

// N-102-34: statistics larger than the WebSocket limit are told as such,
// not as "Home Assistant not reachable", so the agent can ask for less.
func TestTooLargeStatisticsAreToldAsSuch(t *testing.T) {
	m := connected(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := acceptHA(w, r)
		if conn == nil {
			return
		}
		conn.Read(r.Context())
		big := `{"id":1,"type":"result","success":true,"result":{"sensor.x":"` + strings.Repeat("a", 5<<20) + `"}}`
		conn.Write(r.Context(), websocket.MessageText, []byte(big))
		conn.Read(r.Context())
	}))
	_, err := m.Statistics(context.Background(), []string{"sensor.x"}, time.Now().AddDate(0, 0, -365), time.Now(), "hour")
	if !errors.Is(err, ErrAnswerTooLarge) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("too large an answer: %v", err)
	}
}

// N-102-35: while the watcher reads the states over REST, it answers the
// pings of Home Assistant on the WebSocket (Home Assistant ends a
// connection whose pings go unanswered).
func TestWatcherAnswersPingsWhileReadingStates(t *testing.T) {
	pinged := make(chan error, 1)
	m := connected(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/states/") {
			// The answer comes only after the ping (or its failure).
			select {
			case err := <-pinged:
				pinged <- err
			case <-time.After(5 * time.Second):
			}
			json.NewEncoder(w).Encode(map[string]any{"entity_id": "light.a", "state": "on"})
			return
		}
		conn := acceptHA(w, r)
		if conn == nil {
			return
		}
		ctx := r.Context()
		conn.Read(ctx) // subscribe_trigger
		conn.Write(ctx, websocket.MessageText, []byte(`{"id":1,"type":"result","success":true}`))
		go func() {
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		pinged <- conn.Ping(pingCtx)
		cancel()
		<-ctx.Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.session(ctx, []string{"light.a"}, make(chan struct{}), make(chan struct{})); close(done) }()
	var err error
	select {
	case err = <-pinged:
	case <-time.After(5 * time.Second):
		t.Fatal("no ping")
	}
	cancel()
	<-done
	if err != nil {
		t.Fatalf("ping while the states were read: %v", err)
	}
}

// N-102-40: only an answer 404 means "no such automation"; an error that
// merely mentions 404 is an error.
func TestOnlyA404MeansNoSuchConfig(t *testing.T) {
	status := http.StatusNotFound
	m := connected(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"row 404 of the database is broken"}`, status)
	}))
	if got, err := m.Config(context.Background(), "automation", "lights"); got != nil || err != nil {
		t.Fatalf("404: %s %v", got, err)
	}
	status = http.StatusInternalServerError
	if _, err := m.Config(context.Background(), "automation", "lights"); err == nil {
		t.Fatal("an error 500 mentioning 404 was taken for no such automation")
	}
}
