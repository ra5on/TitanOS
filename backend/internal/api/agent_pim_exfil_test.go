package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// S-26 (P-1): after calendar_search or contacts_search (the chat has read
// the calendar or the contacts), writing files or notes asks first, also
// where the rights allow it without asking – an invitation could tell the
// model to copy the address book into a shared folder. Other outside
// content keeps file writes as the rights say.
func TestPimReadMakesFileAndNoteWritesAsk(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetAll})
	share := ts.withShare()
	os.WriteFile(filepath.Join(share, "kontakte.txt"), []byte("alt"), 0o644)
	os.WriteFile(filepath.Join(share, "x.txt"), []byte("x"), 0o644)
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond); ts.server.agentSyncShare() })
	settings := ts.server.agentSettings()
	settings.Policy = agent.Policy{Preset: agent.PresetAll, Areas: map[string]string{"files": agent.ModeAllow}}
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), settings)
	writes := [][2]string{
		{"file_write", `{"root":"share/Docs","path":"neu.txt","content":"Anna 0171"}`},
		{"file_write", `{"root":"share/Docs","path":"kontakte.txt","content":"Anna 0171","replace":true}`},
		{"note_write", `{"path":"Kontakte.md","content":"Anna 0171"}`},
	}
	ctx := context.Background()
	prepare := func(chat *agent.Chat, tool, args string) agent.Prepared {
		t.Helper()
		if _, ok := box.tools[tool]; !ok {
			t.Fatalf("no tool %s", tool)
		}
		p, err := box.Prepare(ctx, chat, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		return p
	}
	for _, c := range writes {
		if p := prepare(&agent.Chat{Tainted: true, PIMRead: true}, c[0], c[1]); p.Mode != agent.ModeAsk {
			t.Errorf("after reading the calendar %s %s: %s, want ask", c[0], c[1], p.Mode)
		}
		if p := prepare(&agent.Chat{Tainted: true}, c[0], c[1]); p.Mode != agent.ModeAllow {
			t.Errorf("after other outside content %s %s: %s, want allow", c[0], c[1], p.Mode)
		}
	}
	// Reading stays free.
	if p := prepare(&agent.Chat{Tainted: true, PIMRead: true}, "file_read", `{"root":"share/Docs","path":"x.txt"}`); p.Mode != agent.ModeAllow {
		t.Errorf("file_read after the calendar: %s", p.Mode)
	}
}
