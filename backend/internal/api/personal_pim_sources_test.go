package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	store "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func TestPersonalSelectedPDFAndExactConversation(t *testing.T) {
	ts, provider, m := newMailServer(t)
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	u, _ := ts.users.Get("alex")
	raw := strings.Replace(fixtureDelivery, "Content-Type: text/plain; charset=utf-8", "MIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=fixture\n\n--fixture\nContent-Type: text/plain; charset=utf-8", 1)
	for i, name := range []string{"Rechnung.pdf", "Privat.pdf"} {
		pdf, err := office.PDF([]string{"Beleg Nr. AZ12345", "DarfNichtInsModell"}[i], "Beleg")
		if err != nil {
			t.Fatal(err)
		}
		raw += "\n--fixture\nContent-Type: application/pdf; name=" + name + "\nContent-Disposition: attachment; filename=" + name + "\nContent-Transfer-Encoding: base64\n\n" + base64.StdEncoding.EncodeToString(pdf)
	}
	raw += "\n--fixture--\n"
	provider.Deliver("INBOX", raw)
	provider.Deliver("INBOX", strings.ReplaceAll(strings.Replace(fixtureDelivery, "Message-ID: <delivery@example.test>", "Message-ID: <second@example.test>\nReferences: <delivery@example.test>", 1), "Bitte am 30. November um 10 Uhr liefern.", "Zugehoerige Antwort"))
	provider.Deliver("INBOX", strings.ReplaceAll(strings.Replace(fixtureDelivery, "<delivery@example.test>", "<other@example.test>", 1), "Bitte am 30. November um 10 Uhr liefern.", "GleicherBetreffFremderThread"))
	msg, err := m.ReadQuiet(t.Context(), u.Name, box, "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	ref := map[string]string{"account": box, "folder": "INBOX", "uid": "1", "messageId": msg.MessageID, "date": msg.Date.Format(time.RFC3339Nano), "scope": "thread"}
	anchor := pa.Source{ID: "selected", Kind: "mail", Ref: ref}
	pdf := pa.Source{ID: "invoice", Kind: "mail.pdf", Ref: maps.Clone(ref)}
	pdf.Ref["filename"] = "Rechnung.pdf"
	n := 0
	tool := personalMailTools{m, func(_ context.Context, _ auth.User, cloud bool, _, _ string, data any) (string, error) {
		n++
		serialized := string(jsonPIM(data))
		if cloud || strings.Contains(serialized, "DarfNichtInsModell") || strings.Contains(serialized, "GleicherBetreffFremderThread") || !strings.Contains(serialized, "Zugehoerige Antwort") || !strings.Contains(serialized, "AZ12345") {
			t.Fatalf("source mismatch: %s", serialized)
		}
		return "AZ12345", nil
	}}
	req := pa.Request{Tool: "mail.summarize", Sources: []pa.Source{anchor, pdf}}
	p, err := tool.readPreview(t.Context(), u, req)
	if err != nil || len(p.Sources) != 3 || p.Sources[2].Ref["part"] == "" {
		t.Fatal(err, p)
	}
	for i := range 3 {
		v, err := m.ReadQuiet(t.Context(), u.Name, box, "INBOX", uint32(i+1))
		if err != nil || v.Flags.Seen {
			t.Fatal("read mark changed", err)
		}
	}
	// The exact name must match only one part. No wildcard expands the scope.
	for _, filename := range []string{"*.pdf", "missing.pdf"} {
		pdf.Ref["filename"] = filename
		if _, err = tool.readPreview(t.Context(), u, req); err == nil {
			t.Fatal(filename)
		}
	}
	provider.Deliver("INBOX", strings.ReplaceAll(raw, "Privat.pdf", "Rechnung.pdf"))
	ref["uid"] = "4"
	pdf.Ref = maps.Clone(ref)
	pdf.Ref["filename"] = "Rechnung.pdf"
	req.Sources[1] = pdf
	if _, err = tool.readPreview(t.Context(), u, req); err == nil || n != 1 {
		t.Fatal("ambiguous PDF", err, n)
	}
}
func TestPersonalNativeCollectionsReceiptsAndOwner(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	u, _ := ts.users.Get("alex")
	other, _ := ts.users.Create("bea", testPassword, auth.RoleUser)
	for _, row := range []struct {
		kind     store.Kind
		app, col string
	}{{store.Calendar, "calendar", "arbeit"}, {store.Tasks, "reminders", "aufgaben"}, {store.Contacts, "contacts", "kontakte"}} {
		t.Run(row.app, func(t *testing.T) {
			if _, err := pim.GetCollection(row.kind, u.Name, row.col); err != nil {
				if _, err = pim.CreateCollection(row.kind, u.Name, row.col, store.Meta{Name: row.app}); err != nil {
					t.Fatal(err)
				}
			}
			fields := pimCreateFields{Title: "Fixture", Collection: row.col}
			switch row.kind {
			case store.Calendar:
				fields.Start = "2026-11-30T10:00"
				fields.End = "2026-11-30T11:00"
				fields.Timezone = "Europe/Berlin"
			case store.Contacts:
				fields.Email = "test@example.test"
			case store.Tasks:
				fields.Due = "2026-11-30"
			}
			r := pa.Request{Tool: row.app + ".create", RequestID: "fixture-" + row.app, Args: jsonPIM(fields)}
			p := personalPIM{ts.server.pimDataArea(), row.kind, row.app}
			plan, name, _, err := p.createPlan(t.Context(), u, r)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.write(t.Context(), u, r, plan); !errors.Is(err, pa.ErrAuthority) {
				t.Fatal("missing broker authority", err)
			}
			allow := func(context.Context) error { return nil }
			denied := pa.WithAuthority(t.Context(), allow, func(context.Context) error { return errors.New("revoked") })
			if _, err := p.write(denied, u, r, plan); !errors.Is(err, pa.ErrAuthority) {
				t.Fatal("collection guard missed revocation", err)
			}
			finish, err := p.write(pa.WithAuthority(t.Context(), allow, allow), u, r, plan)
			if err != nil || !strings.Contains(finish.Link, name) {
				t.Fatal(finish, err)
			}
			r = pa.Request{Tool: row.app + ".read", Sources: []pa.Source{{Kind: row.app, ID: "selected", Ref: map[string]string{"collection": row.col, "object": name}}}}
			if got, err := p.read(t.Context(), u, r); err != nil || !strings.Contains(got.Text, "Fixture") {
				t.Fatal(got, err)
			}
			if _, err = p.read(t.Context(), other, r); err == nil {
				t.Fatal("another account read the object")
			}
		})
	}
}
func TestPersonalCalendarCancellationScopeAndICS(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	const calendar = "fixture"
	u, _ := ts.users.Get("alex")
	if _, err := pim.CreateCollection(store.Calendar, u.Name, calendar, store.Meta{Name: calendar}); err != nil {
		t.Fatal(err)
	}
	data := bytes.Replace(pimEvent("Lieferung"), []byte("END:VEVENT"), []byte("ORGANIZER:mailto:owner@example.test\r\nATTENDEE:mailto:guest@example.test\r\nATTENDEE:mailto:hidden@example.test\r\nDESCRIPTION:Private notes\r\nSEQUENCE:2\r\nEND:VEVENT"), 1)
	if _, err := pim.Put(store.Calendar, u.Name, calendar, "event.ics", data, store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	area := ts.server.pimDataArea()
	v, _, err := area.openEvent(pim, u.Name, calendar, "event.ics", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, cancel := range []bool{true, false} {
		out, err := inviteICS(v, data, "owner@example.test", "Owner", "guest@example.test", time.UTC, cancel, false)
		if err != nil || bytes.Contains(out, []byte("hidden@example.test")) || bytes.Contains(out, []byte("Private notes")) || !bytes.Contains(out, []byte("mailto:guest@example.test")) {
			t.Fatal(err, string(out))
		}
		n := 2
		if cancel {
			n++
		}
		if !bytes.Contains(out, []byte("SEQUENCE:"+strconv.Itoa(n))) {
			t.Error(string(out))
		}
	}
	for _, pair := range [][]string{{"someone@example.test", "guest@example.test"}, {"owner@example.test", "other@example.test"}} {
		if _, err = inviteICS(v, data, pair[0], "Owner", pair[1], time.UTC, true, false); err == nil {
			t.Fatal(pair)
		}
	}
}

func TestPersonalFollowupIgnoresOwnMailAndUnrelatedSubjects(t *testing.T) {
	ts, provider, m := newMailServer(t)
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	u, _ := ts.users.Get("alex")
	provider.Deliver("INBOX", fixtureDelivery)
	root, err := m.ReadQuiet(t.Context(), u.Name, box, "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	req := pa.Request{Tool: "mail.followup", Sources: []pa.Source{{ID: "mail", Kind: "mail", Ref: map[string]string{"account": box, "folder": "INBOX", "uid": "1", "messageId": root.MessageID, "date": root.Date.Format(time.RFC3339Nano)}}}}
	reply := strings.Replace(fixtureDelivery, "Message-ID: <delivery@example.test>", "Message-ID: <reply@example.test>\nIn-Reply-To: <delivery@example.test>", 1)
	provider.Deliver("INBOX", strings.Replace(reply, "vendor@example.test", "alice@example.org", 1))
	provider.Deliver("INBOX", strings.Replace(fixtureDelivery, "delivery@example.test", "same-subject@example.test", 1))
	tool := personalMailTools{mail: m}
	got, err := tool.readPreview(t.Context(), u, req)
	if err != nil || got.Silent || !strings.Contains(got.Text, "nachfassen") {
		t.Fatal(got, err)
	}
	provider.Deliver("INBOX", reply)
	got, err = tool.readPreview(t.Context(), u, req)
	if err != nil || !got.Silent {
		t.Fatal(got, err)
	}
	if len(provider.Sent()) != 0 {
		t.Fatal("observation sent a message")
	}
}
