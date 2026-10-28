package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/webauthn"
	"github.com/ra5on/TitanOS/backend/internal/webauthn/webauthntest"
)

func TestPasskeyRegisterLoginAndRemove(t *testing.T) {
	ts := newTestServer(t)
	// Browsers allow passkeys on localhost without HTTPS, never on an IP.
	ts.url = strings.Replace(ts.url, "127.0.0.1", "localhost", 1)
	origin := strings.ToLower(ts.url)
	ts.client = ts.newClient()
	ts.setup()

	if res, body := ts.request("POST", "/api/v1/account/passkeys/begin", map[string]string{"password": "wrong"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("begin without the password: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/account/passkeys/begin", map[string]string{"password": testPassword})
	var begun struct {
		ChallengeID string `json:"challengeId"`
		PublicKey   struct {
			Challenge string
			RP        struct{ ID string } `json:"rp"`
			User      struct{ ID string }
		} `json:"publicKey"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &begun) != nil || begun.PublicKey.RP.ID != "localhost" {
		t.Fatalf("begin: %d %s", res.StatusCode, body)
	}
	key := webauthntest.New()
	key.Handle, _ = decodeB64(begun.PublicKey.User.ID)
	cd, att := key.Create(begun.PublicKey.Challenge, origin, "localhost")
	res, body = ts.request("POST", "/api/v1/account/passkeys", map[string]string{"challengeId": begun.ChallengeID, "name": "Laptop", "clientDataJSON": cd, "attestationObject": att})
	var list struct{ Passkeys []struct{ ID, Name string } }
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || len(list.Passkeys) != 1 || list.Passkeys[0].Name != "Laptop" {
		t.Fatalf("finish: %d %s", res.StatusCode, body)
	}
	// The challenge is used up.
	if res, _ := ts.request("POST", "/api/v1/account/passkeys", map[string]string{"challengeId": begun.ChallengeID, "clientDataJSON": cd, "attestationObject": att}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("challenge used twice: %d", res.StatusCode)
	}

	login := func(client *http.Client, tamper bool) (*http.Response, []byte) {
		_, body := ts.do(client, "POST", "/api/v1/login/passkey/begin", nil)
		var c struct {
			ChallengeID string                     `json:"challengeId"`
			PublicKey   struct{ Challenge string } `json:"publicKey"`
		}
		json.Unmarshal(body, &c)
		challenge := c.PublicKey.Challenge
		if tamper {
			challenge = strings.Repeat("A", len(challenge))
		}
		id, cd, ad, sig, handle := key.Get(challenge, origin, "localhost")
		return ts.do(client, "POST", "/api/v1/login/passkey", map[string]string{"challengeId": c.ChallengeID, "id": id, "clientDataJSON": cd, "authenticatorData": ad, "signature": sig, "userHandle": handle})
	}
	fresh := ts.newClient()
	if res, body := login(fresh, true); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered login: %d %s", res.StatusCode, body)
	}
	if res, body := login(fresh, false); res.StatusCode != http.StatusOK {
		t.Fatalf("passkey login: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(fresh, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("no session after the passkey login: %d", res.StatusCode)
	}

	res, body = ts.request("DELETE", "/api/v1/account/passkeys/"+list.Passkeys[0].ID, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"passkeys":[]`) {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
	if res, _ := login(ts.newClient(), false); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("removed passkey still logs in: %d", res.StatusCode)
	}
}

func TestPasskeyOrigin(t *testing.T) {
	for host, want := range map[string]bool{"nas.example.org": true, "nas.local:8443": true, "192.168.1.5": false, "[fd00::1]:443": false, "localhost:5173": true} {
		r, _ := http.NewRequest("GET", "/", nil)
		r.Host = host
		if host != "localhost:5173" {
			r.TLS = &tlsState
		}
		if _, _, ok := passkeyOrigin(r); ok != want {
			t.Errorf("%s: %v", host, ok)
		}
	}
	r, _ := http.NewRequest("GET", "/", nil)
	r.Host = "nas.local"
	if _, _, ok := passkeyOrigin(r); ok {
		t.Error("plain HTTP accepted for a host name")
	}
}

var tlsState = tls.ConnectionState{}

func decodeB64(s string) ([]byte, error) { return webauthn.Encoding.DecodeString(s) }
