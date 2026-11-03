package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// releases is a project page with several releases.
type releases struct {
	mu   sync.Mutex
	tags []string // in the feed, as GitHub lists them
	// files by tag and name: manifest.json, manifest.json.minisig and assets.
	files map[string]map[string][]byte
}

func (r *releases) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.URL.Path == "/releases.atom" {
		fmt.Fprint(w, "<feed>")
		for _, tag := range r.tags {
			fmt.Fprintf(w, `<entry><link rel="alternate" type="text/html" href="https://example/releases/tag/%s"/></entry>`, tag)
		}
		fmt.Fprint(w, "</feed>")
		return
	}
	path := req.URL.Path
	if strings.HasPrefix(path, "/releases/latest/download/") {
		// GitHub's latest: the newest tag with a release.
		for _, tag := range r.tags {
			if r.files[tag] != nil {
				path = "/releases/download/" + tag + "/" + strings.TrimPrefix(path, "/releases/latest/download/")
				break
			}
		}
	}
	tag, name, ok := strings.Cut(strings.TrimPrefix(path, "/releases/download/"), "/")
	data, found := r.files[tag][name]
	if !ok || !found {
		http.NotFound(w, req)
		return
	}
	w.Write(data)
}

// add publishes a release; change may alter the manifest before it is signed.
func (r *releases) add(e *env, version string, change func(*Manifest)) {
	manifest := Manifest{Version: version, Tag: "v" + version, Channel: ChannelStable, Kernel: "6.12.115", Released: time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC), Changelog: "Release " + version + ".", Cmdline: cmdline}
	files := map[string][]byte{}
	for _, name := range slotFiles {
		data := make([]byte, 20_000)
		rand.Read(data)
		asset := "titanos-" + version + "-" + name
		files[asset] = data
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, File{Name: name, Asset: asset, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	}
	if change != nil {
		change(&manifest)
	}
	raw, _ := json.Marshal(manifest)
	files["manifest.json"], files["manifest.json.minisig"] = raw, []byte(e.signer.sign(raw))
	r.mu.Lock()
	r.files["v"+version] = files
	r.mu.Unlock()
}

// newChoiceEnv runs 1.0.0 against a page with 1.0.0, 1.1.0 and 1.2.0, a
// tag 1.3.0 without release files and an old 0.45.1.
func newChoiceEnv(t *testing.T) (*env, *releases) {
	t.Helper()
	e := newEnv(t)
	page := &releases{files: map[string]map[string][]byte{}, tags: []string{"v1.3.0", "v1.2.0", "v1.1.0", "v1.1.0", "v1.0.0", "v0.45.1"}}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "0.45.1"} {
		page.add(e, version, nil)
	}
	server := httptest.NewServer(page)
	t.Cleanup(server.Close)
	settings := e.m.Status().Settings
	settings.Source = server.URL
	e.store.WriteJSON(settingsFile, settings)
	e.m = e.manager(t, "1.0.0", "a", "boot-1")
	return e, page
}

func TestVersionTagsFromTheFloorOn(t *testing.T) {
	feed := []byte(`<a href="https://x/releases/tag/v0.46.3"/><a href="https://x/releases/tag/v0.46.4-beta.1"/>` +
		`<a href="https://x/releases/tag/v0.45.1"/><a href="https://x/releases/tag/v0.46.2"/><a href="https://x/releases/tag/v0.46.3"/>` +
		`<a href="https://x/releases/tag/nightly"/><a href="https://x/releases/tag/v0.46.10"/>`)
	if got := strings.Join(versionTags(feed, ChannelStable), " "); got != "v0.46.10 v0.46.3 v0.46.2" {
		t.Errorf("stable: %s", got)
	}
	if got := strings.Join(versionTags(feed, ChannelBeta), " "); got != "v0.46.10 v0.46.4-beta.1 v0.46.3 v0.46.2" {
		t.Errorf("beta: %s", got)
	}
}

func TestVersionsListsPublishedSignedReleases(t *testing.T) {
	e, _ := newChoiceEnv(t)
	list, err := e.m.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, choice := range list {
		got = append(got, fmt.Sprintf("%s running=%v newest=%v", choice.Version, choice.Running, choice.Newest))
	}
	// 1.3.0 has no release files, 0.45.1 lies before the floor.
	want := "1.2.0 running=false newest=true|1.1.0 running=false newest=false|1.0.0 running=true newest=false"
	if strings.Join(got, "|") != want {
		t.Errorf("versions:\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if list[1].Kernel != "6.12.115" || list[1].Size != 60_000 || list[1].Changelog != "Release 1.1.0." {
		t.Errorf("details: %+v", list[1])
	}
}

func TestVersionsLeaveOutWhatIsNotTrusted(t *testing.T) {
	e, page := newChoiceEnv(t)
	other := newSigner(t)
	page.mu.Lock()
	page.files["v1.2.0"]["manifest.json.minisig"] = []byte(other.sign(page.files["v1.2.0"]["manifest.json"]))
	page.mu.Unlock()
	// The manifest of 1.0.0 under the tag of 1.1.0.
	page.add(e, "1.0.0", nil)
	page.mu.Lock()
	page.files["v1.1.0"] = page.files["v1.0.0"]
	page.mu.Unlock()
	list, err := e.m.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Version != "1.0.0" || !list[0].Newest {
		t.Errorf("versions: %+v", list)
	}
	if err := e.m.InstallVersion(context.Background(), "1.2.0"); !errors.Is(err, ErrSignature) {
		t.Errorf("foreign signature: %v", err)
	}
	if err := e.m.InstallVersion(context.Background(), "1.1.0"); !errors.Is(err, ErrManifest) {
		t.Errorf("manifest of another version: %v", err)
	}
}

func TestInstallVersionRefusals(t *testing.T) {
	e, _ := newChoiceEnv(t)
	for version, want := range map[string]error{"0.45.1": ErrInvalid, "0.46.1": ErrInvalid, "1.x": ErrInvalid, "1.0.0": ErrNothing} {
		if err := e.m.InstallVersion(context.Background(), version); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", version, err, want)
		}
	}
	if err := e.m.InstallVersion(context.Background(), "1.3.0"); err == nil {
		t.Error("a tag without release files was installed")
	}
	if e.m.Status().Pending != nil {
		t.Error("something was installed")
	}
}

func waitInstalled(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if phase := m.Status().Progress.Phase; phase == "done" || phase == "failed" {
			if phase == "failed" {
				t.Fatalf("installation failed: %+v", m.Status().Progress)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the installation did not finish")
}

func TestOlderVersionSwitchesAutomaticInstallationOff(t *testing.T) {
	e, _ := newChoiceEnv(t)
	settings := e.m.Status().Settings
	settings.AutoInstall = true
	if err := e.m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.m.InstallVersion(context.Background(), "1.1.0"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, e.m)
	status := e.m.Status()
	if status.Pending == nil || status.Pending.Version != "1.1.0" || status.Pending.Slot != "b" {
		t.Fatalf("pending: %+v", status.Pending)
	}
	if status.Settings.AutoInstall {
		t.Error("automatic installation would bring 1.2.0 back the next night")
	}
	data, _ := e.store.Read(settingsFile)
	if strings.Contains(string(data), `"autoInstall":true`) {
		t.Errorf("stored settings: %s", data)
	}
}

func TestNewestVersionKeepsAutomaticInstallation(t *testing.T) {
	e, _ := newChoiceEnv(t)
	settings := e.m.Status().Settings
	settings.AutoInstall = true
	if err := e.m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.m.InstallVersion(context.Background(), "1.2.0"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, e.m)
	if status := e.m.Status(); status.Pending == nil || status.Pending.Version != "1.2.0" || !status.Settings.AutoInstall {
		t.Errorf("status: pending %+v, auto %v", status.Pending, status.Settings.AutoInstall)
	}
}
