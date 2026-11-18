package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// TestAgentEvalMail is 114-1 against a real model (owner 19 Nov 2026, chat
// "E Mail Versenden Nicht Möglich" with gpt-oss:20b: "Kannst du eine
// email an … senden" → "Ich kann leider keine E-Mails für dich
// versenden", without a single tool call). The account has Mail and a
// mailbox; the agent must ask for what is missing or write the mail, never
// say it cannot. Runs only with TITANOS_OLLAMA, like TestAgentEval:
//
//	TITANOS_OLLAMA=http://titan:11434 TITANOS_OLLAMA_MODEL=gpt-oss:20b \
//	  go test ./internal/api -run TestAgentEvalMail -v -timeout 30m
func TestAgentEvalMail(t *testing.T) {
	address := os.Getenv("TITANOS_OLLAMA")
	if address == "" {
		t.Skip("TITANOS_OLLAMA not set")
	}
	model := os.Getenv("TITANOS_OLLAMA_MODEL")
	if model == "" {
		model = "gpt-oss:20b"
	}
	client, err := agent.NewOllama(address)
	if err != nil {
		t.Fatal(err)
	}
	refusal := []string{"kann leider keine", "keine möglichkeit", "kann keine e-mail", "kann ich nicht", "nicht möglich", "kann ich leider nicht"}
	cases := []struct {
		name, ask string
		// send: the mail must come out as a call of mail_send or
		// mail_draft; otherwise a question back is enough.
		send bool
		// off: Mail is not switched on for the account; the answer
		// names that and where to change it.
		off bool
	}{
		{"ask", "Kannst du eine email an joost@example.org senden", false, false},
		{"off", "Kannst du eine email an joost@example.org senden", false, true},
		{"send", "Schick eine Mail an joost@example.org mit dem Betreff Test und dem Text: Hallo, das ist ein Test.", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newAgentMailEnv(t)
			if c.off {
				e.ts.server.opts.Mail = nil
			}
			user := mustUser(t, e.ts, "alex")
			settings := e.ts.server.agentSettings()
			box := e.ts.server.newAgentToolbox(user, settings)
			chat := &agent.Chat{ID: agent.NewID(), Origin: "web"}
			chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), Role: "user", Content: c.ask})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			r := &agent.Runner{Ollama: client, Model: model, Tools: box, MaxSteps: 15, Context: evalContext(), KeepAlive: os.Getenv("TITANOS_EVAL_KEEPALIVE"),
				System: e.ts.server.agentPrompt(ctx, user, settings, chat, box)}
			agentEvalTune(r)
			runErr := r.Run(ctx, chat)
			for i := 0; i < 4 && runErr == nil && chat.Pending != nil; i++ {
				runErr = r.Decide(ctx, chat, chat.Pending.ID, true)
			}
			if runErr != nil {
				t.Fatal(runErr)
			}
			answer, tools := "", []string{}
			for _, en := range chat.Entries {
				switch en.Role {
				case "tool":
					tools = append(tools, en.Tool)
					if testing.Verbose() {
						t.Logf("  %s %.200s → %.300s", en.Tool, en.Args, en.Result)
					}
				case "assistant":
					if strings.TrimSpace(en.Content) != "" {
						answer = en.Content
					}
				}
			}
			t.Logf("tools %v\nanswer %.400s", tools, answer)
			low := strings.ToLower(answer)
			for _, r := range refusal {
				if !c.off && strings.Contains(low, r) {
					t.Fatalf("the agent says it cannot (%q)", r)
				}
			}
			wrote := false
			for _, tl := range tools {
				if tl == "mail_send" || tl == "mail_draft" {
					wrote = true
				}
			}
			if c.off {
				if !strings.Contains(low, "konto") && !strings.Contains(low, "einstellung") {
					t.Fatalf("the answer does not say why Mail is closed or where to open it")
				}
				return
			}
			if c.send && !wrote {
				t.Fatalf("no mail_send or mail_draft")
			}
			cards := 0
			for _, en := range chat.Entries {
				if en.Role == "tool" && en.Draft != nil {
					cards++
				}
			}
			if cards > 1 {
				t.Fatalf("%d cards for one mail", cards)
			}
			if !c.send && !wrote && !strings.Contains(answer, "?") && !strings.Contains(low, "betreff") {
				t.Fatalf("neither a mail nor a question back")
			}
		})
	}
}
