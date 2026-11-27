package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	gomail "github.com/emersion/go-message/mail"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	store "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
	"github.com/ra5on/TitanOS/backend/internal/webui"
)

// Mail, calendars, permissions, workflow broker and external-effect receipts
// are real production code. Only the inference response and mailservers are fixtures.
func TestPersonalDeliveryMailCalendarDraftAndApprovedSend(t *testing.T) {
	browser := os.Getenv("TITANOS_AGENT_APPROVAL_BROWSER") == "1"
	ts, provider, m := newMailServer(t, func(o *Options) {
		if browser {
			if !webui.Embedded {
				t.Fatal("browser acceptance requires -tags embedui")
			}
			o.UI = webui.FS()
		}
	})
	s := ts.server
	pim, _, _ := pimTestStore(t)
	s.opts.PIM = pim
	s.opts.PersonalDataDir = t.TempDir()
	s.initPersonalTools()
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	provider.Deliver("INBOX", fixtureDelivery)
	src, err := m.ReadQuiet(t.Context(), "alex", box, "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	cal := "lieferung"
	if _, err = pim.CreateCollection(store.Calendar, "alex", cal, store.Meta{Name: "Privat"}); err != nil {
		t.Fatal(err)
	}
	if _, err = pim.Put(store.Calendar, "alex", cal, "busy.ics", pimEvent("Privater Titel"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	tools := personalMailTools{m, func(_ context.Context, _ auth.User, cloud bool, task, prompt string, data any) (string, error) {
		calls++
		ref := string(jsonPIM(data))
		if cloud || !strings.Contains(ref, "30. November") || strings.Contains(ref, "Privater Titel") {
			t.Fatal("unexpected data scope")
		}
		if strings.Contains(task, "appointment") {
			return `{"title":"Liefertermin","start":"2026-11-30T10:00","end":"2026-11-30T11:00"}`, nil
		}
		if !strings.Contains(ref, "prior_step_result") || !strings.Contains(ref, cal) && !strings.Contains(ref, "Privat") {
			t.Fatal("calendar context not sent to reply", ref)
		}
		return "Am 30. November um 10 Uhr passt es.", nil
	}}
	for _, tool := range tools.tools() {
		s.personal.tools[tool.ID] = tool
	}
	rights := map[string]string{"mail.event": "auto", "calendar.free": "auto", "mail.reply": "auto", "mail.send": "auto"}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": rights})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": rights})
	sources := []pa.Source{{Kind: "mail", ID: box + "/INBOX/1", Ref: map[string]string{"account": box, "folder": "INBOX", "uid": "1", "messageId": src.MessageID, "date": src.Date.Format(time.RFC3339Nano)}}, {Kind: "calendar.range", ID: cal, Ref: map[string]string{"collection": cal, "start": "2026-11-30T09:00:00Z", "end": "2026-11-30T12:00:00Z"}}}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/skills/delivery/run", map[string]any{"sources": sources, "requestId": "liefertermin", "prompt": "Liefertermin prüfen"})
	read := func() pa.Job { return personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil) }
	for i := 0; i < 3; i++ {
		workOne(t, s)
		j = read()
		if j.CompletedSteps != i+1 || j.Result == nil || len(provider.Sent()) != 0 {
			t.Fatalf("step %d %+v", i, j)
		}
		if i == 2 {
			var result struct {
				Draft struct{ Fields map[string]any }
			}
			if json.Unmarshal(j.Result.Data, &result) != nil || result.Draft.Fields["a"] != box {
				t.Fatal("no native draft")
			}
		}
	}
	s.initPersonalTools() // Browser closed and IP service restarted after the saved draft.
	workOne(t, s)
	j = read()
	if j.Phase != "approval" || j.CurrentTool != "mail.send" || j.Result == nil || j.Result.Title != "Antwortvorschlag" || j.Preview == nil || !strings.Contains(j.Preview.Text, "vendor@example.test") || !strings.Contains(j.Preview.Text, "10 Uhr passt") || calls != 2 {
		t.Fatalf("%+v", j)
	}
	// The mail task is still bound to the same thread, while the reviewed editor
	// version has a different envelope, body and attachment than the prior step.
	oldToken := j.Token
	uid, _, err := m.SaveDraft(t.Context(), "alex", box, tmail.Outgoing{
		To: []tmail.Address{{Addr: "reviewed@example.test"}}, Cc: []tmail.Address{{Addr: "copy@example.test"}}, Bcc: []tmail.Address{{Addr: "private@example.test"}},
		Subject: "Revidierter Termin", Text: "Geprüfte Antwortfassung zwei.", InReplyTo: src.MessageID, References: src.MessageID,
		Files: []tmail.File{{Name: "Bestaetigung.txt", Type: "text/plain", Data: []byte("revision two")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := m.ReadQuiet(t.Context(), "alex", box, "Drafts", uid)
	if err != nil {
		t.Fatal(err)
	}
	personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/revise", map[string]any{"token": oldToken, "args": map[string]any{"draftUid": uid, "draftMessageId": draft.MessageID, "draftDate": draft.Date.Format(time.RFC3339Nano)}})
	workOne(t, s)
	j = read()
	if j.Preview == nil || j.Result == nil || j.CurrentTool != "mail.send" || j.Token == oldToken || !strings.Contains(j.Preview.Text, "reviewed@example.test") || strings.Contains(j.Result.Text, "Geprüfte Antwortfassung zwei.") {
		t.Fatal("did not prepare the revised version alongside its prior receipt", j)
	}
	if res, _ := ts.request("POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": oldToken}); res.StatusCode != 409 || len(provider.Sent()) != 0 {
		t.Fatal("superseded version accepted", res.StatusCode)
	}
	if browser {
		testDeliveryApprovalUI(t, ts, j, box)
	} else {
		personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
		workOne(t, s)
	}
	j = read()
	if j.Status != "done" || j.CurrentTool != "mail.send" || j.Preview != nil || j.Result == nil || !bytes.Contains(j.Result.Data, []byte(`"accepted":true`)) || len(j.Receipts) != 4 || len(provider.Sent()) != 1 {
		t.Fatal(j)
	}
	wire := provider.Sent()[0]
	if len(wire.To) != 3 || !strings.Contains(strings.Join(wire.To, ","), "private@example.test") || bytes.Contains(wire.Data, []byte("vendor@example.test")) || !bytes.Contains(wire.Data, []byte("Bestaetigung.txt")) {
		t.Fatal("SMTP did not receive the reviewed envelope and attachment")
	}
	// Run the second built-in with the native folder picked as its writing destination.
	dir := ts.withShare()
	for _, tool := range tools.tools() {
		s.personal.tools[tool.ID] = tool
	}
	rights["office.document"] = "auto"
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": rights})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": rights})
	target := pa.Source{Kind: "folder", ID: ".", Label: "Docs", Ref: map[string]string{"root": "share/Docs"}}
	j = personalHTTP[pa.Job](t, ts, ts.client, "POST", "/skills/delivery-document/run", map[string]any{"sources": append(sources, target), "requestId": "liefertermin-dokument", "prompt": "Liefertermin dokumentieren", "args": map[string]string{"name": "Liefertermin.docx"}})
	for range 3 {
		workOne(t, s)
	}
	j = read()
	if j.Status != "done" || len(j.Receipts) != 3 || j.Receipts[2].Tool != "office.document" || len(provider.Sent()) != 1 {
		t.Fatal(j)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Liefertermin.docx"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 10; i++ {
		line, _, err := office.Paragraph(b, ".docx", 0, i, nil)
		if err != nil {
			break
		}
		lines = append(lines, line)
	}
	content := strings.Join(lines, "\n")
	if !strings.Contains(content, "Liefertermin 2026-11-30T10:00") || !strings.Contains(content, "Geprüfte freie Zeiten") || !strings.Contains(content, "Privat") || strings.Contains(content, "Privater Titel") {
		t.Fatal("incorrect Word contents", content)
	}

}

func TestPersonalInvitationUsesApprovedMailboxIdentity(t *testing.T) {
	ts, provider, m := newMailServer(t)
	s := ts.server
	pim, _, _ := pimTestStore(t)
	s.opts.PIM = pim
	s.opts.PersonalDataDir = t.TempDir()
	s.initPersonalTools()
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	owner, _ := ts.users.Get("alex")
	const cal = "termine"
	const event = "termin.ics"
	if _, err := pim.CreateCollection(store.Calendar, owner.Name, cal, store.Meta{Name: "Liefertermine"}); err != nil {
		t.Fatal(err)
	}
	data := bytes.Replace(pimEvent("Übergabe"), []byte("END:VEVENT"), []byte("ORGANIZER:mailto:alice@example.org\r\nATTENDEE:mailto:private@example.test\r\nEND:VEVENT"), 1)
	if _, err := pim.Put(store.Calendar, owner.Name, cal, event, data, store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	senderName := "Alex – Versand"
	settings := func() {
		view := m.Mailboxes(owner.Name)[0]
		in := tmail.MailboxInput{Name: "Büro", SenderName: senderName, Email: view.Email, User: view.User, IMAP: view.IMAP, SMTP: view.SMTP}
		if _, err := m.UpdateMailbox(owner.Name, box, in); err != nil {
			t.Fatal(err)
		}
	}
	settings()
	permissions := map[string]string{"calendar.invite": "auto", "calendar.cancelInvite": "auto"}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": permissions})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": permissions})
	r := pa.Request{Tool: "calendar.invite", RequestID: "invite-receipt", Sources: []pa.Source{{Kind: "calendar", ID: event, Ref: map[string]string{"collection": cal, "object": event}}}, Args: jsonPIM(map[string]string{"account": box, "to": "chosen@example.test"})}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", r)
	get := func() pa.Job { return personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil) }
	approve := func() {
		personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	}
	workOne(t, s)
	j = get()
	if j.Phase != "approval" || j.Preview == nil || !strings.Contains(j.Preview.Text, "Alex – Versand") {
		t.Fatal(j)
	}
	approve()
	senderName = "Alex – Logistik"
	settings()
	workOne(t, s)
	j = get()
	if j.Phase != "approval" || j.Preview == nil || !strings.Contains(j.Preview.Text, "Alex – Logistik") || len(provider.Sent()) != 0 {
		t.Fatal("identity change did not renew approval", j)
	}
	approve()
	workOne(t, s)
	j = get()
	if j.Status != "done" || len(provider.Sent()) != 1 {
		t.Fatal(j)
	}
	sent := provider.Sent()[0]
	wire, err := gomail.CreateReader(bytes.NewReader(sent.Data))
	if err != nil {
		t.Fatal(err)
	}
	senders, err := wire.Header.AddressList("From")
	if err != nil || len(senders) != 1 || senders[0].Name != "Alex – Logistik" || senders[0].Address != "alice@example.org" {
		t.Fatal("SMTP identity", senders, err)
	}
	var ics []byte
	for {
		part, e := wire.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if _, ok := part.Header.(*gomail.AttachmentHeader); ok {
			ics, err = io.ReadAll(part.Body)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	ic, err := ical.NewDecoder(bytes.NewReader(ics)).Decode()
	if err != nil {
		t.Fatal(err, string(ics))
	}
	ev := ic.Children[0]
	org := ev.Props.Get("ORGANIZER")
	if org == nil || org.Value != "mailto:alice@example.org" || org.Params.Get("CN") != "Alex – Logistik" || len(ev.Props.Values("ATTENDEE")) != 1 || ev.Props.Get("ATTENDEE").Value != "mailto:chosen@example.test" || bytes.Contains(ics, []byte("private@example.test")) {
		t.Fatal("wrong calendar identity or attendee", string(ics))
	}
	r.Tool = "calendar.cancelInvite"
	r.RequestID = "cancel-invitation"
	j = personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", r)
	workOne(t, s)
	j = get()
	if j.Preview == nil || j.Phase != "approval" || len(provider.Sent()) != 1 {
		t.Fatal(j)
	}
	approve()
	workOne(t, s)
	if j = get(); j.Status != "done" || len(provider.Sent()) != 2 {
		t.Fatal(j)
	}
}
