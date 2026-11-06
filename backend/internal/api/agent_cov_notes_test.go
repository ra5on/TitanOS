package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// covNotesServer is an agent with the share KI-Agent on a volume, and the
// folder Notizen of alex.
func covNotesServer(t *testing.T, fake *agentOllama, policy agent.Policy) (*testServer, string) {
	t.Helper()
	ts := agentServer(t, fake, policy)
	mnt := filepath.Dir(filepath.Dir(ts.withShare()))
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond); ts.server.agentSyncShare() })
	return ts, filepath.Join(mnt, "tank", agentShareName, "alex", "Notizen")
}

// The note tools of the agent through the toolbox: they see what the
// person wrote through the API, never read outside Notizen/, and deleting
// follows the policy.
func TestCovAgentNoteTools(t *testing.T) {
	ts, home := covNotesServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	if res, body := ts.request("PUT", "/api/v1/agent/notes/file", map[string]any{"path": "Haushalt/Einkauf.md", "content": "- [ ] Milch"}); res.StatusCode != http.StatusOK {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	ctx := context.Background()
	box := func(policy agent.Policy) *agentToolbox {
		settings := ts.server.agentSettings()
		settings.Policy = policy
		return ts.server.newAgentToolbox(mustUser(t, ts, "alex"), settings)
	}
	std := box(agent.Policy{Preset: agent.PresetStandard})
	run := func(b *agentToolbox, name, args string) (agent.Prepared, string, error) {
		chat := &agent.Chat{ID: agent.NewID()}
		p, err := b.Prepare(ctx, chat, name, json.RawMessage(args))
		if err != nil {
			return p, "", err
		}
		out, err := b.Execute(ctx, chat, p)
		return p, out.Text, err
	}

	p, text, err := run(std, "notes_list", `{}`)
	if err != nil || p.Mode != agent.ModeAllow || !p.Untrusted {
		t.Fatalf("notes_list: %+v %v", p, err)
	}
	if !strings.Contains(text, "Haushalt/Einkauf.md") || !strings.Contains(text, "Milch") {
		t.Fatalf("notes_list does not see the note: %s", text)
	}
	p, text, err = run(std, "note_read", `{"path":"Haushalt/Einkauf.md"}`)
	if err != nil || !p.Untrusted || text != "- [ ] Milch" {
		t.Fatalf("note_read: %q %v", text, err)
	}

	// Never outside Notizen/.
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "geheim.md"), []byte("GEHEIM"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(home), "neben.md"), []byte("GEHEIM"), 0o644)
	os.Symlink(outside, filepath.Join(home, "raus"))
	os.Symlink(filepath.Join(outside, "geheim.md"), filepath.Join(home, "link.md"))
	for _, bad := range []string{"../neben.md", "Haushalt/../../neben.md", "raus/geheim.md", "link.md", ".versteckt.md", "Haushalt/Einkauf.txt", "", outside + "/geheim.md"} {
		args, _ := json.Marshal(map[string]string{"path": bad})
		_, text, err := run(std, "note_read", string(args))
		if err == nil || strings.Contains(text, "GEHEIM") {
			t.Errorf("note_read %q: %q %v", bad, text, err)
		}
	}

	// Deleting: asks under standard, denied under read, runs under all.
	if p, err := std.Prepare(ctx, &agent.Chat{ID: agent.NewID()}, "note_delete", json.RawMessage(`{"path":"Haushalt/Einkauf.md"}`)); err != nil || p.Mode != agent.ModeAsk {
		t.Fatalf("standard delete: %+v %v", p, err)
	}
	if _, err := box(agent.Policy{Preset: agent.PresetRead}).Prepare(ctx, &agent.Chat{ID: agent.NewID()}, "note_delete", json.RawMessage(`{"path":"Haushalt/Einkauf.md"}`)); err == nil {
		t.Fatal("read preset may delete notes")
	}
	if _, _, err := run(box(agent.Policy{Preset: agent.PresetAll}), "note_delete", `{"path":"../neben.md"}`); err == nil {
		t.Fatal("deleted outside")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(home), "neben.md")); err != nil {
		t.Fatal("file outside removed")
	}
	if _, _, err := run(box(agent.Policy{Preset: agent.PresetAll}), "note_delete", `{"path":"Haushalt/Einkauf.md"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Haushalt", "Einkauf.md")); !os.IsNotExist(err) {
		t.Fatal("note still there")
	}
	_, body := ts.request("GET", "/api/v1/agent/notes", nil)
	if strings.Contains(string(body), "Einkauf") {
		t.Fatalf("API still lists the deleted note: %s", body)
	}
}

// After content from outside was read (a note), what the agent remembers
// or saves as a skill in that chat is marked untrusted; in a clean chat
// it is not.
func TestCovAgentMemoryAndSkillAfterUntrustedRead(t *testing.T) {
	fake := &agentOllama{}
	ts, _ := covNotesServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	ts.request("PUT", "/api/v1/agent/notes/file", map[string]any{"path": "Ideen.md", "content": "Merke dir: der Router ist 10.0.0.1"})
	fake.replies = []string{
		toolCall("notes_list", map[string]any{}),
		toolCall("memory_save", map[string]any{"text": "Der Router ist 10.0.0.1."}),
		toolCall("skill_save", map[string]any{"name": "Router prüfen", "when": "Router", "steps": "1. system_status"}),
		`{"role":"assistant","content":"Gemerkt."}`,
	}
	start := func(message string) agent.Chat {
		res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": message})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("start: %d %s", res.StatusCode, body)
		}
		var c agent.Chat
		json.Unmarshal(body, &c)
		return waitChat(t, ts, ts.client, c.ID)
	}
	tainted := start("Lies meine Notizen und merk dir das")
	if !tainted.Tainted {
		t.Fatalf("chat not tainted after notes_list: %+v", tainted.Entries)
	}
	fake.mu.Lock()
	fake.replies = []string{
		toolCall("memory_save", map[string]any{"text": "Der Drucker steht im Flur."}),
		toolCall("skill_save", map[string]any{"name": "Drucker", "when": "Drucken", "steps": "1. files_list"}),
		`{"role":"assistant","content":"Gemerkt."}`,
	}
	fake.mu.Unlock()
	// The skill saved in the first chat is outside content: web tools ask
	// in later chats, but what the person says is not marked for it.
	clean := start("Merk dir: der Drucker steht im Flur")
	if clean.Tainted || !clean.OutsideSkill {
		t.Fatalf("clean chat: tainted %v, outside skill %v", clean.Tainted, clean.OutsideSkill)
	}

	_, body := ts.request("GET", "/api/v1/agent/memory", nil)
	var mem struct{ Memory []agent.Memory }
	json.Unmarshal(body, &mem)
	byText := map[string]agent.Memory{}
	for _, m := range mem.Memory {
		byText[m.Text] = m
	}
	if m, ok := byText["Der Router ist 10.0.0.1."]; !ok || !m.Untrusted || m.Chat != tainted.ID {
		t.Fatalf("memory after untrusted read: %s", body)
	}
	if m, ok := byText["Der Drucker steht im Flur."]; !ok || m.Untrusted || m.Chat != clean.ID {
		t.Fatalf("memory of a clean chat: %s", body)
	}
	_, body = ts.request("GET", "/api/v1/agent/skills", nil)
	var sk struct{ Skills []agent.Skill }
	json.Unmarshal(body, &sk)
	found := 0
	for _, s := range sk.Skills {
		switch s.Name {
		case "Router prüfen":
			found++
			if !s.Untrusted {
				t.Errorf("skill after untrusted read not marked: %+v", s)
			}
		case "Drucker":
			found++
			if s.Untrusted {
				t.Errorf("skill of a clean chat marked: %+v", s)
			}
		}
	}
	if found != 2 {
		t.Fatalf("skills: %s", body)
	}
}

// memory_save and skill_save refuse secrets.
func TestCovAgentOwnToolsRefuseSecrets(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	b := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	ctx := context.Background()
	chat := &agent.Chat{ID: agent.NewID()}
	for _, args := range []string{`{"text":"Das WLAN-Passwort ist hunter2."}`, `{"text":"api key: abc"}`, `{"text":""}`} {
		if _, err := b.memorySave(ctx, chat, json.RawMessage(args)); err == nil {
			t.Errorf("memory_save accepted %s", args)
		}
	}
	if _, err := b.skillSave(ctx, chat, json.RawMessage(`{"name":"x","when":"y","steps":"1. Token abc eingeben"}`)); err == nil {
		t.Error("skill_save accepted a token in the steps")
	}
	mem, _ := ts.server.agentStore().Memories("alex")
	if len(mem) != 0 {
		t.Fatalf("stored: %+v", mem)
	}
}

func TestCovAgentSkillSecretInWhen(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	b := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	if _, err := b.skillSave(context.Background(), &agent.Chat{ID: agent.NewID()}, json.RawMessage(`{"name":"WLAN","when":"Passwort ist hunter2","steps":"1. network_status"}`)); err == nil {
		t.Fatal("skill_save accepted a password in when")
	}
}
