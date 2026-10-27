package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestNewRecoveryCodes(t *testing.T) {
	users := NewUsers(config.Open(t.TempDir()))
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateOwner("alex", "a long test password"); err != nil {
		t.Fatal(err)
	}
	if _, err := users.NewRecoveryCodes("alex"); !errors.Is(err, ErrNoTwoFactor) {
		t.Errorf("new codes without a second factor: %v", err)
	}
	secret, _, _ := users.BeginTwoFactor("alex", "TitanOS")
	code, _ := totpCode(secret, uint64(time.Now().Unix())/30)
	old, err := users.EnableTwoFactor("alex", code)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := users.NewRecoveryCodes("alex")
	if err != nil || len(fresh) != recoveryCodes {
		t.Fatalf("new codes: %v, %v", fresh, err)
	}
	user, _ := users.Get("alex")
	if user.TOTPSecret != secret || len(user.RecoveryCodes) != recoveryCodes {
		t.Fatal("the secret changed or the codes were not replaced")
	}
	for _, stored := range user.RecoveryCodes {
		if stored == fresh[0] {
			t.Fatal("a recovery code is stored in the clear")
		}
	}
	if users.CheckSecondFactor("alex", old[0]) {
		t.Error("an old recovery code still works")
	}
	if !users.CheckSecondFactor("alex", fresh[0]) || users.CheckSecondFactor("alex", fresh[0]) {
		t.Error("a new recovery code does not work exactly once")
	}
}
