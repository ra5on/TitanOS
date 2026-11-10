package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Wrong passwords sent at the same time all pass the limiter before the
// first is counted, because hashing is slow. Once the limit is reached,
// the answers of the guesses still being hashed must not be told.
func TestParallelGuessesAreLimited(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	const guesses = 12
	data, _ := json.Marshal(credentials{Name: "alex", Password: "a wrong guess, long enough"})
	codes := make(chan int, guesses)
	var wg sync.WaitGroup
	for range guesses {
		wg.Go(func() {
			res, err := http.Post(ts.url+"/api/v1/login", "application/json", bytes.NewReader(data))
			if err != nil {
				codes <- 0
				return
			}
			res.Body.Close()
			codes <- res.StatusCode
		})
	}
	wg.Wait()
	close(codes)
	answered, limited := 0, 0
	for code := range codes {
		switch code {
		case http.StatusUnauthorized:
			answered++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	// Five free attempts; a few more may slip in between the check and the
	// count of guesses that finish hashing at the same moment.
	if answered > 7 || limited < guesses-7 {
		t.Fatalf("%d guesses answered, %d limited", answered, limited)
	}
}

// The same holds for the confirmation of a session (stick backup): whoever
// holds a session cookie must not get more answers than the limit allows
// by sending guesses at the same time.
func TestParallelConfirmGuessesAreLimited(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	const guesses = 12
	codes := make(chan int, guesses)
	var wg sync.WaitGroup
	for range guesses {
		wg.Go(func() {
			res, _ := ts.request("POST", "/api/v1/session/confirm", map[string]string{"password": "a wrong guess, long enough"})
			codes <- res.StatusCode
		})
	}
	wg.Wait()
	close(codes)
	answered, limited := 0, 0
	for code := range codes {
		switch code {
		case http.StatusForbidden:
			answered++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if answered > 7 || limited < guesses-7 {
		t.Fatalf("%d guesses answered, %d limited", answered, limited)
	}
}

// A reset of the second factor with a role the server refuses must not
// happen at all.
func TestResetTwoFactorWithInvalidRoleChangesNothing(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if _, err := ts.users.Create("gast", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	secret, _, err := ts.users.BeginTwoFactor("gast", "TitanOS test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.EnableTwoFactor("gast", auth.CodeForTest(secret, 0)); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("PUT", "/api/v1/users/gast", map[string]any{"resetTwoFactor": true})
	if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_role" {
		t.Fatalf("reset without role: %d %s", res.StatusCode, body)
	}
	if user, _ := ts.users.Get("gast"); !user.TwoFactor() {
		t.Fatal("two-factor login was reset by a request that failed")
	}
	if res, body := ts.request("PUT", "/api/v1/users/gast", map[string]any{"role": "user", "resetTwoFactor": true}); res.StatusCode != http.StatusOK {
		t.Fatalf("reset: %d %s", res.StatusCode, body)
	}
	if user, _ := ts.users.Get("gast"); user.TwoFactor() {
		t.Fatal("two-factor login not reset")
	}
}

// The last owner cannot be demoted; a request that asks for that and for a
// reset of the second factor is refused as a whole.
func TestRefusedDemotionResetsNoFactor(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	secret, _, err := ts.users.BeginTwoFactor("alex", "TitanOS test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.EnableTwoFactor("alex", auth.CodeForTest(secret, 0)); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("PUT", "/api/v1/users/alex", map[string]any{"role": "admin", "resetTwoFactor": true, "password": testPassword, "code": auth.CodeForTest(secret, 1)})
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "last_owner" {
		t.Fatalf("demotion of the last owner: %d %s", res.StatusCode, body)
	}
	if user, _ := ts.users.Get("alex"); !user.TwoFactor() {
		t.Fatal("two-factor login was reset by a request that was refused")
	}
}
