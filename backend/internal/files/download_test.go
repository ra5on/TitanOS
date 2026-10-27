package files

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/httpfetch"
)

// downloadManager returns a manager whose confined client only reaches the
// servers listed as public; all test servers listen on loopback.
func downloadManager(t *testing.T, tlsConfig *tls.Config, public ...*httptest.Server) (*Manager, Root) {
	t.Helper()
	m, root, _ := newRoot(t)
	ports := map[uint16]bool{}
	for _, s := range public {
		u, _ := url.Parse(s.URL)
		port, _ := strconv.Atoi(u.Port())
		ports[uint16(port)] = true
	}
	confined := httpfetch.NewClient(httpfetch.Policy{TLS: tlsConfig, Allow: func(a netip.AddrPort) bool { return ports[a.Port()] }})
	admin := httpfetch.NewClient(httpfetch.Policy{TLS: tlsConfig, Allow: func(a netip.AddrPort) bool { return true }})
	m.DownloadClient = func(c bool) *http.Client {
		if c {
			return confined
		}
		return admin
	}
	m.downloadDelay = 10 * time.Millisecond
	return m, root
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func partFiles(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), downloadPrefix) {
			found = append(found, e.Name())
		}
	}
	return found
}

func TestDownloadPublishesWithoutOverwriting(t *testing.T) {
	data := payload(3 << 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="debian.iso"`)
		http.ServeContent(w, r, "x", time.Unix(1700000000, 0), bytes.NewReader(data))
	}))
	defer server.Close()
	m, root := downloadManager(t, nil, server)
	probe, err := m.ProbeDownload(t.Context(), root, "urlaub", server.URL+"/get?id=1", true)
	if err != nil || probe.Name != "debian.iso" || probe.Size != int64(len(data)) || probe.Exists {
		t.Fatalf("probe = %+v, %v", probe, err)
	}
	sum := sha256.Sum256(data)
	job, err := m.StartDownload(root, DownloadRequest{URL: server.URL + "/get?token=secret#frag", Dir: "urlaub", Name: "debian.iso", SHA256: strings.ToUpper(hex.EncodeToString(sum[:]))}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(job.From, "secret") || job.Operation != "download" {
		t.Fatalf("job shows the query string or wrong kind: %+v", job)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone || job.Copied != int64(len(data)) || job.Bytes != int64(len(data)) || job.Names[0] != "debian.iso" {
		t.Fatalf("job = %+v", job)
	}
	got, err := os.ReadFile(filepath.Join(root.Dir, "urlaub/debian.iso"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("published file differs: %v", err)
	}
	if info, _ := os.Stat(filepath.Join(root.Dir, "urlaub/debian.iso")); info.Mode().Perm() != 0o660 {
		t.Errorf("mode = %v", info.Mode())
	}
	if parts := partFiles(t, filepath.Join(root.Dir, "urlaub")); len(parts) != 0 {
		t.Errorf("part files left: %v", parts)
	}
	// The same name again: a visible conflict, then "keep both".
	if _, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Dir: "urlaub", Name: "debian.iso"}, "alex", os.Getuid(), true); !errors.Is(err, ErrExists) {
		t.Fatalf("existing name: %v", err)
	}
	probe, _ = m.ProbeDownload(t.Context(), root, "urlaub", server.URL, true)
	if !probe.Exists {
		t.Error("probe did not see the existing name")
	}
	job, err = m.StartDownload(root, DownloadRequest{URL: server.URL, Dir: "urlaub", Name: "debian.iso", Conflict: ConflictRename}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobDone || job.Names[0] != "debian (2).iso" {
		t.Fatalf("keep both: %+v", job)
	}
	if got, _ := os.ReadFile(filepath.Join(root.Dir, "urlaub/debian.iso")); !bytes.Equal(got, data) {
		t.Error("the existing file was changed")
	}
}

func TestDownloadResumesAfterDroppedConnection(t *testing.T) {
	data := payload(5 << 20)
	var requests, ranged atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if requests.Add(1) == 1 {
			// Send a part and drop the connection.
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			w.Write(data[:1<<20])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		if r.Header.Get("Range") == "bytes=1048576-" && r.Header.Get("If-Range") == `"v1"` {
			ranged.Add(1)
		}
		http.ServeContent(w, r, "x", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	m, root := downloadManager(t, nil, server)
	m.Acquire = nil
	sum := sha256.Sum256(data)
	job, err := m.StartDownload(root, DownloadRequest{URL: server.URL + "/big.iso", Name: "big.iso", SHA256: hex.EncodeToString(sum[:])}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone || ranged.Load() != 1 {
		t.Fatalf("job = %+v, ranged requests = %d", job, ranged.Load())
	}
	if got, _ := os.ReadFile(filepath.Join(root.Dir, "big.iso")); !bytes.Equal(got, data) {
		t.Fatal("resumed file differs")
	}
}

func TestDownloadRetryAfterFailure(t *testing.T) {
	data := payload(1 << 20)
	var broken atomic.Bool
	broken.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	m, root := downloadManager(t, nil, server)
	job, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "a.bin"}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobFailed || !job.Retryable || !strings.Contains(job.Error, "HTTP 503") {
		t.Fatalf("job = %+v", job)
	}
	if _, err := m.RetryDownload("sam", job.ID, root); !errors.Is(err, ErrNoJob) {
		t.Fatalf("another account retried: %v", err)
	}
	readOnly := root
	readOnly.Writable = false
	if _, err := m.RetryDownload("alex", job.ID, readOnly); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("retry without write access: %v", err)
	}
	broken.Store(false)
	if _, err := m.RetryDownload("alex", job.ID, root); err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobDone {
		t.Fatalf("after retry: %+v", job)
	}
	if got, _ := os.ReadFile(filepath.Join(root.Dir, "a.bin")); !bytes.Equal(got, data) {
		t.Fatal("retried file differs")
	}
}

func TestDownloadRedirectToPrivateAddressRefusedForUsers(t *testing.T) {
	var reached atomic.Bool
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		w.Write([]byte("router admin page"))
	}))
	defer private.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/secret", http.StatusFound)
	}))
	defer public.Close()
	m, root := downloadManager(t, nil, public)
	if _, err := m.ProbeDownload(t.Context(), root, "", private.URL, true); !errors.Is(err, ErrBlocked) {
		t.Fatalf("probe of a private address: %v", err)
	}
	job, err := m.StartDownload(root, DownloadRequest{URL: public.URL, Name: "x.bin"}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobFailed || !strings.Contains(job.Error, "may not be reached") {
		t.Fatalf("job = %+v", job)
	}
	if reached.Load() {
		t.Fatal("the private server was reached")
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "x.bin")); err == nil {
		t.Fatal("a file was published")
	}
	// Owners and administrators may use their home network.
	job, err = m.StartDownload(root, DownloadRequest{URL: public.URL, Name: "x.bin"}, "alex", os.Getuid(), false)
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobDone || !reached.Load() {
		t.Fatalf("administrator: %+v", job)
	}
}

func TestDownloadRefusesHTTPSDowngradeAndTooManyRedirects(t *testing.T) {
	var reached atomic.Bool
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		w.Write([]byte("plain"))
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loop" {
			http.Redirect(w, r, "/loop", http.StatusFound)
			return
		}
		http.Redirect(w, r, plain.URL+"/file", http.StatusFound)
	}))
	defer secure.Close()
	trust := secure.Client().Transport.(*http.Transport).TLSClientConfig
	m, root := downloadManager(t, trust)
	for _, target := range []string{secure.URL + "/file", secure.URL + "/loop"} {
		job, err := m.StartDownload(root, DownloadRequest{URL: target, Name: "x.bin"}, "alex", os.Getuid(), false)
		if err != nil {
			t.Fatal(err)
		}
		if job = finish(t, m, job.ID); job.State != JobFailed || !strings.Contains(job.Error, "redirect") {
			t.Fatalf("%s: job = %+v", target, job)
		}
	}
	if reached.Load() {
		t.Fatal("https was followed to plain http")
	}
}

func TestDownloadSizeLimitAndHashMismatch(t *testing.T) {
	data := payload(2 << 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	m, root := downloadManager(t, nil, server)
	m.downloadFree = func(*os.File) (uint64, error) { return downloadReserve + 1<<20, nil }
	job, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "big.bin"}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobFailed || !strings.Contains(job.Error, "free space") || job.Copied != 0 {
		t.Fatalf("too little space: %+v", job)
	}
	m.downloadFree = nil
	job, err = m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "wrong.bin", SHA256: strings.Repeat("ab", 32)}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobFailed || !strings.Contains(job.Error, "SHA-256") {
		t.Fatalf("hash mismatch: %+v", job)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "wrong.bin")); err == nil {
		t.Fatal("a file with the wrong digest was published")
	}
	if parts := partFiles(t, root.Dir); len(parts) != 1 {
		// The failed space check keeps its empty part for a retry; the
		// mismatched one is removed.
		t.Fatalf("part files: %v", parts)
	}
	if err := m.ForgetTransfer("alex", job.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearTransfers("alex"); err != nil {
		t.Fatal(err)
	}
	if parts := partFiles(t, root.Dir); len(parts) != 0 {
		t.Fatalf("forgetting failed downloads left part files: %v", parts)
	}
}

func TestDownloadCancelRemovesPartFile(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(4<<20))
		w.Write(payload(64 << 10))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	m, root := downloadManager(t, nil, server)
	job, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "slow.iso"}, "alex", os.Getuid(), true)
	if err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if len(partFiles(t, root.Dir)) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := m.CancelJob("alex", job.ID); err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobCancelled || !job.Retryable {
		t.Fatalf("job = %+v", job)
	}
	if parts := partFiles(t, root.Dir); len(parts) != 0 {
		t.Fatalf("cancel left part files: %v", parts)
	}
}

func TestDownloadRightsAndLimits(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.(http.Flusher).Flush()
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(block)
	m, root := downloadManager(t, nil, server)
	readOnly := root
	readOnly.Writable = false
	stick := root
	stick.Hide = []string{"users"}
	for _, bad := range []struct {
		root Root
		req  DownloadRequest
		want error
	}{
		{readOnly, DownloadRequest{URL: server.URL, Name: "a"}, ErrReadOnly},
		{stick, DownloadRequest{URL: server.URL, Name: "a"}, ErrReadOnly},
		{root, DownloadRequest{URL: "ftp://example.org/a", Name: "a"}, ErrInvalid},
		{root, DownloadRequest{URL: "file:///etc/passwd", Name: "a"}, ErrInvalid},
		{root, DownloadRequest{URL: "https://user:pw@example.org/a", Name: "a"}, ErrInvalid},
		{root, DownloadRequest{URL: server.URL, Name: "../a"}, ErrInvalid},
		{root, DownloadRequest{URL: server.URL, Name: "a.titanos-part"}, ErrInvalid},
		{root, DownloadRequest{URL: server.URL, Name: "a", Dir: "../.."}, ErrInvalid},
		{root, DownloadRequest{URL: server.URL, Name: "a", SHA256: "abc"}, ErrInvalid},
		{root, DownloadRequest{URL: server.URL, Name: "a", Conflict: ConflictReplace}, ErrInvalid},
		{root, DownloadRequest{URL: server.URL, Name: "a", Dir: "nope"}, ErrNotFound},
	} {
		if _, err := m.StartDownload(bad.root, bad.req, "alex", os.Getuid(), true); !errors.Is(err, bad.want) {
			t.Errorf("%+v: %v, want %v", bad.req, err, bad.want)
		}
	}
	m.Protected = func(p string) bool { return strings.HasSuffix(p, "vm.qcow2") }
	if _, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "vm.qcow2"}, "alex", os.Getuid(), true); !errors.Is(err, ErrProtected) {
		t.Errorf("VM file: %v", err)
	}
	for i := range downloadPerAccount {
		if _, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: fmt.Sprintf("f%d", i)}, "alex", os.Getuid(), true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "more"}, "alex", os.Getuid(), true); !errors.Is(err, ErrDownloadLimit) {
		t.Fatalf("fourth download: %v", err)
	}
	if _, err := m.StartDownload(root, DownloadRequest{URL: server.URL, Name: "sams"}, "sam", os.Getuid(), true); err != nil {
		t.Fatalf("another account is limited separately: %v", err)
	}
	if !m.Busy() {
		t.Error("running downloads do not count as busy")
	}
	if jobs := m.Jobs("sam"); len(jobs) != 1 {
		t.Errorf("sam sees %d jobs", len(jobs))
	}
	for _, job := range m.Jobs("alex") {
		m.CancelJob("alex", job.ID)
	}
	for _, job := range m.Jobs("sam") {
		m.CancelJob("sam", job.ID)
	}
}

func TestDownloadLeftoverRemovedWhenFolderOpened(t *testing.T) {
	m, root, _ := newRoot(t)
	old := filepath.Join(root.Dir, downloadPrefix+strings.Repeat("a", 24)+partSuffix)
	fresh := filepath.Join(root.Dir, downloadPrefix+strings.Repeat("b", 24)+partSuffix)
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-uploadIdle - time.Hour)
	os.Chtimes(old, past, past)
	if _, err := m.Uploads(root, "", "alex", os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("an idle leftover of an earlier run stayed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a recent part file was removed")
	}
}
