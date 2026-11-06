package viewer

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestCheckClient(t *testing.T) {
	big := `{"t":"clipboard","text":"` + strings.Repeat("a", MaxText) + `"}`
	for _, tc := range []struct {
		binary bool
		data   string
		input  bool
		ok     bool
	}{
		{false, `{"t":"mouse","x":1,"y":2,"buttons":1}`, true, true},
		{false, `{"t":"key","code":"KeyA","key":"a","down":true}`, true, true},
		{false, `{"t":"size","width":1280,"height":800,"scale":2}`, false, true},
		{false, `{"t":"keyframe"}`, false, true},
		{false, `{"t":"quality","mode":"auto","fps":60}`, false, true},
		{false, `{"t":"upload","id":1,"name":"a.pdf","size":3}`, true, true},
		{false, `{"t":"exec","cmd":"rm -rf /"}`, false, false},
		{false, `{"t":"hello"}`, false, false}, // agent → browser only
		{false, `["t","mouse"]`, false, false},
		{false, `not json`, false, false},
		{false, big, false, false},
		{true, "\x03\x00\x00\x00\x01data", true, true},
		{true, "\x01\x00video", false, false},
		{true, "\x03\x00\x00", false, false},
		{true, "\x03\x00\x00\x00\x01" + strings.Repeat("x", MaxChunk+1), false, false},
	} {
		input, err := CheckClient(tc.binary, []byte(tc.data))
		if (err == nil) != tc.ok || input != tc.input {
			t.Errorf("%.40q: input %v err %v", tc.data, input, err)
		}
	}
}

// pair returns the server end of a WebSocket and its client end.
func pair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()
	got := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		got <- c
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	client, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.CloseNow() })
	server = <-got
	t.Cleanup(func() { server.CloseNow() })
	return server, client
}

func TestRelayPassesAndChecks(t *testing.T) {
	browserSide, browser := pair(t) // titand's end, the window
	agentSide, agent := pair(t)     // titand's end, the agent
	inputs := 0
	done := make(chan End, 1)
	go func() { done <- Relay(context.Background(), browserSide, agentSide, func() { inputs++ }) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Agent → browser passes unchanged, binary and text.
	frame := []byte{0x01, 0x01, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1}
	agent.Write(ctx, websocket.MessageBinary, frame)
	if kind, data, err := browser.Read(ctx); err != nil || kind != websocket.MessageBinary || !bytes.Equal(data, frame) {
		t.Fatalf("video: %v %v", data, err)
	}
	// Browser → agent: a known message passes and counts as input.
	browser.Write(ctx, websocket.MessageText, []byte(`{"t":"mouse","x":1,"y":1,"buttons":0}`))
	if _, data, err := agent.Read(ctx); err != nil || !strings.Contains(string(data), "mouse") {
		t.Fatalf("mouse: %s %v", data, err)
	}
	// An unknown one ends the relay before it reaches the agent.
	browser.Write(ctx, websocket.MessageText, []byte(`{"t":"shell"}`))
	if end := <-done; end != EndRefused {
		t.Fatalf("end %v", end)
	}
	if inputs != 1 {
		t.Fatalf("inputs %d", inputs)
	}
}
