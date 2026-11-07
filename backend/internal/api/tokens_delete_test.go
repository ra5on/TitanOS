package api

import (
	"net/http"
	"strings"
	"testing"
)

// Deleting an account removes its API tokens from the stick: a new account
// of the same name neither sees them nor has them count toward its limit.
func TestDeletedAccountLosesItsTokens(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "gast", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	gast, _ := ts.users.Get("gast")
	if _, _, err := ts.server.tokens.Create(gast, "Monitoring", nil); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	data, err := ts.store.Read("users/api-tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"gast"`) {
		t.Fatalf("tokens of the deleted account kept: %s", data)
	}
}
