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
// With TITANOS_SPEECH_REAL=<empty folder> the speech services run for real
// (as TestRealSpeech: Docker of this machine, the tiny Whisper model and the
// voice de_DE-thorsten-low, about 0.6 GB of images and 140 MB of models), so
// the Sprechmodus of the app speaks with Whisper and Piper.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

func TestAgentDevServe(t *testing.T) {
	addr := os.Getenv("TITANOS_AGENT_DEVSERVE")
	if addr == "" {
		t.Skip("only for the emulator")
	}
	// The speech services before the server: it registers their routes only when they are there.
	var speechManager *speech.Manager
	if root := os.Getenv("TITANOS_SPEECH_REAL"); root != "" {
		var stop func()
		speechManager, stop = devSpeech(t, root)
		defer stop()
	}
	ts := newTestServerWith(t, func(o *Options) { o.Speech = speechManager })
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

// devSpeechEngine is the Docker of this machine, which no TitanOS manages.
type devSpeechEngine struct{ *docker.Manager }

func (devSpeechEngine) EngineReady() bool { return true }

// devSpeech starts Whisper and Piper for the throwaway NAS; the returned
// function removes the containers again.
func devSpeech(t *testing.T, root string) (*speech.Manager, func()) {
	if err := os.MkdirAll(filepath.Join(root, "tank"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := config.Open(t.TempDir())
	m := speech.New(store)
	m.Engine, m.MountRoot = devSpeechEngine{docker.NewManager(store, store.Dir())}, root
	m.Mounted = func() []string { return []string{"tank"} }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	if err := m.SetSettings(speech.Settings{Volume: "tank"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Voices(ctx); err != nil {
		t.Fatal(err)
	}
	for _, d := range [][2]string{{"model", "tiny"}, {"voice", "de_DE-thorsten-low"}} {
		if err := m.Download(ctx, d[0], d[1]); err != nil {
			t.Fatal(err)
		}
	}
	for len(m.Jobs()) > 0 {
		for k, j := range m.Jobs() {
			if j.Error != "" {
				t.Fatalf("download %s: %s", k, j.Error)
			}
		}
		time.Sleep(time.Second)
	}
	s := speech.Settings{Enabled: true, Volume: "tank", WhisperModel: "tiny", Language: "de", Voice: "de_DE-thorsten-low", Speed: 1, Reach: speech.ReachHA, Agent: true}
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	go func() {
		for last := ""; ctx.Err() == nil; time.Sleep(2 * time.Second) {
			st := m.Status()
			if now := st.Whisper.Phase + "/" + st.Piper.Phase; now != last {
				fmt.Println("SPEECH", now)
				last = now
			}
		}
	}()
	return m, func() {
		s := m.Settings()
		s.Enabled = false
		_ = m.SetSettings(s)
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
			if st := m.Status(); st.Whisper.Phase == "off" && st.Piper.Phase == "off" {
				break
			}
		}
		cancel()
		<-done
	}
}
