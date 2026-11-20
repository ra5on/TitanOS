package api

// 1.19 (PLAN 116, Agent-App): a throwaway NAS with the KI-Agent for the
// emulator – the agent on a temporary folder (as agentServer does), a model
// at TITANOS_AGENT_OLLAMA (a stand-in or a real Ollama), the data of the
// web's screenshot fixtures (alex, Home Assistant, NAS-Doku), and a pairing
// that is confirmed by itself. Skipped unless TITANOS_AGENT_DEVSERVE names
// the address to listen on. Start:
//   TITANOS_AGENT_DEVSERVE=127.0.0.1:8092 TITANOS_AGENT_OLLAMA=http://127.0.0.1:11999 \
//     go test ./internal/api -run TestAgentDevServe -timeout 0 -v
// It ends when the file $TMPDIR/agent-devserve-stop appears.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

func TestAgentDevServe(t *testing.T) {
	addr := os.Getenv("TITANOS_AGENT_DEVSERVE")
	if addr == "" {
		t.Skip("only for the emulator")
	}
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	settings := map[string]any{"enabled": true, "ollama": os.Getenv("TITANOS_AGENT_OLLAMA"), "model": "gpt-oss:20b", "think": "medium", "volume": "tank",
		"policy": agent.Policy{Preset: agent.PresetStandard}}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
	// The fixtures of scripts/test-ui-agent90.mjs.
	for _, p := range []map[string]any{{"name": "Home Assistant", "icon": "🏠"}, {"name": "NAS-Doku", "icon": "📚"}} {
		res, body := ts.request("POST", "/api/v1/agent/projects", p)
		fmt.Println("PROJECT", res.StatusCode, string(body))
	}
	for _, m := range []string{"Home Assistant läuft als VM HAOS.", "Der Zigbee-Stick hängt hinten links.", "Telekom-Rechnungen liegen in Documents › Rechnungen."} {
		res, _ := ts.request("PUT", "/api/v1/agent/memory/new", map[string]string{"text": m})
		fmt.Println("MEMORY", res.StatusCode)
	}
	srv := &http.Server{Addr: addr, Handler: ts.server}
	go srv.ListenAndServe()
	defer srv.Close()
	fmt.Println("LISTEN", addr)
	stop := filepath.Join(os.TempDir(), "agent-devserve-stop")
	os.Remove(stop)
	pair := func() string {
		res, body := ts.request("POST", "/api/v1/account/apps/pair", map[string]string{"app": "agent"})
		var start pairStart
		json.Unmarshal(body, &start)
		fmt.Println("PAIR", res.StatusCode, start.ManualCode, start.Link)
		return start.ID
	}
	id := pair()
	for {
		if _, err := os.Stat(stop); err == nil {
			return
		}
		_, b := ts.request("GET", "/api/v1/account/apps/pair/"+id, nil)
		var st struct{ State string }
		json.Unmarshal(b, &st)
		switch st.State {
		case "claimed":
			r, b := ts.request("POST", "/api/v1/account/apps/pair/"+id+"/confirm", map[string]string{"password": testPassword})
			fmt.Println("CONFIRM", r.StatusCode, string(b))
		case "confirmed", "expired", "rejected", "cancelled", "":
			if st.State != "confirmed" || os.Getenv("TITANOS_AGENT_REPAIR") != "" {
				id = pair()
			}
		}
		time.Sleep(time.Second)
	}
}
