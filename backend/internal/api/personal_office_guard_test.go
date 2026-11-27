package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

// Exercise the normal job creation and approval paths with the real native
// guard state. A stored guard event is enough; there is no simulated attack.
func TestPersonalOfficeHonorsExistingGuardOnShareAndVolume(t *testing.T) {
	ts, dir, src := collectionFixture(t, "office.table")
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{"office.table": "ask"}})
	res, body := ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"volume": "tank", "access": map[string]string{"alex": "rw"}, "guard": true})
	if res.StatusCode != 204 {
		t.Fatal(res.StatusCode, string(body))
	}
	volume := filepath.Dir(dir)
	if err := os.Symlink("Docs/Ausgabe", filepath.Join(volume, "shortcut")); err != nil {
		t.Fatal(err)
	}
	req := pa.Request{Tool: "office.table", RequestID: "before-guard", Args: json.RawMessage(`{"name":"Entwurf.xlsx","text":"Test"}`), Sources: []pa.Source{src("Ausgabe")}}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	workOne(t, ts.server)
	j = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
	if j.Token == "" {
		t.Fatal(j)
	}
	// Keep the revision to exercise the resource check independently.
	var stored []map[string]json.RawMessage
	if err := ts.store.ReadJSON("shares/shares.json", &stored); err != nil {
		t.Fatal(err)
	}
	lock, err := json.Marshal(shares.GuardLock{At: time.Now().UTC(), User: "alex", Reason: "fixture", Files: 5})
	if err != nil {
		t.Fatal(err)
	}
	stored[0]["guardLocked"] = lock
	if err := ts.store.WriteJSON("shares/shares.json", stored); err != nil {
		t.Fatal(err)
	}
	if err := ts.shares.Load(); err != nil {
		t.Fatal(err)
	}
	res, _ = ts.request("POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	if res.StatusCode != 403 {
		t.Fatal("old proposal allowed while share locked", res.StatusCode)
	}
	u, _ := ts.users.Get("alex")
	for i, selection := range []pa.Source{
		src("Ausgabe"),
		{Kind: "folder", ID: "Docs/Ausgabe", Ref: map[string]string{"root": "volume/tank"}},
		{Kind: "folder", ID: "shortcut", Ref: map[string]string{"root": "volume/tank"}},
	} {
		req.RequestID = "during-guard-" + strconv.Itoa(i)
		req.Sources = []pa.Source{selection}
		res, _ = ts.request("POST", personalBase+"/jobs", req)
		if res.StatusCode != 403 {
			t.Fatal("locked target status", i, res.StatusCode)
		}
		if _, err := ts.server.personalFileRoot(u, selection, false); err != nil {
			t.Fatal("locked target could not be read", selection, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Ausgabe", "Entwurf.xlsx")); !os.IsNotExist(err) {
		t.Fatal("guarded destination changed", err)
	}
	if err := ts.shares.UnlockGuard("Docs"); err != nil {
		t.Fatal(err)
	}
	req.Sources = []pa.Source{src("Ausgabe")}
	req.RequestID = "after-unlock"
	personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
}
