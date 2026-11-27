package api

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func TestPersonalAgentAttachmentsAndSearchStayWithOwner(t *testing.T) {
	ts := personalFixture(t)
	_, err := ts.users.Create("bea", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	body := []byte("%PDF-1.4\nfixture bytes")
	src := personalHTTP[pa.Source](t, ts, ts.client, "POST", "/attachments?name=Rechnung.pdf", body)
	src2 := personalHTTP[pa.Source](t, ts, ts.client, "POST", "/attachments?name=Rechnung.pdf", body)
	if src.ID != src2.ID || src.Kind != "attachment" {
		t.Fatal(src, src2)
	}
	type sources struct{ Sources []pa.Source }
	if got := personalHTTP[sources](t, ts, guest, "GET", "/attachments", nil); len(got.Sources) != 0 {
		t.Fatal(got)
	}
	route := personalBase + "/attachments/" + src.ID
	if res, b := ts.request("GET", route+"/content", nil); res.StatusCode != 200 || !bytes.Equal(b, body) || res.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal(res.StatusCode, res.Header, b)
	}
	if res, _ := ts.do(guest, "GET", route+"/content", nil); res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	if res, _ := ts.do(guest, "POST", route+"/delete", map[string]string{"confirm": src.ID}); res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	req := pa.Request{Tool: "agent.example", Prompt: "mein-eigener-fund", RequestID: "search-test"}
	personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	workOne(t, ts.server)
	type search struct{ Jobs []struct{ ID string } }
	if got := personalHTTP[search](t, ts, ts.client, "GET", "/search?q=eigener-fund", nil); len(got.Jobs) != 1 {
		t.Fatal(got)
	}
	if got := personalHTTP[search](t, ts, guest, "GET", "/search?q=eigener-fund", nil); len(got.Jobs) != 0 {
		t.Fatal(got)
	}
	personalHTTP[map[string]bool](t, ts, ts.client, "POST", "/attachments/"+src.ID+"/delete", map[string]string{"confirm": src.ID})
	if res, _ := ts.request("GET", route+"/content", nil); res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	reimported := personalHTTP[pa.Source](t, ts, ts.client, "POST", "/attachments?name=Rechnung.pdf", body)
	if reimported.ID == src.ID {
		t.Fatal("deleted source consent was resurrected")
	}
}
func TestPersonalAgentGrantRevisionSurvivesMissingVolume(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	tID := "agent.example"
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{tID: "ask"}})
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tID, RequestID: "before-external-grant"})
	workOne(t, s)
	j = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
	if j.Token == "" {
		t.Fatal(j)
	}
	dir := s.opts.PersonalDataDir
	bad := filepath.Join(t.TempDir(), "disconnected")
	if err := os.WriteFile(bad, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	s.opts.PersonalDataDir = bad
	s.personalEntitlementsChanged("alex")
	s.opts.PersonalDataDir = dir
	s.initPersonalTools()
	if r, _ := ts.request("POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token}); r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	j = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
	if j.Token != "" || j.Status != "failed" {
		t.Fatal(j)
	}
}
func TestPersonalAgentCancelAndPauseDoNotResurrect(t *testing.T) {
	ts := personalFixture(t)
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: "agent.example", RequestID: "cancel-now"})
	j = personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/cancel", nil)
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{"agent.example": "ask"}})
	if j.Status != "canceled" || len(j.Audit) != 2 {
		t.Fatal(j)
	}
	if lease, e := ts.server.personalClaim(); e != nil || lease != "" {
		t.Fatal(lease, e)
	}
	res, _ := ts.request("POST", personalBase+"/jobs/"+j.ID+"/resume", nil)
	if res.StatusCode != 409 {
		t.Fatal(res.StatusCode)
	}
}
func TestPersonalAgentMailWatchTakesOnlyNewExactSender(t *testing.T) {
	ts, mail, _ := newMailServer(t)
	s := ts.server
	s.opts.PersonalDataDir = t.TempDir()
	ts.grantMail("alex")
	mb := ts.addMailbox(ts.client, mail)
	u, _ := ts.users.Get("alex")
	ts.request("PUT", personalBase+"/limits/alex", map[string]any{"rights": map[string]string{"mail.watch": "auto"}})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{"mail.watch": "auto", "mail.summarize": "ask"}})
	deliver := func(from, id string) {
		mail.Deliver("INBOX", "From: "+from+"\r\nTo: owner@example.org\r\nSubject: test\r\nMessage-ID: <"+id+"@example.org>\r\nDate: Thu, 26 Nov 2026 10:00:00 +0000\r\n\r\nfixture")
	}
	deliver("invoice@example.org", "before")
	sk := pa.Skill{ID: "invoice-watch-00001", Title: "Rechnung", Steps: []string{"mail.summarize"}, Trigger: "mail", Approval: "inherit", Sender: "invoice@example.org", PDFName: "Anlage.pdf", Sources: []pa.Source{{Kind: "mailbox", ID: "box", Ref: map[string]string{"account": mb, "folder": "INBOX"}}}}
	personalHTTP[pa.Skill](t, ts, ts.client, "POST", "/skills", sk)
	deliver("invoice@example.org.example", "partial")
	deliver("invoice@example.org", "wanted")
	poll := func() {
		if err := s.personalTxn(u, true, func(a *personalAccount) error {
			w := a.Watches[sk.ID]
			w.After = time.Now().Add(-time.Minute)
			a.Watches[sk.ID] = w
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.personalPollWatches(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	s.initPersonalTools()
	poll()
	js := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil).Jobs
	if len(js) != 1 || len(js[0].Request.Sources) != 2 || !strings.Contains(js[0].Request.Sources[0].Ref["messageId"], "wanted@") || js[0].Request.Sources[1].Ref["filename"] != "Anlage.pdf" {
		b, _ := json.Marshal(js)
		t.Fatal(string(b))
	}
	if sent := mail.Sent(); len(sent) != 0 {
		t.Fatal("unexpected mail send")
	}
}

func TestPersonalAgentRoleChangeHidesPreviousAndQueuedWork(t *testing.T) {
	ts := personalFixture(t)
	_, err := ts.users.Create("sam", testPassword, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	sam := ts.newClient()
	login := func() {
		res, b := ts.do(sam, "POST", "/api/v1/login", credentials{Name: "sam", Password: testPassword})
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode, string(b))
		}
	}
	login()
	tool := personalTool{ID: "fixture.role", App: "system", Title: "Status", Default: pa.Auto, Available: func(u auth.User) error {
		if !u.Role.CanAdminister() {
			return errPersonalDenied
		}
		return nil
	}, Prepare: func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
		return pa.Preview{Text: "private-result"}, nil
	}}
	ts.server.personal.tools[tool.ID] = tool
	personalHTTP[pa.State](t, ts, sam, "PUT", "/settings", map[string]any{"rights": map[string]string{tool.ID: "auto"}})
	one := personalHTTP[pa.Job](t, ts, sam, "POST", "/jobs", pa.Request{Tool: tool.ID, RequestID: "first", Prompt: "private-topic"})
	workOne(t, ts.server)
	req := pa.Request{Tool: tool.ID, RequestID: "later", RunAt: time.Now().Add(time.Hour), Args: json.RawMessage(`{"detail":"private-arg"}`), Sources: []pa.Source{{Kind: "file", ID: "private-source"}}}
	personalHTTP[pa.Job](t, ts, sam, "POST", "/jobs", req)
	if got := personalHTTP[pa.Job](t, ts, sam, "GET", "/jobs/"+one.ID, nil); got.Result == nil {
		t.Fatal(got)
	}
	res, b := ts.request("PUT", "/api/v1/users/sam", map[string]string{"role": "user"})
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, string(b))
	}
	login()
	state := personalHTTP[pa.State](t, ts, sam, "GET", "/state", nil)
	raw, _ := json.Marshal(state)
	if bytes.Contains(raw, []byte("private-")) {
		t.Fatal("role-restricted fields visible", string(raw))
	}
	search := personalHTTP[struct {
		Chats []pa.Chat
		Jobs  []pa.Job
	}](t, ts, sam, "GET", "/search?q=private", nil)
	if len(search.Chats)+len(search.Jobs) != 0 {
		t.Fatal(search)
	}
}
