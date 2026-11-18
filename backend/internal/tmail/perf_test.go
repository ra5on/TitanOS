package tmail

import (
	"fmt"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/pimtest"
)

// TestPerfMailLanes measures what waits for what on the connections of a
// mailbox (docs/validation/performance-113.md): opening a mail and the
// list while a search over all folders (300 ms per folder at the
// provider) or the folder counts (100 ms per STATUS) run.
// TITANOS_PERF=1 go test ./internal/tmail -run TestPerfMailLanes -v
func TestPerfMailLanes(t *testing.T) {
	pimtest.Enabled(t)
	e := newEnv(t)
	id := e.addMailbox()
	ctx := t.Context()
	for i := range 8 {
		if err := e.m.CreateFolder(ctx, "alex", id, fmt.Sprintf("Ordner%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 30 {
		e.deliver("INBOX", plainMail("bob@example.net", fmt.Sprintf("Rechnung %d", i), "Hallo"))
	}
	page, err := e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX"})
	if err != nil || len(page.Messages) == 0 {
		t.Fatalf("list: %v", err)
	}
	uid := page.Messages[0].UID
	open := func() {
		if _, err := e.m.Read(ctx, "alex", id, "INBOX", uid, false); err != nil {
			t.Error(err)
		}
	}
	list := func() {
		if _, err := e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX"}); err != nil {
			t.Error(err)
		}
	}
	pimtest.Bench(t, "open a mail (idle connection)", open)
	// Folder counts asked anew each time (something happened in the
	// mailbox: the counts kept for 15 s are stale; 1.13).
	fresh := func() {
		e.m.events.touch("alex", id)
		_, _ = e.m.Folders(ctx, "alex", id)
	}
	pimtest.Bench(t, "Folders with counts (13 folders, STATUS each)", fresh)
	pimtest.Bench(t, "Folders with counts, unchanged mailbox (kept)", func() { _, _ = e.m.Folders(ctx, "alex", id) })
	e.srv.SlowStatus.Store(int64(100 * time.Millisecond))
	start := time.Now()
	fresh()
	t.Logf("%-58s %s", "Folders with counts, 100 ms per STATUS", time.Since(start))
	folders := fresh
	pimtest.Blocked(t, "open a mail while the folder counts run", 6, folders, open)
	e.srv.SlowStatus.Store(0)
	e.srv.SlowSearch.Store(int64(300 * time.Millisecond))
	search := func() { _, _ = e.m.Search(ctx, "alex", id, ListQuery{Q: "rechnung"}) }
	pimtest.Blocked(t, "open a mail while a search runs", 6, search, open)
	pimtest.Blocked(t, "the list of a folder while a search runs", 6, search, list)
	start = time.Now()
	list()
	t.Logf("%-58s %s", "the list alone (its own SEARCH waits 300 ms too)", time.Since(start))
	e.srv.SlowSearch.Store(0)
	t.Logf("IMAP connections opened: %d, open now: %d", e.srv.IMAPConns.Load(), e.srv.Open.Load())
}
