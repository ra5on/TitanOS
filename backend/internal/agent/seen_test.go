package agent

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// A schedule that never ran carries no lastRun (it showed as "01.01.1").
func TestScheduleNeverRanHasNoLastRun(t *testing.T) {
	b, err := json.Marshal(Schedule{ID: "x", Every: "once"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "lastRun") {
		t.Fatalf("zero lastRun marshaled: %s", b)
	}
}

// A list of chats that cannot be read is not replaced by a list with only
// the chat that is being saved: every chat stays on it.
func TestDamagedChatListDoesNotLoseTheOtherChats(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Open: func() (*os.Root, error) { return os.OpenRoot(dir) }}
	start := time.Now().UTC()
	var ids []string
	for i := range 3 {
		c := &Chat{ID: NewID(), Title: "Chat", Created: start, Updated: start.Add(time.Duration(i) * time.Second), State: StateIdle}
		if err := s.SaveChat("alex", c); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	// Find and damage the list.
	var list string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == "chats.json" {
			list = p
		}
		return nil
	})
	if list == "" {
		t.Fatal("no list of chats written")
	}
	if err := os.WriteFile(list, []byte("{ damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	another := &Chat{ID: NewID(), Title: "Neu", Created: start, Updated: start.Add(time.Minute), State: StateIdle}
	if err := s.SaveChat("alex", another); err != nil {
		t.Fatal(err)
	}
	chats, err := s.Chats("alex")
	if err != nil || len(chats) != 4 {
		t.Fatalf("%d chats on the list after saving one more, want 4 (%v)", len(chats), err)
	}
	for _, id := range ids {
		found := false
		for _, c := range chats {
			found = found || c.ID == id
		}
		if !found {
			t.Fatalf("chat %s is gone from the list", id)
		}
	}
}
