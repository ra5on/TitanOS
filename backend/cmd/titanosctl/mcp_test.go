package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPBridgeRelaysStdioToHTTP(t *testing.T) {
	t.Setenv("TITANOS_TOKEN", "titanos_id_secret")
	var versions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer titanos_id_secret" || !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Errorf("request: %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		versions = append(versions, r.Header.Get("MCP-Protocol-Version"))
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.Unmarshal(body, &msg)
		switch msg.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"titanos"}}}`, msg.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			// Answer as a stream, over several lines of JSON.
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\ndata: \"id\":%s,\"result\":{\"tools\":[]}}\n\n", msg.ID)
		default:
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":"mcp_disabled","message":"off"}}`)
		}
	}))
	defer server.Close()
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``,
		`{"jsonrpc":"2.0","id":"b","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x"}}`,
	}, "\n")
	var out bytes.Buffer
	if err := run([]string{"--url", server.URL, "mcp"}, strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("answers: %q", out.String())
	}
	var answers []map[string]any
	for _, line := range lines {
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("not one JSON message per line: %q", line)
		}
		answers = append(answers, v)
	}
	if answers[0]["id"] != float64(1) || answers[1]["id"] != "b" || answers[1]["result"] == nil {
		t.Fatalf("relayed: %v", answers)
	}
	failed, _ := answers[2]["error"].(map[string]any)
	if answers[2]["id"] != float64(3) || failed == nil || !strings.Contains(failed["message"].(string), "mcp_disabled") {
		t.Fatalf("refusal not turned into an error: %v", answers[2])
	}
	if versions[0] != "" || versions[1] != "2025-06-18" || versions[3] != "2025-06-18" {
		t.Fatalf("protocol header: %q", versions)
	}
}

func TestMCPBridgeNeedsTokenOutsideArguments(t *testing.T) {
	t.Setenv("TITANOS_TOKEN", "")
	if err := run([]string{"--url", "https://nas.example", "mcp"}, strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "TITANOS_TOKEN") {
		t.Fatal(err)
	}
	t.Setenv("TITANOS_TOKEN", "x")
	if err := run([]string{"--url", "https://nas.example", "mcp", "extra"}, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("extra argument accepted")
	}
}
