package webapps

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestCatalogMatchesRepository(t *testing.T) {
	repo, err := os.ReadFile("../../../webapps/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repo, catalogJSON) {
		t.Fatal("backend/internal/webapps/catalog.json differs from webapps/catalog.json: copy it")
	}
}

func TestCatalogContents(t *testing.T) {
	apps := Catalog()
	if len(apps) != 28 {
		t.Fatalf("%d apps", len(apps))
	}
	defaults := Defaults()
	slices.Sort(defaults)
	// PLAN 75-20: our own builds are the defaults; the Craft originals stay in the catalog.
	if !slices.Equal(defaults, []string{"t-exel", "t-pdf", "t-word"}) {
		t.Fatalf("defaults %v", defaults)
	}
	want := map[string][]string{"wordcraft": {"docx", "doc", "odt", "rtf"}, "gridcraft": {"xlsx", "xls", "ods", "csv"}, "pdfcraft": {"pdf"}}
	own := map[string]string{"wordcraft": "t-word", "gridcraft": "t-exel", "pdfcraft": "t-pdf"}
	for id, types := range want {
		app, _ := Lookup(id)
		mine, _ := Lookup(own[id])
		if !slices.Equal(app.Types, types) || app.Bridge != "host" || app.Trusted {
			t.Fatalf("%s: %+v", id, app)
		}
		if !slices.Equal(mine.Types, types) || mine.Bridge != "host" || !mine.Trusted || mine.Repo != "ra5on/TitanOS-Office" {
			t.Fatalf("%s: %+v", own[id], mine)
		}
	}
	// PLAN 75-20: every Craft original has its own build as successor, trusted, same types and bridge.
	for _, app := range apps {
		if app.Repo == "ra5on/TitanOS-Office" {
			continue
		}
		next, ok := Lookup(app.Successor)
		if !ok || !next.Trusted || next.Bridge != app.Bridge || !slices.Equal(next.Types, app.Types) {
			t.Fatalf("%s → %q: %+v", app.ID, app.Successor, next)
		}
	}
	if _, ok := Lookup("_host"); ok || ValidID("_host") {
		t.Fatal("_host must be reserved")
	}
}

func TestParseCatalogRejects(t *testing.T) {
	good := `{"id":"a","name":"A","repo":"o/a","version":"1.0.0","upstreamAsset":"a-web-1.0.0.zip","sha256":"` + strings.Repeat("a", 64) + `","size":1,"license":"MIT","bridge":"host","types":[],"default":false}`
	if _, err := ParseCatalog([]byte(`{"apps":[` + good + `]}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(good, `"id":"a"`, `"id":"_a"`, 1),
		strings.Replace(good, `"bridge":"host"`, `"bridge":"x"`, 1),
		strings.Replace(good, `"a-web-1.0.0.zip"`, `"../x.zip"`, 1),
		strings.Replace(good, `"version":"1.0.0"`, `"version":"1.0"`, 1),
		strings.Replace(good, `"types":[]`, `"types":["../x"]`, 1),
		strings.Replace(good, `"size":1`, `"size":0`, 1),
		good[:len(good)-1] + `,"extra":1}`,
	} {
		if _, err := ParseCatalog([]byte(`{"apps":[` + bad + `]}`)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if _, err := ParseCatalog([]byte(`{"apps":[` + good + `,` + good + `]}`)); err == nil {
		t.Error("accepted a repeated id")
	}
}

func TestParseRelease(t *testing.T) {
	sum := strings.Repeat("b", 64)
	manifest := `{"version":"0.63.0","tag":"v0.63.0","webapps":[{"id":"wordcraft","version":"0.16.0","asset":"titanos-webapp-wordcraft-0.16.0.zip","sha256":"` + sum + `","size":10}]}`
	entries, err := ParseRelease([]byte(manifest), "0.63.0")
	if err != nil || len(entries) != 1 || entries[0].ID != "wordcraft" {
		t.Fatal(entries, err)
	}
	if _, err := ParseRelease([]byte(manifest), "0.62.0"); err == nil {
		t.Fatal("accepted the manifest of another release")
	}
	for _, bad := range []string{
		strings.Replace(manifest, "titanos-webapp-wordcraft-0.16.0.zip", "../x.zip", 1),
		strings.Replace(manifest, `"size":10`, `"size":0`, 1),
		strings.Replace(manifest, sum, "zz", 1),
	} {
		if _, err := ParseRelease([]byte(bad), "0.63.0"); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	// Older releases have no entry.
	entries, err = ParseRelease([]byte(`{"version":"0.63.0","tag":"v0.63.0"}`), "0.63.0")
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

type entry struct {
	name string
	body string
	mode os.FileMode
}

func makeZip(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
			if strings.HasSuffix(e.name, "/") {
				mode = os.ModeDir | 0o755
			}
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, e.body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const testIndex = `<!DOCTYPE html><html><head><link rel="icon" href="./app.ico"/>
<script type="module">import init from './app-1.js'; await init();</script></head>
<body><canvas id="wordcraft_canvas"></canvas></body></html>`

func appEntries(prefix string) []entry {
	return []entry{
		{name: prefix},
		{name: prefix + "index.html", body: testIndex},
		{name: prefix + "app-1.js", body: strings.Repeat("export const x = 1;\n", 200)},
		{name: prefix + "app-1_bg.wasm", body: "\x00asm" + strings.Repeat("\x01", 4000)},
		{name: prefix + "app.ico", body: "ico"},
		{name: prefix + ".htaccess", body: "x"},
		{name: prefix + "snippets/"},
		{name: prefix + "snippets/inline0.js", body: "export function f() {}"},
	}
}

func extractTo(t *testing.T, data []byte, id, version string) (string, error) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return dir, Extract(zr, id, version, root)
}

func TestExtract(t *testing.T) {
	dir, err := extractTo(t, makeZip(t, appEntries("wordcraft-web-0.16.0/")), "wordcraft", "0.16.0")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "snippets/inline0.js"))
	if err != nil || string(data) != "export function f() {}" {
		t.Fatal(string(data), err)
	}
	info, _ := os.Stat(filepath.Join(dir, "index.html"))
	if info.Mode().Perm() != 0o644 {
		t.Fatal(info.Mode())
	}
}

func TestExtractRefusesUnsafeArchives(t *testing.T) {
	p := "wordcraft-web-0.16.0/"
	base := appEntries(p)
	cases := map[string][]entry{
		"outside top folder": append(slices.Clone(base), entry{name: "other/x.js", body: "x"}),
		"traversal":          append(slices.Clone(base), entry{name: p + "../evil.js", body: "x"}),
		"dot part":           append(slices.Clone(base), entry{name: p + "a/./b.js", body: "x"}),
		"absolute":           append(slices.Clone(base), entry{name: "/" + p + "x.js", body: "x"}),
		"backslash":          append(slices.Clone(base), entry{name: p + `a\..\b.js`, body: "x"}),
		"symlink":            append(slices.Clone(base), entry{name: p + "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}),
		"device":             append(slices.Clone(base), entry{name: p + "dev", body: "", mode: os.ModeDevice | 0o644}),
		"duplicate":          append(slices.Clone(base), entry{name: p + "app-1.js", body: "again"}),
		"duplicate case":     append(slices.Clone(base), entry{name: p + "APP-1.js", body: "again"}),
		"no index":           {{name: p}, {name: p + "app.js", body: "x"}},
		"wrong version":      appEntries("wordcraft-web-0.15.0/"),
	}
	for name, entries := range cases {
		dir, err := extractTo(t, makeZip(t, entries), "wordcraft", "0.16.0")
		if !errors.Is(err, ErrArchive) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "evil.js")); err == nil {
			t.Errorf("%s: wrote outside", name)
		}
	}
}

func TestExtractLimits(t *testing.T) {
	p := "wordcraft-web-0.16.0/"
	// A bomb: highly compressible data over the limit.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(p + "index.html")
	io.WriteString(w, "x")
	w, _ = zw.Create(p + "big.bin")
	chunk := make([]byte, 1<<20)
	for i := 0; i < MaxUnpacked>>20+1; i++ {
		w.Write(chunk)
	}
	zw.Close()
	if _, err := extractTo(t, buf.Bytes(), "wordcraft", "0.16.0"); !errors.Is(err, ErrArchive) {
		t.Fatalf("bomb: %v", err)
	}
	// Too many files.
	entries := []entry{{name: p + "index.html", body: "x"}}
	for i := 0; i <= MaxFiles; i++ {
		entries = append(entries, entry{name: p + "f" + strings.Repeat("x", i%7) + "/" + string(rune('a'+i%26)) + "-" + hex.EncodeToString([]byte{byte(i >> 8), byte(i)}), body: ""})
	}
	if _, err := extractTo(t, makeZip(t, entries), "wordcraft", "0.16.0"); !errors.Is(err, ErrArchive) {
		t.Fatalf("files: %v", err)
	}
}

func TestRewriteIndex(t *testing.T) {
	word, _ := Lookup("wordcraft")
	out := string(RewriteIndex([]byte(testIndex), word))
	if !strings.Contains(out, `<canvas data-host="page" id="wordcraft_canvas">`) {
		t.Fatal(out)
	}
	host := `<script src="/webapps/_host/host.js" data-app="wordcraft" data-bridge="host"></script><script type="module">`
	if !strings.Contains(out, host) {
		t.Fatal(out)
	}
	// Already set: not twice. Adapter apps get no data-host.
	if again := string(RewriteIndex([]byte(`<canvas data-host="page"></canvas>`), word)); strings.Count(again, "data-host") != 1 {
		t.Fatal(again)
	}
	photo, _ := Lookup("vectorcraft")
	if out := string(RewriteIndex([]byte(testIndex), photo)); strings.Contains(out, "data-host") || !strings.Contains(out, `data-bridge="adapter"`) {
		t.Fatal(out)
	}
	// Script in the body (LightCraft, EffectCraft): the mediator goes into <head>.
	pdf, _ := Lookup("pdfcraft")
	body := `<html><head><title>x</title></head><body><canvas id="pdfcraft"></canvas><script type="module">x</script></body></html>`
	out = string(RewriteIndex([]byte(body), pdf))
	if !strings.Contains(out, `<script src="/webapps/_host/host.js" data-app="pdfcraft" data-bridge="host"></script></head>`) || !strings.Contains(out, `<canvas data-host="page" id="pdfcraft">`) {
		t.Fatal(out)
	}
}

// fakeRelease serves one manifest and archives from memory.
type fakeRelease struct {
	mu        sync.Mutex
	manifest  []byte
	assets    map[string][]byte
	downloads int
}

func (f *fakeRelease) ReleaseManifest(ctx context.Context, version string) ([]byte, error) {
	return f.manifest, nil
}

func (f *fakeRelease) DownloadReleaseAsset(ctx context.Context, version, asset, dest, sum string, size int64) error {
	f.mu.Lock()
	f.downloads++
	data, ok := f.assets[asset]
	f.mu.Unlock()
	if !ok {
		return errors.New("404")
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != sum || int64(len(data)) != size {
		return errors.New("checksum")
	}
	return os.WriteFile(dest, data, 0o600)
}

type harness struct {
	m       *Manager
	mounts  string
	store   *config.Store
	release *fakeRelease
	mounted []string
	notes   []string
}

func newHarness(t *testing.T, apps map[string][]byte) *harness {
	t.Helper()
	old := checkSpace
	checkSpace = func(string, int64) error { return nil }
	t.Cleanup(func() { checkSpace = old })
	h := &harness{mounts: t.TempDir(), store: config.Open(t.TempDir()), mounted: []string{"data"}}
	os.Mkdir(filepath.Join(h.mounts, "data"), 0o755)
	var list []ReleaseEntry
	assets := map[string][]byte{}
	for id, data := range apps {
		app, _ := Lookup(id)
		sum := sha256.Sum256(data)
		list = append(list, ReleaseEntry{ID: id, Version: app.Version, Asset: app.Asset(), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
		assets[app.Asset()] = data
	}
	manifest, _ := json.Marshal(map[string]any{"version": "0.63.0", "tag": "v0.63.0", "webapps": list})
	h.release = &fakeRelease{manifest: manifest, assets: assets}
	h.m = New(h.store)
	h.m.Release, h.m.Version, h.m.MountRoot = h.release, "0.63.0", h.mounts
	h.m.Mounted = func() []string { return slices.Clone(h.mounted) }
	h.m.Notify = func(kind, subject string) { h.notes = append(h.notes, kind+":"+subject) }
	if err := h.m.Load(); err != nil {
		t.Fatal(err)
	}
	return h
}

func appZip(t *testing.T, id string) []byte {
	app, _ := Lookup(id)
	return makeZip(t, appEntries(id+"-web-"+app.Version+"/"))
}

func (h *harness) wait(t *testing.T, id string) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range h.m.List() {
			if st.ID == id && st.State != "downloading" {
				return st
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("install did not finish")
	return Status{}
}

func TestInstallServeUninstall(t *testing.T) {
	h := newHarness(t, map[string][]byte{"wordcraft": appZip(t, "wordcraft")})
	if err := h.m.Install("wordcraft"); err != nil {
		t.Fatal(err)
	}
	st := h.wait(t, "wordcraft")
	if st.State != "ready" || !st.Installed || st.URL != "/webapps/wordcraft/0.16.0/" || st.Icon != "/webapps/wordcraft/0.16.0/app.ico" {
		t.Fatalf("%+v", st)
	}
	private := filepath.Join(h.mounts, "data/.titanos/webapps")
	if info, err := os.Stat(private); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal(info, err)
	}
	if _, err := os.Stat(filepath.Join(private, "wordcraft/0.16.0/app-1_bg.wasm.gz")); err != nil {
		t.Fatal("no gzip copy", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(private, "downloads")); len(entries) != 0 {
		t.Fatal("archive kept", entries)
	}
	var saved fileState
	if err := h.store.ReadJSON(configFile, &saved); err != nil || len(saved.Installed) != 1 || saved.Installed[0].Volume != "data" {
		t.Fatal(saved, err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /webapps/{id}/{version}/{path...}", h.m)
	get := func(path string, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/webapps/wordcraft/0.16.0/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `data-host="page"`) || !strings.Contains(rec.Body.String(), HostScript) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	hd := rec.Header()
	if hd.Get("Content-Security-Policy") != sandboxPolicy || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("Cross-Origin-Resource-Policy") != "cross-origin" ||
		hd.Get("Cache-Control") != "no-cache" || hd.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal(hd)
	}
	rec = get("/webapps/wordcraft/0.16.0/app-1_bg.wasm")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/wasm" || rec.Header().Get("Cache-Control") != cacheForever || rec.Header().Get("Content-Encoding") != "" {
		t.Fatal(rec.Code, rec.Header())
	}
	rec = get("/webapps/wordcraft/0.16.0/app-1_bg.wasm", "Accept-Encoding", "gzip, deflate")
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal(rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := io.ReadAll(zr); !strings.HasPrefix(string(data), "\x00asm") {
		t.Fatal("gzip body")
	}
	if rec := get("/webapps/wordcraft/0.16.0/app-1_bg.wasm", "Accept-Encoding", "gzip;q=0"); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("q=0 ignored")
	}
	if rec := get("/webapps/wordcraft/0.16.0/app-1.js"); rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatal(rec.Header())
	}
	for _, path := range []string{"/webapps/wordcraft/0.16.0/.htaccess", "/webapps/wordcraft/0.15.0/index.html",
		"/webapps/gridcraft/0.17.0/index.html", "/webapps/wordcraft/0.16.0/snippets/", "/webapps/wordcraft/0.16.0/snippets",
		"/webapps/wordcraft/0.16.0/index.html.gz", "/webapps/_host/0.16.0/x", "/webapps/wordcraft/0.16.0/missing.js"} {
		if rec := get(path); rec.Code != 404 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	req := httptest.NewRequest("POST", "/webapps/wordcraft/0.16.0/index.html", nil)
	rec = httptest.NewRecorder()
	h.m.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}

	// Without the volume: listed as an error, not served.
	h.mounted = nil
	if st := status(h.m, "wordcraft"); st.State != "error" || st.Error != ErrCodeVolumeMissing {
		t.Fatalf("%+v", st)
	}
	if rec := get("/webapps/wordcraft/0.16.0/index.html"); rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	h.mounted = []string{"data"}

	if err := h.m.Uninstall("wordcraft"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(private, "wordcraft")); !os.IsNotExist(err) {
		t.Fatal("files kept", err)
	}
	if err := h.m.Uninstall("wordcraft"); !errors.Is(err, ErrNoApp) {
		t.Fatal(err)
	}
	if err := h.m.Install("nope"); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
}

func TestUpdateRemovesOldVersion(t *testing.T) {
	h := newHarness(t, map[string][]byte{"wordcraft": appZip(t, "wordcraft")})
	private := filepath.Join(h.mounts, "data/.titanos/webapps/wordcraft")
	os.MkdirAll(filepath.Join(private, "0.15.0"), 0o755)
	os.WriteFile(filepath.Join(private, "0.15.0/index.html"), []byte("old"), 0o644)
	h.store.WriteJSON(configFile, fileState{Installed: []Installed{{ID: "wordcraft", Version: "0.15.0", Volume: "data"}}})
	h.m.Load()
	h.m.Install("wordcraft")
	if st := h.wait(t, "wordcraft"); st.Version != "0.16.0" || st.State != "ready" {
		t.Fatalf("%+v", st)
	}
	entries, _ := os.ReadDir(private)
	if len(entries) != 1 || entries[0].Name() != "0.16.0" {
		t.Fatal(entries)
	}
}

func TestInstallFailures(t *testing.T) {
	bad := makeZip(t, []entry{{name: "wordcraft-web-0.16.0/index.html", body: "x"}, {name: "wordcraft-web-0.16.0/../x", body: "x"}})
	h := newHarness(t, map[string][]byte{"wordcraft": bad})
	h.m.Install("wordcraft")
	if st := h.wait(t, "wordcraft"); st.State != "error" || st.Error != ErrCodeArchive || st.Installed {
		t.Fatalf("%+v", st)
	}
	if entries, _ := os.ReadDir(filepath.Join(h.mounts, "data/.titanos/webapps/wordcraft")); len(entries) != 0 {
		t.Fatal("staging kept", entries)
	}
	// Not in the release.
	h.m.Install("gridcraft")
	if st := h.wait(t, "gridcraft"); st.Error != ErrCodeNotInRelease {
		t.Fatalf("%+v", st)
	}
	// No volume.
	h.mounted = nil
	h.m.Install("pdfcraft")
	if st := h.wait(t, "pdfcraft"); st.Error != ErrCodeNoVolume {
		t.Fatalf("%+v", st)
	}
}

func TestDefaultsWaitForVolume(t *testing.T) {
	h := newHarness(t, map[string][]byte{"t-word": appZip(t, "t-word"), "t-exel": appZip(t, "t-exel"), "t-pdf": appZip(t, "t-pdf")})
	h.mounted = nil
	if err := h.m.InstallDefaults(nil); err != nil {
		t.Fatal(err)
	}
	var saved fileState
	h.store.ReadJSON(configFile, &saved)
	if len(saved.Pending) != 3 || !saved.Offered {
		t.Fatalf("%+v", saved)
	}
	for _, st := range h.m.List() {
		if st.Default != st.Pending {
			t.Fatalf("%+v", st)
		}
	}
	h.mounted = []string{"data"}
	h.m.startPending()
	for _, id := range []string{"t-word", "t-exel", "t-pdf"} {
		if st := h.wait(t, id); st.State != "ready" {
			t.Fatalf("%+v", st)
		}
	}
	saved = fileState{}
	h.store.ReadJSON(configFile, &saved)
	if len(saved.Pending) != 0 || len(saved.Installed) != 3 {
		t.Fatalf("%+v", saved)
	}
	if err := h.m.InstallDefaults([]string{"nope"}); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
}

func TestOfferOnce(t *testing.T) {
	h := newHarness(t, nil)
	ready := false
	h.m.Ready = func() bool { return ready }
	h.m.maybeOffer()
	if len(h.notes) != 0 {
		t.Fatal("offered before setup")
	}
	ready = true
	h.m.maybeOffer()
	h.m.maybeOffer()
	if !slices.Equal(h.notes, []string{"webapps_offer:"}) {
		t.Fatal(h.notes)
	}
	// After a restart: not again.
	h2 := New(h.store)
	h2.Notify = func(kind, subject string) { h.notes = append(h.notes, kind) }
	h2.Load()
	h2.maybeOffer()
	if len(h.notes) != 1 {
		t.Fatal(h.notes)
	}
	// An NAS that answered at setup (empty choice) gets no notice.
	h3 := newHarness(t, nil)
	h3.m.InstallDefaults([]string{})
	h3.m.maybeOffer()
	if len(h3.notes) != 0 {
		t.Fatal(h3.notes)
	}
}

func TestDevMode(t *testing.T) {
	dir := t.TempDir()
	app, _ := Lookup("wordcraft")
	data := appZip(t, "wordcraft")
	os.WriteFile(filepath.Join(dir, app.UpstreamAsset), data, 0o644)
	m := New(config.Open(t.TempDir()))
	m.Dev, m.DevDir, m.Version = true, dir, "0.63.0"
	m.Load()
	m.Install("wordcraft")
	deadline := time.Now().Add(5 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		st = status(m, "wordcraft")
		if st.State != "downloading" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The test archive is not the catalog's: refused by its SHA-256.
	if st.State != "error" || st.Error != ErrCodeDownload {
		t.Fatalf("%+v", st)
	}
	m.cleanupDev()
}

func TestDetectDocker(t *testing.T) {
	containers := []Container{
		{App: "word", Name: "word-wordcraft-1", Image: "ghcr.io/storytold/wordcraft:0.15.0", State: "running"},
		{App: "grid", Name: "grid-1", Image: "storytold/gridcraft-web@sha256:abc", State: "exited"},
		{App: "pdfcraft", Name: "pdfcraft", Image: "nginx:alpine", State: "running"},
		{App: "photo", Name: "photo-1", Image: "registry.example.com:5000/me/vectorcraft:latest", State: "running"},
		// Not Craft apps.
		{App: "nextcloud", Name: "nextcloud-app-1", Image: "nextcloud:29", State: "running"},
		{App: "fig", Name: "fig-1", Image: "storytold/figcraft-server:1", State: "running"},
		{App: "mixed", Name: "mixed-wordcraft-1", Image: "storytold/wordcraft", State: "running"},
		{App: "mixed", Name: "mixed-db-1", Image: "postgres:16", State: "running"},
		{App: "deckdb", Name: "deckcraft", Image: "postgres:16", State: "running"},
		{App: "two", Name: "a", Image: "storytold/wordcraft", State: "running"},
		{App: "two", Name: "b", Image: "storytold/pdfcraft", State: "running"},
		{App: "lone", Name: "x", Image: "wordcraftish/thing", State: "running"},
		// Not managed by TitanOS: never offered.
		{App: "", Name: "wordcraft", Image: "storytold/wordcraft", State: "running"},
	}
	managed := []string{"word", "grid", "pdfcraft", "photo", "nextcloud", "fig", "mixed", "deckdb", "two", "lone", "cadcraft-web", "empty"}
	got := DetectDocker(managed, containers)
	want := []DockerApp{
		// PLAN 75-20: our own builds replace the originals.
		{App: "word", Webapp: "t-word", Running: true, Image: "ghcr.io/storytold/wordcraft:0.15.0"},
		{App: "grid", Webapp: "t-exel", Image: "storytold/gridcraft-web@sha256:abc"},
		{App: "pdfcraft", Webapp: "t-pdf", Running: true, Image: "nginx:alpine"},
		{App: "photo", Webapp: "t-vector", Running: true, Image: "registry.example.com:5000/me/vectorcraft:latest"},
		{App: "cadcraft-web", Webapp: "t-cad"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestNewCatalogVersionIsInstalled(t *testing.T) {
	h := newHarness(t, map[string][]byte{"wordcraft": appZip(t, "wordcraft")})
	h.store.WriteJSON(configFile, fileState{Installed: []Installed{{ID: "wordcraft", Version: "0.15.0", Volume: "data"}}})
	h.m.Load()
	h.m.startPending()
	if st := h.wait(t, "wordcraft"); st.Version != "0.16.0" || st.State != "ready" {
		t.Fatalf("%+v", st)
	}
	// Up to date: nothing more is fetched.
	before := h.release.downloads
	h.m.startPending()
	time.Sleep(50 * time.Millisecond)
	if h.release.downloads != before {
		t.Fatal("downloaded again")
	}
}

// status is the listed state of one app.
func status(m *Manager, id string) Status {
	for _, st := range m.List() {
		if st.ID == id {
			return st
		}
	}
	return Status{}
}

// PLAN 75-20: our own builds (trusted) are served without the sandbox, on the
// web UI's origin; the Craft originals keep it.
func TestTrustedAppWithoutSandbox(t *testing.T) {
	h := newHarness(t, map[string][]byte{"t-pdf": appZip(t, "t-pdf"), "pdfcraft": appZip(t, "pdfcraft")})
	for _, id := range []string{"t-pdf", "pdfcraft"} {
		if err := h.m.Install(id); err != nil {
			t.Fatal(err)
		}
		if st := h.wait(t, id); st.State != "ready" {
			t.Fatalf("%+v", st)
		}
	}
	mux := http.NewServeMux()
	mux.Handle("GET /webapps/{id}/{version}/{path...}", h.m)
	get := func(path string) http.Header {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		return rec.Header()
	}
	mine := get("/webapps/t-pdf/1.1.0/index.html")
	if mine.Get("Content-Security-Policy") != trustedPolicy || mine.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("trusted: %v", mine)
	}
	if st := status(h.m, "t-pdf"); !st.Trusted {
		t.Fatalf("%+v", st)
	}
	theirs := get("/webapps/pdfcraft/0.19.0/index.html")
	if theirs.Get("Content-Security-Policy") != sandboxPolicy || theirs.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("original: %v", theirs)
	}
}
