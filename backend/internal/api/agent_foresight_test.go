package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/advisor"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/whatif"
)

// fakeNAS is a small NAS for the scenarios: RAID 1 on sda/sdb, sda worn.
func fakeNAS(context.Context) (whatif.Model, error) {
	now := time.Now()
	old := now.Add(-23 * 24 * time.Hour)
	return whatif.Model{
		Now: now,
		Disks: []whatif.Disk{
			{Name: "sda", Kind: "hdd", SizeBytes: 8e12, Volume: "Data", MemberState: "active", SmartKnown: true, Reallocated: 4, PowerOnHours: 36800, WearPercent: -1},
			{Name: "sdb", Serial: "WD-CA4D5E6", Kind: "hdd", SizeBytes: 8e12, Volume: "Data", MemberState: "active", SmartKnown: true, WearPercent: -1},
		},
		Volumes: []whatif.Volume{{Name: "Data", Level: "raid1", Filesystem: "ext4", State: "ok", Mounted: true, SizeBytes: 8e12, UsedBytes: 5.1e12, RaidDisks: 2, Members: []whatif.Member{{Disk: "sda", State: "active"}, {Disk: "sdb", State: "active"}}, MeasuredBps: 205e6}},
		Shares:  []whatif.Share{{Name: "Documents", Volume: "Data"}, {Name: "Fotos", Volume: "Data"}},
		Backups: []whatif.Backup{
			{ID: "docs", Enabled: true, Known: true, Shares: []string{"Documents"}, DestKind: "usb", LastOK: &now},
			{ID: "fotos", Enabled: true, Known: true, Shares: []string{"Fotos"}, DestKind: "s3", LastOK: &old, Bytes: 1.9e12},
		},
	}, nil
}

func setFeatures(t *testing.T, ts *testServer, f map[string]any) {
	t.Helper()
	if res, body := ts.request("PUT", "/api/v1/agent/features", f); res.StatusCode != http.StatusOK {
		t.Fatalf("features: %d %s", res.StatusCode, body)
	}
}

func toolNames(ts *testServer, user auth.User) map[string]*agentTool {
	return ts.server.newAgentToolbox(user, ts.server.agentSettings()).tools
}

func TestForesightOffByDefaultAndRights(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	_, body := ts.request("GET", "/api/v1/agent/features", nil)
	var f struct {
		WhatIf  agentWhatIf  `json:"whatIf"`
		Advisor agentAdvisor `json:"advisor"`
	}
	json.Unmarshal(body, &f)
	a := f.Advisor
	if f.WhatIf.Enabled || a.Enabled || a.Predict == nil || !*a.Predict || a.Bottlenecks == nil || !*a.Bottlenecks || a.Prices || a.Monthly || a.At != "03:30" {
		t.Fatalf("defaults: %s", body)
	}
	for _, r := range []struct{ method, path string }{{"POST", "/agent/whatif"}, {"GET", "/agent/advisor"}, {"POST", "/agent/advisor/run"}} {
		res, body := ts.request(r.method, "/api/v1"+r.path, map[string]any{"kind": "disk", "disk": "sda"})
		if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "agent_feature_off") {
			t.Fatalf("%s while off: %d %s", r.path, res.StatusCode, body)
		}
	}
	if _, body := ts.request("GET", "/api/v1/agent/whatif", nil); !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("state: %s", body)
	}
	owner, _ := ts.users.Get(ownerName(t, ts))
	tools := toolNames(ts, owner)
	if tools["whatif"] != nil || tools["hardware_advice"] != nil {
		t.Fatal("tools offered while off")
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/features", map[string]any{"advisor": map[string]any{"enabled": true, "at": "25:99"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("bad time accepted")
	}
	setFeatures(t, ts, map[string]any{"whatIf": map[string]any{"enabled": true}, "advisor": map[string]any{"enabled": true}})
	tools = toolNames(ts, owner)
	if tools["whatif"] == nil || tools["hardware_advice"] == nil || tools["whatif"].kind != agent.KindRead {
		t.Fatal("tools missing while on")
	}
	// A user of the agent gets neither the routes nor the tools.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	settings := map[string]any{"enabled": true, "users": []string{"gast"}}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, r := range []struct{ method, path string }{{"GET", "/agent/whatif"}, {"POST", "/agent/whatif"}, {"GET", "/agent/advisor"}, {"POST", "/agent/advisor/run"}} {
		if res, _ := ts.do(guest, r.method, "/api/v1"+r.path, map[string]any{"kind": "disk", "disk": "sda"}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s: %d", r.path, res.StatusCode)
		}
	}
	gast, _ := ts.users.Get("gast")
	if tools := toolNames(ts, gast); tools["whatif"] != nil || tools["hardware_advice"] != nil {
		t.Fatal("user got the tools")
	}
	// The public part says it is on.
	_, body = ts.request("GET", "/api/v1/agent/features/me", nil)
	if !strings.Contains(string(body), `"whatIf":true`) || !strings.Contains(string(body), `"advisor":true`) {
		t.Fatalf("public: %s", body)
	}
}

func ownerName(t *testing.T, ts *testServer) string {
	for _, u := range ts.users.List() {
		if u.Role == auth.RoleOwner {
			return u.Name
		}
	}
	t.Fatal("no owner")
	return ""
}

func TestWhatIfRoute(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	setFeatures(t, ts, map[string]any{"whatIf": map[string]any{"enabled": true}})
	ts.server.foresight().model = fakeNAS
	res, body := ts.request("POST", "/api/v1/agent/whatif", whatif.Scenario{Kind: "disk", Disk: "sdb"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var r whatif.Result
	json.Unmarshal(body, &r)
	if r.Outcome != whatif.OutcomeNoRedundancy || r.Serial != "WD-CA4D5E6" || r.DataAtRiskBytes != 5.1e12 || !r.Rebuild.Measured || r.Risk.Level != whatif.RiskHigh {
		t.Fatalf("result %s", body)
	}
	if len(r.Gaps) != 1 || r.Gaps[0].Share != "Fotos" || r.Gaps[0].Days != 23 || len(r.Safe) != 1 {
		t.Fatalf("gaps %s", body)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/whatif", whatif.Scenario{Kind: "disk", Disk: "sdq"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown disk: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/whatif", whatif.Scenario{Kind: "boom"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad kind: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/agent/whatif", whatif.Scenario{Kind: "level", Volume: "Data", Level: "raid6"}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"needDisks":2`) {
		t.Fatalf("level: %d %s", res.StatusCode, body)
	}
	// The agent's tool says the same, and that nothing was changed.
	owner, _ := ts.users.Get(ownerName(t, ts))
	out, err := toolNames(ts, owner)["whatif"].own(t.Context(), nil, json.RawMessage(`{"kind":"disk","disk":"sdb"}`))
	if err != nil || !strings.Contains(out.Text, "without redundancy") || !strings.Contains(out.Text, "nothing on the NAS was changed") {
		t.Fatalf("tool: %v %s", err, out.Text)
	}
	// Without the fake the model comes from the (empty) managers.
	ts.server.foresight().model = nil
	if _, err := ts.server.whatifModel(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestBackupOfCachedView(t *testing.T) {
	ok := time.Now()
	b := backupOf(backup.View{StateKnown: true, State: backup.State{LastOK: &ok, Bytes: 7}, Job: backup.Job{ID: "j", Enabled: true, Sources: []string{"A"},
		CoordinatedSelection: &backup.CoordinatedSelection{Sources: []string{"B"}}, Destination: backup.Destination{Type: "local", Volume: "Data", Password: "secret"}}})
	if !b.Known || b.DestVolume != "Data" || len(b.Shares) != 2 || b.Bytes != 7 {
		t.Fatalf("%+v", b)
	}
	if b := backupOf(backup.View{Job: backup.Job{ID: "x", Destination: backup.Destination{Type: "s3"}}}); b.Known || b.DestVolume != "" {
		t.Fatalf("unknown state: %+v", b)
	}
}

func pu(v uint64) *uint64 { return &v }

func TestAdvisorRunAndKeep(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	fake := &agentOllama{}
	ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": fake.start(t), "model": "m", "policy": agent.Policy{Preset: agent.PresetStandard}})
	// A web search with a price, served locally.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if strings.Contains(r.Form.Get("q"), "WD-") {
			t.Errorf("serial number in the query: %q", r.Form.Get("q"))
		}
		w.Write([]byte(`<div class="result"><a class="result__a" href="https://shop.example/wd80">WD Red Plus 8 TB</a><a class="result__snippet" href="x">Jetzt nur 189,90 € inkl. MwSt.</a></div>`))
	}))
	defer srv.Close()
	old := agent.SearchURL
	agent.SearchURL = srv.URL + "/html/"
	defer func() { agent.SearchURL = old }()
	ts.server.agentWebFactory = func() *agent.Web {
		w := agent.NewWeb()
		w.Check = func(a netip.Addr) bool { return a.IsLoopback() }
		w.Ports = append(w.Ports, srv.URL[strings.LastIndex(srv.URL, ":")+1:])
		return w
	}
	day := func(i int) string { return time.Now().AddDate(0, 0, i).Format(time.DateOnly) }
	ts.server.foresight().input = func(context.Context) advisor.Input {
		return advisor.Input{Now: time.Now(), Disks: []advisor.Disk{{Name: "sda", Model: "WD80EFZZ", Serial: "WD-CA4D5E6", Kind: "hdd", SizeBytes: 8e12, Known: true,
			History: []advisor.Sample{{Day: day(-42), Reallocated: pu(0), Pending: pu(1)}, {Day: day(-1), Reallocated: pu(3), Pending: pu(2)}},
			Current: advisor.Sample{Day: day(0), Reallocated: pu(4), Pending: pu(2)}}}}
	}
	setFeatures(t, ts, map[string]any{"advisor": map[string]any{"enabled": true, "prices": true}})
	res, body := ts.request("POST", "/api/v1/agent/advisor/run", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("run: %d %s", res.StatusCode, body)
	}
	var v advisorView
	json.Unmarshal(body, &v)
	if v.Saved == nil || len(v.Saved.Report.Findings) != 1 || v.Saved.Report.Findings[0].Level != advisor.LevelFail {
		t.Fatalf("report %s", body)
	}
	if offers := v.Saved.Offers["sda"]; len(offers) != 1 || offers[0].Price != "189,90" || offers[0].URL != "https://shop.example/wd80" {
		t.Fatalf("offers %s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, advisorFile)); err != nil {
		t.Fatalf("not kept: %v", err)
	}
	// Prices off: no search.
	setFeatures(t, ts, map[string]any{"advisor": map[string]any{"enabled": true, "predict": false}})
	_, body = ts.request("POST", "/api/v1/agent/advisor/run", nil)
	v = advisorView{}
	json.Unmarshal(body, &v)
	if len(v.Saved.Offers) != 0 || len(v.Saved.Report.Findings) != 0 {
		t.Fatalf("predict and prices off: %s", body)
	}
	// A new server reads the kept report (here: the same state cleared).
	st := ts.server.foresight()
	st.loaded, st.saved = false, advisorSaved{}
	if saved, ok := ts.server.advisorReport(); !ok || saved.LastRun.IsZero() {
		t.Fatal("kept report not read")
	}
}

func TestAdvisorDue(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 11, 6, h, m, 0, 0, loc) }
	if advisorDue("03:30", time.Time{}, at(3, 0)) {
		t.Fatal("before the time")
	}
	if !advisorDue("03:30", time.Time{}, at(3, 31)) || !advisorDue("03:30", at(3, 31).AddDate(0, 0, -1), at(14, 0)) {
		t.Fatal("due, also made up during the day")
	}
	if advisorDue("03:30", at(3, 40), at(4, 0)) {
		t.Fatal("ran tonight already")
	}
	if advisorDue("bad", time.Time{}, at(4, 0)) {
		t.Fatal("bad time")
	}
}

func TestForesightSampleAndSwap(t *testing.T) {
	ts := newTestServer(t)
	path := filepath.Join(t.TempDir(), "meminfo")
	os.WriteFile(path, []byte("MemTotal: 100 kB\nSwapTotal:     4096 kB\nSwapFree:      1024 kB\n"), 0o600)
	old := procMeminfo
	procMeminfo = path
	defer func() { procMeminfo = old }()
	if total, free := swap(); total != 4096*1024 || free != 1024*1024 {
		t.Fatalf("swap %d %d", total, free)
	}
	now := time.Now()
	f := agentFeatures{Advisor: agentAdvisor{Enabled: true}}
	for i := range 3 {
		ts.server.foresightSample(now.Add(time.Duration(i)*time.Minute), f)
	}
	st := ts.server.foresight()
	if len(st.cpu) != 3 || len(st.mem) != 1 || len(st.mem) > 0 && st.mem[0].SwapTotal != 4096*1024 {
		t.Fatalf("samples cpu %d mem %+v", len(st.cpu), st.mem)
	}
	if l := ring([]int{1, 2, 3}, 4, 3); len(l) != 3 || l[0] != 2 {
		t.Fatalf("ring %v", l)
	}
	if q := replacementQuery(advisor.Finding{Model: "WD80EFZZ", Kind: "hdd", SizeBytes: 8e12}); q != "WD80EFZZ 8 TB Festplatte Preis" {
		t.Fatal(q)
	}
}

func TestAdvisorNightly(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	ts.server.foresight().input = func(context.Context) advisor.Input { return advisor.Input{Now: time.Now()} }
	now := time.Now()
	night := time.Date(now.Year(), now.Month(), now.Day(), 3, 31, 0, 0, now.Location())
	// Off: nothing runs.
	ts.server.agentForesightTick(night)
	if _, ok := ts.server.advisorReport(); ok {
		t.Fatal("ran while off")
	}
	setFeatures(t, ts, map[string]any{"advisor": map[string]any{"enabled": true, "monthly": true}})
	ts.server.agentForesightTick(night.Add(-10 * time.Minute)) // before 03:30
	ts.server.agentForesightTick(night)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if saved, ok := ts.server.advisorReport(); ok {
			if !saved.LastRun.Equal(night) {
				t.Fatalf("last run %v", saved.LastRun)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nightly run did not happen")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Not twice in a night.
	st := ts.server.foresight()
	st.tried = time.Time{}
	ts.server.agentForesightTick(night.Add(time.Hour))
	time.Sleep(50 * time.Millisecond)
	if saved, _ := ts.server.advisorReport(); !saved.LastRun.Equal(night) {
		t.Fatal("ran twice")
	}
}
