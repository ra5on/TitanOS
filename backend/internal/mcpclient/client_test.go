package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A server that answers once with JSON and once with a stream of events:
// the client starts a session, lists the tools and calls one; the token
// and the session go along, and a refused sign-in is said plainly.
func TestClient(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var methods []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer geheim" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var msg struct {
				ID     *int            `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			json.NewDecoder(r.Body).Decode(&msg)
			methods = append(methods, msg.Method)
			if msg.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "s1" {
				t.Errorf("%s without the session", msg.Method)
			}
			if msg.ID == nil {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			result := `{}`
			switch msg.Method {
			case "initialize":
				w.Header().Set("Mcp-Session-Id", "s1")
				result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}`
			case "tools/list":
				result = `{"tools":[{"name":"workflow_run","description":"Runs a workflow","inputSchema":{"type":"object","properties":{"name":{"type":"string"}}}}]}`
			case "tools/call":
				if !strings.Contains(string(msg.Params), `"name":"workflow_run"`) || !strings.Contains(string(msg.Params), `"Rechnung"`) {
					t.Errorf("call params %s", msg.Params)
				}
				result = `{"content":[{"type":"text","text":"gestartet"},{"type":"image","data":"x"}]}`
			}
			answer := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *msg.ID, result)
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\nevent: message\ndata: %s\n\n", answer)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(answer))
		}))
		c := &Client{URL: srv.URL, Token: "geheim"}
		tools, err := c.Tools(context.Background())
		if err != nil || len(tools) != 1 || tools[0].Name != "workflow_run" || tools[0].InputSchema["type"] != "object" {
			t.Fatalf("stream %v: tools %+v %v", stream, tools, err)
		}
		text, failed, err := c.Call(context.Background(), "workflow_run", json.RawMessage(`{"name":"Rechnung"}`))
		if err != nil || failed || text != "gestartet\n[image content left out]" {
			t.Fatalf("stream %v: call %q %v %v", stream, text, failed, err)
		}
		if got := strings.Join(methods, " "); got != "initialize notifications/initialized tools/list tools/call" {
			t.Fatalf("stream %v: methods %s", stream, got)
		}
		bad := &Client{URL: srv.URL, Token: "falsch"}
		if _, err := bad.Tools(context.Background()); err == nil || !strings.Contains(err.Error(), "token") {
			t.Fatalf("wrong token: %v", err)
		}
		srv.Close()
	}
}
