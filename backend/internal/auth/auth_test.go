package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestPasswordHash(t *testing.T) {
	hash, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Errorf("hash = %q", hash)
	}
	if ok, err := verifyPassword(hash, "correct horse battery"); err != nil || !ok {
		t.Errorf("right password rejected: %v %v", ok, err)
	}
	if ok, _ := verifyPassword(hash, "correct horse batterx"); ok {
		t.Error("wrong password accepted")
	}
	other, _ := hashPassword("correct horse battery")
	if other == hash {
		t.Error("two hashes of the same password are equal, salt missing")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=99999999,t=3,p=2$c2FsdA$aGFzaA", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$aGFzaA"} {
		if ok, err := verifyPassword(bad, "x"); ok || err == nil {
			t.Errorf("verifyPassword(%q) = %v, %v; want an error", bad, ok, err)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, name := range []string{"alex", "a", "anna-lena", "user_2", "admin"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "Alex", "1alex", "al ex", "../x", "ä", strings.Repeat("a", 33), "-a"} {
		if err := ValidateName(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateName(%q) = %v, want ErrInvalidName", name, err)
		}
	}
	for _, name := range []string{"root", "nobody", "systemd-network", "docker"} {
		if err := ValidateName(name); !errors.Is(err, ErrReservedName) {
			t.Errorf("ValidateName(%q) = %v, want ErrReservedName", name, err)
		}
	}
}

func TestUsers(t *testing.T) {
	store := config.Open(t.TempDir())
	users := NewUsers(store)

	if _, err := users.CreateOwner("alex", "long enough password"); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("create before Load: %v, want ErrNotLoaded", err)
	}
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateOwner("alex", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: %v", err)
	}
	owner, err := users.CreateOwner("alex", "long enough password")
	if err != nil {
		t.Fatal(err)
	}
	if owner.Role != RoleOwner {
		t.Errorf("first account has role %q", owner.Role)
	}
	if _, err := users.CreateOwner("eve", "long enough password"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second owner via setup: %v, want ErrSetupDone", err)
	}
	if _, err := users.Create("alex", "long enough password", RoleUser); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v, want ErrExists", err)
	}

	if _, ok := users.Authenticate("alex", "long enough password"); !ok {
		t.Error("right password rejected")
	}
	if _, ok := users.Authenticate("alex", "wrong password!!"); ok {
		t.Error("wrong password accepted")
	}
	if _, ok := users.Authenticate("nobody-here", "long enough password"); ok {
		t.Error("unknown account accepted")
	}

	// A fresh instance reads the same accounts back from the store.
	again := NewUsers(store)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Authenticate("alex", "long enough password"); !ok || again.Count() != 1 {
		t.Error("account did not survive a reload")
	}
	raw, _ := store.Read("users/alex/account.json")
	if strings.Contains(string(raw), "long enough password") {
		t.Error("password stored in clear text")
	}
}

func TestSessions(t *testing.T) {
	sessions := NewSessions()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	sessions.now = func() time.Time { return now }

	token, err := sessions.Create("alex", "192.0.2.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := sessions.Get(token); !ok || s.User != "alex" {
		t.Fatalf("Get = %+v, %v", s, ok)
	}
	if _, ok := sessions.Get("not-a-token"); ok {
		t.Error("unknown token accepted")
	}
	if _, ok := sessions.Get(""); ok {
		t.Error("empty token accepted")
	}

	now = now.Add(SessionIdleTimeout - time.Hour)
	if _, ok := sessions.Get(token); !ok {
		t.Error("session expired before the idle timeout")
	}
	now = now.Add(SessionIdleTimeout + time.Hour)
	if _, ok := sessions.Get(token); ok {
		t.Error("session survived the idle timeout")
	}

	token, _ = sessions.Create("alex", "192.0.2.1", "test")
	sessions.Delete(token)
	if _, ok := sessions.Get(token); ok {
		t.Error("session survived logout")
	}
}

func TestLimiter(t *testing.T) {
	limiter := NewLimiter()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }

	for range limiterFreeAttempts - 1 {
		limiter.Fail("user:alex", "ip:192.0.2.1")
	}
	if wait := limiter.RetryAfter("user:alex", "ip:192.0.2.1"); wait != 0 {
		t.Fatalf("blocked after %d failures: %v", limiterFreeAttempts-1, wait)
	}
	limiter.Fail("user:alex", "ip:192.0.2.1")
	if wait := limiter.RetryAfter("user:alex"); wait != limiterBaseDelay {
		t.Errorf("wait = %v, want %v", wait, limiterBaseDelay)
	}
	// The address is blocked too, whatever account it tries next.
	if wait := limiter.RetryAfter("user:other", "ip:192.0.2.1"); wait == 0 {
		t.Error("address not blocked")
	}
	if wait := limiter.RetryAfter("user:other", "ip:192.0.2.99"); wait != 0 {
		t.Error("unrelated keys blocked")
	}

	now = now.Add(limiterBaseDelay + time.Second)
	if wait := limiter.RetryAfter("user:alex"); wait != 0 {
		t.Errorf("still blocked after the delay: %v", wait)
	}
	limiter.Fail("user:alex")
	if wait := limiter.RetryAfter("user:alex"); wait != 2*limiterBaseDelay {
		t.Errorf("second block = %v, want %v", wait, 2*limiterBaseDelay)
	}
	for range 20 {
		limiter.Fail("user:alex")
	}
	if wait := limiter.RetryAfter("user:alex"); wait != limiterMaxDelay {
		t.Errorf("block = %v, want the maximum %v", wait, limiterMaxDelay)
	}

	limiter.Reset("user:alex")
	if wait := limiter.RetryAfter("user:alex"); wait != 0 {
		t.Error("blocked after reset")
	}
}
