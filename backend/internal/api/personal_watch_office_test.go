package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func TestPersonalRecurringInvoiceCreatesOneWorksheetPerMail(t *testing.T) {
	ts, provider, nativeMail := newMailServer(t)
	s := ts.server
	s.opts.PersonalDataDir = t.TempDir()
	ts.grantMail("alex")
	box := ts.addMailbox(ts.client, provider)
	dir := ts.withShare()
	u, _ := ts.users.Get("alex")
	const base = "Rechnung.xlsx"
	if err := os.WriteFile(filepath.Join(dir, base), []byte("vorhandene Datei"), 0600); err != nil {
		t.Fatal(err)
	}
	bills := []string{"Beleg-1001", "Beleg-1002"}
	// Only the model response is substituted. The watch reads real IMAP
	// messages and the normal Office writer publishes and reads back the XLSX.
	installTools := func() {
		s.initPersonalTools()
		tools := personalMailTools{nativeMail, func(_ context.Context, _ auth.User, cloud bool, _, _ string, data any) (string, error) {
			for _, bill := range bills {
				if !cloud && strings.Contains(string(jsonPIM(data)), bill) {
					return bill, nil
				}
			}
			t.Fatal("expected exactly one selected invoice")
			return "", nil
		}}
		for _, tool := range tools.tools() {
			s.personal.tools[tool.ID] = tool
		}
	}
	installTools()
	limits := map[string]string{"mail.watch": "auto", "mail.summarize": "auto", "office.table": "auto"}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": limits})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{"mail.watch": "auto", "mail.summarize": "auto", "office.table": "ask"}})
	sk := pa.Skill{ID: "recurring-invoices-001", Title: "Rechnungen erfassen", Sender: "billing@example.org", Trigger: "mail", Approval: "inherit",
		Steps: []string{"mail.summarize", "office.table"}, StepArgs: map[string]json.RawMessage{"office.table": json.RawMessage(`{"name":"Rechnung.xlsx"}`)},
		Sources: []pa.Source{{Kind: "mailbox", ID: box, Ref: map[string]string{"folder": "INBOX", "account": box}}, {Kind: "folder", ID: ".", Ref: map[string]string{"root": "share/Docs"}}}}
	personalHTTP[pa.Skill](t, ts, ts.client, "POST", "/skills", sk)
	watch := func() personalMailWatch {
		t.Helper()
		var w personalMailWatch
		if err := s.personalTxn(u, false, func(a *personalAccount) error { w = a.Watches[sk.ID]; return nil }); err != nil {
			t.Fatal(err)
		}
		return w
	}
	first := watch()
	var paths []string
	for _, bill := range bills {
		provider.Deliver("INBOX", "From: billing@example.org\r\nTo: owner@example.org\r\nSubject: Rechnung\r\nMessage-ID: <"+bill+"@example.org>\r\nDate: Thu, 26 Nov 2026 10:00:00 +0000\r\n\r\n"+bill)
		if err := s.personalPollInbox(t.Context(), u, sk, watch()); err != nil {
			t.Fatal(err)
		}
		workOne(t, s)
		workOne(t, s)
		jobs := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil).Jobs
		index := slices.IndexFunc(jobs, func(j pa.Job) bool { return j.Status == "waiting" && j.Phase == "permission" })
		if index < 0 {
			t.Fatal(jobs)
		}
		j := approveCollection(t, ts, jobs[index])
		if j.Phase != "approval" {
			t.Fatal(j)
		}
		var dest personalOfficeRegion
		if j.Preview == nil || json.Unmarshal(j.Preview.Data, &dest) != nil || dest.Document == nil {
			t.Fatal(j)
		}
		name := dest.Document.Path
		if name == base || !strings.HasPrefix(name, "Rechnung-") || !strings.HasSuffix(name, ".xlsx") || slices.Contains(paths, name) || !strings.Contains(j.Preview.Title, name) {
			t.Fatal("wrong preview target", name)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("target already exists before approval", err)
		}
		j = approveCollection(t, ts, j)
		if j.Status != "done" || j.Result == nil || !strings.Contains(j.Result.Title, name) || len(j.Receipts) != 2 {
			t.Fatal(j)
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		cell, _, err := office.CellRange(b, "Entwurf", "A1", nil, "")
		if err != nil || cell != bill {
			t.Fatal("wrong invoice contents", cell, err)
		}
		paths = append(paths, name)
	}
	// Re-open the persisted account, then re-present the same actual UIDs as
	// if the mailbox delivered old signals. Neither job nor file may repeat.
	installTools()
	if err := s.personalTxn(u, true, func(a *personalAccount) error {
		w := a.Watches[sk.ID]
		w.Cursor = first.Cursor
		a.Watches[sk.ID] = w
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.personalPollInbox(t.Context(), u, sk, watch()); err != nil {
		t.Fatal(err)
	}
	state := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
	names, err := filepath.Glob(filepath.Join(dir, "*.xlsx"))
	if err != nil || len(names) != 3 || len(state.Jobs) != 2 {
		t.Fatal(names, state.Jobs, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, base)); err != nil || string(b) != "vorhandene Datei" {
		t.Fatal("existing file changed", err)
	}
	if lease, err := s.personalClaim(); err != nil || lease != "" {
		t.Fatal("replayed event enqueued work", lease, err)
	}
	for n, path := range paths {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		cell, _, err := office.CellRange(b, "Entwurf", "A1", nil, "")
		if err != nil || cell != bills[n] {
			t.Fatal("earlier file changed", cell, err)
		}
	}
}

func TestPersonalMailDraftNameFitsNativeUnicodeLimit(t *testing.T) {
	name := strings.Repeat("Ä", 97) + ".xlsx"
	got := mailDraftName(name, "account", "mail:fixture")
	if len(got) > 200 || !utf8.ValidString(got) || !strings.HasSuffix(got, ".xlsx") ||
		got != mailDraftName(name, "account", "mail:fixture") ||
		got == mailDraftName(name, "another", "mail:fixture") ||
		got == mailDraftName(name, "account", "mail:other") {
		t.Fatal(got)
	}
}
