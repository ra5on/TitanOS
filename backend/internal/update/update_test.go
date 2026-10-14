package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// signer makes minisign keys and signatures the way the tool does.
type signer struct {
	id      [8]byte
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

func newSigner(t *testing.T) signer {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := signer{public: public, private: private}
	rand.Read(s.id[:])
	return s
}

func (s signer) publicFile() string {
	return "untrusted comment: test key\n" + base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), s.id[:]...), s.public...)) + "\n"
}

func (s signer) sign(message []byte) string {
	hash := blake2b.Sum512(message)
	signature := ed25519.Sign(s.private, hash[:])
	comment := "timestamp:1 file:manifest.json"
	global := ed25519.Sign(s.private, append(append([]byte{}, signature...), comment...))
	return "untrusted comment: signature\n" + base64.StdEncoding.EncodeToString(append(append([]byte("ED"), s.id[:]...), signature...)) +
		"\ntrusted comment: " + comment + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

// release is a project page with one release on it.
type release struct {
	mu       sync.Mutex
	manifest Manifest
	raw      []byte // what is served as manifest.json
	sig      string
	assets   map[string][]byte
	// cut makes the first request for an asset break off after this many bytes.
	cut      int
	requests []string
	noRange  bool
}

func (r *release) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req.URL.Path+" "+req.Header.Get("Range"))
	name := filepath.Base(req.URL.Path)
	switch {
	case req.URL.Path == "/releases.atom":
		fmt.Fprintf(w, `<feed><entry><link rel="alternate" type="text/html" href="https://example/releases/tag/%s"/></entry><entry><link href="https://example/releases/tag/v0.0.1"/></entry></feed>`, r.manifest.Tag)
	case name == "manifest.json":
		w.Write(r.raw)
	case name == "manifest.json.minisig":
		w.Write([]byte(r.sig))
	default:
		data, ok := r.assets[name]
		if !ok || !strings.Contains(req.URL.Path, "/releases/download/"+r.manifest.Tag+"/") {
			http.NotFound(w, req)
			return
		}
		start := 0
		if rng := req.Header.Get("Range"); rng != "" && !r.noRange {
			fmt.Sscanf(rng, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		if r.cut > 0 && start == 0 {
			cut := r.cut
			r.cut = 0
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data[:cut])
			// Close the connection in the middle of the file.
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, _ := hijacker.Hijack()
				conn.Close()
			}
			return
		}
		w.Write(data[start:])
	}
}

type env struct {
	m       *Manager
	store   *config.Store
	boot    string
	release *release
	signer  signer
	events  chan string
}

const cmdline = "boot=live live-media=/dev/disk/by-label/TITANOS live-media-path=/titanos/@SLOT@ toram=filesystem.squashfs"

func (e *env) publish(t *testing.T, version, channel string, change func(*Manifest)) {
	t.Helper()
	assets := map[string][]byte{}
	manifest := Manifest{Version: version, Tag: "v" + version, Channel: channel, Kernel: "6.12.0", Released: time.Now().UTC(), Changelog: "Faster.", Cmdline: cmdline}
	for i, name := range slotFiles {
		data := make([]byte, 300_000+i*1000)
		rand.Read(data)
		asset := "titanos-" + version + "-" + name
		assets[asset] = data
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, File{Name: name, Asset: asset, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	}
	if change != nil {
		change(&manifest)
	}
	raw, _ := json.Marshal(manifest)
	e.release.mu.Lock()
	e.release.manifest, e.release.raw, e.release.sig, e.release.assets = manifest, raw, e.signer.sign(raw), assets
	e.release.mu.Unlock()
}

// newEnv is a stick with version 1.0.0 in slot a, running, and a project
// page to update from.
func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{boot: filepath.Join(root, "efi"), release: &release{}, signer: newSigner(t), events: make(chan string, 10)}
	for _, dir := range []string{"efi/loader/entries", "efi/titanos/a", "efi/config", "keys"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	os.WriteFile(filepath.Join(e.boot, "loader/loader.conf"), []byte("default titanos-a.conf\ntimeout 3\n"), 0o644)
	os.WriteFile(filepath.Join(e.boot, "loader/entries/titanos-a.conf"), []byte("title    TitanOS\nversion  1.0.0\nsort-key titanos-a\nlinux    /titanos/a/vmlinuz\n"), 0o644)
	for _, name := range slotFiles {
		os.WriteFile(filepath.Join(e.boot, "titanos/a", name), []byte("old "+name), 0o644)
	}
	os.WriteFile(filepath.Join(e.boot, "titanos/a/version"), []byte("1.0.0\n"), 0o644)
	os.WriteFile(filepath.Join(root, "keys/release.pub"), []byte(e.signer.publicFile()), 0o644)

	server := httptest.NewServer(e.release)
	t.Cleanup(server.Close)
	e.store = config.Open(filepath.Join(e.boot, "config"))
	e.store.WriteJSON(settingsFile, Settings{Channel: ChannelStable, InstallTime: "03:30", Source: server.URL})
	e.m = e.manager(t, "1.0.0", "a", "boot-1")
	return e
}

// manager returns a manager as a (re)started titand would make it.
func (e *env) manager(t *testing.T, version, slot, bootID string) *Manager {
	t.Helper()
	m := NewManager(e.store, version, slot)
	m.bootDir, m.keyDir, m.bootID, m.stall, m.retryPause = e.boot, filepath.Join(filepath.Dir(e.boot), "keys"), bootID, 5*time.Second, 10*time.Millisecond
	m.OnEvent = func(kind, subject string) { e.events <- kind + ":" + subject }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

func (e *env) install(t *testing.T) {
	t.Helper()
	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 400 {
		if phase := e.m.Status().Progress.Phase; phase == "done" || phase == "failed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the installation did not finish")
}

func (e *env) event(t *testing.T) string {
	t.Helper()
	select {
	case event := <-e.events:
		return event
	case <-time.After(2 * time.Second):
		return ""
	}
}

func entries(e *env) string {
	files, _ := os.ReadDir(filepath.Join(e.boot, "loader/entries"))
	var names []string
	for _, file := range files {
		data, _ := os.ReadFile(filepath.Join(e.boot, "loader/entries", file.Name()))
		names = append(names, file.Name()+"["+sortKey(string(data))+"]")
	}
	return strings.Join(names, " ")
}

func TestUpdateGoesToTheOtherSlotOnTrial(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	status := e.m.Status()
	if !status.CanVerify || !status.Release || len(status.Slots) != 2 || !status.Slots[0].Running || !status.Slots[0].Next || status.Slots[1].Complete || status.CanRollback {
		t.Fatalf("before: %+v", status)
	}
	e.install(t)
	status = e.m.Status()
	if status.Progress.Phase != "done" || status.Pending == nil || status.Pending.Version != "1.1.0" || status.Pending.Slot != "b" || status.Available != nil {
		t.Fatalf("after: %+v", status)
	}
	if got := e.event(t); got != "update_available:1.1.0" {
		t.Errorf("event: %q", got)
	}
	// The new slot starts next, three tries; the running one is the fallback.
	if got := entries(e); got != "titanos-a.conf[titanos-2] titanos-b+3.conf[titanos-1]" {
		t.Errorf("entries: %s", got)
	}
	b := status.Slots[1]
	if !b.Complete || !b.Next || b.Version != "1.1.0" || b.TriesLeft == nil || *b.TriesLeft != 3 || status.Slots[0].Next {
		t.Errorf("slots: %+v", status.Slots)
	}
	entry, _ := os.ReadFile(filepath.Join(e.boot, "loader/entries/titanos-b+3.conf"))
	for _, want := range []string{"version  1.1.0", "linux    /titanos/b/vmlinuz", "initrd   /titanos/b/initrd.img", "live-media-path=/titanos/b "} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("%q missing in the entry:\n%s", want, entry)
		}
	}
	// A fixed default would start an entry even after giving up on it.
	if loader, _ := os.ReadFile(filepath.Join(e.boot, "loader/loader.conf")); strings.Contains(string(loader), "default") || !strings.Contains(string(loader), "timeout 3") {
		t.Errorf("loader.conf: %q", loader)
	}
	for _, file := range e.release.manifest.Files {
		data, _ := os.ReadFile(filepath.Join(e.boot, "titanos/b", file.Name))
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Errorf("%s differs from the release", file.Name)
		}
	}
	// The running system was not touched.
	if data, _ := os.ReadFile(filepath.Join(e.boot, "titanos/a/vmlinuz")); string(data) != "old vmlinuz" {
		t.Errorf("slot a changed: %q", data)
	}
	if err := e.m.Install(context.Background()); !errors.Is(err, ErrBusy) {
		t.Errorf("a second installation: %v", err)
	}
}

func TestOutcomeAfterTheRestart(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.m.Mounted = func() []string { return []string{"tank"} }
	e.install(t)
	e.event(t)

	// titand restarts without a restart of the NAS: nothing is decided yet.
	same := e.manager(t, "1.0.0", "a", "boot-1")
	same.Confirm()
	if same.Status().Pending == nil || e.event(t) != "" {
		t.Error("a restart of titand settled the update")
	}

	// The NAS starts from b; the loader has counted one try.
	os.Rename(filepath.Join(e.boot, "loader/entries/titanos-b+3.conf"), filepath.Join(e.boot, "loader/entries/titanos-b+2-1.conf"))
	started := e.manager(t, "1.1.0", "b", "boot-2")
	started.Confirm()
	if status := started.Status(); status.Pending == nil || !status.Unconfirmed {
		t.Errorf("on trial: %+v", status)
	}
	if err := started.Rollback(); !errors.Is(err, ErrBusy) {
		t.Errorf("going back while on trial: %v", err)
	}
	// It is not good before the volumes of before are back.
	mounted := []string{}
	started.Mounted = func() []string { return mounted }
	if started.Healthy() {
		t.Error("healthy although the volume of before is not mounted")
	}
	mounted = []string{"tank", "fast"}
	if !started.Healthy() || !same.Healthy() {
		t.Error("not healthy although everything is back")
	}
	// The boot check marks it good.
	if !started.OnTrial() || same.OnTrial() {
		t.Error("which system is on trial is not told right")
	}
	if err := started.Bless(); err != nil {
		t.Fatal(err)
	}
	if got := entries(e); got != "titanos-a.conf[titanos-2] titanos-b.conf[titanos-1]" {
		t.Errorf("after marking good: %s", got)
	}
	if started.OnTrial() || started.Bless() != nil {
		t.Error("marking good twice is not harmless")
	}
	started.Confirm()
	if got := e.event(t); got != "update_installed:1.1.0" {
		t.Errorf("event: %q", got)
	}
	status := started.Status()
	if status.Pending != nil || status.Unconfirmed || !status.CanRollback || !status.Slots[1].Next {
		t.Errorf("settled: %+v", status)
	}
	if _, err := e.store.Read(pendingFile); err == nil {
		t.Error("the pending update was not cleared from /config")
	}
}

func TestFallbackIsReported(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.install(t)
	e.event(t)
	// Three starts failed; the loader gave up on b and started a again.
	os.Rename(filepath.Join(e.boot, "loader/entries/titanos-b+3.conf"), filepath.Join(e.boot, "loader/entries/titanos-b+0-3.conf"))
	back := e.manager(t, "1.0.0", "a", "boot-5")
	back.Confirm()
	if got := e.event(t); got != "update_failed:1.1.0" {
		t.Errorf("event: %q", got)
	}
	status := back.Status()
	if status.Pending != nil || !status.Slots[0].Next || status.Slots[1].Next || status.CanRollback {
		t.Errorf("after the fallback: %+v", status)
	}
}

func TestGoingBackAndCancelling(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.install(t)
	e.event(t)
	// Changed one's mind before restarting.
	if err := e.m.Cancel(); err != nil {
		t.Fatal(err)
	}
	if got := entries(e); got != "titanos-a.conf[titanos-1] titanos-b.conf[titanos-2]" {
		t.Errorf("after cancelling: %s", got)
	}
	if status := e.m.Status(); status.Pending != nil || !status.Slots[0].Next || !status.CanRollback {
		t.Errorf("after cancelling: %+v", status)
	}
	// Now running 1.1.0 from b, confirmed; going back to a is on trial too.
	os.WriteFile(filepath.Join(e.boot, "loader/entries/titanos-b.conf"), []byte("title TitanOS\nsort-key titanos-1\n"), 0o644)
	os.WriteFile(filepath.Join(e.boot, "loader/entries/titanos-a.conf"), []byte("title TitanOS\nsort-key titanos-2\n"), 0o644)
	current := e.manager(t, "1.1.0", "b", "boot-2")
	if err := current.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := entries(e); got != "titanos-a+3.conf[titanos-1] titanos-b.conf[titanos-2]" {
		t.Errorf("after going back: %s", got)
	}
	if pending := current.Status().Pending; pending == nil || pending.Kind != "rollback" || pending.Version != "1.0.0" || pending.Slot != "a" {
		t.Errorf("pending: %+v", pending)
	}
	os.Rename(filepath.Join(e.boot, "loader/entries/titanos-a+3.conf"), filepath.Join(e.boot, "loader/entries/titanos-a.conf"))
	old := e.manager(t, "1.0.0", "a", "boot-3")
	old.Confirm()
	if got := e.event(t); got != "update_rolled_back:1.0.0" {
		t.Errorf("event: %q", got)
	}
}

func TestOnlySignedReleasesAreInstalled(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	ctx := context.Background()

	// Signed by someone else.
	e.release.sig = newSigner(t).sign(e.release.raw)
	if err := e.m.Check(ctx); !errors.Is(err, ErrSignature) {
		t.Errorf("foreign key: %v", err)
	}
	// Changed after signing.
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.release.raw = []byte(strings.Replace(string(e.release.raw), `"1.1.0"`, `"9.9.9"`, 1))
	if err := e.m.Check(ctx); !errors.Is(err, ErrSignature) {
		t.Errorf("changed manifest: %v", err)
	}
	if status := e.m.Status(); status.Available != nil || status.CheckError == "" {
		t.Errorf("status: %+v", status)
	}
	if err := e.m.Install(ctx); !errors.Is(err, ErrNothing) {
		t.Errorf("install without a release: %v", err)
	}
	// An image without a key installs nothing at all.
	os.Remove(filepath.Join(filepath.Dir(e.boot), "keys/release.pub"))
	e.publish(t, "1.1.0", ChannelStable, nil)
	if err := e.m.Check(ctx); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
	if e.m.Status().CanVerify {
		t.Error("claims to be able to verify without a key")
	}
	if got := entries(e); got != "titanos-a.conf[titanos-a]" {
		t.Errorf("the stick was changed: %s", got)
	}
}

func TestSignedButUnusableManifests(t *testing.T) {
	e := newEnv(t)
	for name, change := range map[string]func(*Manifest){
		"version":             func(m *Manifest) { m.Version = "latest" },
		"tag leaves the page": func(m *Manifest) { m.Tag = "../../other/repo" },
		"channel":             func(m *Manifest) { m.Channel = "nightly" },
		"file missing":        func(m *Manifest) { m.Files = m.Files[:2] },
		"file twice":          func(m *Manifest) { m.Files[1].Name = "vmlinuz" },
		"foreign file":        func(m *Manifest) { m.Files[0].Name = "../../EFI/BOOT/BOOTX64.EFI" },
		"asset path":          func(m *Manifest) { m.Files[0].Asset = "../x" },
		"checksum":            func(m *Manifest) { m.Files[0].SHA256 = "abc" },
		"size":                func(m *Manifest) { m.Files[0].Size = 5 << 30 },
		"cmdline two lines":   func(m *Manifest) { m.Cmdline += "\nlinux /evil" },
		"cmdline other slot":  func(m *Manifest) { m.Cmdline = "boot=live live-media-path=/titanos/a" },
	} {
		e.publish(t, "1.1.0", ChannelStable, change)
		if err := e.m.Check(context.Background()); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDamagedDownloadIsNotInstalled(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	// The release serves something else than the manifest promised.
	e.release.assets["titanos-1.1.0-initrd.img"][100] ^= 0xff
	e.install(t)
	status := e.m.Status()
	if status.Progress.Phase != "failed" || !strings.Contains(status.Progress.Error, "checksum") || status.Pending != nil {
		t.Fatalf("status: %+v", status)
	}
	// Slot b cannot be started, a still starts as before.
	if got := entries(e); got != "titanos-a.conf[titanos-a]" {
		t.Errorf("entries: %s", got)
	}
	if _, err := os.Stat(filepath.Join(e.boot, "titanos/b/initrd.img")); err == nil {
		t.Error("the damaged file was kept under its name")
	}
	if status.CanRollback || status.Slots[1].Complete {
		t.Errorf("slots: %+v", status.Slots)
	}
}

func TestInterruptedDownloadIsResumed(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.release.cut = 120_000
	e.install(t)
	if status := e.m.Status(); status.Progress.Phase != "done" {
		t.Fatalf("status: %+v", status.Progress)
	}
	resumed := false
	for _, request := range e.release.requests {
		if strings.HasSuffix(request, "bytes=120000-") {
			resumed = true
		}
	}
	if !resumed {
		t.Errorf("not taken up where it stopped: %v", e.release.requests)
	}

	// A server that does not do ranges sends everything again; fine too.
	e = newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.release.cut, e.release.noRange = 50_000, true
	e.install(t)
	if status := e.m.Status(); status.Progress.Phase != "done" {
		t.Fatalf("without ranges: %+v", status.Progress)
	}
}

func TestWhatCountsAsNewer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	check := func(m *Manager) string {
		if err := m.Check(ctx); err != nil {
			return "error: " + err.Error()
		}
		if available := m.Status().Available; available != nil {
			return available.Version
		}
		return ""
	}
	e.publish(t, "1.0.0", ChannelStable, nil)
	if got := check(e.m); got != "" {
		t.Errorf("the same version offered: %q", got)
	}
	e.publish(t, "0.9.0", ChannelStable, nil)
	if got := check(e.m); got != "" {
		t.Errorf("an older version offered: %q", got)
	}
	// A development build takes any release.
	dev := e.manager(t, "83ce4d5-dirty", "a", "boot-1")
	if got := check(dev); got != "0.9.0" || dev.Status().Release {
		t.Errorf("development build: %q", got)
	}
	// The beta channel follows the feed to the newest release.
	e.publish(t, "1.2.0-beta.1", ChannelBeta, nil)
	e.m.SetSettings(Settings{Channel: ChannelBeta, InstallTime: "03:30"})
	if got := check(e.m); got != "1.2.0-beta.1" {
		t.Errorf("beta: %q", got)
	}
	// A release that asks for a step in between.
	e.publish(t, "3.0.0", ChannelStable, func(m *Manifest) { m.MinVersion = "2.0.0" })
	e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "03:30"})
	check(e.m)
	if err := e.m.Install(ctx); !errors.Is(err, ErrTooOld) {
		t.Errorf("skipping a required version: %v", err)
	}
	for _, bad := range []Settings{{Channel: "nightly", InstallTime: "03:30"}, {Channel: ChannelStable, InstallTime: "25:00"}, {Channel: ChannelStable, InstallTime: "3:30"}} {
		if err := e.m.SetSettings(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("settings %+v: %v", bad, err)
		}
	}
	// The source cannot be changed through the settings of the web UI.
	e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "04:00", Source: "https://evil.example"})
	if !strings.HasPrefix(e.m.source(), "http://127.0.0.1") || e.m.Status().Settings.Source != "" {
		t.Errorf("source: %q", e.m.source())
	}
}

func TestDevelopmentModeTouchesNothing(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.m.Disabled = true
	e.m.Check(context.Background())
	for name, err := range map[string]error{"install": e.m.Install(context.Background()), "rollback": e.m.Rollback(), "cancel": e.m.Cancel()} {
		if !errors.Is(err, ErrDisabled) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := entries(e); got != "titanos-a.conf[titanos-a]" {
		t.Errorf("entries: %s", got)
	}
}

func TestPluginPreparationFailurePreservesFallbackSlot(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	oldEntry := filepath.Join(e.boot, "loader/entries/titanos-b.conf")
	oldKernel := filepath.Join(e.boot, "titanos/b/vmlinuz")
	os.MkdirAll(filepath.Dir(oldKernel), 0o755)
	os.WriteFile(oldEntry, []byte("fallback entry"), 0o644)
	os.WriteFile(oldKernel, []byte("fallback kernel"), 0o644)
	called := false
	e.m.PreparePlugins = func(context.Context, string, string, string, map[string]string, func(context.Context, string, int64) ([]byte, error), func(context.Context, string, string, string, int64) error) error {
		called = true
		return errors.New("bad plugin signature")
	}
	e.install(t)
	if !called || e.m.Status().Progress.Phase != "failed" || e.m.Status().Pending != nil {
		t.Fatal(e.m.Status())
	}
	for path, want := range map[string]string{oldEntry: "fallback entry", oldKernel: "fallback kernel"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatal("plugin failure overwrote rollback", path, err)
		}
	}
}
