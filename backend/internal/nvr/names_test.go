package nvr

import (
	"bytes"
	"testing"
	"time"
)

// PLAN 82-3 (owner 13.11.2026): who may see the names and plates the NVR
// recognised at a camera is a right of its own, next to live and
// recordings. Owners and administrators always have it; nobody else
// unless ticked, also after an update from 0.78.
func TestNamesRight(t *testing.T) {
	a, store, accounts := testAccess(t)
	cameras := []string{"haustuer", "garten"}
	// A file of 0.78: anna may watch the front door live and its recordings.
	given := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	file078 := []byte(`{"cameras":{"haustuer":{"live":["anna"],"recordings":["anna"]}},"given":{"anna":"2026-11-01T00:00:00Z"}}`)
	if err := store.Write(accessFile, file078); err != nil {
		t.Fatal(err)
	}
	if err := a.Load(); err != nil {
		t.Fatalf("a file of 0.78: %v", err)
	}
	if !a.Allows("anna", "haustuer", RightRecordings) || a.Allows("anna", "haustuer", RightNames) || a.User("anna", cameras)["haustuer"].Names {
		t.Fatal("after the update nobody but administrators sees names")
	}
	if !a.Allows("owner", "haustuer", RightNames) || !a.Allows("owner", "garten", RightNames) {
		t.Fatal("administrators always see names")
	}
	// The same answer again writes nothing.
	if err := a.SetUser("anna", UserRights{"haustuer": {Live: true, Recordings: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	if data, _ := store.Read(accessFile); !bytes.Equal(data, file078) {
		t.Errorf("an unchanged answer rewrote the file: %s", data)
	}
	// Names without recordings are not given: they belong to the events.
	if err := a.SetUser("ben", UserRights{"garten": {Live: true, Names: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	if a.Allows("ben", "garten", RightNames) || a.User("ben", cameras)["garten"].Names {
		t.Error("names without recordings")
	}
	if err := a.SetUser("anna", UserRights{"haustuer": {Live: true, Recordings: true, Names: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	if !a.Allows("anna", "haustuer", RightNames) || a.Allows("anna", "garten", RightNames) || !a.User("anna", cameras)["haustuer"].Names {
		t.Fatal("anna's names on the front door")
	}
	// Taking the recordings takes the names along.
	if err := a.SetUser("anna", UserRights{"haustuer": {Live: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	if a.Allows("anna", "haustuer", RightNames) {
		t.Error("names stay without recordings")
	}
	if err := a.SetUser("anna", UserRights{"haustuer": {Live: true, Recordings: true, Names: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	// Read back from the stick: the same.
	again := NewAccess(store)
	again.Lookup = a.Lookup
	if err := again.Load(); err != nil || !again.Allows("anna", "haustuer", RightNames) {
		t.Fatalf("read back: %v", err)
	}
	// A locked account has none.
	accounts["anna"] = Account{Name: "anna", Disabled: true}
	if a.Allows("anna", "haustuer", RightNames) {
		t.Error("a locked account sees names")
	}
	// anna is deleted (the stick could not be told) and made anew: the new
	// anna inherits nothing.
	accounts["anna"] = Account{Name: "anna", Created: time.Now().Add(time.Hour)}
	if a.Allows("anna", "haustuer", RightNames) || a.User("anna", cameras)["haustuer"].Names {
		t.Error("a new account of the same name inherits the names")
	}
	accounts["anna"] = Account{Name: "anna", Created: given}
	// Forgetting the account or the camera takes the names too.
	if err := a.ForgetUser("anna"); err != nil {
		t.Fatal(err)
	}
	if a.Allows("anna", "haustuer", RightNames) {
		t.Error("a forgotten account keeps names")
	}
	if err := a.SetUser("anna", UserRights{"haustuer": {Live: true, Recordings: true, Names: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	if err := a.ForgetCamera("haustuer"); err != nil {
		t.Fatal(err)
	}
	if a.Allows("anna", "haustuer", RightNames) {
		t.Error("a deleted camera keeps names")
	}
}
