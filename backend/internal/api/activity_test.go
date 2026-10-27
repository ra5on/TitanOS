package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func activityEntries(t *testing.T, log *activity.Manager) []activity.Entry {
	t.Helper()
	rows, _, _, err := log.Query(context.Background(), activity.Filter{Days: 1, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func findActivity(rows []activity.Entry, action string) (activity.Entry, bool) {
	for _, e := range rows {
		if e.Action == action {
			return e, true
		}
	}
	return activity.Entry{}, false
}

func TestActivityRecordsChangesLoginsAndNoSecrets(t *testing.T) {
	ts := newTestServer(t)
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	ts.setup()
	if _, ok := findActivity(activityEntries(t, log), "setup"); !ok {
		t.Fatal("setup not recorded")
	}
	ts.request("PUT", "/api/v1/preferences/dashboard", map[string]any{})
	ts.request("POST", "/api/v1/files/search", map[string]any{"query": "x"})
	if res, _ := ts.request("POST", "/api/v1/users", map[string]string{"name": "gast", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		t.Fatalf("create user: %d", res.StatusCode)
	}
	ts.request("DELETE", "/api/v1/users/gast", nil)
	rows := activityEntries(t, log)
	for _, e := range rows {
		if strings.Contains(e.Action, "preferences") || strings.Contains(e.Action, "files/search") {
			t.Fatalf("quiet route recorded: %+v", e)
		}
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), testPassword) {
			t.Fatalf("password recorded: %s", raw)
		}
	}
	if e, ok := findActivity(rows, "DELETE /api/v1/users/{name}"); !ok || e.Target != "gast" || e.User != "alex" || !e.OK || e.Via != activity.ViaWeb {
		t.Fatalf("delete user: %+v %v", e, ok)
	}
	if _, ok := findActivity(rows, "POST /api/v1/users"); !ok {
		t.Fatal("create user not recorded")
	}

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: "wrong password here"})
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	ts.do(guest, "PUT", "/api/v1/system/hostname", map[string]string{"hostname": "x"})
	if res, _ := ts.do(guest, "GET", "/api/v1/activity", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user reads activity: %d", res.StatusCode)
	}
	ts.do(guest, "POST", "/api/v1/logout", nil)
	rows = activityEntries(t, log)
	if e, ok := findActivity(rows, "login_failed"); !ok || e.User != "gast" || e.OK {
		t.Fatalf("failed login: %+v", e)
	}
	if e, ok := findActivity(rows, "PUT /api/v1/system/hostname"); !ok || e.User != "gast" || e.OK || e.Status != http.StatusForbidden {
		t.Fatalf("forbidden change: %+v", e)
	}
	if _, ok := findActivity(rows, "logout"); !ok {
		t.Fatal("logout not recorded")
	}

	res, body := ts.request("GET", "/api/v1/activity?days=1&user=gast&q=login", nil)
	var page struct {
		Entries []activity.Entry `json:"entries"`
		Stored  bool             `json:"stored"`
	}
	if res.StatusCode != 200 || json.Unmarshal(body, &page) != nil || len(page.Entries) != 2 || page.Stored {
		t.Fatalf("query: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/activity?days=1&format=csv", nil)
	if res.StatusCode != 200 || !strings.HasPrefix(string(body), "time,user,via") || !strings.Contains(res.Header.Get("Content-Disposition"), ".csv") {
		t.Fatalf("csv: %d %s", res.StatusCode, body)
	}

	ts.request("PUT", "/api/v1/activity", activity.Settings{Enabled: false, Days: 30, MaxMiB: 8})
	if e, ok := findActivity(activityEntries(t, log), "PUT /api/v1/activity"); !ok || e.Target != "off" {
		t.Fatalf("switching off not recorded: %+v", e)
	}
	before := len(activityEntries(t, log))
	ts.request("POST", "/api/v1/users", map[string]string{"name": "drei", "password": testPassword, "role": "user"})
	if len(activityEntries(t, log)) != before {
		t.Fatal("recorded while off")
	}
}

func TestActivityCSVKeepsFormulasOut(t *testing.T) {
	if csvSafe("=cmd()") != "'=cmd()" || csvSafe("fotos") != "fotos" {
		t.Fatal("formula")
	}
}
