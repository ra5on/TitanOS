package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 56: all chats at once; pinned ones stay on request, other accounts'
// chats never go.
func TestAgentDeleteAllChats(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{})
	store := ts.server.agentStore()
	for i, pinned := range []bool{false, true, false} {
		c := &agent.Chat{ID: agent.NewID(), Title: "Chat", Pinned: pinned, State: agent.StateIdle, Entries: []agent.Entry{{ID: agent.NewID(), Role: "user", Content: "x"}}}
		if err := store.SaveChat("alex", c); err != nil {
			t.Fatal(i, err)
		}
	}
	other := &agent.Chat{ID: agent.NewID(), Title: "Fremd", State: agent.StateIdle}
	if err := store.SaveChat("maria", other); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("DELETE", "/api/v1/agent/chats?keepPinned=true", nil)
	var got map[string]int
	json.Unmarshal(body, &got)
	if res.StatusCode != http.StatusOK || got["deleted"] != 2 || got["kept"] != 1 {
		t.Fatalf("keep pinned: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("DELETE", "/api/v1/agent/chats", nil)
	json.Unmarshal(body, &got)
	if res.StatusCode != http.StatusOK || got["deleted"] != 1 {
		t.Fatalf("all: %d %s", res.StatusCode, body)
	}
	if list, _ := store.Chats("alex"); len(list) != 0 {
		t.Fatalf("left %v", list)
	}
	if list, _ := store.Chats("maria"); len(list) != 1 {
		t.Fatal("another account's chat went")
	}
}
