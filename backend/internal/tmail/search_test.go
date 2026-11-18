package tmail

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func mixedMail(from, subject string) string {
	return fmt.Sprintf("From: %s\nSubject: %s\nDate: Tue, 17 Nov 2026 10:00:00 +0100\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=m\n\n--m\nContent-Type: text/plain\n\nSiehe Anhang\n--m\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=a.pdf\n\nJVBERi0=\n--m--\n", from, subject)
}

// The filters of 1.12: sender, attachment (by the type of the head) and
// days of arrival, both counted.
func TestListFilters(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	day := func(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }
	e.srv.DeliverAt("INBOX", plainMail("Bob <bob@example.net>", "Eins", "x"), day(10))
	e.srv.DeliverAt("INBOX", mixedMail("carol@example.org", "Zwei"), day(12))
	e.srv.DeliverAt("INBOX", mixedMail("bob@example.net", "Drei"), day(14))
	ctx := t.Context()
	uids := func(q ListQuery) string {
		t.Helper()
		q.Folder = "INBOX"
		page, err := e.m.List(ctx, "alex", id, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range page.Messages {
			out = append(out, fmt.Sprint(s.UID))
		}
		return strings.Join(out, ",")
	}
	if got := uids(ListQuery{From: "bob"}); got != "3,1" {
		t.Errorf("from: %s", got)
	}
	if got := uids(ListQuery{Attachment: true}); got != "3,2" {
		t.Errorf("attachment: %s", got)
	}
	if got := uids(ListQuery{Since: day(12), Until: day(12)}); got != "2" {
		t.Errorf("one day: %s", got)
	}
	if got := uids(ListQuery{Since: day(11)}); got != "3,2" {
		t.Errorf("since: %s", got)
	}
	if got := uids(ListQuery{Until: day(12), From: "carol"}); got != "2" {
		t.Errorf("until with from: %s", got)
	}
	for _, bad := range []ListQuery{{From: "a\r\nX"}, {From: strings.Repeat("a", 257)}, {Since: day(14), Until: day(12)}} {
		bad.Folder = "INBOX"
		if _, err := e.m.List(ctx, "alex", id, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

// A search goes through all folders and names the folder of each hit,
// newest first by arrival, within its limit.
func TestSearchAllFolders(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	ctx := t.Context()
	for _, f := range []string{"Rechnungen", "Rechnungen/2026"} {
		if err := e.m.CreateFolder(ctx, "alex", id, f); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	e.srv.DeliverAt("INBOX", plainMail("bob@example.net", "Rechnung Strom", "x"), base.Add(1*time.Hour))
	e.srv.DeliverAt("Rechnungen", plainMail("bob@example.net", "Rechnung Gas", "x"), base.Add(3*time.Hour))
	e.srv.DeliverAt("Rechnungen/2026", plainMail("carol@example.org", "Rechnung Wasser", "x"), base.Add(2*time.Hour))
	e.srv.DeliverAt("Archiv", plainMail("bob@example.net", "Urlaub", "x"), base.Add(4*time.Hour))
	res, err := e.m.Search(ctx, "alex", id, ListQuery{Q: "rechnung"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range res.Messages {
		got = append(got, h.Folder+":"+h.Subject)
	}
	if strings.Join(got, "|") != "Rechnungen:Rechnung Gas|Rechnungen/2026:Rechnung Wasser|INBOX:Rechnung Strom" || res.Total != 3 || res.Truncated {
		t.Fatalf("search: %v total %d truncated %v", got, res.Total, res.Truncated)
	}
	if res.Folders < 4 {
		t.Fatalf("folders searched: %d", res.Folders)
	}
	if res.Messages[0].Preview != "x" {
		t.Fatalf("hit without its line: %+v", res.Messages[0])
	}
	// Limit and filters.
	res, err = e.m.Search(ctx, "alex", id, ListQuery{From: "bob", Limit: 2})
	if err != nil || len(res.Messages) != 2 || res.Total != 3 || res.Messages[0].Subject != "Urlaub" {
		t.Fatalf("limited: %+v %v", res, err)
	}
	// Nothing to search for is refused; a listing of every folder is no search.
	if _, err := e.m.Search(ctx, "alex", id, ListQuery{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty search: %v", err)
	}
	// Search marks nothing read.
	if unread, _ := e.m.List(ctx, "alex", id, ListQuery{Folder: "Rechnungen", Unread: true}); unread.Total != 1 {
		t.Fatalf("search marked mails read")
	}
}

// One search per account at a time; the time budget ends a search with
// what it found; more than maxSearchFolders are not searched.
func TestSearchLimits(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	ctx := t.Context()
	done, ok := e.m.searchTurn("alex")
	if !ok {
		t.Fatal("no turn")
	}
	if _, err := e.m.Search(ctx, "alex", id, ListQuery{Q: "x"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second search: %v", err)
	}
	done()
	for i := range maxSearchFolders + 5 {
		if err := e.m.CreateFolder(ctx, "alex", id, fmt.Sprintf("F%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	e.deliver("F104", plainMail("bob@example.net", "Spät", "x"))
	res, err := e.m.Search(ctx, "alex", id, ListQuery{Q: "spät"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.Folders != maxSearchFolders || len(res.Messages) != 0 {
		t.Fatalf("over the bound: folders %d truncated %v hits %d", res.Folders, res.Truncated, len(res.Messages))
	}
	old := searchTime
	searchTime = 0
	t.Cleanup(func() { searchTime = old })
	res, err = e.m.Search(ctx, "alex", id, ListQuery{Q: "x"})
	if err != nil || !res.Truncated || res.Folders > 1 {
		t.Fatalf("out of time: %+v %v", res, err)
	}
}

func TestArchiveAndForwarded(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	ctx := t.Context()
	e.deliver("INBOX", plainMail("bob@example.net", "A", "x"))
	e.deliver("INBOX", plainMail("bob@example.net", "B", "y"))
	// The archive by its name; without one, one is made.
	if to, err := e.m.Archive(ctx, "alex", id, "INBOX", []uint32{1}); err != nil || to != "Archiv" || e.count("Archiv") != 1 {
		t.Fatalf("archive by name: %q %v", to, err)
	}
	if err := e.m.DeleteFolder(ctx, "alex", id, "Archiv"); err != nil {
		t.Fatal(err)
	}
	e.deliver("INBOX", plainMail("bob@example.net", "C", "z"))
	to, err := e.m.Archive(ctx, "alex", id, "INBOX", []uint32{3})
	if err != nil || to != "Archive" || e.count("Archive") != 1 || e.count("INBOX") != 1 {
		t.Fatalf("archive: %q %v, archive %d", to, err, e.count("Archive"))
	}
	if _, err := e.m.Archive(ctx, "alex", id, "Archive", []uint32{1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("from the archive: %v", err)
	}
	if _, err := e.m.Archive(ctx, "alex", id, "INBOX", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no uids: %v", err)
	}
	// Forwarding marks the mail $Forwarded.
	if _, err := e.m.Send(ctx, "alex", id, Outgoing{To: []Address{{Addr: "carol@example.net"}}, Subject: "Fwd: B", Text: "siehe unten",
		Forwards: &MailRef{Folder: "INBOX", UID: 2}}); err != nil {
		t.Fatal(err)
	}
	page, _ := e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX"})
	if len(page.Messages) != 1 || !page.Messages[0].Flags.Forwarded || page.Messages[0].Flags.Answered {
		t.Fatalf("forwarded: %+v", page.Messages)
	}
}
