package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 57: notes as Markdown files with folders below KI-Agent/<konto>/
// Notizen; moving, pinning, deleting; no way out of the folder; the agent
// writes with its rights.
func TestAgentNotes(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	mnt := filepath.Dir(filepath.Dir(ts.withShare()))
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond); ts.server.agentSyncShare() })
	home := filepath.Join(mnt, "tank", agentShareName, "alex", "Notizen")

	put := func(p, content string, modified time.Time) (int, map[string]any) {
		res, body := ts.request("PUT", "/api/v1/agent/notes/file", map[string]any{"path": p, "content": content, "modified": modified})
		var out map[string]any
		json.Unmarshal(body, &out)
		return res.StatusCode, out
	}
	if code, _ := put("Haushalt/Einkauf.md", "- [ ] Milch\n- [ ] Brot", time.Time{}); code != http.StatusOK {
		t.Fatalf("save: %d", code)
	}
	if code, _ := put("Ideen.md", "# Ideen\nGarten neu", time.Time{}); code != http.StatusOK {
		t.Fatal(code)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "Haushalt", "Einkauf.md")); !strings.Contains(string(data), "Milch") {
		t.Fatalf("file: %q", data)
	}
	for _, bad := range []string{"../x.md", "a/../../x.md", ".versteckt.md", "x.txt", "a/b/c/d/e/f.md", "a\x01.md"} {
		if code, _ := put(bad, "x", time.Time{}); code != http.StatusBadRequest {
			t.Fatalf("%q accepted: %d", bad, code)
		}
	}
	// A link someone put in the folder leads nowhere.
	os.Symlink("/etc", filepath.Join(home, "raus"))
	if code, _ := put("raus/passwd.md", "x", time.Time{}); code == http.StatusOK {
		t.Fatal("wrote through a link")
	}

	res, body := ts.request("POST", "/api/v1/agent/notes/folders", map[string]any{"path": "Arbeit"})
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("folder: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/agent/notes/move", map[string]any{"from": "Ideen.md", "to": "Arbeit/Ideen.md"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/notes/move", map[string]any{"from": "Arbeit", "to": "Arbeit/Neu"}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("folder moved into itself")
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/notes/pin", map[string]any{"path": "Arbeit/Ideen.md", "pinned": true}); res.StatusCode != http.StatusNoContent {
		t.Fatal("pin")
	}
	// Renaming the folder keeps the pin.
	if res, _ := ts.request("POST", "/api/v1/agent/notes/move", map[string]any{"from": "Arbeit", "to": "Projekte"}); res.StatusCode != http.StatusNoContent {
		t.Fatal("rename folder")
	}
	_, body = ts.request("GET", "/api/v1/agent/notes", nil)
	var list struct {
		Folders []string
		Notes   []noteInfo
	}
	json.Unmarshal(body, &list)
	if strings.Join(list.Folders, ",") != "Haushalt,Projekte" || len(list.Notes) != 2 {
		t.Fatalf("list: %s", body)
	}
	var ideas noteInfo
	for _, n := range list.Notes {
		if n.Title == "Ideen" {
			ideas = n
		}
	}
	if ideas.Path != "Projekte/Ideen.md" || !ideas.Pinned || ideas.Folder != "Projekte" || !strings.Contains(ideas.Preview, "Garten neu") {
		t.Fatalf("note %+v", ideas)
	}

	// Saving over a change made elsewhere is refused.
	_, body = ts.request("GET", "/api/v1/agent/notes/file?path="+url.QueryEscape("Projekte/Ideen.md"), nil)
	var file struct {
		Content  string
		Modified time.Time
	}
	json.Unmarshal(body, &file)
	if file.Content != "# Ideen\nGarten neu" {
		t.Fatalf("read %s", body)
	}
	os.WriteFile(filepath.Join(home, "Projekte", "Ideen.md"), []byte("über SMB geändert"), 0o644)
	os.Chtimes(filepath.Join(home, "Projekte", "Ideen.md"), time.Now(), time.Now().Add(time.Minute))
	if code, _ := put("Projekte/Ideen.md", "alt", file.Modified); code != http.StatusConflict {
		t.Fatalf("conflict not seen: %d", code)
	}

	// The agent: a new note without asking, appending asks (standard).
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	chat := &agent.Chat{ID: agent.NewID()}
	ctx := context.Background()
	p, err := box.Prepare(ctx, chat, "note_write", json.RawMessage(`{"path":"Haushalt/Baumarkt","content":"- [ ] Schrauben"}`))
	if err != nil || p.Mode != agent.ModeAllow {
		t.Fatalf("new note: %+v %v", p, err)
	}
	if _, err := box.Execute(ctx, chat, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Haushalt", "Baumarkt.md")); err != nil {
		t.Fatal("agent note missing")
	}
	p, err = box.Prepare(ctx, chat, "note_write", json.RawMessage(`{"path":"Haushalt/Einkauf.md","content":"- [ ] Eier","mode":"append"}`))
	if err != nil || p.Mode != agent.ModeAsk {
		t.Fatalf("append: %+v %v", p, err)
	}
	if _, err := box.Execute(ctx, chat, p); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "Haushalt", "Einkauf.md")); !strings.HasSuffix(string(data), "- [ ] Brot\n- [ ] Eier") {
		t.Fatalf("appended: %q", data)
	}
	if p, _ := box.Prepare(ctx, chat, "note_delete", json.RawMessage(`{"path":"Haushalt/Einkauf.md"}`)); p.Mode != agent.ModeAsk {
		t.Fatalf("delete without asking: %+v", p)
	}

	if res, _ := ts.request("DELETE", "/api/v1/agent/notes?path=Projekte", nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete folder")
	}
	if _, err := os.Stat(filepath.Join(home, "Projekte")); !os.IsNotExist(err) {
		t.Fatal("folder still there")
	}
	if _, err := os.Stat("/etc/passwd.md"); err == nil {
		t.Fatal("wrote outside")
	}
}
