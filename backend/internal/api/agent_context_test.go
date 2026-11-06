package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 59a: the agent knows the window in the first round. A model that is
// not loaded is loaded first and then read from /api/ps; without an answer
// there the model's own length counts.
func TestAgentProbeLoadsModel(t *testing.T) {
	fake := &agentOllama{loadedContext: 65536}
	client, err := agent.NewOllama(fake.start(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := agentProbe(client, "gpt-oss:20b", "-1")(context.Background()); n != 65536 || fake.preloads != 1 {
		t.Fatalf("window %d, preloads %d", n, fake.preloads)
	}
	// Loaded already: no second load.
	if n := agentProbe(client, "gpt-oss:20b", "-1")(context.Background()); n != 65536 || fake.preloads != 1 {
		t.Fatalf("window %d, preloads %d", n, fake.preloads)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			w.Write([]byte(`{"capabilities":["tools"],"model_info":{"gemma4.context_length":262144}}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[]}`))
		default:
			w.Write([]byte(`{"done":true}`))
		}
	}))
	defer srv.Close()
	other, _ := agent.NewOllama(srv.URL)
	if n := agentProbe(other, "gemma4:12b", "")(context.Background()); n != agentGuessContext {
		t.Fatalf("fallback %d", n)
	}
}

func TestSameModelContext(t *testing.T) {
	s := agentSettings{Model: "gemma4:12b", Context: 32768}
	if sameModelContext(s, "", "gemma4:12b") != 32768 || sameModelContext(s, "", "llava:latest") != 0 || sameModelContext(s, "llava", "llava:latest") != 32768 {
		t.Fatal("context for picture model")
	}
}

// PLAN 59: the agent's pages come with the translucent status bar in the
// HTML itself (iOS reads it at load); other pages keep the black bar.
func TestAgentIndexStatusBar(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte(`<meta name="viewport" content="width=device-width, initial-scale=1" /><meta name="apple-mobile-web-app-status-bar-style" content="black" />`)}}
	h := uiHandler(ui)
	get := func(p string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec.Body.String()
	}
	if b := get("/agent?chat=x"); !strings.Contains(b, `content="black-translucent"`) || !strings.Contains(b, "viewport-fit=cover") {
		t.Fatalf("agent page: %s", b)
	}
	if b := get("/settings"); strings.Contains(b, "translucent") || strings.Contains(b, "viewport-fit") {
		t.Fatalf("TitanOS page: %s", b)
	}
	if b := get("/agentx"); strings.Contains(b, "translucent") {
		t.Fatal("not an agent path")
	}
}
