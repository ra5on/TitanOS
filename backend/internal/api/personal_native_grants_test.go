package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func TestPersonalShareRegrantWithUnwritableAgentMarker(t *testing.T) {
	ts, dir, src := collectionFixture(t, "office.compare")
	if _, err := ts.users.Create("bea", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	changeShare := func(level string) {
		t.Helper()
		access := map[string]string{"alex": "rw"}
		if level != "" {
			access["bea"] = level
		}
		res, body := ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"volume": "tank", "access": access})
		if res.StatusCode != 204 {
			t.Fatalf("native share update: %d %s", res.StatusCode, body)
		}
	}
	changeShare("rw")
	guest := ts.newClient()
	if res, _ := ts.do(guest, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	rights := map[string]any{"rights": map[string]string{"office.compare": "auto"}}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/bea", rights)
	personalHTTP[pa.State](t, ts, guest, "PUT", "/settings", rights)
	for _, name := range []string{"A.txt", "B.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("Preis: 15 EUR"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	j := personalHTTP[pa.Job](t, ts, guest, "POST", "/jobs", pa.Request{Tool: "office.compare", RequestID: "grant-cycle", Args: json.RawMessage(`{"name":"Ergebnis.xlsx"}`), Sources: []pa.Source{src("A.txt"), src("B.txt"), src("Ausgabe")}})
	workOne(t, ts.server)
	j = personalHTTP[pa.Job](t, ts, guest, "GET", "/jobs/"+j.ID, nil)
	if j.Token == "" {
		t.Fatal(j)
	}

	// Simulate one unreadable directory entry on the config volume. Other native
	// configuration files remain writable. Restoring the old marker models reboot
	// with neither change having reached this file.
	marker := filepath.Join(ts.store.Dir(), globalPersonalGrants)
	old, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(marker, marker+".before"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(marker, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ts.store.WriteJSON(globalPersonalGrants, "would-replace-marker"); err == nil {
		t.Fatal("injected write failure did not take effect")
	}
	changeShare("")
	changeShare("rw")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(marker+".before", marker); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(old, got) {
		t.Fatal("secondary agent revision changed", err)
	}
	if err := ts.shares.Load(); err != nil {
		t.Fatal(err)
	}
	ts.server.initPersonalTools()
	res, body := ts.do(guest, "POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	if res.StatusCode != 403 {
		t.Fatalf("old approval after restart: %d %s", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "Ausgabe", "Ergebnis.xlsx")); !os.IsNotExist(err) {
		t.Fatal("stale request published an output", err)
	}
}

func TestPersonalNativeAccountAndMailRevisionsPersist(t *testing.T) {
	ts, _, _ := newMailServer(t)
	u, err := ts.users.Create("bea", testPassword, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	stamp := func() string {
		t.Helper()
		r, err := ts.server.personalGrantRevisions(u, u.Name)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := stamp()
	if _, err = ts.users.SetRole(u.Name, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err = ts.users.SetRole(u.Name, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := ts.users.Load(); err != nil {
		t.Fatal(err)
	}
	if after := stamp(); after == before {
		t.Fatal("role cycle reused approval")
	}
	before = stamp()
	if _, err = ts.users.SetDisabled(u.Name, true, "alex"); err != nil {
		t.Fatal(err)
	}
	if _, err = ts.users.SetDisabled(u.Name, false, "alex"); err != nil {
		t.Fatal(err)
	}
	if after := stamp(); after == before {
		t.Fatal("account lock cycle reused approval")
	}
	mail := ts.server.opts.Mail
	if err := mail.SetAllowed(u.Name, true); err != nil {
		t.Fatal(err)
	}
	before = stamp()
	if err := mail.SetAllowed(u.Name, false); err != nil {
		t.Fatal(err)
	}
	if err := mail.SetAllowed(u.Name, true); err != nil {
		t.Fatal(err)
	}
	after := stamp()
	if after == before {
		t.Fatal("mail grant cycle reused approval")
	}
	if err := mail.Load(); err != nil {
		t.Fatal(err)
	}
	if stamp() != after {
		t.Fatal("native mail grant revision changed at restart")
	}
}

func TestPersonalQueuedJobRequestsConsentAfterRegrant(t *testing.T) {
	ts := personalFixture(t)
	_, err := ts.users.Create("bea", testPassword, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	client := ts.newClient()
	if res, _ := ts.do(client, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	j := personalHTTP[pa.Job](t, ts, client, "POST", "/jobs", pa.Request{Tool: "agent.example", RequestID: "waiting-for-worker"})
	// The worker was offline during both native role changes.
	for _, role := range []auth.Role{auth.RoleUser, auth.RoleAdmin} {
		if _, err := ts.users.SetRole("bea", role); err != nil {
			t.Fatal(err)
		}
	}
	ts.server.initPersonalTools()
	workOne(t, ts.server)
	j = personalHTTP[pa.Job](t, ts, client, "GET", "/jobs/"+j.ID, nil)
	if j.Phase != "permission" || j.Token == "" || j.Result != nil {
		t.Fatal("queued job used the restored authority unattended", j)
	}
	personalHTTP[pa.Job](t, ts, client, "POST", "/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	workOne(t, ts.server)
	j = personalHTTP[pa.Job](t, ts, client, "GET", "/jobs/"+j.ID, nil)
	if j.Status != "done" || j.Result == nil {
		t.Fatal(j)
	}
}

func TestPersonalClusterAccountKeepsAccessRevisionAcrossMixedVersions(t *testing.T) {
	ts := personalFixture(t)
	u, err := ts.users.Create("bea", testPassword, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	// A newer peer observed a full role change cycle while this NAS was offline.
	peer := u
	peer.AccessRevision = 2
	if _, _, err := ts.users.Import(peer); err != nil {
		t.Fatal(err)
	}
	actual, _ := ts.users.Get(u.Name)
	if actual.AccessRevision != 2 {
		t.Fatal(actual.AccessRevision)
	}
	// Older peers send no revision. Each observed role transition must advance
	// the local grant, and a repeated unchanged import should leave it alone.
	for _, role := range []auth.Role{auth.RoleUser, auth.RoleAdmin, auth.RoleAdmin} {
		peer.Role, peer.AccessRevision = role, 0
		if _, _, err := ts.users.Import(peer); err != nil {
			t.Fatal(err)
		}
	}
	if err := ts.users.Load(); err != nil {
		t.Fatal(err)
	}
	actual, _ = ts.users.Get(u.Name)
	if actual.Role != u.Role || actual.AccessRevision != 4 {
		t.Fatal(actual.Role, actual.AccessRevision)
	}
}
