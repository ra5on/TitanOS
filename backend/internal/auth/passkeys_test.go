package auth

import "testing"

// A login that finishes late with a lower signature counter must not set
// the counter back: that would let a cloned authenticator's count pass.
func TestPasskeyCounterOnlyGoesUp(t *testing.T) {
	users := newTestUsers(t)
	if _, err := users.Create("alex", "a long test password", RoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := users.AddPasskey("alex", Passkey{ID: "key", Name: "Laptop", RPID: "nas.local", SignCount: 4}); err != nil {
		t.Fatal(err)
	}
	if err := users.UsePasskey("alex", "key", 6); err != nil {
		t.Fatal(err)
	}
	if err := users.UsePasskey("alex", "key", 5); err != nil {
		t.Fatal(err)
	}
	user, _ := users.Get("alex")
	if got := user.Passkeys[0].SignCount; got != 6 {
		t.Fatalf("counter = %d, want 6", got)
	}
}
