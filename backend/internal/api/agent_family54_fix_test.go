package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/buddy"
)

func init() {
	// The test servers of the partners listen on loopback.
	buddy.AllowHost = func(host string) bool { return host == "127.0.0.1" }
}

// Fix 1: an id is never given again while its folder exists, and ids of
// the same host name differ.
func TestFamily54BuddyIDNotReused(t *testing.T) {
	calls := 0
	old := map[string]bool{}
	id := buddyNewID("nas-tom.tail.ts.net:8752", nil, func(id string) bool {
		calls++
		if calls <= 3 {
			old[id] = true
			return true // a folder of a removed partner
		}
		return false
	})
	if old[id] || calls != 4 || !strings.HasPrefix(id, "nas-tom-") || !buddy.ValidID(id) {
		t.Fatalf("id %q after %d tries", id, calls)
	}
	long := buddyNewID(strings.Repeat("sehr-langer-name", 4), nil, nil)
	if !buddy.ValidID(long) || len("buddy-"+long) > 31 {
		t.Fatalf("long id %q", long)
	}
	if one, two := buddyNewID("nas-tom", nil, nil), buddyNewID("nas-tom", nil, nil); one == two {
		t.Fatal("same id twice")
	}
}

// pairedServers pairs b with a (shares media on both sides) and returns
// the address of a's buddy server.
func pairedServers(t *testing.T) (a, b *testServer, addrA string) {
	t.Helper()
	a = agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	b = agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	listenA := httptest.NewServer(a.server.buddyHandler())
	t.Cleanup(listenA.Close)
	addrA = strings.TrimPrefix(listenA.URL, "http://")
	setAgent(a, func(s *agentSettings) {
		s.Features.Buddy = agentBuddy{Enabled: true, Volume: "tank", Address: "nas-a.tail.ts.net", ProtectDays: 60}
	})
	setAgent(b, func(s *agentSettings) {
		s.Features.Buddy = agentBuddy{Enabled: true, Volume: "tank", Address: "nas-b.tail.ts.net", ProtectDays: 45}
	})
	_, body := a.request("POST", "/api/v1/agent/buddy/code", map[string]any{"quota": 2 << 30, "shares": []string{"media"}})
	var code struct{ Code string }
	json.Unmarshal(body, &code)
	if res, body := b.request("POST", "/api/v1/agent/buddy/pair", map[string]any{"address": addrA, "code": code.Code, "quota": 3 << 30, "shares": []string{"media"}}); res.StatusCode != 200 {
		t.Fatalf("pair: %d %s", res.StatusCode, body)
	}
	return a, b, addrA
}

// Fix 2: each side learns the protection period of the other and keeps
// at least that many daily versions.
func TestFamily54BuddyKeepsProtection(t *testing.T) {
	a, b, _ := pairedServers(t)
	pa, pb := a.server.buddyPartners()[0], b.server.buddyPartners()[0]
	if pa.TheirProtect != 45 || pb.TheirProtect != 60 {
		t.Fatalf("protection: a knows %d, b knows %d", pa.TheirProtect, pb.TheirProtect)
	}
	if job := b.server.buddyJobs()[buddyJobPrefix+pb.ID]; job.Job.Keep.Daily != 61 {
		t.Fatalf("b keeps %d days", job.Job.Keep.Daily)
	}
	if job := a.server.buddyJobs()[buddyJobPrefix+pa.ID]; job.Job.Keep.Daily != 46 {
		t.Fatalf("a keeps %d days", job.Job.Keep.Daily)
	}
	if buddyKeepDays(0) != 31 || buddyKeepDays(10) != 31 {
		t.Fatal("at least 31 days")
	}
}

// Fix 3: unpairing refuses while the job runs and removes the job otherwise.
func TestFamily54BuddyDeleteRemovesJob(t *testing.T) {
	_, b, _ := pairedServers(t)
	pb := b.server.buddyPartners()[0]
	old := buddyJobRunning
	buddyJobRunning = func(*Server, string) bool { return true }
	res, _ := b.request("DELETE", "/api/v1/agent/buddy/partners/"+pb.ID, nil)
	buddyJobRunning = old
	if res.StatusCode != http.StatusConflict || len(b.server.buddyPartners()) != 1 {
		t.Fatalf("while running: %d", res.StatusCode)
	}
	if _, ok := b.server.buddyJobs()[buddyJobPrefix+pb.ID]; !ok {
		t.Fatal("job gone while running")
	}
	if res, _ := b.request("DELETE", "/api/v1/agent/buddy/partners/"+pb.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, ok := b.server.buddyJobs()[buddyJobPrefix+pb.ID]; ok || len(b.server.buddyPartners()) != 0 {
		t.Fatal("job or partner left")
	}
}

// Fix 4: a NAS paired already is refused, and a refusal leaves the code
// usable; a full list does not use up the code either.
func TestFamily54BuddyNoDoublePairing(t *testing.T) {
	a, b, addrA := pairedServers(t)
	// b pairs with a again: refused on b's side before anything is sent.
	_, body := a.request("POST", "/api/v1/agent/buddy/code", map[string]any{"quota": 2 << 30})
	var code struct{ Code string }
	json.Unmarshal(body, &code)
	if res, _ := b.request("POST", "/api/v1/agent/buddy/pair", map[string]any{"address": addrA, "code": code.Code, "quota": 3 << 30}); res.StatusCode != http.StatusConflict {
		t.Fatalf("pair twice: %d", res.StatusCode)
	}
	// The same NAS asks a directly: refused, the code stays.
	pa := a.server.buddyPartners()[0]
	req := buddy.PairRequest{Code: code.Code, Name: pa.Name, Address: pa.Address, ID: "nas-x", Token: strings.Repeat("k", 64)}
	if _, err := a.server.buddyAnswerPair(req); err == nil {
		t.Fatal("same NAS paired twice")
	}
	if _, _, ok := a.server.family54().codes.Active(); !ok {
		t.Fatal("refusal used up the code")
	}
	// Too many partners: refused, the code stays.
	f := a.server.family54()
	f.mu.Lock()
	full := f.partners
	for i := len(full); i < maxBuddyPartners; i++ {
		full = append(full, buddyPartner{ID: "p" + string(rune('a'+i)), Address: "host" + string(rune('a'+i)) + ":8752"})
	}
	f.partners = full
	f.mu.Unlock()
	req.Name, req.Address = "nas-neu", "nas-neu.tail.ts.net:8752"
	if _, err := a.server.buddyAnswerPair(req); err == nil {
		t.Fatal("paired beyond the limit")
	}
	if _, _, ok := f.codes.Active(); !ok {
		t.Fatal("full list used up the code")
	}
}

// Fix 6: the stored Immich key goes only with the address it was given for.
func TestFamily54DiaryKeyFollowsAddress(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	put := func(d map[string]any) {
		if res, body := ts.request("PUT", "/api/v1/agent/features/me", map[string]any{"diary": d}); res.StatusCode != 200 {
			t.Fatalf("put: %d %s", res.StatusCode, body)
		}
	}
	put(map[string]any{"immichUrl": "http://immich.local:2283", "immichKey": "k-1"})
	put(map[string]any{"immichUrl": "http://immich.local:2283"})
	if mine, _ := ts.server.agentFeaturesOf("alex"); mine.Diary.ImmichKey != "k-1" {
		t.Fatal("key lost for the same address")
	}
	put(map[string]any{"immichUrl": "http://elsewhere.example:2283"})
	if mine, _ := ts.server.agentFeaturesOf("alex"); mine.Diary.ImmichKey != "" {
		t.Fatal("key went to another address")
	}
}

// Audit C: Immich and Frigate addresses take no fragment, user info or
// query, and the diary never connects to loopback, link-local or the NAS.
func TestFamily54DiaryNoSSRF(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	for _, bad := range []string{"http://immich.local:2283#", "http://immich.local:2283/#x", "http://user:pw@immich.local", "http://immich.local/?a=1", "http://evil.example@immich.local"} {
		if res, _ := ts.request("PUT", "/api/v1/agent/features/me", map[string]any{"diary": map[string]any{"immichUrl": bad}}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, addr := range []string{"127.0.0.1:2283", "[::1]:2283", "169.254.169.254:80", "0.0.0.0:80"} {
		if diaryAddressAllowed(netip.MustParseAddrPort(addr)) {
			t.Errorf("allowed %s", addr)
		}
	}
	if !diaryAddressAllowed(netip.MustParseAddrPort("10.255.254.253:2283")) {
		t.Error("home network refused")
	}
	// At connection time, after name resolution.
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("[]")) }))
	defer frigate.Close()
	var out []any
	if err := diaryRequest(context.Background(), http.MethodGet, frigate.URL+"/api/events", nil, nil, &out); err == nil {
		t.Fatal("reached a loopback server")
	}
}
