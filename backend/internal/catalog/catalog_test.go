package catalog

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const plain = "services:\n  web:\n    image: traefik/whoami\n    ports:\n      - \"8080:80\"\n"

const withMeta = `x-titanos:
  name: Beispiel
  category: Media
  description:
    de: Ein Beispiel.
    en: An example.
services:
  eins:
    image: example/one:1
  zwei:
    image: example/two:2
`

func tarball(files map[string]string) []byte {
	var buf bytes.Buffer
	zipped := gzip.NewWriter(&buf)
	archive := tar.NewWriter(zipped)
	for name, content := range files {
		archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
		archive.Write([]byte(content))
	}
	archive.Close()
	zipped.Close()
	return buf.Bytes()
}

func zipFile(files map[string]string) []byte {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := archive.Create(name)
		w.Write([]byte(content))
	}
	archive.Close()
	return buf.Bytes()
}

// officialServer serves a rebuilt archive of the official repository.
func officialServer(t *testing.T, files map[string]string) (*httptest.Server, *int) {
	t.Helper()
	fetched := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ra5on/TitanOS-Apps/archive/HEAD.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"o1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		fetched++
		w.Header().Set("ETag", `"o1"`)
		w.Write(tarball(files))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &fetched
}

func TestOfficialCatalog(t *testing.T) {
	srv, fetched := officialServer(t, map[string]string{
		"TitanOS-Apps-abc/beispiel/compose.yaml": withMeta,
		"TitanOS-Apps-abc/immich/LICENSE":        "AGPL",
		"TitanOS-Apps-abc/README.md":             "hi",
	})
	m := NewManager(config.Open(t.TempDir()))
	m.SetOfficialURL(srv.URL + "/ra5on/TitanOS-Apps")
	m.Load()

	// Before the first fetch the store is empty, without an error.
	sources, apps := m.Status()
	if len(sources) != 1 || !sources[0].Builtin || sources[0].ID != BuiltinID || sources[0].Count != 0 || sources[0].Error != "" || len(apps) != 0 {
		t.Fatalf("before the fetch: %+v, %d apps", sources, len(apps))
	}

	m.Refresh(context.Background())
	sources, apps = m.Status()
	if !sources[0].Builtin || sources[0].Format != "titanos" || sources[0].Count != 1 || sources[0].Updated.IsZero() || len(apps) != 1 {
		t.Fatalf("after the fetch: %+v, %d apps", sources, len(apps))
	}
	app := apps[0]
	if app.Source != BuiltinID || app.Name != "Beispiel" || app.Description["de"] == "" || app.Description["en"] == "" {
		t.Errorf("template: %+v", app)
	}
	if compose, err := m.Compose(BuiltinID, app.ID); err != nil || compose != withMeta {
		t.Errorf("Compose = %q, %v", compose, err)
	}
	if _, err := m.Compose(BuiltinID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown app: %v", err)
	}
	if err := m.Remove(BuiltinID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing the official catalog: %v", err)
	}

	// An unchanged archive is not sent again; a lost connection keeps the apps.
	m.Refresh(context.Background())
	if *fetched != 1 {
		t.Errorf("archive sent %d times", *fetched)
	}
	srv.Close()
	m.Refresh(context.Background())
	sources, apps = m.Status()
	if sources[0].Error != "unreachable" || len(apps) != 1 {
		t.Errorf("unreachable: %+v, %d apps", sources[0], len(apps))
	}
}

func TestOfficialCatalogOffline(t *testing.T) {
	srv, _ := officialServer(t, nil)
	srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	m.SetOfficialURL(srv.URL + "/ra5on/TitanOS-Apps")
	m.Refresh(context.Background())
	sources, apps := m.Status()
	if sources[0].Error != "unreachable" || len(apps) != 0 {
		t.Errorf("offline: %+v, %d apps", sources[0], len(apps))
	}
}

func TestParse(t *testing.T) {
	apps, format, err := parse("src", map[string]string{
		"repo-HEAD/beispiel/compose.yaml":        withMeta,
		"repo-HEAD/Who Am I.yml":                 plain,
		"repo-HEAD/built/docker-compose.yml":     "services:\n  app:\n    build: .\n",
		"repo-HEAD/notes.yaml":                   "just: text\n",
		"repo-HEAD/broken.yaml":                  "services: [unbalanced\n",
		"repo-HEAD/casa/docker-compose.yml":      "x-casaos:\n  title: {en_us: X}\n" + plain,
		"repo-HEAD/beispiel/docker-compose.yaml": plain, // same app again
	})
	if err != nil || format != "titanos" || len(apps) != 3 {
		t.Fatalf("parse = %+v, %q, %v", apps, format, err)
	}
	if casa := apps[2]; casa.ID != "casa" || casa.Name != "X" {
		t.Errorf("CasaOS template: %+v", casa)
	}
	first, second := apps[0], apps[1]
	if first.ID != "who-am-i" || first.Name != "who-am-i" || first.Image != "traefik/whoami" || first.Services != 1 {
		t.Errorf("plain compose file: %+v", first)
	}
	if second.ID != "beispiel" || second.Name != "Beispiel" || second.Category != "media" || second.Description["en"] != "An example." || second.Image != "example/one:1" || second.Services != 2 {
		t.Errorf("template with metadata: %+v", second)
	}

	if _, _, err := parse("src", map[string]string{"readme.yaml": "a: b\n"}); !errors.Is(err, ErrEmpty) {
		t.Errorf("no compose file: %v", err)
	}
}

const casa = `name: big-bear-example
services:
  app:
    image: example/app:1
    volumes:
      - /DATA/AppData/$AppID/data:/data
    x-casaos:
      envs: []
x-casaos:
  architectures: [amd64, arm64]
  main: app
  category: Networking
  description:
    en_US: |
      A long text
      over several lines.
  tagline:
    en_US: Short and good
    de_DE: Kurz und gut
    zh_CN: 12345
  title:
    en_US: Example
    custom: ~
  port_map: "8000"
`

func TestParseCasaOS(t *testing.T) {
	apps, format, err := parse("src", map[string]string{
		"store-HEAD/Apps/Example/docker-compose.yml": casa,
		"store-HEAD/Apps/ArmOnly/docker-compose.yml": strings.Replace(casa, "[amd64, arm64]", "[arm64]", 1),
		"store-HEAD/Apps/Loose/docker-compose.yml":   "services:\n  a:\n    image: x/y\nx-casaos:\n  title: Plain\n  category: [not, a, string]\n  description: {en_US: Only a description}\n",
	})
	if err != nil || format != "casaos" || len(apps) != 2 {
		t.Fatalf("parse = %+v, %q, %v", apps, format, err)
	}
	example, loose := apps[0], apps[1]
	if example.ID != "example" || example.Name != "Example" || example.Category != "network" || example.Image != "example/app:1" ||
		len(example.Description) != 2 || example.Description["de"] != "Kurz und gut" || example.Description["en"] != "Short and good" {
		t.Errorf("CasaOS template: %+v", example)
	}
	if loose.ID != "loose" || loose.Name != "Plain" || loose.Category != "" || loose.Description["en"] != "Only a description" {
		t.Errorf("loosely written template: %+v", loose)
	}
}

func TestMissingDevices(t *testing.T) {
	compose := "services:\n  a:\n    image: x\n    devices:\n      - /dev/null:/dev/null\n      - /dev/titanos-not-there:/dev/x\n      - /dev/titanos-not-there\n      - driver: nvidia\n"
	if got := MissingDevices(compose); len(got) != 1 || got[0] != "/dev/titanos-not-there" {
		t.Errorf("MissingDevices = %v", got)
	}
	if got := MissingDevices("services: [broken"); len(got) != 0 {
		t.Errorf("broken file: %v", got)
	}
}

func TestSources(t *testing.T) {
	repo := map[string]string{"apps-HEAD/whoami/compose.yaml": plain, "apps-HEAD/.github/workflows/ci.yaml": plain, "apps-HEAD/README.md": "hi"}
	mux := http.NewServeMux()
	full := 0
	mux.HandleFunc("GET /forge/apps/archive/HEAD.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		full++
		w.Header().Set("ETag", `"v1"`)
		w.Write(tarball(repo))
	})
	mux.HandleFunc("GET /lab/apps/-/archive/HEAD/archive.tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(tarball(repo)) })
	mux.HandleFunc("GET /files/apps.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(zipFile(repo)) })
	mux.HandleFunc("GET /raw/einzeln/compose.yaml", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(withMeta)) })
	mux.HandleFunc("GET /page", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	store := config.Open(t.TempDir())
	m := NewManager(store)
	m.SetOfficialURL(srv.URL + "/official-unavailable")
	ctx := context.Background()
	if err := m.Add(ctx, "", srv.URL+"/forge/apps"); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("before Load: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}

	for address, want := range map[string]error{
		"ftp://example.org/apps":    ErrInvalidURL,
		"https://user:pw@host/apps": ErrInvalidURL,
		"not a url":                 ErrInvalidURL,
		srv.URL + "/missing":        ErrUnreachable,
		srv.URL + "/page":           ErrUnreachable,
	} {
		if err := m.Add(ctx, "", address); !errors.Is(err, want) {
			t.Errorf("Add(%q) = %v, want %v", address, err, want)
		}
	}
	for _, address := range []string{"/forge/apps", "/lab/apps.git", "/files/apps.zip", "/raw/einzeln/compose.yaml"} {
		if err := m.Add(ctx, "", srv.URL+address); err != nil {
			t.Fatalf("Add(%s): %v", address, err)
		}
	}
	if err := m.Add(ctx, "again", srv.URL+"/forge/apps"); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	sources, apps := m.Status()
	if len(sources) != 5 || len(apps) != 4 {
		t.Fatalf("%d sources, %d apps", len(sources), len(apps))
	}
	single := sources[4]
	if single.Count != 1 || single.Format != "titanos" || single.Updated.IsZero() || !strings.HasSuffix(single.Name, "/raw/einzeln/compose.yaml") {
		t.Errorf("single file source: %+v", single)
	}
	if compose, err := m.Compose(single.ID, "einzeln"); err != nil || compose != withMeta {
		t.Errorf("Compose = %q, %v", compose, err)
	}
	// The hidden folder of the repository is not an app.
	if sources[1].Count != 1 || sources[1].Format != "compose" {
		t.Errorf("repository source: %+v", sources[1])
	}

	// Sources survive a restart; their apps are fetched again.
	again := NewManager(store)
	again.SetOfficialURL(srv.URL + "/missing")
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if sources, apps := again.Status(); len(sources) != 5 || len(apps) != 0 {
		t.Fatalf("after a restart: %d sources, %d apps", len(sources), len(apps))
	}
	again.Refresh(ctx)
	if _, apps := again.Status(); len(apps) != 4 {
		t.Errorf("after the refresh: %d apps", len(apps))
	}
	// A source that did not change is not sent again.
	before := full
	again.Refresh(ctx)
	if sources, apps := again.Status(); full != before || len(apps) != 4 || sources[1].Error != "" || sources[1].Count != 1 {
		t.Errorf("unchanged source: fetched %d times more, %+v", full-before, sources[1])
	}

	// A source that stops answering keeps what was read.
	srv.Close()
	again.Refresh(ctx)
	sources, apps = again.Status()
	if len(apps) != 4 || sources[1].Error != "unreachable" || sources[1].Count != 1 {
		t.Errorf("unreachable source: %+v, %d apps", sources[1], len(apps))
	}

	if err := again.Remove(sources[1].ID); err != nil {
		t.Fatal(err)
	}
	if sources, apps := again.Status(); len(sources) != 4 || len(apps) != 3 {
		t.Errorf("after removing: %d sources, %d apps", len(sources), len(apps))
	}
}
