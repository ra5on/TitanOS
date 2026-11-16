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
	"github.com/ra5on/TitanOS/backend/internal/docindex"
)

// Review 1.02: the name of a reminder was cut at 79 characters plus "…",
// but a schedule takes 80 bytes: a long or umlaut-rich file name was always
// refused, and in "Gleich als Erinnerung" the deadline was never planned.
func TestDocsRemindLongName(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	now := time.Now().In(ts.server.agentLocation())
	date := now.AddDate(0, 1, 0)
	for _, p := range []string{
		"Dokumente/Verträge/2026-11-03 Mobilfunkvertrag Kündigungsbestätigung Telekom Deutschland GmbH.pdf",
		"Dokumente/Kündigung Größe Übergabe.pdf",
	} {
		item := deadlineItem("share/Docs", p, 1, docindex.Deadline{Kind: docindex.KindCancel, Date: date, Snippet: "Kündigung bis"}, "", "")
		if err := ts.server.docsRemind("alex", item, []int{14}, now); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	list, err := ts.server.agentStore().Schedules("alex")
	if err != nil || len(list) != 2 {
		t.Fatalf("schedules: %v %d", err, len(list))
	}
	for _, sc := range list {
		if len(sc.Name) > 80 || strings.Contains(sc.Name, "…") || !strings.HasPrefix(sc.Name, "Frist: ") {
			t.Fatalf("name %q", sc.Name)
		}
	}
}

// Review 1.02: a run that hit its time limit (2 h, a large first run, a
// slow picture model) took every file it had not reached out of the index
// and marked the folders as read; the next night skipped them.
func TestDocsCancelledRunKeepsTheIndex(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true}})
	os.WriteFile(filepath.Join(home, "Dokumente", "Vertrag.txt"), []byte("Kfz-Versicherung HUK"), 0o644)
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	at := time.Now()
	if err := ts.server.docsRun(context.Background(), alex, m.Docs, true, at); err != nil {
		t.Fatal(err)
	}
	if hits, _ := ts.server.docsSearch(alex, "Kfz-Versicherung", 10, nil); len(hits) != 1 {
		t.Fatalf("not indexed: %d", len(hits))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = ts.server.docsRun(ctx, alex, m.Docs, true, at.Add(time.Hour))
	if hits, _ := ts.server.docsSearch(alex, "Kfz-Versicherung", 10, nil); len(hits) != 1 {
		t.Fatalf("index lost by a cancelled run: %d hits", len(hits))
	}
	var st docsState
	ts.server.docsRead("alex", docsStateFile, &st, maxDocsState)
	for k, f := range st.Folders {
		if f.LastRun.After(at) {
			t.Fatalf("%s marked as read by a cancelled run: %+v", k, *f)
		}
	}
}

// Review 1.02: a question ("Zuordnen") about a file that was deleted in
// the file manager stayed for good and kept every later file of the same
// name (scan.pdf of a scanner) from being read; so did a file replaced
// under the same name.
func TestPaperQuestionAboutAGoneFile(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"paper": map[string]any{"on": true}})
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	inbox := filepath.Join(home, paperInbox)
	os.MkdirAll(inbox, 0o755)
	letter, err := os.ReadFile("../docindex/testdata/briefe/stadtwerke.txt")
	if err != nil {
		t.Fatal(err)
	}
	unclear := func(at time.Time) {
		os.WriteFile(filepath.Join(inbox, "scan.txt"), []byte("Bitte bringen Sie die Unterlagen mit."), 0o644)
		os.Chtimes(filepath.Join(inbox, "scan.txt"), at, at)
	}
	clear := func(at time.Time) {
		os.WriteFile(filepath.Join(inbox, "scan.txt"), letter, 0o644)
		os.Chtimes(filepath.Join(inbox, "scan.txt"), at, at)
	}
	asks := func() int {
		lg, _ := ts.server.paperLoad("alex")
		n := 0
		for _, e := range lg.Entries {
			if e.State == "ask" {
				n++
			}
		}
		return n
	}
	old := time.Now().Add(-10 * time.Minute)
	// Deleted, then a clear letter under the same name.
	unclear(old)
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	if asks() != 1 {
		t.Fatal("no question")
	}
	os.Remove(filepath.Join(inbox, "scan.txt"))
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	if n := asks(); n != 0 {
		t.Fatalf("question about a gone file stayed: %d", n)
	}
	// Replaced between two runs: read anew.
	unclear(old)
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	clear(old.Add(time.Minute))
	ts.server.paperRun(context.Background(), alex, m, false, time.Now())
	if _, err := os.Stat(filepath.Join(inbox, "scan.txt")); err == nil {
		t.Fatal("the new scan.txt was never read")
	}
}

// Review 1.02: a bill the paperwork filed offered its due date, and the
// index offered it a second time once it read the filed letter (the
// default filing folder lies inside the default index folder).
func TestDocsBillDueOfferedOnce(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "deadlines": true, "paper": true})
	docsMine(t, ts, ts.client, map[string]any{"docs": map[string]any{"on": true, "deadlines": true}, "paper": map[string]any{"on": true}})
	alex, _ := ts.users.Get("alex")
	m, _ := ts.server.agentFeaturesOf("alex")
	inbox := filepath.Join(home, paperInbox)
	os.MkdirAll(inbox, 0o755)
	letter, err := os.ReadFile("../docindex/testdata/briefe/stadtwerke.txt")
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().AddDate(0, 0, 9).Format("02.01.2006")
	letter = []byte(strings.Replace(string(letter), "15.11.2026", due, 1))
	old := time.Now().Add(-5 * time.Minute)
	os.WriteFile(filepath.Join(inbox, "scan1.txt"), letter, 0o644)
	os.Chtimes(filepath.Join(inbox, "scan1.txt"), old, old)
	if err := ts.server.paperRun(context.Background(), alex, m, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := len(ts.server.agentInbox("alex")); n != 1 {
		t.Fatalf("paperwork offered %d", n)
	}
	if err := ts.server.docsRun(context.Background(), alex, m.Docs, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if items := ts.server.agentInbox("alex"); len(items) != 1 {
		t.Fatalf("deadline offered %d times: %+v", len(items), items)
	}
}

// Review 1.02: deleting an account left its own settings of the agent
// (with the password of its mailbox) and its document memory; a new
// account of the same name took them over, and the paperwork fetched the
// former person's mail into the new person's folder.
func TestDeletedAccountLeavesNoDocuments(t *testing.T) {
	ts, home := docsServer(t)
	docsNAS(t, ts, map[string]any{"enabled": true, "paper": true})
	ts.users.Create("gast", testPassword, "user")
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	ts.server.agentSyncShare()
	gastHome := filepath.Join(filepath.Dir(home), "gast")
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	docsMine(t, ts, guest, map[string]any{"docs": map[string]any{"on": true}, "paper": map[string]any{"on": true, "mail": map[string]any{"enabled": true, "host": "imap.example.org", "user": "gast", "password": "alt-geheim-1"}}})
	os.MkdirAll(filepath.Join(gastHome, "Dokumente"), 0o755)
	os.WriteFile(filepath.Join(gastHome, "Dokumente", "Arzt.txt"), []byte("Befund Diagnose Zahnarzt vertraulich"), 0o644)
	g, _ := ts.users.Get("gast")
	m, _ := ts.server.agentFeaturesOf("gast")
	if err := ts.server.docsRun(context.Background(), g, m.Docs, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	// The clearing runs in the background.
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if m, _ := ts.server.agentFeaturesOf("gast"); m.Paper.Mail.Password == "" {
			break
		}
	}
	ts.users.Create("gast", testPassword, "user")
	m2, _ := ts.server.agentFeaturesOf("gast")
	if m2.Paper.Mail.Password != "" || m2.Paper.On || m2.Docs.On {
		t.Fatalf("settings taken over: %+v", m2)
	}
	var idx docindex.Index
	ts.server.docsRead("gast", docsIndexFile, &idx, maxDocsIndex)
	if _, kept := idx.Get("share/KI-Agent", "Dokumente/Arzt.txt"); kept {
		t.Fatal("document memory taken over")
	}
}
