package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/buddy"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// setAgent changes the stored agent settings directly (the volume checks
// of the routes need mounted volumes the test server does not have).
func setAgent(ts *testServer, change func(*agentSettings)) {
	ts.server.agentSettings()
	a := &ts.server.agentConf
	a.mu.Lock()
	change(&a.settings)
	a.mu.Unlock()
}

func guestClient(t *testing.T, ts *testServer) *http.Client {
	t.Helper()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != 200 {
		t.Fatalf("allow guest: %d %s", res.StatusCode, body)
	}
	return guest
}

// Ab Werk: all three are off; the diary tool is not there, the catalog
// and pairing refuse, the server answers nothing.
func TestFamily54Defaults(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	if _, ok := box.tools["diary"]; ok {
		t.Fatal("diary on by default")
	}
	if res, _ := ts.request("GET", "/api/v1/agent/catalog", nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("catalog: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/buddy/code", map[string]any{"quota": 2 << 30}); res.StatusCode != http.StatusConflict {
		t.Fatalf("code while off: %d", res.StatusCode)
	}
	srv := httptest.NewServer(ts.server.buddyHandler())
	defer srv.Close()
	if res, _ := http.Post(srv.URL+"/v1/pair", "application/json", strings.NewReader(`{}`)); res.StatusCode != http.StatusNotFound {
		t.Fatalf("pairing while off: %d", res.StatusCode)
	}
	_, body := ts.request("GET", "/api/v1/agent/buddy", nil)
	if !strings.Contains(string(body), `"problem":"off"`) {
		t.Fatalf("buddy view: %s", body)
	}
	// Settings are checked.
	for _, bad := range []map[string]any{
		{"buddy": map[string]any{"enabled": true, "volume": "nowhere"}},
		{"buddy": map[string]any{"address": "http://x/y"}},
	} {
		if res, _ := ts.request("PUT", "/api/v1/agent/features", bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("accepted %v: %d", bad, res.StatusCode)
		}
	}
	// Only owners and administrators see the buddy.
	guest := guestClient(t, ts)
	if res, _ := ts.do(guest, "GET", "/api/v1/agent/buddy", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest buddy: %d", res.StatusCode)
	}
}

// Two NAS pair with a code; each keeps only the hash of the token it
// handed out, the view shows no token, and the token works only at the
// partner's own folder.
func TestFamily54BuddyPairing(t *testing.T) {
	a := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	b := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	listenA := httptest.NewServer(a.server.buddyHandler())
	defer listenA.Close()
	addrA := strings.TrimPrefix(listenA.URL, "http://")
	for _, ts := range []*testServer{a, b} {
		setAgent(ts, func(s *agentSettings) {
			s.Features.Buddy = agentBuddy{Enabled: true, Volume: "tank", Address: "nas-b.tail.ts.net:8752"}
		})
	}
	res, body := a.request("POST", "/api/v1/agent/buddy/code", map[string]any{"quota": 2 << 30})
	var code struct {
		Code    string    `json:"code"`
		Expires time.Time `json:"expires"`
	}
	json.Unmarshal(body, &code)
	if res.StatusCode != 200 || len(code.Code) != 11 || time.Until(code.Expires) > 10*time.Minute {
		t.Fatalf("code: %d %s", res.StatusCode, body)
	}
	// A wrong code is refused and pairs nothing.
	if res, _ := b.request("POST", "/api/v1/agent/buddy/pair", map[string]any{"address": addrA, "code": "AAA-AAA-AAA", "quota": 3 << 30}); res.StatusCode != http.StatusBadGateway {
		t.Fatalf("wrong code: %d", res.StatusCode)
	}
	res, body = b.request("POST", "/api/v1/agent/buddy/pair", map[string]any{"address": addrA, "code": code.Code, "quota": 3 << 30})
	if res.StatusCode != 200 {
		t.Fatalf("pair: %d %s", res.StatusCode, body)
	}
	pa, pb := a.server.buddyPartners(), b.server.buddyPartners()
	if len(pa) != 1 || len(pb) != 1 {
		t.Fatalf("partners: %+v %+v", pa, pb)
	}
	if pa[0].Hash != buddy.HashToken(pb[0].Token) || pb[0].Hash != buddy.HashToken(pa[0].Token) {
		t.Fatal("tokens do not match the hashes")
	}
	if pa[0].Quota != 2<<30 || pa[0].TheirQuota != 3<<30 || pb[0].Quota != 3<<30 || pb[0].TheirQuota != 2<<30 {
		t.Fatalf("quotas: %+v %+v", pa[0], pb[0])
	}
	if pa[0].Remote != pb[0].ID || pb[0].Remote != pa[0].ID {
		t.Fatalf("ids: %+v %+v", pa[0], pb[0])
	}
	// The code works only once (and this NAS is paired already).
	if _, err := a.server.buddyAnswerPair(buddy.PairRequest{Code: code.Code, Name: "nas-c", Address: "nas-c.tail.ts.net", ID: "nas-x", Token: strings.Repeat("k", 64)}); err == nil {
		t.Fatal("code twice")
	}
	// No secret in the view, nor in the settings.
	for _, path := range []string{"/api/v1/agent/buddy", "/api/v1/agent/features", "/api/v1/agent/settings"} {
		_, body := a.request("GET", path, nil)
		if strings.Contains(string(body), pa[0].Token) || strings.Contains(string(body), pa[0].Hash) || strings.Contains(string(body), `"token"`) {
			t.Fatalf("%s shows a secret: %s", path, body)
		}
	}
	// B's token signs in at A, but only for its own folder. No volume is
	// mounted here, so the signed-in request ends at 503, not 401.
	req, _ := http.NewRequest("GET", listenA.URL+"/v1/"+pb[0].Remote+"/config", nil)
	req.SetBasicAuth(pb[0].Remote, pb[0].Token)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("partner request: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", listenA.URL+"/v1/"+pb[0].Remote+"/config", nil)
	req.SetBasicAuth(pb[0].Remote, "wrong-token")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", res.StatusCode)
	}
	// A PUT of the settings from a page cannot touch the partners.
	if res, body := a.request("PUT", "/api/v1/agent/features", map[string]any{"buddy": map[string]any{"enabled": true, "volume": "tank"}}); res.StatusCode != 200 {
		t.Fatalf("features: %d %s", res.StatusCode, body)
	}
	if len(a.server.buddyPartners()) != 1 {
		t.Fatal("settings dropped the partner")
	}
	// An expired code: refused.
	a.server.family54().codes.Now = func() time.Time { return time.Now().Add(-11 * time.Minute) }
	_, body = a.request("POST", "/api/v1/agent/buddy/code", map[string]any{"quota": 2 << 30})
	json.Unmarshal(body, &code)
	a.server.family54().codes.Now = nil
	if _, err := a.server.buddyAnswerPair(buddy.PairRequest{Code: code.Code, Name: "nas-c", Address: "nas-c.tail.ts.net", ID: "nas-x", Token: strings.Repeat("k", 64)}); !errors.Is(err, buddy.ErrNoCode) {
		t.Fatalf("expired code: %v", err)
	}
	// Ending the partnership.
	if res, _ := a.request("DELETE", "/api/v1/agent/buddy/partners/"+pa[0].ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", listenA.URL+"/v1/"+pb[0].Remote+"/config", nil)
	req.SetBasicAuth(pb[0].Remote, pb[0].Token)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("removed partner: %d", res.StatusCode)
	}
}

// Pairing with shares makes the sending job; it follows the schedule of
// the settings and keeps the token out of its address.
func TestFamily54BuddySendingJob(t *testing.T) {
	a := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	b := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	listenA := httptest.NewServer(a.server.buddyHandler())
	defer listenA.Close()
	addrA := strings.TrimPrefix(listenA.URL, "http://")
	for _, ts := range []*testServer{a, b} {
		setAgent(ts, func(s *agentSettings) {
			s.Features.Buddy = agentBuddy{Enabled: true, Volume: "tank", Address: "nas-b.tail.ts.net:8752"}
		})
	}
	_, body := a.request("POST", "/api/v1/agent/buddy/code", map[string]any{"quota": 2 << 30, "shares": []string{"media"}})
	var code struct{ Code string }
	json.Unmarshal(body, &code)
	res, body := b.request("POST", "/api/v1/agent/buddy/pair", map[string]any{"address": addrA, "code": code.Code, "quota": 3 << 30, "shares": []string{"media"}})
	if res.StatusCode != 200 || strings.Contains(string(body), "jobError") {
		t.Fatalf("pair: %d %s", res.StatusCode, body)
	}
	pb := b.server.buddyPartners()[0]
	for _, side := range []struct {
		ts *testServer
		p  buddyPartner
	}{{b, pb}, {a, a.server.buddyPartners()[0]}} {
		job, ok := side.ts.server.buddyJobs()[buddyJobPrefix+side.p.ID]
		if !ok {
			t.Fatalf("no job for %s", side.p.ID)
		}
		d := job.Job.Destination
		if d.Type != "rest" || d.URL != buddy.BaseURL(side.p.Address)+"/"+side.p.Remote || d.Username != side.p.Remote || strings.Contains(d.URL, side.p.Token) {
			t.Fatalf("destination: %+v", d)
		}
		if job.Job.Every != "weekly" || job.Job.Keep.Daily != 31 || !job.SecretSet["password"] {
			t.Fatalf("job: %+v", job)
		}
	}
	for _, every := range []string{"daily", "monthly", "weekly"} {
		setAgent(b, func(s *agentSettings) { s.Features.Buddy.Every, s.Features.Buddy.Time = every, "03:30" })
		b.server.buddyTick(time.Now())
		job := b.server.buddyJobs()[buddyJobPrefix+pb.ID]
		if job.Job.Every != every || job.Job.Time != "03:30" {
			t.Fatalf("%s: %+v", every, job.Job)
		}
	}
	// Changing the shares changes the job; none removes it.
	if res, body := b.request("PUT", "/api/v1/agent/buddy/partners/"+pb.ID, map[string]any{"quota": 4 << 30, "shares": []string{}}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("partner: %d %s", res.StatusCode, body)
	}
	if _, ok := b.server.buddyJobs()[buddyJobPrefix+pb.ID]; ok {
		t.Fatal("job without shares")
	}
	if res, _ := b.request("PUT", "/api/v1/agent/buddy/partners/"+pb.ID, map[string]any{"quota": 4 << 30, "shares": []string{"nosuch"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("unknown share")
	}
}

func TestFamily54BuddyJob(t *testing.T) {
	p := buddyPartner{ID: "nas-tom", Address: "nas-tom:8752", Remote: "nas-eva", Token: "secret", Shares: []string{"Documents"}}
	job := (&Server{}).buddyJob(p, agentBuddy{}.withDefaults())
	if job.ID != "buddy-nas-tom" || job.Destination.Type != "rest" || job.Destination.URL != "http://nas-tom:8752/v1/nas-eva" || job.Destination.Username != "nas-eva" || job.Every != "weekly" || job.Keep.Daily != 31 || job.Keep.Weekly != 8 || job.Keep.Monthly != 12 {
		t.Fatalf("job: %+v", job)
	}
	// The token goes to restic in the environment, never in the URL.
	if strings.Contains(job.Destination.URL, "secret") {
		t.Fatal("token in the URL")
	}
}

// ---------- catalog ----------

// skillSource serves an App-Store source with a skills/ folder; the
// official source of the test server is pointed at it.
func skillSource(t *testing.T, ts *testServer, files map[string]string) *map[string]string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		zipped := gzip.NewWriter(&buf)
		archive := tar.NewWriter(zipped)
		for name, content := range files {
			archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
			archive.Write([]byte(content))
		}
		archive.Close()
		zipped.Close()
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	ts.server.opts.Catalog.SetClient(srv.Client())
	ts.server.opts.Catalog.SetOfficialURL(srv.URL + "/ra5on/TitanOS-Apps")
	ts.server.opts.Catalog.Refresh(context.Background())
	return &files
}

func TestFamily54Catalog(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	steps := "1. files_search nach Rechnungen\n2. Ergebnis zeigen"
	skillSource(t, ts, map[string]string{
		"apps-HEAD/whoami/compose.yaml":       "services:\n  web:\n    image: traefik/whoami\n",
		"apps-HEAD/skills/steuer/skill.yaml":  "name: Rechnungen nach Steuerjahr\nwhen: Wenn Belege sortiert werden sollen\ntools: [files_search]\nsteps: |\n  1. files_search nach Rechnungen\n  2. Ergebnis zeigen\n",
		"apps-HEAD/skills/strange/skill.yaml": "name: Fremd\nwhen: nie\nsteps: 1. evil_tool aufrufen\ntools: [evil_tool]\n",
		"apps-HEAD/skills/kaputt/skill.yaml":  "name: Kaputt\nsteps: [kein text\n",
	})
	// Off: nothing.
	if res, _ := ts.request("GET", "/api/v1/agent/catalog", nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("off: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/features", map[string]any{"catalog": map[string]any{"enabled": true}}); res.StatusCode != 200 {
		t.Fatalf("features: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/agent/catalog", nil)
	var list struct{ Skills []catalogView }
	json.Unmarshal(body, &list)
	// The malformed skill is skipped; each shows its source.
	if res.StatusCode != 200 || len(list.Skills) != 2 || list.Skills[0].ID != "steuer" || list.Skills[0].SourceName != "TitanOS-Apps" || len(list.Skills[0].Unknown) != 0 || len(list.Skills[1].Unknown) != 1 {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	good, strange := list.Skills[0], list.Skills[1]
	// Unknown tool: refused.
	if res, body := ts.request("POST", "/api/v1/agent/catalog/install", map[string]any{"key": strange.Key, "digest": strange.Digest}); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "evil_tool") {
		t.Fatalf("unknown tool: %d %s", res.StatusCode, body)
	}
	// Not what was shown: refused.
	if res, _ := ts.request("POST", "/api/v1/agent/catalog/install", map[string]any{"key": good.Key, "digest": "0000"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("digest: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/catalog/install", map[string]any{"key": "titanos/nosuch", "digest": good.Digest}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown skill: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/agent/catalog/install", map[string]any{"key": good.Key, "digest": good.Digest}); res.StatusCode != 200 {
		t.Fatalf("install: %d %s", res.StatusCode, body)
	}
	skills, _ := ts.server.agentStore().Skills("alex")
	if len(skills) != 1 || !skills[0].Untrusted || skills[0].Description != catalogMarker+good.Key || skills[0].Steps != steps {
		t.Fatalf("installed: %+v", skills)
	}
	_, body = ts.request("GET", "/api/v1/agent/catalog", nil)
	json.Unmarshal(body, &list)
	if !list.Skills[0].Installed {
		t.Fatal("not shown as installed")
	}
	// Export of an own skill as a submission package for the repository.
	res, body = ts.request("GET", "/api/v1/agent/catalog/export/"+skills[0].ID, nil)
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(string(body), catalogSubmitFmt) || !strings.Contains(string(body), "files_search") || !strings.Contains(string(body), "skill.yaml") {
		t.Fatalf("export: %d %s", res.StatusCode, body)
	}
}

// ---------- diary ----------

func TestFamily54DiarySettings(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	put := func(d map[string]any) (*http.Response, []byte) {
		return ts.request("PUT", "/api/v1/agent/features/me", map[string]any{"diary": d})
	}
	if res, body := put(map[string]any{"immichUrl": "http://immich.local:2283", "immichKey": "immich-secret-key", "frigateUrl": "http://frigate.local:5000", "off": []string{"chats", "nonsense"}}); res.StatusCode != 200 || strings.Contains(string(body), "immich-secret-key") {
		t.Fatalf("set: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/agent/features/me", nil)
	if strings.Contains(string(body), "immich-secret-key") || !strings.Contains(string(body), `"immichKeySet":true`) || strings.Contains(string(body), "nonsense") {
		t.Fatalf("view: %s", body)
	}
	// Saving without the key keeps it.
	put(map[string]any{"immichUrl": "http://immich.local:2283"})
	mine, _ := ts.server.agentFeaturesOf("alex")
	if mine.Diary.ImmichKey != "immich-secret-key" {
		t.Fatal("key lost")
	}
	// A folder in a root this account cannot open is refused.
	if res, _ := put(map[string]any{"photos": map[string]any{"root": "share/gast-privat", "path": "Fotos"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign folder: %d", res.StatusCode)
	}
	if res, _ := put(map[string]any{"immichUrl": "ftp://x"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad address: %d", res.StatusCode)
	}
	if res, _ := put(map[string]any{"clearImmichKey": true, "immichUrl": "http://immich.local:2283"}); res.StatusCode != 200 {
		t.Fatal("clear")
	}
	if mine, _ := ts.server.agentFeaturesOf("alex"); mine.Diary.ImmichKey != "" {
		t.Fatal("key not cleared")
	}
}

func TestFamily54DiaryTool(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	loc := ts.server.agentLocation()
	day := time.Date(2026, 10, 31, 0, 0, 0, 0, loc)
	// Immich: two photos at the lake, with Bello.
	var immichKey string
	immich := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		immichKey = r.Header.Get("x-api-key")
		if r.URL.Path != "/api/search/metadata" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"assets":{"items":[
			{"localDateTime":"2026-10-31T15:10:00.000Z","exifInfo":{"city":"Lohsa"},"people":[{"name":"Bello"}]},
			{"localDateTime":"2026-10-31T15:20:00.000Z","exifInfo":{"city":"Lohsa"},"people":[]}],"nextPage":null}}`))
	}))
	defer immich.Close()
	// Frigate: the doorbell rang twice on Sunday.
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		at := time.Date(2026, 11, 1, 18, 5, 0, 0, loc).Unix()
		json.NewEncoder(w).Encode([]map[string]any{
			{"camera": "haustuer", "label": "person", "sub_label": nil, "start_time": at},
			{"camera": "haustuer", "label": "person", "sub_label": []any{"Oma", 0.9}, "start_time": at + 3600},
		})
	}))
	defer frigate.Close()
	// Home Assistant: the alarm was armed, a lock not released stays out.
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/states":
			w.Write([]byte(`[{"entity_id":"alarm_control_panel.home","state":"disarmed","attributes":{"friendly_name":"Alarmanlage"}},{"entity_id":"lock.garage","state":"locked","attributes":{}}]`))
		case r.URL.Path == "/api/template":
			w.Write([]byte(""))
		case strings.HasPrefix(r.URL.Path, "/api/history/period/"):
			if r.URL.Query().Get("filter_entity_id") != "alarm_control_panel.home" {
				t.Errorf("history of an entity not released: %s", r.URL.RawQuery)
			}
			start, _ := time.Parse(time.RFC3339, strings.TrimPrefix(r.URL.Path, "/api/history/period/"))
			if start.In(loc).Day() != 31 {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[[{"entity_id":"alarm_control_panel.home","state":"armed_away","last_changed":"2026-10-31T08:40:00Z"},{"state":"disarmed","last_changed":"2026-10-31T13:10:00Z"}]]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ha.Close()
	if err := ts.home.SetSettings(homeassistant.Settings{Enabled: true, URL: ha.URL, Token: "t", Domains: []string{"alarm_control_panel", "lock"}}); err != nil {
		t.Fatal(err)
	}
	setAgent(ts, func(s *agentSettings) {
		s.Features.Diary.Enabled = true
		s.HA.Release = homeassistant.Release{Entities: []string{"alarm_control_panel.home"}}
	})
	if res, body := ts.request("PUT", "/api/v1/agent/features/me", map[string]any{"diary": map[string]any{"immichUrl": immich.URL, "immichKey": "k-123", "frigateUrl": frigate.URL}}); res.StatusCode != 200 {
		t.Fatalf("mine: %d %s", res.StatusCode, body)
	}
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	tool, ok := box.tools["diary"]
	if !ok || !tool.untrusted || tool.kind != agent.KindRead {
		t.Fatalf("diary tool: %+v", tool)
	}
	args, _ := json.Marshal(map[string]string{"from": day.Format("2006-01-02"), "to": day.AddDate(0, 0, 1).Format("2006-01-02")})
	p, err := box.Prepare(context.Background(), nil, "diary", args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := box.Execute(context.Background(), nil, p)
	if err != nil {
		t.Fatal(err)
	}
	if immichKey != "k-123" {
		t.Fatalf("immich key: %q", immichKey)
	}
	for _, want := range []string{"Lohsa", "Bello", "haustuer: Oma", "Alarmanlage: armed_away", "2026-10-31 Saturday", "2026-11-01 Sunday"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("missing %q in %s", want, out.Text)
		}
	}
	if strings.Contains(out.Text, "lock.garage") || strings.Contains(out.Text, "k-123") {
		t.Fatalf("leak: %s", out.Text)
	}
	titles := []string{}
	for _, s := range out.Sources {
		titles = append(titles, s.Title)
	}
	if got := strings.Join(titles, ","); got != "Home Assistant,Immich (2),Frigate (2)" {
		t.Fatalf("sources: %s", got)
	}
	// "Wer war an der Tür?" only from Frigate.
	args, _ = json.Marshal(map[string]string{"from": "2026-11-01", "to": "2026-11-01", "focus": "door"})
	out, _ = box.diary(context.Background(), nil, args)
	if !strings.Contains(out.Text, "Oma") || strings.Contains(out.Text, "Lohsa") {
		t.Fatalf("door: %s", out.Text)
	}
	// A user does not get Home Assistant; its own settings decide the rest.
	guestClient(t, ts)
	gbox := ts.server.newAgentToolbox(mustUser(t, ts, "gast"), ts.server.agentSettings())
	args, _ = json.Marshal(map[string]string{"from": "2026-10-31", "to": "2026-11-01"})
	out, _ = gbox.diary(context.Background(), nil, args)
	if strings.Contains(out.Text, "Alarmanlage") || strings.Contains(out.Text, "Lohsa") {
		t.Fatalf("guest sees the owner's sources: %s", out.Text)
	}
	// Ranges are checked.
	for _, bad := range []map[string]string{{"from": "2026-11-01", "to": "2026-10-01"}, {"from": "2026-01-01", "to": "2026-12-31"}, {"from": "gestern", "to": "heute"}} {
		args, _ := json.Marshal(bad)
		if _, err := box.diary(context.Background(), nil, args); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
