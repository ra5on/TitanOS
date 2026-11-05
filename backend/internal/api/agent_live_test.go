package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// TestAgentLiveOllama runs one question against a real Ollama server when
// TITANOS_OLLAMA is set (e.g. http://192.168.10.5:11434), and logs how much
// context the tools take. It changes nothing on the server.
func TestAgentLiveOllama(t *testing.T) {
	address := os.Getenv("TITANOS_OLLAMA")
	if address == "" {
		t.Skip("TITANOS_OLLAMA not set")
	}
	model := os.Getenv("TITANOS_OLLAMA_MODEL")
	if model == "" {
		model = "gpt-oss:20b"
	}
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	client, err := agent.NewOllama(address)
	if err != nil {
		t.Fatal(err)
	}
	settings := ts.server.agentSettings()
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), settings)
	chat := &agent.Chat{ID: agent.NewID(), Origin: "web", Entries: []agent.Entry{{ID: agent.NewID(), Role: "user", Content: os.Getenv("TITANOS_OLLAMA_ASK")}}}
	if chat.Entries[0].Content == "" {
		chat.Entries[0].Content = "Welche Freigaben gibt es und was liegt in Docs?"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	r := &agent.Runner{Ollama: client, Model: model, Think: "low", Tools: box, MaxSteps: 8,
		System: ts.server.agentPrompt(ctx, mustUser(t, ts, "alex"), settings, chat, box)}
	t.Logf("tools offered: %d, system prompt %d chars", len(box.Specs(chat)), len(r.System))
	start := time.Now()
	if err := r.Run(ctx, chat); err != nil {
		t.Fatal(err)
	}
	for _, e := range chat.Entries {
		switch e.Role {
		case "assistant":
			if e.Stats != nil {
				t.Logf("round: prompt %d tokens, output %d", e.Stats.PromptTokens, e.Stats.OutputTokens)
			}
			for _, c := range e.Calls {
				t.Logf("call %s %s", c.Function.Name, c.Function.Arguments)
			}
			if e.Content != "" {
				t.Logf("answer: %s", e.Content)
			}
		case "tool":
			t.Logf("result %s: %.200s", e.Tool, e.Result)
		}
	}
	t.Logf("took %s, state %s", time.Since(start).Round(time.Second), chat.State)
}
