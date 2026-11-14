package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// PLAN 91: Assist with a key, its own rights and its own profile.

func plan91AssistOn(t *testing.T, ts *testServer, ha map[string]any) {
	t.Helper()
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	ha["assist"] = true
	settings["homeassistant"] = ha
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("assist on: %d %s", res.StatusCode, body)
	}
}

func TestPlan91AssistKey(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	plan91AssistOn(t, ts, map[string]any{})
	// Home Assistant lives elsewhere: without a key its address decides.
	ha, _ := ts.home.Settings()
	ha.URL = "http://192.0.2.10:8123"
	ts.home.SetSettings(ha)
	ask := func(key string) int {
		req, _ := http.NewRequest("GET", ts.url+"/agent-ollama/api/tags", nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := ask(""); got != http.StatusForbidden {
		t.Fatalf("other address without a key: %d", got)
	}
	// A key is made; the settings never carry it.
	res, body := ts.request("POST", "/api/v1/agent/assist/key", nil)
	var made struct{ Key string }
	json.Unmarshal(body, &made)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(made.Key, "tos-assist-") || len(made.Key) < 40 {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	if strings.Contains(string(body), made.Key) || !strings.Contains(string(body), `"assistKeySet":true`) {
		t.Fatalf("settings carry the key or hide that there is one: %s", body)
	}
	// With a key the address no longer counts: right key in, others 401.
	if got := ask(made.Key); got != http.StatusOK {
		t.Fatalf("right key: %d", got)
	}
	for _, bad := range []string{"", "falsch", made.Key + "x"} {
		if got := ask(bad); got != http.StatusUnauthorized {
			t.Fatalf("key %q: %d", bad, got)
		}
	}
	// Saving the settings keeps the key, also when the page sends another.
	plan91AssistOn(t, ts, map[string]any{"assistKey": "untergeschoben"})
	if got := ask(made.Key); got != http.StatusOK {
		t.Fatalf("after saving: %d", got)
	}
	if got := ask("untergeschoben"); got != http.StatusUnauthorized {
		t.Fatalf("a key from the settings page: %d", got)
	}
	// A new key ends the old one. Only owners and administrators see it.
	_, body = ts.request("POST", "/api/v1/agent/assist/key", nil)
	var next struct{ Key string }
	json.Unmarshal(body, &next)
	if next.Key == made.Key || ask(made.Key) != http.StatusUnauthorized || ask(next.Key) != http.StatusOK {
		t.Fatal("the old key still works or the new one does not")
	}
	guest := covGuest(t, ts)
	for _, route := range []string{"/api/v1/agent/assist", "/api/v1/agent/assist/key"} {
		if res, _ := ts.do(guest, "GET", route, nil); res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s for a plain account: %d", route, res.StatusCode)
		}
	}
	// The page reads the last request: the refused try with its reason.
	_, body = ts.request("GET", "/api/v1/agent/assist", nil)
	var info struct {
		KeySet bool
		Last   *assistSeen
		Levels map[string]string
	}
	json.Unmarshal(body, &info)
	if !info.KeySet || info.Last == nil || info.Last.OK || info.Last.Reason != "key" {
		t.Fatalf("assist info: %s", body)
	}
	if info.Levels[toolcatalog.AreaFiles] != assistDeny || info.Levels[agent.AreaWeb] != assistDeny || info.Levels[toolcatalog.AreaHome] != assistChange || info.Levels[toolcatalog.AreaSystem] != assistRead {
		t.Fatalf("default levels: %v", info.Levels)
	}
}

func TestPlan91AssistRights(t *testing.T) {
	h := agentHASettings{}
	for _, c := range []struct {
		tool, area, kind, general, want string
	}{
		// Defaults: files and the internet are closed, whatever the agent may.
		{"file_read", toolcatalog.AreaFiles, agent.KindRead, agent.ModeAllow, agent.ModeDeny},
		{"web_search", agent.AreaWeb, agent.KindRead, agent.ModeAllow, agent.ModeDeny},
		// Reading the state of the NAS is open, changing it is not.
		{"system_status", toolcatalog.AreaSystem, agent.KindRead, agent.ModeAllow, agent.ModeAllow},
		{"container_stop", toolcatalog.AreaDocker, agent.KindChange, agent.ModeAsk, agent.ModeDeny},
		// Home Assistant: as the general rights say, never more.
		{"ha_call", toolcatalog.AreaHome, agent.KindChange, agent.ModeAsk, agent.ModeAsk},
		{"ha_call", toolcatalog.AreaHome, agent.KindChange, agent.ModeDeny, agent.ModeDeny},
		// The agent's own tools: the memory is read only, planning is open.
		{"memory_save", agent.AreaAgent, agent.KindAgent, agent.ModeAllow, agent.ModeDeny},
		{"schedule_create", agent.AreaAgent, agent.KindAgent, agent.ModeAllow, agent.ModeAllow},
		{"ask_user", agent.AreaAgent, agent.KindAgent, agent.ModeAllow, agent.ModeAllow},
	} {
		if got := h.assistMode(c.tool, c.area, c.kind, c.general); got != c.want {
			t.Errorf("default %s (%s): %s, want %s", c.tool, c.general, got, c.want)
		}
	}
	// Each area is set on its own.
	h.AssistAreas = map[string]string{toolcatalog.AreaFiles: assistRead, agent.AreaWeb: assistChange, toolcatalog.AreaHome: assistDeny, assistAreaMemory: assistChange, assistAreaPlan: assistDeny}
	for _, c := range []struct {
		tool, area, kind, want string
	}{
		{"file_read", toolcatalog.AreaFiles, agent.KindRead, agent.ModeAllow},
		{"file_write", toolcatalog.AreaFiles, agent.KindChange, agent.ModeDeny},
		{"web_search", agent.AreaWeb, agent.KindRead, agent.ModeAllow},
		{"ha_states", toolcatalog.AreaHome, agent.KindRead, agent.ModeDeny},
		{"memory_save", agent.AreaAgent, agent.KindAgent, agent.ModeAllow},
		{"schedule_create", agent.AreaAgent, agent.KindAgent, agent.ModeDeny},
	} {
		if got := h.assistMode(c.tool, c.area, c.kind, agent.ModeAllow); got != c.want {
			t.Errorf("set %s: %s, want %s", c.tool, got, c.want)
		}
	}

	// The settings refuse unknown areas and levels.
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	for _, bad := range []map[string]string{{"gibtsnicht": assistRead}, {toolcatalog.AreaFiles: "alles"}} {
		_, body := ts.request("GET", "/api/v1/agent/settings", nil)
		var settings map[string]any
		json.Unmarshal(body, &settings)
		settings["homeassistant"] = map[string]any{"assist": true, "assistAreas": bad}
		if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%v: %d", bad, res.StatusCode)
		}
	}
	plan91AssistOn(t, ts, map[string]any{"assistAreas": map[string]string{agent.AreaWeb: assistChange}})
	if got := ts.server.agentSettings().HA.assistLevel(agent.AreaWeb); got != assistChange {
		t.Fatalf("internet after saving: %s", got)
	}
	// A run in the profile offers only what Home Assistant may use.
	profile, ok := ts.server.assistProfile()
	if !ok {
		t.Fatal("no profile")
	}
	box := ts.server.newAgentToolbox(profile, ts.server.agentSettings())
	if box.session.user.Name != "alex" {
		t.Fatalf("tools act as %q", box.session.user.Name)
	}
	for _, tool := range box.ordered {
		mode := box.mode(tool, tool.kind)
		if tool.area == toolcatalog.AreaFiles && mode != agent.ModeDeny {
			t.Errorf("%s of files is %s for Home Assistant", tool.name, mode)
		}
		if tool.name == "memory_save" && mode != agent.ModeDeny {
			t.Errorf("memory_save is %s", mode)
		}
	}
	own := ts.server.newAgentToolbox(box.session.user, ts.server.agentSettings())
	for _, tool := range own.ordered {
		if tool.name == "memory_save" && own.mode(tool, tool.kind) != agent.ModeAllow {
			t.Error("the limits of Home Assistant reach an account")
		}
	}
}

func TestPlan91AssistProfile(t *testing.T) {
	fake := &agentOllama{replies: []string{`{"role":"assistant","content":"Das Licht ist aus."}`}}
	ts := agentServer(t, fake, agent.Policy{})
	plan91AssistOn(t, ts, map[string]any{})
	_, body := ts.request("POST", "/api/v1/agent/assist/key", nil)
	var made struct{ Key string }
	json.Unmarshal(body, &made)
	// The owner's memory is not Home Assistant's.
	ts.request("PUT", "/api/v1/agent/memory/new", map[string]any{"text": "Mein Kontostand steht in Finanzen.xlsx."})
	ts.request("PUT", "/api/v1/agent/memory/new?profile=assist", map[string]any{"text": "Das Flurlicht heißt light.flur."})

	req, _ := http.NewRequest("POST", ts.url+"/agent-ollama/api/chat", strings.NewReader(`{"stream":false,"messages":[{"role":"user","content":"Ist das Licht im Flur an?"}]}`))
	req.Header.Set("Authorization", "Bearer "+made.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("question: %v %v", err, res)
	}
	res.Body.Close()
	// What the model was told: the profile's memory, nothing of the owner's.
	fake.mu.Lock()
	prompt := mustJSON(fake.got)
	fake.mu.Unlock()
	if !strings.Contains(string(prompt), "light.flur") || strings.Contains(string(prompt), "Finanzen.xlsx") {
		t.Fatalf("the prompt mixes the memories: %s", prompt)
	}
	// Owners and administrators see the profile's chats and memory …
	list := func(client *http.Client, path string) (int, string) {
		res, body := ts.do(client, "GET", path, nil)
		return res.StatusCode, string(body)
	}
	if code, body := list(ts.client, "/api/v1/agent/chats?profile=assist"); code != http.StatusOK || !strings.Contains(body, "Home Assistant (Assist)") {
		t.Fatalf("profile chats: %d %s", code, body)
	}
	if code, body := list(ts.client, "/api/v1/agent/chats"); code != http.StatusOK || strings.Contains(body, "Home Assistant (Assist)") {
		t.Fatalf("own chats: %d %s", code, body)
	}
	if code, body := list(ts.client, "/api/v1/agent/memory?profile=assist"); code != http.StatusOK || !strings.Contains(body, "light.flur") || strings.Contains(body, "Finanzen") {
		t.Fatalf("profile memory: %d %s", code, body)
	}
	// … a plain account does not, even when it may use the agent.
	guest := covGuest(t, ts)
	for _, path := range []string{"/api/v1/agent/chats?profile=assist", "/api/v1/agent/memory?profile=assist"} {
		if code, _ := list(guest, path); code != http.StatusForbidden {
			t.Fatalf("%s for a plain account: %d", path, code)
		}
	}
	// The profile is no account: nobody logs in as it, the store lists it.
	if _, ok := ts.users.Get(agent.AssistProfile); ok {
		t.Fatal("the profile became an account")
	}
	if got := ts.server.agentStore().Accounts(); len(got) != 2 {
		t.Fatalf("store accounts: %v", got)
	}
}
