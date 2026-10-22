package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/office"
)

func TestAgentWordPlanReloadAndChangedTarget(t *testing.T) {
	for _, mode := range []string{"confirm", "direct"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
				if n == 1 {
					return []string{`{"role":"assistant","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"overview.docx","content":"# Volumes\n\n| Volume | State |\n| --- | --- |\n| nvme | ok |","reason":"requested"}}}]}`}
				}
				return []string{`{"role":"assistant","content":"Document saved"}`}
			}}
			srv := fake.start(t)
			ts := newTestServer(t)
			ts.setup()
			dir := ts.withShare()
			ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": mode, "ollama": srv.URL, "model": "qwen3:8b"})
			id := ts.newChat(ts.client)
			ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]string{"text": "Write a Word overview in the first existing share"})
			if mode == "confirm" {
				c, err := ts.server.chats.Get("alex", id)
				if err != nil {
					t.Fatal(err)
				}
				a := assistant.Pending(c)
				if a == nil {
					t.Fatalf("no action: %+v", c)
				}
				res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/actions/"+a.ID, map[string]string{"state": "approved"})
				if res.StatusCode != 200 {
					t.Fatalf("%s", body)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "overview.docx"))
			if err != nil {
				t.Fatal(err)
			}
			text, err := office.DOCXText(data)
			if err != nil || !strings.Contains(text, "nvme") {
				t.Fatal(text, err)
			}
			c, _ := ts.server.chats.Get("alex", id)
			if c.Run.State != "done" {
				t.Fatal(c.Run)
			}
			found := false
			for _, e := range c.Messages {
				if e.Action != nil && e.Action.Tool == "file_write" {
					found = strings.Contains(e.Action.Result, `"state":"verified"`)
				}
			}
			if !found {
				t.Fatal("content not verified")
			}
			if len(ts.server.opts.Shares.List()) != 1 {
				t.Fatal("created unrelated share")
			}
		})
	}
}
func TestAgentChangedPlanExpiresBeforeDispatch(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	u, _ := ts.users.Get("alex")
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	ts.server.opts.Assistant.SetSettings(settings)
	args := json.RawMessage(`{"root":"share/Docs","path":"safe.txt","content":"safe"}`)
	a, err := ts.server.planWrite(u)(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	a.ID = "1234567890abcdef"
	a.Tool = "file_write"
	a.State = assistant.ActionPending
	a.Path = "/api/v1/assistant/files/move"
	c, _ := ts.server.chats.New("alex", "changed")
	c.Run = &assistant.Run{}
	c.Messages = []assistant.Entry{{Role: "tool", Tool: a.Tool, Args: json.RawMessage(`{"root":"share/Docs","path":"safe.txt","content":"safe","reason":"requested"}`), Action: a}}
	req := httptest.NewRequest("GET", "/", nil)
	if err = ts.server.executeAgentAction(context.Background(), u, c, a, req, func(c *assistant.Chat) error { return ts.server.chats.Save(u.Name, c) }); err != nil {
		t.Fatal(err)
	}
	if a.State != assistant.ActionExpired {
		t.Fatal(a.State)
	}
	if _, err = os.Stat(filepath.Join(dir, "safe.txt")); !os.IsNotExist(err) {
		t.Fatal("dispatched changed target")
	}
}
func TestSameJSONPrecisionAndOrder(t *testing.T) {
	if !sameJSON([]byte(`{"a":1,"b":{"x":2,"y":3}}`), []byte(`{"b":{"y":3,"x":2},"a":1}`)) {
		t.Fatal("order matters")
	}
	if sameJSON([]byte(`{"a":9007199254740993}`), []byte(`{"a":9007199254740992}`)) {
		t.Fatal("precision lost")
	}
}
func TestAutomationDispatchResumeOverlapAndIsolation(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	u, _ := ts.users.Get("alex")
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	ts.server.opts.Assistant.SetSettings(settings)
	now := time.Now()
	a, err := ts.server.automations.Put("alex", assistant.Automation{Name: "Report", Prompt: "Inspect storage", Enabled: true, Trigger: assistant.Trigger{Kind: "interval", Minutes: 1}, Overlap: "queue"}, now)
	if err != nil {
		t.Fatal(err)
	}
	ts.server.scanAutomations(context.Background(), u, now.Add(time.Minute))
	list, _ := ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 || list[0].Runs[0].State != "starting" {
		t.Fatalf("not reserved: %+v", list)
	}
	// Recreate the storage manager, simulating a scheduler restart between claim and chat.
	ts.server.automations = assistant.NewAutomations(ts.server.chatsDir)
	ts.server.scanAutomations(context.Background(), u, now.Add(2*time.Minute))
	list, _ = ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 || !list[0].Pending {
		t.Fatalf("duplicate or missing queue: %+v", list)
	}
	c, err := ts.server.chats.Get("alex", list[0].Runs[0].Chat)
	if err != nil || c.Automation != a.ID || c.Messages[0].Content != "Inspect storage" {
		t.Fatal(c, err)
	}
	if _, err = ts.server.chats.Get("bob", c.ID); err == nil {
		t.Fatal("cross-account chat")
	}
	c.Run.State = "done"
	ts.server.chats.Save("alex", c)
	ts.server.scanAutomations(context.Background(), u, now.Add(2*time.Minute+time.Second))
	list, _ = ts.server.automations.List("alex")
	if len(list[0].Runs) != 2 || list[0].Runs[0].State != "done" {
		t.Fatal(list)
	}
}
func TestAutomationFolderEventsAndCondition(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	u, _ := ts.users.Get("alex")
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	ts.server.opts.Assistant.SetSettings(settings)
	now := time.Now()
	a, err := ts.server.automations.Put("alex", assistant.Automation{Name: "Folder", Prompt: "Summarize new files", Enabled: true, Trigger: assistant.Trigger{Kind: "event", Event: "files", Root: "share/Docs"}, Conditions: assistant.Conditions{Volume: "missing"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	ts.server.scanAutomations(context.Background(), u, now)
	os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("fixture"), 0600)
	ts.server.scanAutomations(context.Background(), u, now.Add(time.Minute))
	list, _ := ts.server.automations.List("alex")
	if len(list[0].Runs) != 0 || !list[0].Pending || list[0].Detail == "" {
		t.Fatal(list)
	}
	a = list[0]
	a.Conditions.Volume = ""
	if _, err = ts.server.automations.Put("alex", a, now); err != nil {
		t.Fatal(err)
	}
	ts.server.scanAutomations(context.Background(), u, now.Add(2*time.Minute))
	list, _ = ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 {
		t.Fatal(list)
	}
	ts.server.scanAutomations(context.Background(), u, now.Add(3*time.Minute))
	list, _ = ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 {
		t.Fatal("unchanged folder triggered again")
	}
}
func TestSettingsChangeCancelsWaitingModel(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			w.Write([]byte(`{"capabilities":["tools"]}`))
			return
		}
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer ollama.Close()
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	settings := map[string]any{"enabled": true, "execution": "confirm", "ollama": ollama.URL, "model": "qwen3:8b"}
	ts.request("PUT", "/api/v1/assistant", settings)
	id := ts.newChat(ts.client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]string{"text": "inspect"})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no model request")
	}
	settings["execution"] = "direct"
	ts.request("PUT", "/api/v1/assistant", settings)
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("mode change left model hanging")
	}
	<-done
	c, _ := ts.server.chats.Get("alex", id)
	if c.Run.State != "queued" {
		t.Fatal(c.Run)
	}
}

func TestAutomationCreatedByAgentThenRunsDocument(t *testing.T) {
	fake := &fakeChatOllama{next: func(n int, b map[string]any) []string {
		switch n {
		case 1:
			return []string{`{"role":"assistant","tool_calls":[{"function":{"name":"automation_save","arguments":{"automation":{"name":"Weekly report","prompt":"Write report.docx in share/Docs with a volume overview","enabled":true,"execution":"direct","trigger":{"kind":"interval","minutes":60}},"reason":"User requested a scheduled report"}}}]}`}
		case 3:
			return []string{`{"role":"assistant","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"report.docx","content":"# Volumes\n\nnvme: ok (fixture)","reason":"Scheduled report"}}}]}`}
		default:
			return []string{`{"role":"assistant","content":"Completed"}`}
		}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "direct", "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]string{"text": "Create an hourly document automation"})
	list, err := ts.server.automations.List("alex")
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	u, _ := ts.users.Get("alex")
	now := list[0].Next
	ts.server.scanAutomations(context.Background(), u, now)
	ts.server.scanAutomations(context.Background(), u, now.Add(time.Second))
	list, _ = ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 {
		t.Fatal(list)
	}
	c, err := ts.server.chats.Get("alex", list[0].Runs[0].Chat)
	if err != nil {
		t.Fatal(err)
	}
	release, err := ts.server.chats.Reserve("alex", c.ID)
	if err != nil {
		t.Fatal(err)
	}
	job := ts.server.startAgent(u, c, "", "", release)
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("automation stuck")
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.docx"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := office.DOCXText(data)
	if err != nil || !strings.Contains(content, "nvme") {
		t.Fatal(content, err)
	}
	ts.server.scanAutomations(context.Background(), u, now.Add(2*time.Second))
	list, _ = ts.server.automations.List("alex")
	if list[0].Runs[0].State != "done" {
		t.Fatal(list)
	}
}

func TestAutomationMissedSchedulesAndRevokedArea(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	u, _ := ts.users.Get("alex")
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	ts.server.opts.Assistant.SetSettings(settings)
	now := time.Now()
	for _, mode := range []string{"skip", "latest"} {
		_, err := ts.server.automations.Put("alex", assistant.Automation{Name: mode, Prompt: "Inspect", Enabled: true, CatchUp: mode, Trigger: assistant.Trigger{Kind: "once", At: now.Add(-2 * time.Hour)}}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	ts.server.scanAutomations(context.Background(), u, now)
	list, _ := ts.server.automations.List("alex")
	for _, a := range list {
		if a.Name == "skip" && len(a.Runs) != 0 || a.Name == "latest" && len(a.Runs) != 1 || a.Enabled {
			t.Fatalf("catchup: %+v", a)
		}
	}
	a, err := ts.server.automations.Put("alex", assistant.Automation{Name: "Private files", Prompt: "Read files", Enabled: true, Trigger: assistant.Trigger{Kind: "event", Event: "files", Root: "share/Docs"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	settings.Areas = map[string]string{"files": "off"}
	ts.server.opts.Assistant.SetSettings(settings)
	ts.server.scanAutomations(context.Background(), u, now)
	list, _ = ts.server.automations.List("alex")
	for _, v := range list {
		if v.ID == a.ID && (v.EventReady || v.Detail != "event_access_denied") {
			t.Fatalf("observed forbidden root: %+v", v)
		}
	}
}

func TestDirectModeQueuesOnlyWaitingConfirmations(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "confirm"})
	ids := map[string]string{}
	for _, kind := range []string{"confirmation", "paused", "stopped", "dry", "automation"} {
		c, _ := ts.server.chats.New("alex", kind)
		c.Run = &assistant.Run{State: kind}
		if kind == "dry" {
			c.Run.State = "confirmation"
			c.DryRun = true
		}
		if kind == "automation" {
			c.Run.State = "confirmation"
			c.Automation = "id"
			c.Execution = "confirm"
		}
		ts.server.chats.Save("alex", c)
		ids[kind] = c.ID
	}
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "direct"})
	for kind, id := range ids {
		c, _ := ts.server.chats.Get("alex", id)
		want := kind
		if kind == "confirmation" {
			want = "queued"
		}
		if kind == "dry" || kind == "automation" {
			want = "confirmation"
		}
		if c.Run.State != want {
			t.Fatalf("%s: %+v", kind, c.Run)
		}
	}
}

func TestAutomationDismissedNotificationDoesNotReplay(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	u, _ := ts.users.Get("alex")
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	ts.server.opts.Assistant.SetSettings(settings)
	now := time.Now()
	_, err := ts.server.automations.Put("alex", assistant.Automation{Name: "Backup failures", Prompt: "Inspect backups", Enabled: true, Trigger: assistant.Trigger{Kind: "event", Event: "notification", Target: "backup_failed"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	ts.server.opts.Notify.Notify("error", "backup_failed", "first")
	ts.server.opts.Notify.Notify("error", "backup_failed", "second")
	ts.server.scanAutomations(context.Background(), u, now)
	newest := ts.server.opts.Notify.Messages()[0]
	ts.server.opts.Notify.Dismiss([]int{newest.ID})
	ts.server.scanAutomations(context.Background(), u, now.Add(time.Minute))
	list, _ := ts.server.automations.List("alex")
	if len(list[0].Runs) != 0 {
		t.Fatal("dismissal triggered older message")
	}
	ts.server.opts.Notify.Notify("error", "backup_failed", "third")
	ts.server.scanAutomations(context.Background(), u, now.Add(2*time.Minute))
	list, _ = ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 {
		t.Fatal("new message not dispatched")
	}
}

func TestAutomationCanPauseAfterAccessRevoked(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	ts.server.opts.Assistant.SetSettings(settings)
	a, err := ts.server.automations.Put("alex", assistant.Automation{Name: "Folder", Prompt: "Inspect files", Enabled: true, Trigger: assistant.Trigger{Kind: "event", Event: "files", Root: "share/Docs"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	settings.Areas = map[string]string{"files": "off"}
	ts.server.opts.Assistant.SetSettings(settings)
	a.Enabled = false
	res, body := ts.request("POST", "/api/v1/assistant/automations", a)
	if res.StatusCode != 200 {
		t.Fatalf("cannot pause: %d %s", res.StatusCode, body)
	}
	// Run now still checks the current permissions even for a disabled schedule.
	ts.request("POST", "/api/v1/assistant/automations/"+a.ID+"/run", nil)
	u, _ := ts.users.Get("alex")
	ts.server.scanAutomations(context.Background(), u, time.Now())
	list, _ := ts.server.automations.List("alex")
	if len(list[0].Runs) != 0 || list[0].Detail == "" {
		t.Fatal("manual run bypassed access", list)
	}
}
