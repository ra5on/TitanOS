package auth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestLockAccount(t *testing.T) {
	store := config.Open(t.TempDir())
	users := NewUsers(store)
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	users.CreateOwner("alex", "long enough password")
	users.Create("gast", "long enough password", RoleUser)
	users.Create("bea", "long enough password", RoleAdmin)

	// The only owner cannot be locked.
	if _, err := users.SetDisabled("alex", true, "bea"); !errors.Is(err, ErrLastActiveOwner) {
		t.Errorf("lock the only owner: %v", err)
	}
	gast, err := users.SetDisabled("gast", true, "alex")
	if err != nil || !gast.Disabled || gast.DisabledBy != "alex" || gast.DisabledAt.IsZero() {
		t.Fatalf("lock: %+v %v", gast, err)
	}
	if _, ok := users.Authenticate("gast", "long enough password"); ok {
		t.Error("a locked account logs in")
	}
	if _, ok := users.Verify("gast", "long enough password"); !ok {
		t.Error("Verify does not tell a locked account by its password")
	}
	if _, ok := users.Verify("gast", "wrong password!!"); ok {
		t.Error("Verify accepts a wrong password")
	}
	// Locking again keeps who and when, without writing the stick.
	before := store.Changes()
	again, err := users.SetDisabled("gast", true, "bea")
	if err != nil || again.DisabledBy != "alex" || store.Changes() != before {
		t.Errorf("second lock: %+v %v, writes %d", again, err, store.Changes()-before)
	}

	// The lock survives a restart of titand.
	reloaded := NewUsers(store)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if user, _ := reloaded.Get("gast"); !user.Disabled || user.DisabledBy != "alex" || user.SMBHash != gast.SMBHash || user.PasswordHash != gast.PasswordHash {
		t.Errorf("after reload: %+v", user)
	}

	// A password set by an administrator keeps the lock.
	if user, err := users.SetPassword("gast", "a completely new password"); err != nil || !user.Disabled {
		t.Errorf("password on a locked account: %+v %v", user, err)
	}
	if _, ok := users.Authenticate("gast", "a completely new password"); ok {
		t.Error("a new password unlocked the account")
	}
	user, err := users.SetDisabled("gast", false, "alex")
	if err != nil || user.Disabled || user.DisabledBy != "" || !user.DisabledAt.IsZero() {
		t.Errorf("unlock: %+v %v", user, err)
	}
	if _, ok := users.Authenticate("gast", "a completely new password"); !ok {
		t.Error("an unlocked account does not log in")
	}
	if _, err := users.SetDisabled("nobody-here", true, "alex"); !errors.Is(err, ErrNotFound) {
		t.Errorf("lock a missing account: %v", err)
	}
}

func TestLastActiveOwner(t *testing.T) {
	users := NewUsers(config.Open(t.TempDir()))
	users.Load()
	users.CreateOwner("alex", "long enough password")
	users.Create("chris", "long enough password", RoleOwner)

	if _, err := users.SetDisabled("chris", true, "alex"); err != nil {
		t.Fatal(err)
	}
	// alex is now the one owner who can log in: not to be locked, demoted or removed.
	if _, err := users.SetDisabled("alex", true, "chris"); !errors.Is(err, ErrLastActiveOwner) {
		t.Errorf("lock: %v", err)
	}
	if _, err := users.SetRole("alex", RoleAdmin); !errors.Is(err, ErrLastActiveOwner) {
		t.Errorf("demote: %v", err)
	}
	if err := users.Delete("alex"); !errors.Is(err, ErrLastActiveOwner) {
		t.Errorf("delete: %v", err)
	}
	// The locked owner can be demoted or removed.
	if _, err := users.SetRole("chris", RoleAdmin); err != nil {
		t.Errorf("demote the locked owner: %v", err)
	}
	if err := users.Delete("chris"); err != nil {
		t.Errorf("delete the locked admin: %v", err)
	}
}

// TestLockedFileForOlderVersions checks what titand before 0.28.0 sees in
// the file: it does not know "disabled", so the stored form must refuse the
// account on its own.
func TestLockedFileForOlderVersions(t *testing.T) {
	hash, _ := hashPassword("long enough password")
	user := User{Name: "bea", Role: RoleAdmin, PasswordHash: hash, UID: 2001, SMBHash: "AABB", SSHKeys: []string{"ssh-ed25519 AAAA bea@laptop"}, Disabled: true, DisabledBy: "alex"}
	data, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	// What an older version reads: the same struct without the new fields.
	var old struct {
		PasswordHash string   `json:"passwordHash"`
		SMBHash      string   `json:"smbHash"`
		SSHKeys      []string `json:"sshKeys"`
	}
	json.Unmarshal(data, &old)
	if ok, _ := verifyPassword(old.PasswordHash, "long enough password"); ok {
		t.Error("an older version would accept the password of a locked account")
	}
	if old.SMBHash != "" || len(old.SSHKeys) != 0 {
		t.Errorf("an older version would offer SMB or SSH: %+v", old)
	}
	if TokenAccountKey(User{PasswordHash: old.PasswordHash}) == TokenAccountKey(user) {
		t.Error("API tokens would still match in an older version")
	}

	var back User
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Disabled || back.PasswordHash != hash || back.SMBHash != "AABB" || len(back.SSHKeys) != 1 || back.DisabledBy != "alex" {
		t.Errorf("read back: %+v", back)
	}

	// An older version rewrote the file: the flag and the parked fields are
	// gone, the prefixed hash stays. The account is still locked.
	rewritten := strings.NewReplacer(`"disabled": true,`, "", `"disabled":true,`, "").Replace(string(data))
	var stripped map[string]any
	json.Unmarshal([]byte(rewritten), &stripped)
	delete(stripped, "disabledSmbHash")
	delete(stripped, "disabledSshKeys")
	delete(stripped, "disabledBy")
	delete(stripped, "disabled")
	rewrittenData, _ := json.Marshal(stripped)
	var after User
	if err := json.Unmarshal(rewrittenData, &after); err != nil {
		t.Fatal(err)
	}
	if !after.Disabled || after.PasswordHash != hash || after.SMBHash != "" {
		t.Errorf("after an older version rewrote it: %+v", after)
	}

	// An unlocked account is written as before.
	user.Disabled, user.DisabledBy = false, ""
	plain, _ := json.Marshal(user)
	for _, field := range []string{"disabled", lockedHashPrefix} {
		if strings.Contains(string(plain), field) {
			t.Errorf("unlocked account carries %q: %s", field, plain)
		}
	}
}
