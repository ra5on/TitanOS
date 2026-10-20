package backuptarget

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func newTarget(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "mnt/tank"), 0o755)
	m := NewManager(config.Open(filepath.Join(root, "config")))
	m.mountRoot = filepath.Join(root, "mnt")
	m.Addresses = func() []string {
		return []string{"fe80::1", "192.168.1.5", "100.101.102.103", "100.40.0.1", "fd7a:115c:a1e0::1"}
	}
	m.Host = func() string { return "nas2.tail1234.ts.net" }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.Configure(true, "tank", []string{"tank"}); err != nil {
		t.Fatal(err)
	}
	return m, root
}

type code struct {
	URL, User, Password string
}

func decode(t *testing.T, text string) code {
	t.Helper()
	if !strings.HasPrefix(text, CodePrefix) {
		t.Fatalf("code: %q", text)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(text, CodePrefix))
	if err != nil {
		t.Fatal(err)
	}
	var c code
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func do(t *testing.T, c code, method, path, body string, headers ...string) (*http.Response, string) {
	t.Helper()
	return doAs(t, c.User, c.Password, method, path, body, headers...)
}

var serverURL string

func doAs(t *testing.T, user, password, method, path, body string, headers ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, serverURL+path, strings.NewReader(body))
	req.SetBasicAuth(user, password)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

const blob = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPairingAndRepository(t *testing.T) {
	m, root := newTarget(t)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	serverURL = srv.URL

	text, err := m.Pair("Backup")
	if err != nil {
		t.Fatal(err)
	}
	c := decode(t, text)
	if c.URL != "http://nas2.tail1234.ts.net:8750/Backup" || c.User != "Backup" || len(c.Password) != 64 {
		t.Fatalf("code = %+v", c)
	}
	if _, err := m.Pair("backup"); !errors.Is(err, ErrExists) {
		t.Errorf("same name twice: %v", err)
	}
	if _, err := m.Pair("Böse Name"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad name: %v", err)
	}
	// The token is kept as a hash only.
	data, _ := os.ReadFile(filepath.Join(root, "config/backup/target.json"))
	if strings.Contains(string(data), c.Password) {
		t.Error("the token is stored in the clear")
	}

	// The way restic uses it.
	if res, _ := do(t, c, "HEAD", "/Backup/config", ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("config before init: %d", res.StatusCode)
	}
	if res, _ := do(t, c, "POST", "/Backup/?create=true", ""); res.StatusCode != http.StatusOK {
		t.Errorf("create: %d", res.StatusCode)
	}
	if res, _ := do(t, c, "POST", "/Backup/config", "the config"); res.StatusCode != http.StatusOK {
		t.Errorf("save config: %d", res.StatusCode)
	}
	if res, _ := do(t, c, "POST", "/Backup/config", "again"); res.StatusCode != http.StatusForbidden {
		t.Errorf("config overwritten: %d", res.StatusCode)
	}
	if res, body := do(t, c, "GET", "/Backup/config", ""); res.StatusCode != http.StatusOK || body != "the config" {
		t.Errorf("config: %d %q", res.StatusCode, body)
	}
	if res, _ := do(t, c, "POST", "/Backup/data/"+blob, "0123456789"); res.StatusCode != http.StatusOK {
		t.Errorf("save blob: %d", res.StatusCode)
	}
	if res, _ := do(t, c, "POST", "/Backup/data/"+blob, "other"); res.StatusCode != http.StatusForbidden {
		t.Errorf("blob overwritten: %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "mnt/tank/.titanos/backup-target/Backup/data/01", blob)); err != nil {
		t.Errorf("blob is not where restic's layout puts it: %v", err)
	}
	if res, body := do(t, c, "GET", "/Backup/data/"+blob, "", "Range", "bytes=2-4"); res.StatusCode != http.StatusPartialContent || body != "234" {
		t.Errorf("range: %d %q", res.StatusCode, body)
	}
	if res, body := do(t, c, "GET", "/Backup/data/", "", "Accept", "application/vnd.x.restic.rest.v2"); res.StatusCode != http.StatusOK || !strings.Contains(body, `"size":10`) || !strings.Contains(body, blob) {
		t.Errorf("list v2: %d %q", res.StatusCode, body)
	}
	if _, body := do(t, c, "GET", "/Backup/data/", ""); strings.TrimSpace(body) != `["`+blob+`"]` {
		t.Errorf("list v1: %q", body)
	}
	if res, _ := do(t, c, "DELETE", "/Backup/data/"+blob, ""); res.StatusCode != http.StatusOK {
		t.Errorf("delete blob: %d", res.StatusCode)
	}
	if res, _ := do(t, c, "DELETE", "/Backup/config", ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("config deleted: %d", res.StatusCode)
	}
	// Nothing else goes through, and nothing leaves the folder.
	for _, path := range []string{"/Backup/data/../../x", "/Backup/other/" + blob, "/Backup/keys/not-a-hash", "/Backup/data/..%2f..%2fconfig"} {
		if res, _ := do(t, c, "GET", path, ""); res.StatusCode == http.StatusOK {
			t.Errorf("GET %s: %d", path, res.StatusCode)
		}
	}

	view := m.View()
	if len(view.Sources) != 1 || view.Sources[0].Name != "Backup" || view.Sources[0].Bytes == 0 || view.Sources[0].Last == nil {
		t.Errorf("view = %+v", view)
	}
}

func TestAccessIsLimited(t *testing.T) {
	m, _ := newTarget(t)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	serverURL = srv.URL
	a := decode(t, must(t, m, "alpha"))
	b := decode(t, must(t, m, "beta"))
	for name, tc := range map[string]struct{ user, pass, path string }{
		"wrong token":      {"alpha", "x", "/alpha/config"},
		"someone else":     {"beta", b.Password, "/alpha/config"},
		"token of another": {"alpha", b.Password, "/alpha/config"},
		"no such source":   {"gamma", a.Password, "/gamma/config"},
		"empty":            {"", "", "/alpha/config"},
	} {
		if res, _ := doAs(t, tc.user, tc.pass, "GET", tc.path, ""); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, res.StatusCode)
		}
	}
	// Without credentials at all.
	res, _ := http.Get(srv.URL + "/alpha/config")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", res.StatusCode)
	}
	// Switched off, the right token is no use either.
	m.Configure(false, "", nil)
	if res, _ := do(t, a, "GET", "/alpha/config", ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("switched off: %d", res.StatusCode)
	}
	m.Configure(true, "tank", []string{"tank"})
	if err := m.Remove("alpha", true); err != nil {
		t.Fatal(err)
	}
	if res, _ := do(t, a, "GET", "/alpha/config", ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("removed source: %d", res.StatusCode)
	}
	if err := m.Remove("alpha", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v", err)
	}
}

func must(t *testing.T, m *Manager, name string) string {
	t.Helper()
	text, err := m.Pair(name)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestOnlyTailscaleAddresses(t *testing.T) {
	m, _ := newTarget(t)
	got := strings.Join(m.addresses(), ",")
	if got != "100.101.102.103,fd7a:115c:a1e0::1" {
		t.Errorf("addresses = %s", got)
	}
}

func TestNeedsTailscaleAndVolume(t *testing.T) {
	m, _ := newTarget(t)
	m.Host = func() string { return "" }
	m.Addresses = func() []string { return nil }
	if _, err := m.Pair("keller"); !errors.Is(err, ErrNoNetwork) {
		t.Errorf("no tailscale: %v", err)
	}
	if err := m.Configure(true, "gone", []string{"tank"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unmounted volume: %v", err)
	}
	m.Mounted = func() []string { return []string{"tank"} }
	m.reconcile()
	if m.View().Problem != "tailscale" {
		t.Errorf("problem = %q", m.View().Problem)
	}
	m.Mounted = func() []string { return nil }
	m.reconcile()
	if m.View().Problem != "volume" {
		t.Errorf("problem = %q", m.View().Problem)
	}
}
