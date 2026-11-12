package nvr

import (
	"errors"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func testAccess(t *testing.T) (*Access, *config.Store, map[string]Account) {
	t.Helper()
	store := config.Open(t.TempDir())
	accounts := map[string]Account{
		"owner": {Name: "owner", Admin: true},
		"anna":  {Name: "anna"},
		"ben":   {Name: "ben"},
		"lena":  {Name: "lena", Disabled: true},
	}
	a := NewAccess(store)
	a.Lookup = func(name string) (Account, bool) {
		account, ok := accounts[name]
		return account, ok
	}
	if err := a.Load(); err != nil {
		t.Fatal(err)
	}
	return a, store, accounts
}

func TestAccess(t *testing.T) {
	a, store, accounts := testAccess(t)
	cameras := []string{"haustuer", "garten"}
	// Owners see everything, a new account nothing, an unknown one nothing.
	if !a.Allows("owner", "haustuer", RightLive) || !a.Allows("owner", "garten", RightRecordings) {
		t.Error("owners and administrators see every camera")
	}
	if a.Allows("anna", "haustuer", RightLive) || a.Sees("anna", "haustuer") || a.Allows("nobody", "haustuer", RightLive) {
		t.Error("a new account sees no camera")
	}
	rights := UserRights{"haustuer": {Live: true}, "garten": {Live: true, Recordings: true}}
	if err := a.SetUser("anna", rights, cameras); err != nil {
		t.Fatal(err)
	}
	if !a.Allows("anna", "haustuer", RightLive) || a.Allows("anna", "haustuer", RightRecordings) || !a.Allows("anna", "garten", RightRecordings) {
		t.Error("live and recordings are separate ticks")
	}
	if a.Allows("ben", "haustuer", RightLive) || a.Allows("anna", "haustuer", "admin") {
		t.Error("only the ticked account and only the two rights")
	}
	if got := a.User("anna", cameras); got["haustuer"].Recordings || !got["garten"].Recordings || !got["haustuer"].Live {
		t.Errorf("User = %+v", got)
	}
	// Refused: administrators have no ticks, unknown accounts and cameras.
	for name, err := range map[string]error{
		"admin":          a.SetUser("owner", rights, cameras),
		"unknown user":   a.SetUser("nobody", rights, cameras),
		"unknown camera": a.SetUser("anna", UserRights{"keller": {Live: true}}, cameras),
	} {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A locked account sees nothing, ticked or not.
	if err := a.SetUser("lena", rights, cameras); err != nil || a.Allows("lena", "garten", RightLive) {
		t.Errorf("locked account: %v", err)
	}
	// The same rights again write nothing.
	writes := store.Writes()
	if err := a.SetUser("anna", rights, cameras); err != nil || store.Writes() != writes {
		t.Errorf("unchanged rights were written again (%v)", err)
	}
	// The rights survive a restart.
	again := NewAccess(store)
	again.Lookup = a.Lookup
	if err := again.Load(); err != nil || !again.Allows("anna", "garten", RightRecordings) {
		t.Errorf("after Load: %v", err)
	}
	// A cleared tick, a deleted camera, a deleted account.
	if err := a.SetUser("anna", UserRights{"haustuer": {}}, cameras); err != nil || a.Sees("anna", "haustuer") || !a.Sees("anna", "garten") {
		t.Errorf("clearing: %v", err)
	}
	if err := a.ForgetCamera("garten"); err != nil || a.Sees("anna", "garten") || a.Sees("lena", "garten") {
		t.Errorf("deleted camera: %v", err)
	}
	if err := a.SetUser("ben", UserRights{"haustuer": {Live: true, Recordings: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	if err := a.ForgetUser("ben"); err != nil {
		t.Fatal(err)
	}
	// A new account of the same name starts with nothing.
	accounts["ben"] = Account{Name: "ben"}
	if a.Sees("ben", "haustuer") {
		t.Error("a deleted account keeps its rights")
	}
}
