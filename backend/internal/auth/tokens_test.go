package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestPersonalTokensHashRestartAndRevoke(t *testing.T) {
	dir := t.TempDir()
	// Tokens exist only next to accounts: the stick has its users folder.
	os.Mkdir(filepath.Join(dir, "users"), 0o700)
	store := config.Open(dir)
	tokens := NewTokens(store)
	user := User{Name: "alex", Role: RoleOwner, Created: time.Now().UTC()}
	for i := 0; i < 20; i++ {
		token, plain, err := tokens.Create(user, "Monitoring", nil)
		if err != nil {
			t.Fatal(err)
		}
		if token.Hash != "" || len(token.Scopes) != 1 || token.Scopes[0] != "system:read" {
			t.Fatal(token)
		}
		raw, _ := store.Read(tokenFile)
		if strings.Contains(string(raw), plain) {
			t.Fatal("plaintext token persisted")
		}
		restarted := NewTokens(store)
		if _, ok := restarted.Lookup(plain); !ok {
			t.Fatal("token lost at restart")
		}
		if _, ok := restarted.Lookup(plain + "x"); ok {
			t.Fatal("wrong secret accepted")
		}
		if err := restarted.Revoke(user.Name, token.ID); err != nil {
			t.Fatal(err)
		}
		if _, ok := restarted.Lookup(plain); ok {
			t.Fatal("revoked token accepted")
		}
		if _, ok := NewTokens(store).Lookup(plain); ok {
			t.Fatal("revocation lost")
		}
		tokens = NewTokens(store)
	}
	if _, _, err := tokens.Create(User{Name: "user", Role: RoleUser}, "admin", []string{"docker:write"}); err == nil {
		t.Fatal("user minted administrative scope")
	}
	if err := store.Write(tokenFile, []byte("broken")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewTokens(store).Create(user, "new", nil); err == nil {
		t.Fatal("corrupt token inventory overwritten")
	}
}

// Tokens of a deleted account do not belong to a new account of the same
// name: not listed, not counted toward the limit, cleared with the next
// token of the new account, and removed with the account (RemoveUser).
func TestTokensOfAnEarlierAccountDoNotCount(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "users"), 0o700)
	store := config.Open(dir)
	tokens := NewTokens(store)
	old := User{Name: "alex", Role: RoleOwner, Created: time.Now().UTC().Add(-time.Hour), PasswordHash: "old"}
	for range 32 {
		if _, _, err := tokens.Create(old, "old", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := tokens.Create(old, "one too many", nil); err == nil {
		t.Fatal("limit of 32 not kept")
	}
	again := User{Name: "alex", Role: RoleOwner, Created: time.Now().UTC(), PasswordHash: "new"}
	if list := tokens.List(again); len(list) != 0 {
		t.Fatalf("tokens of the earlier account listed: %d", len(list))
	}
	if _, _, err := tokens.Create(again, "new", nil); err != nil {
		t.Fatalf("earlier tokens counted: %v", err)
	}
	restarted := NewTokens(store)
	if n := len(restarted.tokens); n != 1 {
		t.Fatalf("%d tokens kept on the stick, want only the new one", n)
	}
	if err := restarted.RemoveUser("alex"); err != nil {
		t.Fatal(err)
	}
	if n := len(NewTokens(store).tokens); n != 0 {
		t.Fatalf("%d tokens left after removing the account", n)
	}
	if err := restarted.RemoveUser("nobody"); err != nil {
		t.Fatal(err)
	}
}
