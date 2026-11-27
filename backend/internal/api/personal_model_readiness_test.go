package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/webui"
)

func TestPersonalModelReadinessThroughAccountSettings(t *testing.T) {
	browser := os.Getenv("TITANOS_MODEL_BROWSER") == "1"
	ts := newTestServerWith(t, func(o *Options) {
		o.PersonalDataDir = t.TempDir()
		if browser {
			if !webui.Embedded {
				t.Fatal("browser acceptance requires -tags embedui")
			}
			o.UI = webui.FS()
		}
	})
	ts.setup()
	var offline, empty atomic.Bool
	var probes atomic.Int32
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if offline.Load() {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/api/tags":
			if empty.Load() {
				io.WriteString(w, `{"models":[]}`)
				return
			}
			io.WriteString(w, `{"models":[{"name":"remote-alias"},{"name":"embeddings"},{"name":"fixture:latest"}]}`)
		case "/api/show":
			var body struct{ Model string }
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			switch body.Model {
			case "fixture:latest", "fixture":
				io.WriteString(w, `{"model_info":{"general.architecture":"llama"}}`)
			case "embeddings":
				io.WriteString(w, `{"capabilities":["embedding"],"model_info":{"general.architecture":"bert"}}`)
			case "remote-alias":
				io.WriteString(w, `{"remote_host":"https://example.invalid","model_info":{"general.architecture":"llama"}}`)
			default:
				http.NotFound(w, r)
			}
		default:
			t.Errorf("readiness requested inference or another endpoint: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()
	ts.server.personal.localModel, _ = agent.NewOllama(ollama.URL)
	bea, err := ts.users.Create("bea", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	other := ts.newClient()
	if resp, _ := ts.do(other, "POST", "/api/v1/login", credentials{Name: bea.Name, Password: testPassword}); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	get := func(c *http.Client, wantState, wantModel string) {
		t.Helper()
		v := personalHTTP[personalLocalStatus](t, ts, c, "GET", "/model/readiness", nil)
		if v.State != wantState || v.Model != wantModel {
			t.Fatalf("want %q %q, got %+v", wantState, wantModel, v)
		}
	}
	choose := func(c *http.Client, name string) {
		t.Helper()
		personalHTTP[map[string]any](t, ts, c, "PUT", "/model", map[string]string{"local": name})
	}
	empty.Store(true)
	get(ts.client, "missing", "")
	empty.Store(false)
	get(ts.client, "ready", "fixture:latest")
	models := personalHTTP[[]string](t, ts, ts.client, "GET", "/models", nil)
	if !slices.Equal(models, []string{"fixture:latest"}) {
		t.Fatal(models)
	}
	choose(ts.client, "remote-alias")
	get(ts.client, "missing", "remote-alias")
	choose(ts.client, "alex-only")
	choose(other, "bea-only")
	get(ts.client, "missing", "alex-only")
	get(other, "missing", "bea-only")
	choose(other, "fixture")
	get(other, "ready", "fixture")
	offline.Store(true)
	get(ts.client, "offline", "")
	get(other, "offline", "")
	offline.Store(false)
	personalHTTP[pa.State](t, ts, other, "PUT", "/settings", map[string]any{"rights": map[string]string{"chat.ask": "never"}})
	before := probes.Load()
	if resp, _ := ts.do(other, "GET", personalBase+"/model/readiness", nil); resp.StatusCode != 403 || before != probes.Load() {
		t.Fatal("revoked local model still probed", resp.StatusCode)
	}
	personalHTTP[pa.State](t, ts, other, "PUT", "/settings", map[string]any{"rights": map[string]string{"chat.ask": "ask"}})
	choose(other, "bea-only")
	if browser {
		if resp, _ := ts.request("POST", "/api/v1/system/disclaimer", map[string]int{"version": 1}); resp.StatusCode >= 300 {
			t.Fatal(resp.StatusCode)
		}
		for _, c := range []*http.Client{ts.client, other} {
			if resp, _ := ts.do(c, "PUT", "/api/v1/preferences/hints", map[string]bool{"webapps-0.63": true}); resp.StatusCode != 204 {
				t.Fatal(resp.StatusCode)
			}
		}
		b, err := json.Marshal(map[string]string{"origin": ts.url, "password": testPassword})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "node", "../../../scripts/test-ui-personal-model.mjs")
		cmd.Stdin = bytes.NewReader(b)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		get(ts.client, "ready", "fixture:latest")
		get(other, "missing", "bea-only")
	}
	ollama.Close()
	get(other, "offline", "")
}
