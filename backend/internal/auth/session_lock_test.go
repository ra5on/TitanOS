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
// from its login time, so the automatic lock catches it: the lock itself
// was never written to the stick.
func TestRememberedSessionLocksAfterRestart(t *testing.T) {
	stick := &memKeeper{files: map[string][]byte{}}
	first := NewSessions()
	first.Remember(stick)
	if err := first.SetStay(30); err != nil {
		t.Fatal(err)
	}
	token, _ := first.Create("alex", "192.0.2.1", "test")
	first.Lock(token)
	first.Flush()
	if strings.Contains(string(stick.files[RememberFile]), "Locked") {
		t.Fatal("the lock was written to the stick")
	}
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
