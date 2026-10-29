package auth

import (
	"encoding/json"
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

// memKeeper is /config in memory and counts the writes.
type memKeeper struct {
	files  map[string][]byte
	writes int
	broken bool
}

func (k *memKeeper) ReadJSON(name string, v any) error {
	if k.broken {
		return os.ErrPermission
	}
	data, ok := k.files[name]
	if !ok {
		return os.ErrNotExist
	}
	return json.Unmarshal(data, v)
}

func (k *memKeeper) WriteJSON(name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	k.files[name] = data
	k.writes++
	return nil
}

func TestRememberedSessionsOutliveARestartOfTheNAS(t *testing.T) {
	stick := &memKeeper{files: map[string][]byte{}}
	first := NewSessions()
	if err := first.Remember(stick); err != nil {
		t.Fatal(err)
	}
	if stick.writes != 0 {
		t.Fatal("a start without remembered logins wrote to the stick")
	}
	kept, _ := first.CreateRemembered("alex", "192.0.2.1", "Safari")
	plain, _ := first.Create("kim", "192.0.2.2", "Firefox")
	if stick.writes != 1 || strings.Contains(string(stick.files[RememberFile]), kept) || strings.Contains(string(stick.files[RememberFile]), "kim") {
		t.Fatalf("stick after login: %d writes, %s", stick.writes, stick.files[RememberFile])
	}
	first.now = func() time.Time { return time.Now().Add(10 * 24 * time.Hour) }
	first.Get(kept)
	if stick.writes != 1 {
		t.Error("using a remembered login wrote to the stick")
	}

	// A restart of the NAS: RAM is empty, the stick is not.
	second := NewSessions()
	second.now = func() time.Time { return time.Now().Add(20 * 24 * time.Hour) }
	if err := second.Remember(stick); err != nil {
		t.Fatal(err)
	}
	if session, ok := second.Get(kept); !ok || session.User != "alex" || !session.Remember {
		t.Fatalf("remembered login after the restart: %+v %v", session, ok)
	}
	if _, ok := second.Get(plain); ok {
		t.Error("a normal login outlived the restart")
	}
	if stick.writes != 1 {
		t.Error("taking the logins over wrote to the stick")
	}

	// Ending it, also for the whole account, removes it from the stick.
	second.DeleteUser("alex", "")
	third := NewSessions()
	third.Remember(stick)
	if _, ok := third.Get(kept); ok {
		t.Error("an ended remembered login came back")
	}

	// It ends after SessionMaxAge, not after the idle time.
	fourth := NewSessions()
	fourth.Remember(stick)
	token, _ := fourth.CreateRemembered("alex", "192.0.2.1", "Safari")
	fourth.now = func() time.Time { return time.Now().Add(SessionIdleTimeout + time.Hour) }
	if _, ok := fourth.Get(token); !ok {
		t.Error("a remembered login ended after the idle time")
	}
	fourth.now = func() time.Time { return time.Now().Add(SessionMaxAge + time.Hour) }
	if _, ok := fourth.Get(token); ok {
		t.Error("a remembered login outlived SessionMaxAge")
	}

	// An unreadable stick is left alone.
	stick.broken = true
	writes := stick.writes
	fifth := NewSessions()
	if err := fifth.Remember(stick); err == nil {
		t.Error("an unreadable stick was not reported")
	}
	fifth.CreateRemembered("alex", "192.0.2.1", "Safari")
	if stick.writes != writes {
		t.Error("the file on an unreadable stick was overwritten")
	}
}
