package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// The test values of RFC 6238 for SHA-1, cut to six digits.
func TestTOTPVectors(t *testing.T) {
	secret := base32NoPad.EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		got, err := totpCode(secret, uint64(unix)/30)
		if err != nil || got != want {
			t.Errorf("code at %d = %s, %v; want %s", unix, got, err, want)
		}
	}
	now := time.Unix(1111111109, 0)
	if checkTOTP(secret, "081 804", now) == 0 || checkTOTP(secret, "081804", now.Add(30*time.Second)) == 0 || checkTOTP(secret, "081804", now.Add(-30*time.Second)) == 0 {
		t.Error("a code is not accepted in its own and the neighbouring time steps")
	}
	if checkTOTP(secret, "081804", now.Add(2*time.Minute)) != 0 || checkTOTP(secret, "81804", now) != 0 || checkTOTP(secret, "", now) != 0 {
		t.Error("an old or malformed code is accepted")
	}
}

func TestTwoFactor(t *testing.T) {
	users := NewUsers(config.Open(t.TempDir()))
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateOwner("alex", "a long test password"); err != nil {
		t.Fatal(err)
	}
	if !users.CheckSecondFactor("alex", "") {
		t.Error("an account without a second factor is asked for one")
	}
	if _, err := users.EnableTwoFactor("alex", "123456"); !errors.Is(err, ErrNoTwoFactor) {
		t.Errorf("enable without a secret: %v", err)
	}
	secret, uri, err := users.BeginTwoFactor("alex", "TitanOS nas")
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/TitanOS%20nas:alex?") || !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("begin: %q, %v", uri, err)
	}
	// Nothing counts until the app proved that it has the secret.
	if user, _ := users.Get("alex"); user.TwoFactor() {
		t.Fatal("two-factor login is on before a code was seen")
	}
	if _, err := users.EnableTwoFactor("alex", "000000"); !errors.Is(err, ErrWrongCode) {
		t.Errorf("enable with a wrong code: %v", err)
	}
	step := uint64(time.Now().Unix()) / 30
	code, _ := totpCode(secret, step)
	codes, err := users.EnableTwoFactor("alex", code)
	if err != nil || len(codes) != recoveryCodes || len(codes[0]) != 11 {
		t.Fatalf("enable: %v, %v", codes, err)
	}
	if _, _, err := users.BeginTwoFactor("alex", "x"); !errors.Is(err, ErrTwoFactorEnabled) {
		t.Errorf("begin while it is on: %v", err)
	}

	// The code that switched it on is used up; the next one works once.
	if users.CheckSecondFactor("alex", code) {
		t.Error("a code was accepted twice")
	}
	next, _ := totpCode(secret, step+1)
	if !users.CheckSecondFactor("alex", next) || users.CheckSecondFactor("alex", next) {
		t.Error("the following code does not work exactly once")
	}
	if users.CheckSecondFactor("alex", "000000") || users.CheckSecondFactor("alex", "") {
		t.Error("a wrong code is accepted")
	}

	// A recovery code works once, however it is typed, also after a restart.
	again := NewUsers(users.store)
	again.Load()
	if user, _ := again.Get("alex"); !user.TwoFactor() || strings.Contains(strings.Join(user.RecoveryCodes, ""), codes[0]) {
		t.Fatal("after a restart: second factor gone or recovery codes stored in the clear")
	}
	if !again.CheckSecondFactor("alex", " "+strings.ToUpper(codes[0])+" ") || again.CheckSecondFactor("alex", codes[0]) {
		t.Error("a recovery code does not work exactly once")
	}
	if user, _ := again.Get("alex"); len(user.RecoveryCodes) != recoveryCodes-1 {
		t.Errorf("%d recovery codes left", len(user.RecoveryCodes))
	}
	if err := again.DisableTwoFactor("alex"); err != nil {
		t.Fatal(err)
	}
	if user, _ := again.Get("alex"); user.TwoFactor() || len(user.RecoveryCodes) != 0 || !again.CheckSecondFactor("alex", "") {
		t.Error("switching off left something behind")
	}
}

func TestSessionList(t *testing.T) {
	sessions := NewSessions()
	first, _ := sessions.Create("alex", "192.168.1.5", "Firefox")
	second, _ := sessions.Create("alex", "192.168.1.6", "Safari")
	sessions.Create("gast", "192.168.1.7", "Chrome")
	list := sessions.List("alex", second)
	if len(list) != 2 || list[0].ID == list[1].ID || strings.Contains(first+second, list[0].ID) {
		t.Fatalf("list = %+v", list)
	}
	var current, other SessionInfo
	for _, info := range list {
		if info.Current {
			current = info
		} else {
			other = info
		}
	}
	if current.UserAgent != "Safari" || other.RemoteIP != "192.168.1.5" {
		t.Errorf("current = %+v, other = %+v", current, other)
	}
	// Only one's own sessions can be ended.
	if sessions.DeleteByID("gast", other.ID) || !sessions.DeleteByID("alex", other.ID) || sessions.DeleteByID("alex", other.ID) {
		t.Error("ending a session by its ID")
	}
	if _, ok := sessions.Get(first); ok {
		t.Error("the ended session still works")
	}
	if _, ok := sessions.Get(second); !ok {
		t.Error("the other session was ended as well")
	}
}
