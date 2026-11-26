package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

const fixtureDelivery = "From: Supplier <vendor@example.test>\nTo: alice@example.org\nDate: Mon, 9 Nov 2026 08:00:00 +0000\nSubject: Liefertermin\nMessage-ID: <delivery@example.test>\nContent-Type: text/plain; charset=utf-8\n\nBitte am 30. November um 10 Uhr liefern."

func TestPersonalNativeMailEditedDraftApprovalAndSMTPOutcomes(t *testing.T) {
	for _, mode := range []string{"confirmed", "rejected", "lost-reply", "missing-copy"} {
		t.Run(mode, func(t *testing.T) {
			ts, provider, m := newMailServer(t)
			ts.server.opts.PersonalDataDir = t.TempDir()
			ts.server.initPersonalTools()
			ts.grantMail("alex")
			id := ts.addMailbox(ts.client, provider)
			provider.Deliver("INBOX", fixtureDelivery)
			ctx := t.Context()
			src, err := m.ReadQuiet(ctx, "alex", id, "INBOX", 1)
			if err != nil {
				t.Fatal(err)
			}
			ref := pa.Source{ID: "message", Kind: "mail", Ref: map[string]string{"account": id, "folder": "INBOX", "uid": "1", "messageId": src.MessageID, "date": src.Date.Format(time.RFC3339Nano)}}
			uid, _, err := m.SaveDraft(ctx, "alex", id, tmail.Outgoing{To: []tmail.Address{{Addr: "edited@example.test"}}, Cc: []tmail.Address{{Addr: "cc@example.test"}}, Bcc: []tmail.Address{{Addr: "private@example.test"}}, Subject: "Geänderter Betreff", Text: "Geänderte Antwort", InReplyTo: src.MessageID, References: src.MessageID, Files: []tmail.File{{Name: "lieferung.txt", Type: "text/plain", Data: []byte("Fixture document")}}})
			if err != nil || uid == 0 {
				t.Fatal(uid, err)
			}
			d, err := m.ReadQuiet(ctx, "alex", id, "Drafts", uid)
			if err != nil {
				t.Fatal(err)
			}
			personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": map[string]string{"mail.send": "auto"}})
			personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{"mail.send": "auto"}})
			req := pa.Request{Tool: "mail.send", RequestID: "native-send", Sources: []pa.Source{ref}, Args: jsonPIM(savedReply{DraftUID: uid, DraftID: d.MessageID, DraftDate: d.Date.Format(time.RFC3339Nano)})}
			j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
			workOne(t, ts.server)
			read := func() pa.Job { return personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil) }
			j = read()
			if j.Phase != "approval" || j.Preview == nil || len(provider.Sent()) != 0 {
				t.Fatalf("no approval: %+v", j)
			}
			var evidence sendEvidence
			if err = json.Unmarshal(j.Preview.Data, &evidence); err != nil || evidence.Account != id || evidence.From != "alice@example.org" || !strings.Contains(strings.Join(evidence.Bcc, " "), "private@example.test") || !strings.Contains(evidence.Text, "Geänderte Antwort") || len(evidence.Attachments) != 1 || evidence.Attachments[0].Name != "lieferung.txt" || evidence.Attachments[0].Size != 16 {
				t.Fatalf("wrong approved content: %v %+v", err, evidence)
			}
			guest, _ := ts.users.Create("bea", testPassword, auth.RoleUser)
			other := ts.newClient()
			ts.do(other, "POST", "/api/v1/login", credentials{Name: guest.Name, Password: testPassword})
			if res, _ := ts.do(other, "POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token}); res.StatusCode != 404 {
				t.Fatal("another account could access approval", res.StatusCode)
			}
			personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
			provider.RejectData.Store(mode == "rejected")
			provider.LoseDataReply.Store(mode == "lost-reply")
			provider.RefuseSentCopy.Store(mode == "missing-copy")
			workOne(t, ts.server)
			j = read()
			switch mode {
			case "rejected", "lost-reply":
				want := "effect_rejected"
				n := 0
				if mode == "lost-reply" {
					want = "effect_unconfirmed"
					n = 1
				}
				if j.Status != "failed" || j.Error != want || j.Result != nil || j.Preview == nil || len(provider.Sent()) != n {
					t.Fatal(j, len(provider.Sent()))
				}
				if _, err = m.ReadQuiet(ctx, "alex", id, "Drafts", uid); err != nil {
					t.Fatal("draft not retained", err)
				}
			default:
				if j.Status != "done" || j.Result == nil || len(provider.Sent()) != 1 {
					t.Fatalf("%+v", j)
				}
				var receipt struct {
					Accepted, SentCopy bool
					MessageID          string
					AcceptedAt         time.Time
				}
				if err = json.Unmarshal(j.Result.Data, &receipt); err != nil || !receipt.Accepted || receipt.MessageID == "" || receipt.AcceptedAt.IsZero() || receipt.SentCopy != (mode == "confirmed") {
					t.Fatal(receipt, j.Result)
				}
				wire := provider.Sent()[0]
				if len(wire.To) != 3 || bytes.Contains(wire.Data, []byte("private@example.test")) {
					t.Fatal("Bcc envelope/header mismatch")
				}
			}
			again := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
			if again.ID != j.ID {
				t.Fatal("replay started another SMTP operation")
			}
			ts.server.initPersonalTools()
			lease, err := ts.server.personalClaim()
			if err != nil || lease != "" {
				t.Fatal("restart retries a concluded send", lease, err)
			}
		})
	}
}
func TestPersonalSelectedMailIsolationAndUnseen(t *testing.T) {
	ts, srv, m := newMailServer(t)
	ts.grantMail("alex")
	id := ts.addMailbox(ts.client, srv)
	srv.Deliver("INBOX", fixtureDelivery)
	u, _ := ts.users.Get("alex")
	a, err := m.ReadQuiet(t.Context(), u.Name, id, "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	ref := pa.Source{Kind: "mail", ID: "one", Ref: map[string]string{"folder": "INBOX", "uid": "1", "account": id, "messageId": a.MessageID, "date": a.Date.Format(time.RFC3339Nano)}}
	calls := 0
	tool := personalMailTools{m, func(_ context.Context, _ auth.User, cloud bool, _, _ string, source any) (string, error) {
		calls++
		if cloud || !strings.Contains(string(jsonPIM(source)), "30. November") {
			t.Fatal("wrong scope")
		}
		return "Zusammenfassung", nil
	}}
	req := pa.Request{Tool: "mail.summarize", Sources: []pa.Source{ref}}
	p, err := tool.readPreview(t.Context(), u, req)
	if err != nil || p.Text != "Zusammenfassung" {
		t.Fatal(p, err)
	}
	a, err = m.ReadQuiet(t.Context(), u.Name, id, "INBOX", 1)
	if err != nil || a.Flags.Seen {
		t.Fatal("summary marked read")
	}
	guest, _ := ts.users.Create("bea", testPassword, auth.RoleUser)
	ts.grantMail(guest.Name)
	if _, err = tool.readPreview(t.Context(), guest, req); err == nil {
		t.Fatal("accepted another account's mailbox")
	}
	ref.Ref["messageId"] = "<unrelated@example.test>"
	if _, err = tool.readPreview(t.Context(), u, req); !errors.Is(err, errPersonalSource) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("model received out-of-scope sources")
	}
}

// Exercise the authority callback after the actual TLS and AUTH exchange.
// A second guard is checked after acceptance, before mutating Sent or Drafts.
func TestPersonalMailRevokesAtSMTPAndPostSendBoundary(t *testing.T) {
	ts, provider, m := newMailServer(t)
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	owner, _ := ts.users.Get("alex")
	view := m.Mailboxes(owner.Name)[0]
	out := tmail.Outgoing{ExpectedFrom: &tmail.Address{Name: view.SenderName, Addr: view.Email}, To: []tmail.Address{{Addr: "fixture@example.test"}}, Subject: "Fixture", Text: "fixture only", Authorize: pa.Reauthorize}
	uid, _, err := m.SaveDraft(t.Context(), owner.Name, box, out)
	if err != nil {
		t.Fatal(err)
	}
	out.DraftUID = uid
	if _, err = m.Send(t.Context(), owner.Name, box, out); !errors.Is(err, pa.ErrAuthority) || len(provider.Sent()) != 0 {
		t.Fatal("unbound broker authority", err)
	}
	checks := 0
	authority := func(context.Context) error {
		checks++
		if checks > 1 {
			return errors.New("permission revoked")
		}
		return nil
	}
	ctx := pa.WithAuthority(t.Context(), authority, authority)
	receipt, err := m.Send(ctx, owner.Name, box, out)
	if err != nil || receipt.SentCopy || receipt.MessageID == "" || len(provider.Sent()) != 1 {
		t.Fatal(receipt, err)
	}
	if _, err = m.ReadQuiet(t.Context(), owner.Name, box, "Drafts", uid); err != nil {
		t.Fatal("draft removed after revocation", err)
	}
	p, err := m.List(t.Context(), owner.Name, box, tmail.ListQuery{Folder: "Sent", Limit: 5})
	if err != nil || len(p.Messages) != 0 {
		t.Fatal("mailbox modified after revocation", p, err)
	}
}
