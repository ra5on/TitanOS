package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

// The owner confirms the disclaimer once (23 Nov 2026); other accounts can
// read it but are not asked and cannot confirm it for the owner.
func TestDisclaimer(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	view := func(client *http.Client) map[string]any {
		t.Helper()
		res, body := ts.do(client, "GET", "/api/v1/system/disclaimer", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("get: %d %s", res.StatusCode, body)
		}
		var v map[string]any
		json.Unmarshal(body, &v)
		return v
	}
	if v := view(ts.client); v["accepted"] != false || v["required"] != true {
		t.Fatalf("before: %v", v)
	}

	if _, err := ts.users.Create("gast", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	if res, body := ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}
	if v := view(guest); v["required"] != false {
		t.Errorf("a user is asked: %v", v)
	}
	if res, body := ts.do(guest, "POST", "/api/v1/system/disclaimer", map[string]int{"version": disclaimerVersion}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "owner_only" {
		t.Errorf("user confirms: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/system/disclaimer", map[string]int{"version": disclaimerVersion + 1}); res.StatusCode != http.StatusConflict {
		t.Errorf("other version: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/system/disclaimer", map[string]int{"version": disclaimerVersion}); res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d %s", res.StatusCode, body)
	}
	if v := view(ts.client); v["accepted"] != true || v["required"] != false || v["by"] != "alex" {
		t.Errorf("after: %v", v)
	}
	var st disclaimerState
	if err := ts.store.ReadJSON(disclaimerFile, &st); err != nil || st.Version != disclaimerVersion || st.By != "alex" {
		t.Errorf("stored: %+v %v", st, err)
	}
}

// A confirmation on the stick from before a restart counts; an unreadable
// file is never taken as confirmed.
func TestDisclaimerStoreReadsOnce(t *testing.T) {
	store := config.Open(t.TempDir())
	var d disclaimerStore
	if st := d.get(store); st.Version != 0 {
		t.Fatalf("empty: %+v", st)
	}
	var e disclaimerStore
	if _, err := e.accept(store, "alex"); err != nil {
		t.Fatal(err)
	}
	var f disclaimerStore
	if st := f.get(store); st.Version != disclaimerVersion || st.By != "alex" {
		t.Errorf("after restart: %+v", st)
	}
	bad := config.Open(t.TempDir())
	bad.Write(disclaimerFile, []byte("{"))
	var g disclaimerStore
	if st := g.get(bad); st.Version != 0 || g.loaded {
		t.Errorf("unreadable: %+v loaded=%v", st, g.loaded)
	}
}
