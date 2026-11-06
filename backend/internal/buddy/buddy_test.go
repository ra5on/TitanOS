package buddy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const blobA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const blobB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type fixture struct {
	s     *Server
	url   string
	mount string
	now   time.Time
	token string
}

func newFixture(t *testing.T, quota int64) *fixture {
	t.Helper()
	mount := t.TempDir()
	if err := os.Mkdir(filepath.Join(mount, "tank"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fixture{mount: mount, now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), token: strings.Repeat("t", 64)}
	s := NewServer()
	s.MountRoot = mount
	s.Now = func() time.Time { return f.now }
	s.Volume = func() string { return "tank" }
	s.Mounted = func() []string { return []string{"tank"} }
	s.Partner = func(id string) (Partner, bool) {
		switch id {
		case "tom":
			return Partner{ID: "tom", Hash: HashToken(f.token), Quota: quota}, true
		case "eva":
			return Partner{ID: "eva", Hash: HashToken("eva-token"), Quota: quota}, true
		}
		return Partner{}, false
	}
	f.s = s
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *fixture) do(t *testing.T, method, path, user, token string, body []byte, header ...string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, f.url+path, bytes.NewReader(body))
	if body == nil {
		req.Body = nil
	}
	if user != "" {
		req.SetBasicAuth(user, token)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

// The restic REST protocol, as restic sends it: create, config, upload,
// list (v1 and v2), read with a range, and the 404 of a missing config.
func TestProtocol(t *testing.T) {
	f := newFixture(t, 1<<20)
	tom := func(method, path string, body []byte, h ...string) (int, []byte) {
		return f.do(t, method, "/v1/tom"+path, "tom", f.token, body, h...)
	}
	if code, _ := tom("HEAD", "/config", nil); code != http.StatusNotFound {
		t.Fatalf("missing config: %d", code)
	}
	if code, _ := tom("POST", "/?create=true", nil); code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	if code, _ := tom("POST", "/config", []byte("cfg")); code != http.StatusOK {
		t.Fatalf("config: %d", code)
	}
	if code, body := tom("GET", "/config", nil); code != 200 || string(body) != "cfg" {
		t.Fatalf("read config: %d %q", code, body)
	}
	if code, _ := tom("POST", "/data/"+blobA, []byte("hello world")); code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "tank", Dir, "tom", "data", "aa", blobA)); err != nil {
		t.Fatalf("not stored under .titanos/buddy/tom: %v", err)
	}
	if code, body := tom("GET", "/data/"+blobA, nil, "Range", "bytes=6-10"); code != http.StatusPartialContent || string(body) != "world" {
		t.Fatalf("range: %d %q", code, body)
	}
	if code, body := tom("GET", "/data/", nil); code != 200 || !strings.Contains(string(body), blobA) {
		t.Fatalf("list v1: %d %s", code, body)
	}
	code, body := tom("GET", "/data/", nil, "Accept", "application/vnd.x.restic.rest.v2")
	var items []struct {
		Name string
		Size int64
	}
	if json.Unmarshal(body, &items); code != 200 || len(items) != 1 || items[0].Size != 11 {
		t.Fatalf("list v2: %d %s", code, body)
	}
	if n, ok := f.s.Usage("tom"); !ok || n != 14 {
		t.Fatalf("usage %d %v", n, ok)
	}
	if code, _ := tom("GET", "/nothing/", nil); code != http.StatusNotFound {
		t.Fatalf("unknown type: %d", code)
	}
	if code, _ := tom("GET", "/data/../../x", nil); code == 200 {
		t.Fatal("path escaped")
	}
}

// Sign-in: a wrong token, no token, and another partner's folder all get
// the same 401.
func TestAuth(t *testing.T) {
	f := newFixture(t, 1<<20)
	for _, c := range []struct{ path, user, token string }{
		{"/v1/tom/config", "", ""},
		{"/v1/tom/config", "tom", "wrong"},
		{"/v1/tom/config", "eva", "eva-token"},
		{"/v1/eva/config", "tom", f.token},
		{"/v1/nobody/config", "nobody", "x"},
	} {
		if code, _ := f.do(t, "GET", c.path, c.user, c.token, nil); code != http.StatusUnauthorized {
			t.Errorf("%+v: %d", c, code)
		}
	}
	if code, _ := f.do(t, "GET", "/v1/eva/config", "eva", "eva-token", nil); code != http.StatusNotFound {
		t.Fatalf("own folder: %d", code)
	}
	if code, _ := f.do(t, "GET", "/other", "tom", f.token, nil); code != http.StatusNotFound {
		t.Fatalf("outside /v1: %d", code)
	}
}

func TestQuota(t *testing.T) {
	f := newFixture(t, 20)
	tom := func(method, path string, body []byte) int {
		code, _ := f.do(t, method, "/v1/tom"+path, "tom", f.token, body)
		return code
	}
	tom("POST", "/?create=true", nil)
	if code := tom("POST", "/data/"+blobA, bytes.Repeat([]byte("x"), 15)); code != 200 {
		t.Fatalf("within quota: %d", code)
	}
	if code := tom("POST", "/data/"+blobB, bytes.Repeat([]byte("x"), 10)); code != http.StatusInsufficientStorage {
		t.Fatalf("beyond quota: %d", code)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "tank", Dir, "tom", "data", "bb", blobB)); err == nil {
		t.Fatal("a refused upload was kept")
	}
	// Without Content-Length (chunked) the limit holds as well.
	req, _ := http.NewRequest("POST", f.url+"/v1/tom/index/"+blobB, io.MultiReader(strings.NewReader("123456"), strings.NewReader("7890")))
	req.ContentLength = -1
	req.SetBasicAuth("tom", f.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("chunked beyond quota: %v %v", err, res.StatusCode)
	}
	// A lock still goes in, so restic can prune.
	if code := tom("POST", "/locks/"+blobB, []byte("lock")); code != 200 {
		t.Fatalf("lock when full: %d", code)
	}
}

// Append-only: nothing is replaced, config and keys stay, locks go, data,
// index and snapshots go to the quarantine, which is emptied only after
// the protection period and never listed.
func TestAppendOnly(t *testing.T) {
	f := newFixture(t, 1<<20)
	tom := func(method, path string, body []byte) (int, string) {
		code, out := f.do(t, method, "/v1/tom"+path, "tom", f.token, body)
		return code, string(out)
	}
	tom("POST", "/?create=true", nil)
	for _, p := range []string{"/config", "/keys/" + blobA, "/locks/" + blobA, "/data/" + blobA, "/snapshots/" + blobA, "/index/" + blobA} {
		if code, _ := tom("POST", p, []byte("v1")); code != 200 {
			t.Fatalf("upload %s: %d", p, code)
		}
	}
	if code, _ := tom("POST", "/data/"+blobA, []byte("v2")); code != http.StatusForbidden {
		t.Fatalf("overwrite: %d", code)
	}
	for _, p := range []string{"/config", "/keys/" + blobA} {
		if code, _ := tom("DELETE", p, nil); code != http.StatusForbidden {
			t.Errorf("delete %s: %d", p, code)
		}
	}
	for _, p := range []string{"/locks/" + blobA, "/data/" + blobA, "/snapshots/" + blobA, "/index/" + blobA} {
		if code, _ := tom("DELETE", p, nil); code != 200 {
			t.Errorf("delete %s: %d", p, code)
		}
	}
	// Gone for the partner, but kept in quarantine and counted.
	for _, p := range []string{"/data/", "/snapshots/", "/index/"} {
		if _, body := tom("GET", p, nil); strings.Contains(body, blobA) {
			t.Errorf("%s still lists the file: %s", p, body)
		}
	}
	trash := filepath.Join(f.mount, "tank", Dir, "tom", Trash)
	var kept []string
	filepath.Walk(trash, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			kept = append(kept, filepath.Base(p))
		}
		return nil
	})
	if len(kept) != 3 {
		t.Fatalf("quarantine: %v", kept)
	}
	if n, _ := f.s.Usage("tom"); n != 10 {
		t.Fatalf("usage counts the quarantine: %d", n)
	}
	// A day later it stays; after the protection period it goes.
	f.now = f.now.Add(25 * time.Hour)
	tom("GET", "/config", nil)
	if _, err := os.Stat(trash); err != nil {
		t.Fatal("quarantine emptied too early")
	}
	f.now = f.now.Add(30 * 24 * time.Hour)
	tom("GET", "/config", nil)
	if entries, _ := os.ReadDir(trash); len(entries) != 0 {
		t.Fatalf("quarantine not emptied: %v", entries)
	}
	if n, _ := f.s.Usage("tom"); n != 4 {
		t.Fatalf("usage after emptying: %d", n)
	}
}

// Parallel uploads cannot pass the quota together; lock files are capped;
// a partner has a bounded number of requests at once.
func TestQuotaReservedAndLimits(t *testing.T) {
	f := newFixture(t, 100)
	f.do(t, "POST", "/v1/tom/?create=true", "tom", f.token, nil)
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := strings.Repeat(string("0123456789"[i]), 64)
			code, _ := f.do(t, "POST", "/v1/tom/data/"+name, "tom", f.token, bytes.Repeat([]byte("x"), 30))
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	ok := 0
	for c := range codes {
		if c == 200 {
			ok++
		}
	}
	if n, _ := f.s.Usage("tom"); ok > 3 || n > 100 {
		t.Fatalf("quota passed: %d uploads, %d bytes", ok, n)
	}
	// Locks: at most maxLocks.
	for i := range maxLocks + 1 {
		name := strings.Repeat("0", 60) + string("0123456789abcdef"[i/16]) + string("0123456789abcdef"[i%16]) + "00"
		code, _ := f.do(t, "POST", "/v1/tom/locks/"+name, "tom", f.token, []byte("lock"))
		if i < maxLocks && code != 200 || i == maxLocks && code != http.StatusInsufficientStorage {
			t.Fatalf("lock %d: %d", i, code)
		}
	}
	// Requests at once.
	for range maxConcurrent {
		if !f.s.enter("tom") {
			t.Fatal("enter")
		}
	}
	if code, _ := f.do(t, "GET", "/v1/tom/config", "tom", f.token, nil); code != http.StatusTooManyRequests {
		t.Fatalf("too many at once: %d", code)
	}
}

func TestVolumeGone(t *testing.T) {
	f := newFixture(t, 1<<20)
	f.s.Mounted = func() []string { return nil }
	if code, _ := f.do(t, "GET", "/v1/tom/config", "tom", f.token, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("unmounted volume: %d", code)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "tank", Dir)); err == nil {
		t.Fatal("wrote below an unmounted volume")
	}
}

func TestCodes(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	c := &Codes{Now: func() time.Time { return now }}
	code, expires, err := c.Issue("meta")
	if err != nil || len(code) != 11 || code[3] != '-' || !expires.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("issue: %q %v %v", code, expires, err)
	}
	if _, err := c.Redeem("WRONG-CODE"); err != ErrNoCode {
		t.Fatal("wrong code")
	}
	meta, err := c.Redeem(strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	if err != nil || meta != "meta" {
		t.Fatalf("redeem: %v %v", meta, err)
	}
	if _, err := c.Redeem(code); err != ErrNoCode {
		t.Fatal("a code works only once")
	}
	// Expiry.
	code, _, _ = c.Issue(nil)
	now = now.Add(10*time.Minute + time.Second)
	if _, _, ok := c.Active(); ok {
		t.Fatal("expired code active")
	}
	if _, err := c.Redeem(code); err != ErrNoCode {
		t.Fatal("expired code worked")
	}
	// Five wrong tries end a code.
	code, _, _ = c.Issue(nil)
	for range maxTries {
		c.Redeem("AAA-AAA-AAA")
	}
	if _, err := c.Redeem(code); err != ErrNoCode {
		t.Fatal("code survived guessing")
	}
}

func TestPairRoundTrip(t *testing.T) {
	f := newFixture(t, 1)
	codes := &Codes{}
	code, _, _ := codes.Issue(int64(5 << 30))
	f.s.Pair = func(req PairRequest) (PairResponse, error) {
		meta, err := codes.Redeem(req.Code)
		if err != nil {
			return PairResponse{}, err
		}
		return PairResponse{Name: "nas-a", ID: "nas-b", Token: strings.Repeat("k", 64), Quota: meta.(int64)}, nil
	}
	address := strings.TrimPrefix(f.url, "http://")
	client := Client{}
	if _, err := client.Pair(context.Background(), address, PairRequest{Code: "XXX-XXX-XXX"}); err == nil {
		t.Fatal("wrong code paired")
	}
	res, err := client.Pair(context.Background(), address, PairRequest{Code: code, Name: "nas-b", ID: "nas-a", Token: strings.Repeat("j", 64)})
	if err != nil || res.ID != "nas-b" || res.Quota != 5<<30 {
		t.Fatalf("pair: %+v %v", res, err)
	}
	f.s.Pair = nil
	if code, _ := f.do(t, "POST", "/v1/pair", "", "", []byte("{}")); code != http.StatusNotFound {
		t.Fatalf("closed pairing: %d", code)
	}
}

func TestCheckAddress(t *testing.T) {
	for in, want := range map[string]string{"nas-tom.tail1234.ts.net": "nas-tom.tail1234.ts.net:8752", "nas-tom.tail1234.ts.net:9000": "nas-tom.tail1234.ts.net:9000", "100.64.1.2": "100.64.1.2:8752", "[fd7a:115c:a1e0::1]:8752": "[fd7a:115c:a1e0::1]:8752"} {
		if got, err := CheckAddress(in); err != nil || got != want {
			t.Errorf("%s: %s %v", in, got, err)
		}
	}
	// Not over Tailscale: the token would go in the clear.
	for _, bad := range []string{"", "nas-tom", "192.168.1.20", "example.org", "8.8.8.8:8752", "http://x", "a b", "x.ts.net:0", "x.ts.net:70000", "x/../y"} {
		if _, err := CheckAddress(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got := TailscaleOnly([]string{"192.168.1.2", "100.100.1.1", "100.200.1.1", "fd7a:115c:a1e0::5"}); len(got) != 2 {
		t.Fatalf("tailscale: %v", got)
	}
}

func TestServerLimits(t *testing.T) {
	srv := NewHTTPServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
		t.Fatalf("timeouts: %+v", srv)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	limited := LimitListener(ln, 1)
	defer limited.Close()
	go func() {
		for {
			c, err := limited.Accept()
			if err != nil {
				return
			}
			_ = c // held open: the slot stays taken
		}
	}()
	first, _ := net.Dial("tcp", ln.Addr().String())
	defer first.Close()
	time.Sleep(50 * time.Millisecond)
	second, _ := net.Dial("tcp", ln.Addr().String())
	defer second.Close()
	// The second connection is not accepted while the first is open.
	second.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if l := limited.(*limitListener); len(l.slots) != 1 {
		t.Fatalf("slots: %d", len(l.slots))
	}
}

// With restic installed: init, two backups, forget --prune go through;
// what prune deleted is in quarantine, and putting it back (plus restic
// repair index) brings the forgotten snapshot back, restorable.
func TestRealRestic(t *testing.T) {
	binary, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic is not installed")
	}
	f := newFixture(t, 64<<20)
	work := t.TempDir()
	source := filepath.Join(work, "source")
	os.Mkdir(source, 0o700)
	os.WriteFile(filepath.Join(source, "doc.txt"), []byte("Backup-Buddy round trip\n"), 0o600)
	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "RESTIC_REPOSITORY=rest:"+f.url+"/v1/tom", "RESTIC_PASSWORD=sender-only-password",
			"RESTIC_REST_USERNAME=tom", "RESTIC_REST_PASSWORD="+f.token, "RESTIC_CACHE_DIR="+filepath.Join(work, "cache"))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(args ...string) string {
		out, err := run(args...)
		if err != nil {
			t.Fatalf("restic %v: %v\n%s", args, err, out)
		}
		return out
	}
	must("init")
	must("backup", "doc.txt")
	first := strings.TrimSpace(must("snapshots", "--json", "--latest", "1"))
	os.WriteFile(filepath.Join(source, "doc.txt"), []byte("second version, another text entirely\n"), 0o600)
	must("backup", "doc.txt")
	must("check", "--read-data")
	// The receiving side holds no plain text.
	filepath.Walk(filepath.Join(f.mount, "tank", Dir), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			if data, _ := os.ReadFile(p); bytes.Contains(data, []byte("round trip")) {
				t.Errorf("plain text in %s", p)
			}
		}
		return nil
	})
	// forget --prune as the sender runs it: works, nothing refused.
	out := must("forget", "--keep-last", "1", "--prune")
	if strings.Contains(out, "403") || strings.Contains(out, "unable to remove") {
		t.Fatalf("prune refused: %s", out)
	}
	must("check")
	if snaps := must("snapshots", "--json"); strings.Count(snaps, `"short_id"`) != 1 {
		t.Fatalf("snapshots after forget: %s", snaps)
	}
	// Putting the quarantine back brings the first snapshot back.
	repo := filepath.Join(f.mount, "tank", Dir, "tom")
	trash := filepath.Join(repo, Trash)
	stamps, _ := os.ReadDir(trash)
	if len(stamps) == 0 {
		t.Fatal("nothing in quarantine")
	}
	for _, st := range stamps {
		base := filepath.Join(trash, st.Name())
		filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(base, p)
			if _, err := os.Stat(filepath.Join(repo, rel)); err == nil {
				return nil
			}
			os.MkdirAll(filepath.Dir(filepath.Join(repo, rel)), 0o700)
			return os.Rename(p, filepath.Join(repo, rel))
		})
	}
	must("repair", "index")
	if snaps := must("snapshots", "--json"); strings.Count(snaps, `"short_id"`) != 2 {
		t.Fatalf("snapshots after putting back: %s", snaps)
	}
	var list []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(first), &list)
	if len(list) != 1 {
		t.Fatalf("first snapshot: %s", first)
	}
	must("restore", list[0].ID, "--target", filepath.Join(work, "restored"))
	if data, _ := os.ReadFile(filepath.Join(work, "restored", "doc.txt")); string(data) != "Backup-Buddy round trip\n" {
		t.Fatalf("restored %q", data)
	}
}
