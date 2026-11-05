package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// A node uses the model of the admin node (PLAN 47): only after the admin
// node released it, only for the model (chats stay on the node), and the
// release can be taken back.
func TestAgentSharedByTheAdminNode(t *testing.T) {
	a, _ := clusterTestServer(t, "nas-a")
	b, nodeB := clusterTestServer(t, "nas-b")
	a.setup()
	b.setup()
	if res, body := a.request("POST", "/api/v1/cluster", map[string]string{"name": "Home"}); res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	_, body := a.request("POST", "/api/v1/cluster/join-info", map[string]string{"password": testPassword})
	var view struct {
		JoinInfo string `json:"joinInfo"`
	}
	json.Unmarshal(body, &view)
	if res, body := b.request("POST", "/api/v1/cluster/join", map[string]string{"info": view.JoinInfo}); res.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %s", res.StatusCode, body)
	}
	if res, body := b.request("POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login on B: %d %s", res.StatusCode, body)
	}
	// A runs the agent with its Ollama.
	fake := &agentOllama{}
	if res, body := a.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b"}); res.StatusCode != http.StatusOK {
		t.Fatalf("settings A: %d %s", res.StatusCode, body)
	}
	// The admin node cannot use itself.
	if res, _ := a.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "source": "cluster"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("admin node with the cluster source: %d", res.StatusCode)
	}
	// B chooses the admin node's agent: no address of its own needed.
	dir := t.TempDir()
	b.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	if res, body := b.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "source": "cluster", "model": "gpt-oss:20b"}); res.StatusCode != http.StatusOK {
		t.Fatalf("settings B: %d %s", res.StatusCode, body)
	}
	_, body = b.request("GET", "/api/v1/agent/cluster", nil)
	if !strings.Contains(string(body), `"member":true`) || !strings.Contains(string(body), `"state":"off"`) {
		t.Fatalf("before the request: %s", body)
	}
	// Not released: the status says so, Ollama is not reached.
	_, body = b.request("GET", "/api/v1/agent/status", nil)
	if !strings.Contains(string(body), `"share":"off"`) || strings.Contains(string(body), `"models"`) {
		t.Fatalf("status without release: %s", body)
	}
	_, body = b.request("POST", "/api/v1/agent/cluster/request", nil)
	if !strings.Contains(string(body), `"state":"pending"`) {
		t.Fatalf("request: %s", body)
	}
	_, body = a.request("GET", "/api/v1/agent/settings", nil)
	if !strings.Contains(string(body), `"name":"nas-b"`) || !strings.Contains(string(body), `"allowed":false`) {
		t.Fatalf("request on A: %s", body)
	}
	// Saving A's settings keeps the requests.
	var settingsA map[string]any
	json.Unmarshal(body, &settingsA)
	delete(settingsA, "shared")
	a.request("PUT", "/api/v1/agent/settings", settingsA)
	if res, body := a.request("PUT", "/api/v1/agent/cluster/nodes/"+nodeB.ID(), map[string]bool{"allowed": true}); res.StatusCode != http.StatusOK {
		t.Fatalf("release: %d %s", res.StatusCode, body)
	}
	_, body = b.request("GET", "/api/v1/agent/status", nil)
	if !strings.Contains(string(body), `"share":"allowed"`) || !strings.Contains(string(body), `"gpt-oss:20b"`) || !strings.Contains(string(body), `"levels":["low","medium","high"]`) {
		t.Fatalf("status with release: %s", body)
	}
	// A chat on B runs with A's model and stays on B.
	res, body := b.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("chat on B: %d %s", res.StatusCode, body)
	}
	var started agent.Chat
	json.Unmarshal(body, &started)
	chat := waitChat(t, b, b.client, started.ID)
	if last := chat.Entries[len(chat.Entries)-1]; last.Content != "Fertig." {
		t.Fatalf("answer: %+v", chat.Entries)
	}
	if _, body := a.request("GET", "/api/v1/agent/chats", nil); strings.Contains(string(body), started.ID) {
		t.Fatal("the chat of B is on A")
	}
	// No model download through the admin node.
	if res, _ := b.request("POST", "/api/v1/agent/models/pull", map[string]string{"model": "x:1"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("pull on B: %d", res.StatusCode)
	}
	// Taken back: B is pending again.
	a.request("PUT", "/api/v1/agent/cluster/nodes/"+nodeB.ID(), map[string]bool{"allowed": false})
	_, body = b.request("GET", "/api/v1/agent/status", nil)
	if !strings.Contains(string(body), `"share":"pending"`) {
		t.Fatalf("after taking back: %s", body)
	}
	// Ollama itself is only reachable through the release, never pull.
	b.server.agentClient.Store(nil)
	client, _ := b.server.agentOllama()
	if _, err := client.Models(t.Context()); err == nil {
		t.Fatal("models without release")
	}
}
