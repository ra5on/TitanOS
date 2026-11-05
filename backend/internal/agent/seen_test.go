package agent

import (
	"os"
	"testing"
	"time"
)

// PLAN 48: read states. The page reports a visible chat every 15 seconds;
// without a change that must not touch the volume (Ruhemodus).
func TestSeenNeedsNoDiskWithoutChange(t *testing.T) {
	dir := t.TempDir()
	opens := 0
	s := &Store{Open: func() (*os.Root, error) { opens++; return os.OpenRoot(dir) }}
	start := time.Now().UTC()
	if _, err := s.Chats("alex"); err != nil { // read states begin
		t.Fatal(err)
	}
	c := &Chat{ID: NewID(), Title: "Hallo", Created: start, Updated: start.Add(time.Second), State: StateIdle}
	if err := s.SaveChat("alex", c); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Chats("alex")
	s.MarkUnread("alex", list)
	if len(list) != 1 || !list[0].Unread {
		t.Fatalf("new answer not unread: %+v", list)
	}
	if err := s.Seen("alex", c.ID, start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Chats("alex")
	s.MarkUnread("alex", list)
	if list[0].Unread {
		t.Fatal("still unread after seen")
	}
	before := opens
	for range 5 {
		if err := s.Seen("alex", c.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if opens != before {
		t.Fatalf("seen without a change opened the volume %d times", opens-before)
	}
	if err := s.Seen("alex", NewID(), time.Now()); err != ErrNotFound {
		t.Fatalf("unknown chat: %v", err)
	}
	// A new answer is unread again; a deleted chat is forgotten.
	c.Updated = time.Now().UTC().Add(time.Minute)
	if err := s.SaveChat("alex", c); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Chats("alex")
	s.MarkUnread("alex", list)
	if !list[0].Unread {
		t.Fatal("new answer not unread")
	}
	if err := s.DeleteChat("alex", c.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Seen("alex", c.ID, time.Now()); err != ErrNotFound {
		t.Fatalf("deleted chat: %v", err)
	}
	// Chats from before read states existed count as read.
	old := &Chat{ID: NewID(), Title: "Alt", Created: start.Add(-time.Hour), Updated: start.Add(-time.Hour), State: StateIdle}
	if err := s.SaveChat("alex", old); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Chats("alex")
	s.MarkUnread("alex", list)
	if list[0].Unread {
		t.Fatal("old chat unread")
	}
}
