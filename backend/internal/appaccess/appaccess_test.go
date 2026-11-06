package appaccess

import (
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fixture struct {
	m        *Manager
	store    *config.Store
	accounts map[string]Account
	now      time.Time
	// sessions are the TitanOS logins (ID → account); locked ones are
	// marked in locked.
	sessions map[string]string
	locked   map[string]bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{store: config.Open(t.TempDir()), now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	f.accounts = map[string]Account{
		"alex": {Name: "alex", Admin: true},
		"anna": {Name: "anna"},
		"leon": {Name: "leon"},
	}
	f.m = New(f.store)
	f.m.Lookup = func(name string) (Account, bool) { a, ok := f.accounts[name]; return a, ok }
	f.m.Now = func() time.Time { return f.now }
	f.sessions = map[string]string{"s-alex": "alex", "s-anna": "anna", "s-anna2": "anna"}
	f.locked = map[string]bool{}
	f.m.SessionUsable = func(id, user string) bool { return f.sessions[id] == user && !f.locked[id] }
	if err := f.m.Load(); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRights(t *testing.T) {
	f := newFixture(t)
	if !f.m.Allows("alex", "immich") {
		t.Error("administrators may use every app")
	}
	if f.m.Allows("anna", "immich") {
		t.Error("a new app is for administrators only by default")
	}
	if err := f.m.Set(Settings{Apps: map[string]App{"immich": {Users: []string{"anna", "alex", "ghost", "anna"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := f.m.Settings().Apps["immich"].Users; len(got) != 1 || got[0] != "anna" {
		t.Errorf("stored ticks = %v, want only anna", got)
	}
	if !f.m.Allows("anna", "immich") || f.m.Allows("leon", "immich") {
		t.Error("ticks not applied")
	}
	if f.m.Allows("ghost", "immich") {
		t.Error("unknown accounts never may")
	}
	f.accounts["anna"] = Account{Name: "anna", Disabled: true}
	if f.m.Allows("anna", "immich") {
		t.Error("a locked account may not")
	}
	if err := f.m.Set(Settings{NewApps: NewAppsEveryone, Apps: map[string]App{"immich": {}}}); err != nil {
		t.Fatal(err)
	}
	if !f.m.Allows("leon", "jellyfin") || f.m.Allows("leon", "immich") {
		t.Error("new apps for everyone must not override an app's own entry")
	}
	if err := f.m.Set(Settings{NewApps: "nobody"}); err == nil {
		t.Error("unknown new-apps setting accepted")
	}
	// Read back from the stick.
	again := New(f.store)
	again.Lookup = f.m.Lookup
	if err := again.Load(); err != nil || again.Settings().NewApps != NewAppsEveryone {
		t.Errorf("settings not stored: %v %+v", err, again.Settings())
	}
}

func TestSetWritesOnlyChanges(t *testing.T) {
	f := newFixture(t)
	s := Settings{Apps: map[string]App{"immich": {Users: []string{"anna"}, NoGate: true}}}
	if err := f.m.Set(s); err != nil {
		t.Fatal(err)
	}
	before := f.store.Changes()
	if err := f.m.Set(s); err != nil {
		t.Fatal(err)
	}
	if f.store.Changes() != before {
		t.Error("unchanged rights were written again")
	}
	if f.m.Gated("immich") || !f.m.Gated("jellyfin") {
		t.Error("gate switch per app wrong")
	}
}

func TestEnterAndCookie(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Set(Settings{Apps: map[string]App{"immich": {Users: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	token, expiry := f.m.EnterToken("anna", "immich", "s-anna")
	if expiry.Sub(f.now) != EnterTTL {
		t.Errorf("expiry %v", expiry)
	}
	if _, _, ok := f.m.Enter(token, "jellyfin"); ok {
		t.Error("token taken for another app")
	}
	cookie, ttl, ok := f.m.Enter(token, "immich")
	if !ok || ttl != CookieTTL {
		t.Fatal("valid token refused")
	}
	if _, _, ok := f.m.Enter(token, "immich"); ok {
		t.Error("token used twice")
	}
	if !f.m.Admit(cookie, "immich") || f.m.Admit(cookie, "jellyfin") {
		t.Error("cookie check wrong")
	}
	if f.m.Admit(token, "immich") {
		t.Error("an enter token must not work as cookie")
	}
	if f.m.Admit(cookie[:len(cookie)-2]+"xx", "immich") {
		t.Error("forged cookie admitted")
	}
	// Rights are checked each time.
	if err := f.m.Set(Settings{Apps: map[string]App{"immich": {}}}); err != nil {
		t.Fatal(err)
	}
	if f.m.Admit(cookie, "immich") {
		t.Error("revoked right still admitted")
	}
	if err := f.m.Set(Settings{Apps: map[string]App{"immich": {Users: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	delete(f.accounts, "anna")
	if f.m.Admit(cookie, "immich") {
		t.Error("deleted account still admitted")
	}
	f.accounts["anna"] = Account{Name: "anna"}
	f.now = f.now.Add(CookieTTL)
	if f.m.Admit(cookie, "immich") {
		t.Error("expired cookie admitted")
	}
}

func TestEnterTokenExpires(t *testing.T) {
	f := newFixture(t)
	token, _ := f.m.EnterToken("alex", "immich", "s-alex")
	f.now = f.now.Add(EnterTTL)
	if _, _, ok := f.m.Enter(token, "immich"); ok {
		t.Error("expired token taken")
	}
}

func TestKeyKept(t *testing.T) {
	f := newFixture(t)
	token, _ := f.m.EnterToken("alex", "immich", "s-alex")
	again := New(f.store)
	again.Lookup, again.Now, again.SessionUsable = f.m.Lookup, f.m.Now, f.m.SessionUsable
	if _, _, ok := again.Enter(token, "immich"); !ok {
		t.Error("key not kept across restarts")
	}
}

func TestPassEndsWithTheLogin(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Set(Settings{Apps: map[string]App{"immich": {Users: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	enter := func(session string) string {
		t.Helper()
		token, _ := f.m.EnterToken("anna", "immich", session)
		cookie, _, ok := f.m.Enter(token, "immich")
		if !ok {
			t.Fatal("valid token refused")
		}
		return cookie
	}
	cookie := enter("s-anna")
	other := enter("s-anna2")
	// The lock screen shuts the app, unlocking opens it again.
	f.locked["s-anna"] = true
	if f.m.Admit(cookie, "immich") {
		t.Error("cookie of a locked login admitted")
	}
	if !f.m.Admit(other, "immich") {
		t.Error("the lock of one login shut another")
	}
	delete(f.locked, "s-anna")
	if !f.m.Admit(cookie, "immich") {
		t.Error("cookie refused after unlocking")
	}
	// Logging out (or a password change ending the session, or expiry).
	delete(f.sessions, "s-anna")
	if f.m.Admit(cookie, "immich") {
		t.Error("cookie of an ended login admitted")
	}
	// A token of a locked or ended login is not taken; nor one of another
	// account's login, nor one without a login.
	f.locked["s-anna2"] = true
	token, _ := f.m.EnterToken("anna", "immich", "s-anna2")
	if _, _, ok := f.m.Enter(token, "immich"); ok {
		t.Error("token of a locked login taken")
	}
	for _, session := range []string{"s-anna", "s-alex", ""} {
		token, _ := f.m.EnterToken("anna", "immich", session)
		if _, _, ok := f.m.Enter(token, "immich"); ok {
			t.Errorf("token for login %q taken", session)
		}
	}
	// Without a session store nothing is let in.
	f.m.SessionUsable = nil
	if f.m.Admit(other, "immich") {
		t.Error("admitted without a session store")
	}
}
