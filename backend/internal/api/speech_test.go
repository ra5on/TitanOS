package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

// PLAN 92: the page of the speech services belongs to owners and
// administrators; nothing can be made active that is not downloaded.
func TestSpeechAPI(t *testing.T) {
	mnt := t.TempDir()
	os.Mkdir(filepath.Join(mnt, "Data"), 0755)
	m := speech.New(config.Open(t.TempDir()))
	m.MountRoot = mnt
	m.Mounted = func() []string { return []string{"Data"} }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	ts := newTestServerWith(t, func(o *Options) { o.Speech = m })
	ts.setup()

	res, body := ts.request("GET", "/api/v1/speech", nil)
	var page struct {
		Settings speech.Settings
		Status   speech.Status
		Models   []struct {
			ID        string
			Installed bool
		}
		Volumes []string
	}
	json.Unmarshal(body, &page)
	if res.StatusCode != http.StatusOK || page.Settings.Enabled || page.Settings.Reach != speech.ReachHA || page.Status.Whisper.Phase != "off" || len(page.Models) != len(speech.WhisperModels) || page.Models[0].ID != "large-v3-turbo" || page.Models[0].Installed || len(page.Volumes) != 1 {
		t.Fatalf("page: %d %s", res.StatusCode, body)
	}
	set := func(s speech.Settings) int {
		res, _ := ts.request("PUT", "/api/v1/speech/settings", s)
		return res.StatusCode
	}
	// Switching on needs a volume that is there.
	if got := set(speech.Settings{Enabled: true}); got != http.StatusBadRequest {
		t.Fatalf("on without a volume: %d", got)
	}
	if got := set(speech.Settings{Enabled: true, Volume: "Weg"}); got != http.StatusBadRequest {
		t.Fatalf("on with a missing volume: %d", got)
	}
	if got := set(speech.Settings{Volume: "Data", Reach: speech.ReachLAN, Speed: 1.5}); got != http.StatusOK {
		t.Fatalf("volume: %d", got)
	}
	// A model or voice that is not on the volume cannot become active.
	if got := set(speech.Settings{Volume: "Data", WhisperModel: "large-v3"}); got != http.StatusBadRequest {
		t.Fatalf("model not downloaded: %d", got)
	}
	if got := set(speech.Settings{Volume: "Data", Voice: "de_DE-thorsten-high"}); got != http.StatusBadRequest {
		t.Fatalf("voice not downloaded: %d", got)
	}
	for _, bad := range []speech.Settings{{Volume: "Data", Speed: 9}, {Volume: "Data", Reach: "internet"}, {Volume: "Data", Language: "deutsch"}} {
		if got := set(bad); got != http.StatusBadRequest {
			t.Fatalf("%+v: %d", bad, got)
		}
	}
	if got := m.Settings(); got.Reach != speech.ReachLAN || got.Speed != 1.5 {
		t.Fatalf("kept settings: %+v", got)
	}
	// Unknown things are not downloaded or removed.
	if res, _ := ts.request("POST", "/api/v1/speech/download", map[string]string{"kind": "model", "id": "../x"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("download of an unknown model: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/speech/model/riesig", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("remove of an unknown model: %d", res.StatusCode)
	}
	// Nothing runs: speaking and hearing say so.
	if res, _ := ts.request("POST", "/api/v1/speech/say", map[string]string{"text": "Hallo"}); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("say without a service: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/speech/say", map[string]string{"text": strings.Repeat("a", 4001)}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("too long a text: %d", res.StatusCode)
	}

	// A plain account sees and changes nothing.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, c := range [][2]string{{"GET", "/api/v1/speech"}, {"PUT", "/api/v1/speech/settings"}, {"POST", "/api/v1/speech/download"}, {"POST", "/api/v1/speech/say"}, {"DELETE", "/api/v1/speech/model/tiny"}} {
		if res, _ := ts.do(guest, c[0], c[1], map[string]string{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s for a plain account: %d", c[0], c[1], res.StatusCode)
		}
	}
}
