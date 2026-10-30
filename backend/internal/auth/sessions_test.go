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
	if first.Stay() != 0 || stick.writes != 0 {
		t.Fatalf("a first start: stay %d, %d writes", first.Stay(), stick.writes)
	}
	plain, _ := first.Create("kim", "192.0.2.2", "Firefox")
	if stick.writes != 0 {
		t.Fatal("a login with the setting off wrote to the stick")
	}

	// Switched on, the setting takes the login there is along.
	if err := first.SetStay(90); err != nil {
		t.Fatal(err)
	}
	kept, _ := first.Create("alex", "192.0.2.1", "Safari")
	// Setting, logins after switching on, the new login.
	if stick.writes != 3 || strings.Contains(string(stick.files[RememberFile]), kept) || !strings.Contains(string(stick.files[RememberFile]), "kim") {
		t.Fatalf("stick after login: %d writes, %s", stick.writes, stick.files[RememberFile])
	}
	first.now = func() time.Time { return time.Now().Add(40 * 24 * time.Hour) }
	first.Get(kept)
	if stick.writes != 3 {
		t.Error("using a remembered login wrote to the stick")
	}
	if list := first.List("alex", kept); len(list) != 1 || !list[0].Kept || list[0].Until == nil {
		t.Errorf("list: %+v", list)
	}

	// A restart of the NAS: RAM is empty, the stick is not. 40 days are
	// past the idle time and SessionMaxAge, not past 90 days.
	second := NewSessions()
	second.now = func() time.Time { return time.Now().Add(40 * 24 * time.Hour) }
	if err := second.Remember(stick); err != nil {
		t.Fatal(err)
	}
	if session, ok := second.Get(kept); !ok || session.User != "alex" || !session.Remember {
		t.Fatalf("remembered login after the restart: %+v %v", session, ok)
	}
	if _, ok := second.Get(plain); !ok {
		t.Error("a login from before switching on did not outlive the restart")
	}
	if stick.writes != 3 {
		t.Error("taking the logins over wrote to the stick")
	}
	second.now = func() time.Time { return time.Now().Add(91 * 24 * time.Hour) }
	if _, ok := second.Get(kept); ok {
		t.Error("a login outlived the 90 days")
	}

	// Without an end it stays.
	if err := second.SetStay(StayForever); err != nil {
		t.Fatal(err)
	}
	forever, _ := second.Create("alex", "192.0.2.1", "Safari")
	second.now = func() time.Time { return time.Now().Add(5 * 365 * 24 * time.Hour) }
	if _, ok := second.Get(forever); !ok {
		t.Error("a login without an end ended")
	}
	if list := second.List("alex", forever); len(list) != 1 || !list[0].Kept || list[0].Until != nil {
		t.Errorf("list without an end: %+v", list)
	}

	// Ending it, also for the whole account, removes it from the stick.
	second.DeleteUser("alex", "")
	third := NewSessions()
	third.Remember(stick)
	if _, ok := third.Get(forever); ok {
		t.Error("an ended remembered login came back")
	}

	// Switched off, the logins end with the next restart and the idle time
	// counts again.
	token, _ := third.Create("alex", "192.0.2.1", "Safari")
	if err := third.SetStay(0); err != nil {
		t.Fatal(err)
	}
	fourth := NewSessions()
	fourth.Remember(stick)
	if _, ok := fourth.Get(token); ok {
		t.Error("a login outlived the restart with the setting off")
	}
	if err := fourth.SetStay(7); err == nil {
		t.Error("a choice that is not offered was taken")
	}

	// An unreadable stick is left alone.
	stick.broken = true
	writes := stick.writes
	fifth := NewSessions()
	if err := fifth.Remember(stick); err == nil {
		t.Error("an unreadable stick was not reported")
	}
	fifth.Create("alex", "192.0.2.1", "Safari")
	if stick.writes != writes {
		t.Error("the file on an unreadable stick was overwritten")
	}
}

// Whoever ticked "stay logged in" in 0.37.0 keeps 30 days.
func TestStayFromBeforeTheSetting(t *testing.T) {
	stick := &memKeeper{files: map[string][]byte{}}
	old := NewSessions()
	old.Remember(stick)
	old.SetStay(30)
	token, _ := old.Create("alex", "192.0.2.1", "Safari")
	delete(stick.files, StayFile)

	next := NewSessions()
	if err := next.Remember(stick); err != nil {
		t.Fatal(err)
	}
	if next.Stay() != 30 {
		t.Errorf("stay %d", next.Stay())
	}
	if _, ok := next.Get(token); !ok {
		t.Error("the login of 0.37.0 is gone")
	}
}
