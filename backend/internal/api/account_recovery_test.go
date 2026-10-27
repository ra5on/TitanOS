package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestTwoFactorRecoveryCodes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/account/2fa/recovery", map[string]string{"password": testPassword}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "two_factor_not_begun" {
		t.Errorf("new codes without a second factor: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("POST", "/api/v1/account/2fa/begin", nil)
	var begun struct{ Secret string }
	json.Unmarshal(body, &begun)
	_, body = ts.request("POST", "/api/v1/account/2fa/enable", map[string]string{"code": auth.CodeForTest(begun.Secret, 0)})
	var enabled struct{ RecoveryCodes []string }
	if json.Unmarshal(body, &enabled) != nil || len(enabled.RecoveryCodes) != 8 {
		t.Fatalf("enable: %s", body)
	}

	// Both the password and a code are needed.
	if res, body := ts.request("POST", "/api/v1/account/2fa/recovery", map[string]string{"password": "wrong password", "code": auth.CodeForTest(begun.Secret, 1)}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Errorf("wrong password: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/2fa/recovery", map[string]string{"password": testPassword, "code": "000000"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "invalid_code" {
		t.Errorf("wrong code: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/account/2fa/recovery", map[string]string{"password": testPassword, "code": enabled.RecoveryCodes[0]})
	var renewed struct{ RecoveryCodes []string }
	if json.Unmarshal(body, &renewed) != nil || res.StatusCode != http.StatusOK || len(renewed.RecoveryCodes) != 8 {
		t.Fatalf("new codes: %d %s", res.StatusCode, body)
	}
	login := func(code string) int {
		res, _ := ts.do(ts.newClient(), "POST", "/api/v1/login", map[string]string{"name": "alex", "password": testPassword, "code": code})
		return res.StatusCode
	}
	if login(enabled.RecoveryCodes[1]) != http.StatusUnauthorized {
		t.Error("an old recovery code still logs in")
	}
	if login(renewed.RecoveryCodes[0]) != http.StatusOK {
		t.Error("a new recovery code does not log in")
	}
}

func TestEndOtherSessions(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	other := ts.newClient()
	if res, _ := ts.do(other, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("second login: %d", res.StatusCode)
	}
	res, body := ts.request("DELETE", "/api/v1/account/sessions", nil)
	var devices struct{ Sessions []struct{ Current bool } }
	if json.Unmarshal(body, &devices) != nil || res.StatusCode != http.StatusOK || len(devices.Sessions) != 1 || !devices.Sessions[0].Current {
		t.Fatalf("end others: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(other, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the other device is still logged in: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("this device was logged out: %d", res.StatusCode)
	}
}
