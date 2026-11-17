package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The ids of the fake Immich (version 4 UUIDs, as Immich uses them).
const (
	immichUserA  = "11111111-1111-4111-8111-111111111111"
	immichUserB  = "22222222-2222-4222-8222-222222222222"
	immichAssetA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	immichAssetB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	immichPerson = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	immichKeyA   = "keyOfAlexInImmich0123456789"
	immichKeyB   = "keyOfGastInImmich0123456789"
	immichKeyRO  = "keyWithoutPersonRead012345"
)

// fakeImmich answers the routes of Immich v3.3.3 that TitanOS uses. Alex's
// key sees his own photo and the one of a partner (gast), as Immich's
// search does with partner sharing; gast's key sees only his own.
type fakeImmich struct {
	mu       sync.Mutex
	smart    bool
	bodies   []map[string]any
	paths    []string
	keysSeen []string
}

func (f *fakeImmich) start(t *testing.T) *httptest.Server {
	asset := func(id, owner, city string) map[string]any {
		return map[string]any{"id": id, "ownerId": owner, "type": "IMAGE", "originalFileName": "IMG_" + id[:4] + ".jpg",
			"localDateTime": "2025-07-14T18:02:00.000Z", "fileCreatedAt": "2025-07-14T16:02:00.000Z", "isTrashed": false, "visibility": "timeline",
			"exifInfo": map[string]any{"city": city, "country": "Italien"}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.keysSeen = append(f.keysSeen, key)
		smart := f.smart
		f.mu.Unlock()
		user := map[string]string{immichKeyA: immichUserA, immichKeyRO: immichUserA, immichKeyB: immichUserB}[key]
		if r.URL.Path == "/api/server/features" {
			json.NewEncoder(w).Encode(map[string]any{"smartSearch": smart, "search": true, "facialRecognition": true})
			return
		}
		if r.URL.Path == "/api/server/version" {
			json.NewEncoder(w).Encode(map[string]int{"major": 3, "minor": 3, "patch": 3})
			return
		}
		if user == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/users/me":
			json.NewEncoder(w).Encode(map[string]any{"id": user, "name": map[string]string{immichUserA: "Alex", immichUserB: "Gast"}[user], "email": "x@example.org"})
		case r.URL.Path == "/api/api-keys/me":
			perms := []string{"all"}
			if key == immichKeyRO {
				perms = []string{"asset.read", "asset.view", "user.read"}
			}
			json.NewEncoder(w).Encode(map[string]any{"permissions": perms})
		case r.URL.Path == "/api/search/person":
			if key == immichKeyRO {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if strings.EqualFold(r.URL.Query().Get("name"), "Mia") {
				json.NewEncoder(w).Encode([]map[string]any{{"id": immichPerson, "name": "Mia"}})
				return
			}
			w.Write([]byte("[]"))
		case r.URL.Path == "/api/search/smart" || r.URL.Path == "/api/search/metadata":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.bodies = append(f.bodies, body)
			f.mu.Unlock()
			items := []any{asset(immichAssetA, immichUserA, "Rom")}
			if user == immichUserA {
				// The partner's photo comes along in Alex's search.
				items = append(items, asset(immichAssetB, immichUserB, "Neapel"))
			} else {
				items = []any{asset(immichAssetB, immichUserB, "Neapel")}
			}
			json.NewEncoder(w).Encode(map[string]any{"albums": map[string]any{"items": []any{}}, "assets": map[string]any{"total": len(items), "count": len(items), "items": items, "facets": []any{}, "nextPage": nil}})
		case strings.HasPrefix(r.URL.Path, "/api/assets/"):
			rest := strings.TrimPrefix(r.URL.Path, "/api/assets/")
			id, sub, _ := strings.Cut(rest, "/")
			owner := map[string]string{immichAssetA: immichUserA, immichAssetB: immichUserB}[id]
			// Partner sharing: Alex may view gast's photo in Immich.
			if owner == "" || (user == immichUserB && owner != immichUserB) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if sub == "thumbnail" {
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Set-Cookie", "immich=secret")
				fmt.Fprintf(w, "JPEG-%s-%s", id[:4], r.URL.Query().Get("size"))
				return
			}
			json.NewEncoder(w).Encode(asset(id, owner, "Rom"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func photosServer(t *testing.T) (*testServer, *fakeImmich, *httptest.Server, *http.Client) {
	t.Helper()
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	fake := &fakeImmich{smart: true}
	srv := fake.start(t)
	address := srv.URL
	var mu sync.Mutex
	ts.server.agentPhotos.find = func(context.Context) string {
		mu.Lock()
		defer mu.Unlock()
		return address
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	gast := ts.guest("gast")
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
	return ts, fake, srv, gast
}

func photosTool(t *testing.T, ts *testServer, user auth.User) *agentTool {
	t.Helper()
	return ts.server.newAgentToolbox(user, ts.server.agentSettings()).tools["photos_search"]
}

// Without a key the tool does not exist; a key Immich refuses is not
// stored; a good one is stored sealed, never shown, and makes the tool.
func TestAgentPhotosKey(t *testing.T) {
	ts, _, _, gast := photosServer(t)
	alex := auth.User{Name: "alex", Role: auth.RoleOwner}
	if photosTool(t, ts, alex) != nil {
		t.Fatal("the tool exists without a key")
	}
	res, body := ts.request("GET", "/api/v1/agent/photos", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"immich":true`) || !strings.Contains(string(body), `"keySet":false`) {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	if res, body = ts.request("PUT", "/api/v1/agent/photos", map[string]string{"key": "falsch"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "immich_key_rejected" {
		t.Fatalf("wrong key: %d %s", res.StatusCode, body)
	}
	if photosTool(t, ts, alex) != nil {
		t.Fatal("a refused key made the tool")
	}
	res, body = ts.request("PUT", "/api/v1/agent/photos", map[string]string{"key": immichKeyRO})
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"missing":["person.read"]`) || strings.Contains(string(body), immichKeyRO) {
		t.Fatalf("key without person.read: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("PUT", "/api/v1/agent/photos", map[string]string{"key": immichKeyA})
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), immichKeyA) || !strings.Contains(string(body), `"userName":"Alex"`) || strings.Contains(string(body), "missing") {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	if _, body = ts.request("GET", "/api/v1/agent/photos", nil); strings.Contains(string(body), immichKeyA) || !strings.Contains(string(body), `"keySet":true`) {
		t.Fatalf("status after saving: %s", body)
	}
	if res, body = ts.request("POST", "/api/v1/agent/photos/check", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"smart":true`) || !strings.Contains(string(body), `"version":"3.3.3"`) {
		t.Fatalf("check: %d %s", res.StatusCode, body)
	}
	// The key is nowhere in plain text: not on the stick, not on the volume.
	for _, dir := range []string{ts.server.opts.Store.Dir(), agentDataDir(t, ts)} {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if data, _ := os.ReadFile(p); strings.Contains(string(data), immichKeyA) {
				t.Errorf("the key stands in plain text in %s", p)
			}
			return nil
		})
	}
	if photosTool(t, ts, alex) == nil {
		t.Fatal("no tool with a key")
	}
	// gast has no key of his own: no tool for him, and Alex's key is not his.
	if photosTool(t, ts, auth.User{Name: "gast", Role: auth.RoleUser}) != nil {
		t.Fatal("gast got a tool with Alex's key")
	}
	if res, _ = ts.do(gast, "GET", "/api/v1/agent/photos/"+immichAssetA+"/picture", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("gast without a key gets a picture: %d", res.StatusCode)
	}
	// Without Immich the tool is gone again.
	ts.server.agentPhotos.find = func(context.Context) string { return "" }
	if photosTool(t, ts, alex) != nil {
		t.Fatal("the tool exists without Immich")
	}
	// Removing the key removes the tool.
	ts.server.agentPhotos.find = nil
	ts.server.agentPhotos.found = "x"
	if res, _ = ts.request("DELETE", "/api/v1/agent/photos", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if st, _ := ts.server.photosStored("alex"); st != nil {
		t.Fatal("key still stored")
	}
}

// agentDataDir is the folder the test's agent writes to.
func agentDataDir(t *testing.T, ts *testServer) string {
	t.Helper()
	root, err := ts.server.agentOpen()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return root.Name()
}

// The search returns only the account's own photos, as short entries; the
// picture route gives only the account's own photos and nothing of
// Immich's headers; the profile of Home Assistant has no tool even when
// the owner has a key; the policy can forbid the area.
func TestAgentPhotosSearch(t *testing.T) {
	ts, fake, _, gast := photosServer(t)
	if res, body := ts.request("PUT", "/api/v1/agent/photos", map[string]string{"key": immichKeyA}); res.StatusCode != http.StatusOK {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	alex := auth.User{Name: "alex", Role: auth.RoleOwner}
	tool := photosTool(t, ts, alex)
	if tool == nil || tool.area != agent.AreaPhotos || tool.kind != agent.KindRead || !tool.untrusted {
		t.Fatalf("tool %+v", tool)
	}
	out, err := tool.own(t.Context(), nil, json.RawMessage(`{"query":"Strand","person":"Mia","from":"2025-06","to":"2025-08"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Photos []photoHit `json:"photos"`
		Search string     `json:"search"`
	}
	if err := json.Unmarshal([]byte(out.Text), &result); err != nil {
		t.Fatalf("result %s: %v", out.Text, err)
	}
	if len(result.Photos) != 1 || result.Photos[0].ID != immichAssetA || result.Photos[0].Place != "Rom, Italien" || result.Search != "smart" {
		t.Fatalf("result %s", out.Text)
	}
	fake.mu.Lock()
	last := fake.bodies[len(fake.bodies)-1]
	fake.mu.Unlock()
	if last["query"] != "Strand" || last["takenAfter"] != "2025-06-01T00:00:00Z" || last["takenBefore"] != "2025-08-31T23:59:59Z" || fmt.Sprint(last["personIds"]) != "["+immichPerson+"]" {
		t.Fatalf("asked Immich %v", last)
	}
	// Without smart search the words are not used, and the result says so.
	fake.mu.Lock()
	fake.smart = false
	fake.mu.Unlock()
	if out, err = tool.own(t.Context(), nil, json.RawMessage(`{"query":"Strand"}`)); err != nil || !strings.Contains(out.Text, `"search":"metadata"`) || !strings.Contains(out.Text, "Smart search is off") {
		t.Fatalf("without smart search: %v %s", err, out.Text)
	}
	// An unknown person: no search, a note.
	if out, err = tool.own(t.Context(), nil, json.RawMessage(`{"person":"Niemand"}`)); err != nil || !strings.Contains(out.Text, `"total":0`) {
		t.Fatalf("unknown person: %v %s", err, out.Text)
	}
	if _, err = tool.own(t.Context(), nil, json.RawMessage(`{"from":"letzten Sommer"}`)); err == nil {
		t.Fatal("a date that is none was taken")
	}

	// The picture of an own photo, without Immich's headers.
	res, body := ts.request("GET", "/api/v1/agent/photos/"+immichAssetA+"/picture?size=preview", nil)
	if res.StatusCode != http.StatusOK || string(body) != "JPEG-aaaa-preview" || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("Set-Cookie") != "" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("picture: %d %s %v", res.StatusCode, body, res.Header)
	}
	// The partner's photo: Immich would show it to Alex's key, TitanOS does not.
	if res, _ = ts.request("GET", "/api/v1/agent/photos/"+immichAssetB+"/picture", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("partner's picture: %d", res.StatusCode)
	}
	// No id of another form ever reaches Immich.
	fake.mu.Lock()
	before := len(fake.paths)
	fake.mu.Unlock()
	for _, bad := range []string{"..%2Fusers%2Fme", "aaaaaaaa", "AAAA-x"} {
		if res, _ = ts.request("GET", "/api/v1/agent/photos/"+bad+"/picture", nil); res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", bad, res.StatusCode)
		}
	}
	fake.mu.Lock()
	if len(fake.paths) != before {
		t.Fatalf("Immich was asked for a bad id: %v", fake.paths[before:])
	}
	fake.mu.Unlock()

	// gast with his own key sees his photo, not Alex's.
	if res, body = ts.do(gast, "PUT", "/api/v1/agent/photos", map[string]string{"key": immichKeyB}); res.StatusCode != http.StatusOK {
		t.Fatalf("gast's key: %d %s", res.StatusCode, body)
	}
	if res, _ = ts.do(gast, "GET", "/api/v1/agent/photos/"+immichAssetA+"/picture", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("gast sees Alex's picture: %d", res.StatusCode)
	}
	if res, _ = ts.do(gast, "GET", "/api/v1/agent/photos/"+immichAssetB+"/picture", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("gast's own picture: %d", res.StatusCode)
	}
	gastTool := photosTool(t, ts, auth.User{Name: "gast", Role: auth.RoleUser})
	if out, err = gastTool.own(t.Context(), nil, json.RawMessage(`{}`)); err != nil || strings.Contains(out.Text, immichAssetA) || !strings.Contains(out.Text, immichAssetB) {
		t.Fatalf("gast's search: %v %s", err, out.Text)
	}
	// The profile of Home Assistant acts as the owner but never gets his photos.
	if photosTool(t, ts, auth.User{Name: agent.AssistProfile, Role: auth.RoleOwner}) != nil {
		t.Fatal("the profile of Home Assistant has the owner's photos")
	}
	if res, _ = ts.request("GET", "/api/v1/agent/photos?profile=assist", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("photos of the profile: %d", res.StatusCode)
	}

	// The area is on the page of the rights, and "Verboten" takes the tool away.
	_, body = ts.request("GET", "/api/v1/agent/tools", nil)
	if !strings.Contains(string(body), `"photos_search"`) || !strings.Contains(string(body), `"photos"`) {
		t.Fatalf("rights page: %s", body)
	}
	settings := ts.server.agentSettings()
	settings.Policy.Areas = map[string]string{agent.AreaPhotos: agent.ModeDeny}
	box := ts.server.newAgentToolbox(alex, settings)
	for _, spec := range box.Specs(nil) {
		if spec.Function.Name == "photos_search" {
			t.Fatal("a forbidden tool is offered")
		}
	}
	if _, err := box.Prepare(t.Context(), nil, "photos_search", json.RawMessage(`{}`)); err == nil {
		t.Fatal("a forbidden tool was prepared")
	}
	// Read only: the standard preset runs it without asking.
	box = ts.server.newAgentToolbox(alex, ts.server.agentSettings())
	if p, err := box.Prepare(t.Context(), &agent.Chat{}, "photos_search", json.RawMessage(`{}`)); err != nil || p.Mode != agent.ModeAllow || !p.Untrusted {
		t.Fatalf("prepare: %+v %v", p, err)
	}
	// Without a key, Prepare refuses: there is no such tool.
	box = ts.server.newAgentToolbox(auth.User{Name: "nobody", Role: auth.RoleUser}, ts.server.agentSettings())
	if _, err := box.Prepare(t.Context(), nil, "photos_search", json.RawMessage(`{}`)); err == nil {
		t.Fatal("a tool without a key was prepared")
	}
}

// A sealed key copied into another account's file does not open.
func TestAgentPhotosKeyBoundToAccount(t *testing.T) {
	ts, _, _, _ := photosServer(t)
	if res, body := ts.request("PUT", "/api/v1/agent/photos", map[string]string{"key": immichKeyA}); res.StatusCode != http.StatusOK {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	st, err := ts.server.photosStored("alex")
	if err != nil || st == nil {
		t.Fatal(err)
	}
	if _, err := ts.server.openPhotosKey("gast", st.Key); err == nil {
		t.Fatal("alex's sealed key opened for gast")
	}
	if key, err := ts.server.openPhotosKey("alex", st.Key); err != nil || key != immichKeyA {
		t.Fatalf("open: %q %v", key, err)
	}
}
