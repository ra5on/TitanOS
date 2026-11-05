package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
)

type photosAnswer struct {
	Prefs     photoPrefs   `json:"prefs"`
	Items     []photoItem  `json:"items"`
	Described int          `json:"described"`
	Scanning  bool         `json:"scanning"`
	Describe  struct{ Running bool; Done, Total int; Error string } `json:"describe"`
	AI        bool         `json:"ai"`
}

// PLAN 52: photos of the chosen folders as a timeline with date and GPS,
// descriptions by the picture model, search, albums shared with rights.
func TestPhotos(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	docs := ts.withShare() // share Docs on volume tank
	os.MkdirAll(filepath.Join(docs, "Fotos", "Urlaub"), 0o755)
	os.MkdirAll(filepath.Join(docs, "Fotos", ".thumbs"), 0o755)
	os.WriteFile(filepath.Join(docs, "Fotos", "Urlaub", "strand.jpg"), thumbs.ExifJPEGForTest("iPhone 15", "2026:10:12 15:42:00", 55.5578, 8.0833), 0o644)
	os.WriteFile(filepath.Join(docs, "Fotos", "weihnachten.jpg"), thumbs.ExifJPEGForTest("Pixel 9", "2025:12:24 18:00:00", 53.55, 9.99), 0o644)
	os.WriteFile(filepath.Join(docs, "Fotos", "clip.mp4"), []byte("video"), 0o644)
	os.WriteFile(filepath.Join(docs, "Fotos", "notiz.txt"), []byte("kein Foto"), 0o644)
	os.WriteFile(filepath.Join(docs, "Fotos", ".thumbs", "x.jpg"), []byte("hidden"), 0o644)
	os.WriteFile(filepath.Join(docs, "anderswo.jpg"), []byte("not chosen"), 0o644)

	if res, _ := ts.request("PUT", "/api/v1/photos/prefs", map[string]any{"folders": []map[string]string{{"root": "share/Fremd", "path": ""}}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign root: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/photos/prefs", map[string]any{"folders": []map[string]string{{"root": "share/Docs", "path": "Fotos"}}, "describe": "idle", "from": 1, "to": 6}); res.StatusCode != http.StatusOK {
		t.Fatalf("prefs: %d %s", res.StatusCode, body)
	}
	u, _ := ts.users.Get("alex")
	ts.server.photoScan(u)
	get := func(client *http.Client) photosAnswer {
		t.Helper()
		_, body := ts.do(client, "GET", "/api/v1/photos", nil)
		var a photosAnswer
		json.Unmarshal(body, &a)
		return a
	}
	a := get(ts.client)
	if len(a.Items) != 3 {
		t.Fatalf("items: %+v", a.Items)
	}
	// Newest first: the video (changed today), the beach (Oct 2026), Christmas 2025.
	if a.Items[1].Path != "Fotos/Urlaub/strand.jpg" || a.Items[2].Path != "Fotos/weihnachten.jpg" {
		t.Fatalf("order: %v %v %v", a.Items[0].Path, a.Items[1].Path, a.Items[2].Path)
	}
	beach := a.Items[1]
	if !beach.GPS || beach.Camera != "iPhone 15" || beach.Taken.Year() != 2026 || a.Items[0].Kind != "video" {
		t.Fatalf("beach: %+v", beach)
	}
	if !a.AI || a.Prefs.Describe != "idle" {
		t.Fatalf("answer: %+v", a)
	}
	// Search by month name and year without descriptions.
	_, body := ts.request("GET", "/api/v1/photos/search?q=Dezember+2025", nil)
	if !strings.Contains(string(body), "weihnachten.jpg") || strings.Contains(string(body), "strand.jpg") {
		t.Fatalf("search by date: %s", body)
	}

	// Descriptions by the picture model, started by the button.
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["vision"] = map[string]any{"enabled": true, "model": "gemma4:12b"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	fake.replies = []string{
		`{"role":"assistant","content":"Ein brauner Hund läuft über einen Sandstrand.\nStichwörter: Hund, Strand, Meer"}`,
		`{"role":"assistant","content":"Ein geschmückter Weihnachtsbaum im Wohnzimmer.\nStichwörter: Weihnachten, Baum"}`,
	}
	if res, body := ts.request("POST", "/api/v1/photos/describe", map[string]any{"run": true}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("describe: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a = get(ts.client)
		if !a.Describe.Running && a.Described == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("descriptions: %+v", a)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !a.Prefs.Started {
		t.Fatal("first run by the button does not start the schedule")
	}
	_, body = ts.request("GET", "/api/v1/photos/search?q=Hund+am+Strand", nil)
	var hits struct{ Items []photoItem }
	json.Unmarshal(body, &hits)
	if len(hits.Items) != 1 || hits.Items[0].Path != "Fotos/Urlaub/strand.jpg" {
		t.Fatalf("search: %s", body)
	}
	// The index survives a restart of titand (read from the volume).
	ts.server.photos.mu.Lock()
	ts.server.photos.users = nil
	ts.server.photos.mu.Unlock()
	if a = get(ts.client); a.Described != 2 {
		t.Fatalf("index not kept: %+v", a)
	}

	// The agent finds photos and makes an album.
	box := ts.server.newAgentToolbox(u, ts.server.agentSettings())
	out, err := box.photosSearch(t.Context(), nil, json.RawMessage(`{"query":"Weihnachtsbaum"}`))
	if err != nil || !strings.Contains(out.Text, "weihnachten.jpg") {
		t.Fatalf("photos_search: %v %s", err, out.Text)
	}
	out, err = box.photoAlbumSave(t.Context(), nil, json.RawMessage(`{"name":"Urlaub 2026","photos":[{"root":"share/Docs","path":"Fotos/Urlaub/strand.jpg"},{"root":"share/Fremd","path":"x.jpg"}]}`))
	if err != nil || !strings.Contains(out.Text, "1 photos") {
		t.Fatalf("album: %v %s", err, out.Text)
	}

	// A shared album: maria sees it, but only photos of roots she may open.
	ts.users.Create("maria", testPassword, auth.RoleUser)
	_, body = ts.request("GET", "/api/v1/photos/albums", nil)
	var albums struct{ Albums []photoAlbum }
	json.Unmarshal(body, &albums)
	if len(albums.Albums) != 1 {
		t.Fatalf("albums: %s", body)
	}
	if res, body := ts.request("PUT", "/api/v1/photos/albums/"+albums.Albums[0].ID, map[string]any{"members": []string{"maria"}}); res.StatusCode != http.StatusOK {
		t.Fatalf("share album: %d %s", res.StatusCode, body)
	}
	maria := ts.newClient()
	ts.do(maria, "POST", "/api/v1/login", credentials{Name: "maria", Password: testPassword})
	_, body = ts.do(maria, "GET", "/api/v1/photos/albums", nil)
	json.Unmarshal(body, &albums)
	if len(albums.Albums) != 1 || albums.Albums[0].Owner != "alex" || len(albums.Albums[0].Items) != 0 {
		t.Fatalf("maria without access to Docs: %s", body)
	}
	if a := get(maria); len(a.Items) != 0 {
		t.Fatalf("maria sees alex's photos: %+v", a.Items)
	}
	if res, _ := ts.do(maria, "PUT", "/api/v1/photos/albums/"+albums.Albums[0].ID, map[string]any{"name": "Meins"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("maria changed alex's album: %d", res.StatusCode)
	}
}
