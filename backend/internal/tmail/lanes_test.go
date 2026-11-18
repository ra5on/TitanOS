package tmail

import (
	"fmt"
	"testing"
	"time"
)

// laneEnv is a mailbox with mails and folders, its page's connection open.
func laneEnv(t *testing.T) (*testEnv, string, uint32) {
	t.Helper()
	e := newEnv(t)
	id := e.addMailbox()
	for i := range 3 {
		if err := e.m.CreateFolder(t.Context(), "alex", id, fmt.Sprintf("Ordner%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 5 {
		e.deliver("INBOX", plainMail("bob@example.net", fmt.Sprintf("Rechnung %d", i), "Hallo"))
	}
	page, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX"})
	if err != nil || len(page.Messages) == 0 {
		t.Fatalf("list: %v", err)
	}
	return e, id, page.Messages[0].UID
}

// A search over all folders and the folder counts run on a second
// connection: opening a mail does not wait for them (1.13).
func TestLanesOpenWhileSearching(t *testing.T) {
	e, id, uid := laneEnv(t)
	ctx := t.Context()
	e.srv.SlowSearch.Store(int64(400 * time.Millisecond))
	e.srv.SlowStatus.Store(int64(400 * time.Millisecond))
	for _, busy := range []func(){
		func() { _, _ = e.m.Search(ctx, "alex", id, ListQuery{Q: "rechnung"}) },
		func() { _, _ = e.m.Folders(ctx, "alex", id) },
	} {
		done := make(chan struct{})
		go func() { busy(); close(done) }()
		time.Sleep(100 * time.Millisecond)
		start := time.Now()
		if _, err := e.m.Read(ctx, "alex", id, "INBOX", uid, false); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > 250*time.Millisecond {
			t.Errorf("opening a mail waited %v", took)
		}
		<-done
	}
	if n := e.srv.IMAPConns.Load(); n != 2 {
		t.Fatalf("%d connections, want the page's and one in the background", n)
	}
	// Taking the grant ends both.
	if err := e.m.SetAllowed("alex", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both connections closed", func() bool { return e.srv.Open.Load() == 0 })
}

// A provider that refuses a second connection (BYE at the greeting or NO
// at the login): the background work runs on the one connection, the
// login is not taken as wrong, and the second is not tried again soon.
func TestLanesFallBackToOneConnection(t *testing.T) {
	for _, atLogin := range []bool{false, true} {
		t.Run(fmt.Sprintf("login=%v", atLogin), func(t *testing.T) {
			e, id, _ := laneEnv(t)
			e.srv.MaxConns.Store(1)
			e.srv.RefuseLogin.Store(atLogin)
			folders, err := e.m.Folders(t.Context(), "alex", id)
			if err != nil || len(folders) < 8 {
				t.Fatalf("folders on one connection: %v %d", err, len(folders))
			}
			if mb := e.m.Mailboxes("alex")[0]; mb.Status.State == StateAuthFailed || e.m.authFailed("alex", mustMailbox(t, e, id)) {
				t.Fatalf("a refused second connection counted as a wrong password: %+v", mb.Status)
			}
			if !e.m.conns.isSingle("alex", id, time.Now()) {
				t.Fatal("not remembered")
			}
			tried := e.srv.IMAPConns.Load()
			if _, err := e.m.Search(t.Context(), "alex", id, ListQuery{Q: "rechnung"}); err != nil {
				t.Fatal(err)
			}
			if e.srv.IMAPConns.Load() != tried {
				t.Fatal("the second connection was tried again")
			}
			// New settings of the mailbox forget it.
			e.m.conns.close("alex", id)
			if e.m.conns.isSingle("alex", id, time.Now()) {
				t.Fatal("kept after close")
			}
		})
	}
}

// The folder counts are kept while nothing happens in the mailbox; any
// command of the page (here opening a mail, which marks it read) makes
// them stale.
func TestFolderCountsKeptBriefly(t *testing.T) {
	e, id, uid := laneEnv(t)
	ctx := t.Context()
	first, err := e.m.Folders(ctx, "alex", id)
	if err != nil {
		t.Fatal(err)
	}
	e.srv.SlowStatus.Store(int64(300 * time.Millisecond))
	start := time.Now()
	again, err := e.m.Folders(ctx, "alex", id)
	if err != nil || time.Since(start) > 100*time.Millisecond || len(again) != len(first) {
		t.Fatalf("kept counts: %v after %v", err, time.Since(start))
	}
	e.srv.SlowStatus.Store(0)
	unread := func(list []Folder) int {
		for _, f := range list {
			if f.Role == RoleInbox {
				return f.Unread
			}
		}
		return -1
	}
	if _, err := e.m.Read(ctx, "alex", id, "INBOX", uid, false); err != nil {
		t.Fatal(err)
	}
	after, err := e.m.Folders(ctx, "alex", id)
	if err != nil || unread(after) != unread(first)-1 {
		t.Fatalf("after reading a mail: unread %d, before %d (%v)", unread(after), unread(first), err)
	}
	// Another account never gets them.
	if _, err := e.m.Folders(ctx, "bea", id); err == nil {
		t.Fatal("bea read alex's folders")
	}
}

func mustMailbox(t *testing.T, e *testEnv, id string) Mailbox {
	t.Helper()
	mb, err := e.m.mailbox("alex", id)
	if err != nil {
		t.Fatal(err)
	}
	return mb
}
