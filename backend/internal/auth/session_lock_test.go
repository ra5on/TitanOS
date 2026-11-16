package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionLockAndUnlock(t *testing.T) {
	sessions := NewSessions()
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	sessions.now = func() time.Time { return now }
	file := filepath.Join(t.TempDir(), "sessions.json")
	if err := sessions.Persist(file); err != nil {
		t.Fatal(err)
	}
	token, _ := sessions.Create("alex", "192.0.2.1", "test")
	if sessions.Lock("") || sessions.Lock("unknown") {
		t.Error("locked a session that does not exist")
	}
	if !sessions.Lock(token) {
		t.Fatal("lock refused")
	}
	if s, ok := sessions.Peek(token); !ok || !s.Locked || !s.LockedAt.Equal(now) {
		t.Fatalf("after lock: %+v %v", s, ok)
	}
	// Using a locked session does not unlock it.
	now = now.Add(time.Minute)
	if s, ok := sessions.Get(token); !ok || !s.Locked {
		t.Fatalf("Get unlocked: %+v", s)
	}
	// A restart of titand keeps the lock (the file in /run).
	again := NewSessions()
	again.now = sessions.now
	if err := again.Persist(file); err != nil {
		t.Fatal(err)
	}
	if s, ok := again.Peek(token); !ok || !s.Locked {
		t.Fatalf("lock lost over a restart of titand: %+v", s)
	}
	if !sessions.Unlock(token) {
		t.Fatal("unlock refused")
	}
	if s, _ := sessions.Peek(token); s.Locked || !s.LockedAt.IsZero() || !s.LastSeen.Equal(now) {
		t.Fatalf("after unlock: %+v", s)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), `"Locked":true`) {
		t.Error("file still holds the lock")
	}
}

func TestLockIfIdle(t *testing.T) {
	sessions := NewSessions()
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	sessions.now = func() time.Time { return now }
	token, _ := sessions.Create("alex", "192.0.2.1", "test")
	now = now.Add(14 * time.Minute)
	if sessions.LockIfIdle(token, 15*time.Minute) {
		t.Fatal("locked before the time")
	}
	sessions.Get(token)
	now = now.Add(14 * time.Minute)
	if sessions.LockIfIdle(token, 15*time.Minute) {
		t.Fatal("use did not count")
	}
	if sessions.LockIfIdle(token, 0) {
		t.Fatal("0 locked")
	}
	now = now.Add(2 * time.Minute)
	if !sessions.LockIfIdle(token, 15*time.Minute) {
		t.Fatal("not locked after 16 idle minutes")
	}
	if sessions.LockIfIdle(token, 15*time.Minute) {
		t.Error("a locked session reported as newly locked")
	}
	if s, _ := sessions.Peek(token); !s.Locked {
		t.Error("not locked")
	}
}

// A remembered login that comes back after a restart of the NAS starts
// from its login time, so the automatic lock catches it.
func TestRememberedSessionLocksAfterRestart(t *testing.T) {
	stick := &memKeeper{files: map[string][]byte{}}
	first := NewSessions()
	first.Remember(stick)
	if err := first.SetStay(30); err != nil {
		t.Fatal(err)
	}
	token, _ := first.Create("alex", "192.0.2.1", "test")
	first.Flush()
	second := NewSessions()
	second.now = func() time.Time { return time.Now().Add(time.Hour) }
	if err := second.Remember(stick); err != nil {
		t.Fatal(err)
	}
	if !second.LockIfIdle(token, 15*time.Minute) {
		t.Error("a remembered login came back unlocked after an hour")
	}
}

func TestLockedSessionStillRunsOut(t *testing.T) {
	sessions := NewSessions()
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	sessions.now = func() time.Time { return now }
	token, _ := sessions.Create("alex", "192.0.2.1", "test")
	sessions.Lock(token)
	// The lock screen polls GET /session; that is no use of the session.
	for range 8 {
		now = now.Add(24 * time.Hour)
		sessions.Get(token)
	}
	if _, ok := sessions.Get(token); ok {
		t.Fatal("a locked session kept alive by polling")
	}
	// An unlocked session is kept alive by use.
	token, _ = sessions.Create("alex", "192.0.2.1", "test")
	for range 8 {
		now = now.Add(24 * time.Hour)
		if _, ok := sessions.Get(token); !ok {
			t.Fatal("a used session ran out")
		}
	}
}

// Z-9 (owner 16 Nov 2026): a remembered login locked by hand stays locked
// over a restart of the NAS, also on a phone without an automatic lock. A
// small mark on the stick is written when it is locked or unlocked, never
// else; remembered.json itself does not change.
func TestLockedRememberedSessionStaysLockedAfterRestart(t *testing.T) {
	stick := &memKeeper{files: map[string][]byte{}}
	first := NewSessions()
	first.Remember(stick)
	if err := first.SetStay(30); err != nil {
		t.Fatal(err)
	}
	token, _ := first.Create("alex", "192.0.2.1", "iPhone Mobile")
	other, _ := first.Create("alex", "192.0.2.2", "Firefox")
	first.Flush()
	writes, remembered := stick.writes, string(stick.files[RememberFile])
	first.Get(token)
	first.Flush()
	if stick.writes != writes {
		t.Fatal("using a login wrote to the stick")
	}
	first.Lock(token)
	first.Flush()
	if stick.writes != writes+1 || string(stick.files[RememberFile]) != remembered {
		t.Fatalf("lock: %d writes (want %d), remembered.json changed: %v", stick.writes, writes+1, string(stick.files[RememberFile]) != remembered)
	}
	if strings.Contains(string(stick.files[LockedFile]), token) || !strings.Contains(string(stick.files[LockedFile]), "lockedAt") {
		t.Fatalf("mark: %s", stick.files[LockedFile])
	}
	first.Lock(token)
	first.Flush()
	if stick.writes != writes+1 {
		t.Fatal("locking a locked login wrote again")
	}

	// The NAS restarts: the locked login comes back locked, the other not.
	second := NewSessions()
	if err := second.Remember(stick); err != nil {
		t.Fatal(err)
	}
	if s, ok := second.Peek(token); !ok || !s.Locked {
		t.Fatalf("locked login after the restart: %+v %v", s, ok)
	}
	if s, ok := second.Peek(other); !ok || s.Locked {
		t.Fatalf("other login after the restart: %+v %v", s, ok)
	}
	second.Flush()
	if stick.writes != writes+1 {
		t.Fatal("the start wrote to the stick")
	}
	// Unlocked, it comes back unlocked.
	second.Unlock(token)
	second.Flush()
	if stick.writes != writes+2 {
		t.Fatalf("unlock: %d writes", stick.writes)
	}
	third := NewSessions()
	third.Remember(stick)
	if s, _ := third.Peek(token); s.Locked {
		t.Fatal("an unlocked login came back locked")
	}

	// A mark that cannot be read locks every login taken over from the
	// stick rather than letting a locked one in.
	stick.files[LockedFile] = []byte("{broken")
	fourth := NewSessions()
	fourth.Remember(stick)
	if s, _ := fourth.Peek(other); !s.Locked {
		t.Fatal("unreadable marks: a login came back unlocked")
	}
}

// Without "stay logged in" nothing is written for a lock: the session ends
// with the restart anyway.
func TestLockOfAPlainSessionWritesNothing(t *testing.T) {
	stick := &memKeeper{files: map[string][]byte{}}
	sessions := NewSessions()
	sessions.Remember(stick)
	token, _ := sessions.Create("alex", "192.0.2.1", "test")
	sessions.Lock(token)
	sessions.Unlock(token)
	sessions.Flush()
	if stick.writes != 0 {
		t.Fatalf("%d writes for the lock of a plain session", stick.writes)
	}
}
