package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionsOutliveARestartOfTitand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "run", "sessions.json")
	first := NewSessions()
	if err := first.Persist(file); err != nil {
		t.Fatal(err)
	}
	token, err := first.Create("alex", "192.0.2.1", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := first.Create("kim", "192.0.2.2", "Safari")
	first.Confirm(token)
	first.Delete(other)

	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("session file: %v %v", info, err)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), token) {
		t.Fatal("the file holds a token, not only its hash")
	}

	// The next titand takes the sessions over.
	second := NewSessions()
	if err := second.Persist(file); err != nil {
		t.Fatal(err)
	}
	session, ok := second.Get(token)
	if !ok || session.User != "alex" || !second.Confirmed(token) {
		t.Errorf("session after the restart: %+v %v", session, ok)
	}
	if _, ok := second.Get(other); ok {
		t.Error("a session that was ended came back")
	}

	// Expired sessions are not taken over.
	third := NewSessions()
	third.now = func() time.Time { return time.Now().Add(SessionMaxAge + time.Hour) }
	if err := third.Persist(file); err != nil {
		t.Fatal(err)
	}
	if len(third.sessions) != 0 {
		t.Error("an expired session was taken over")
	}

	// A damaged file is reported, not trusted.
	os.WriteFile(file, []byte("{"), 0o600)
	if err := NewSessions().Persist(file); err == nil {
		t.Error("a damaged file was accepted")
	}
}

func TestPeekDoesNotTouchTheSession(t *testing.T) {
	sessions := NewSessions()
	start := time.Now()
	sessions.now = func() time.Time { return start }
	token, _ := sessions.Create("alex", "192.0.2.1", "Firefox")
	sessions.now = func() time.Time { return start.Add(time.Hour) }
	if session, ok := sessions.Peek(token); !ok || session.User != "alex" || !session.LastSeen.Equal(start) {
		t.Fatalf("peek: %+v %v", session, ok)
	}
	sessions.Delete(token)
	if _, ok := sessions.Peek(token); ok {
		t.Error("an ended session is still found")
	}
	if _, ok := sessions.Peek(""); ok {
		t.Error("an empty token found a session")
	}
}
