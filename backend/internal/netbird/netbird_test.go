package netbird

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const testKey = "A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D"

// fakeRunner answers `netbird status --json` with status and records every
// call with what it got on stdin.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []string
	stdins []string
	fail   bool
	failUp bool
	status string
}

func (f *fakeRunner) Run(_ context.Context, stdin string, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	f.stdins = append(f.stdins, stdin)
	if f.fail || (f.failUp && len(args) > 0 && args[0] == "up") {
		return "", errors.New("command failed")
	}
	if len(args) > 0 && args[0] == "status" {
		return f.status, nil
	}
	return "", nil
}

func (f *fakeRunner) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := f.calls
	f.calls, f.stdins = nil, nil
	return calls
}

func recorded(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func fixture(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	m := NewManager(config.Open(t.TempDir()))
	m.runtime = config.Open(t.TempDir())
	m.mountRoot = t.TempDir()
	m.async = func(f func()) { f() }
	os.Mkdir(filepath.Join(m.mountRoot, "data"), 0700)
	f := &fakeRunner{status: recorded(t, "status-needslogin.json")}
	m.runner = f
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare("netbird", "data"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m.timer != nil {
			m.timer.Stop()
		}
	})
	return m, f
}

func find(calls []string, prefix string) []string {
	var found []string
	for _, call := range calls {
		if strings.HasPrefix(call, prefix) {
			found = append(found, call)
		}
	}
	return found
}

func TestSettingsValidation(t *testing.T) {
	for _, text := range []string{"ftp://nb.example.org", "https://user@nb.example.org", "https://nb.example.org/api", "https://nb.example.org?x=1", "https://nb.example.org:0", "https://nb.example.org:70000", "https://", "https://nb.example.org\n--setup-key=x", "nb.example.org", "https://a.example.org,b"} {
		if _, err := normalize(Settings{ManagementURL: text}); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	for text, want := range map[string]string{"": "", DefaultManagementURL: "", " https://nb.example.org:33073/ ": "https://nb.example.org:33073", "http://10.0.0.2": "http://10.0.0.2"} {
		s, err := normalize(Settings{ManagementURL: text})
		if err != nil || s.ManagementURL != want {
			t.Fatalf("%q: %q %v", text, s.ManagementURL, err)
		}
	}
	all := strings.Join(args(Settings{}), " ")
	for _, flag := range []string{"--management-url=" + DefaultManagementURL, "--disable-auto-connect=true", "--disable-client-routes=true", "--disable-server-routes=true", "--disable-dns=true", "--block-lan-access=false", "--allow-server-ssh=false", "--allow-remote-jobs=false", "--disable-firewall=false"} {
		if !strings.Contains(all, flag) {
			t.Fatalf("missing %s in %s", flag, all)
		}
	}
	all = strings.Join(args(Settings{AcceptRoutes: true, ServeRoutes: true, AcceptDNS: true}), " ")
	if !strings.Contains(all, "--disable-client-routes=false") || !strings.Contains(all, "--disable-server-routes=false") || !strings.Contains(all, "--disable-dns=false") {
		t.Fatal(all)
	}
	for _, key := range []string{"short", "with space-1234", "line\nbreak-1234", strings.Repeat("x", 129)} {
		if validKey(key) {
			t.Fatalf("accepted key %q", key)
		}
	}
}

func TestSetupKeyStaysSecretAndGoesOverStdin(t *testing.T) {
	m, f := fixture(t)
	if err := m.Action("connect"); !errors.Is(err, ErrSetupKey) {
		t.Fatal("connected without a setup key", err)
	}
	if err := m.SetKey(testKey); err != nil {
		t.Fatal(err)
	}
	status := m.Status()
	data, _ := json.Marshal(status)
	if !status.SetupKeySet || strings.Contains(string(data), testKey) {
		t.Fatalf("status %s", data)
	}
	f.take()
	if err := m.Action("connect"); err != nil {
		t.Fatal(err)
	}
	for i, call := range f.calls {
		if strings.Contains(call, testKey) {
			t.Fatal("key on a command line", call)
		}
		if strings.HasPrefix(call, "netbird up") {
			if !strings.Contains(call, "--setup-key-file=/dev/stdin") || f.stdins[i] != testKey+"\n" {
				t.Fatal("key not on stdin", call)
			}
		}
	}
	if len(find(f.calls, "/usr/libexec/titanos-netbird-runtime apply")) != 1 || len(find(f.calls, "netbird up")) != 1 {
		t.Fatalf("calls %v", f.calls)
	}
	// The key and the wish to be connected survive a restart.
	restored := NewManager(m.store)
	restored.Disabled = true
	if err := restored.Load(); err != nil || restored.key != testKey || !restored.connect {
		t.Fatalf("persistence %v", err)
	}
	// Once registered, the key is not sent again.
	f.status = recorded(t, "status.json")
	f.take()
	if err := m.Action("connect"); err != nil {
		t.Fatal(err)
	}
	for i, call := range f.calls {
		if strings.HasPrefix(call, "netbird up") && (strings.Contains(call, "setup-key") || f.stdins[i] != "") {
			t.Fatal("key sent to a registered peer", call)
		}
	}
	if err := m.SetKey(""); err != nil || m.Status().SetupKeySet {
		t.Fatal("key not removed", err)
	}
	if err := m.SetKey("bad key\n"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestTrialReconnectsConfirmAndUndo(t *testing.T) {
	m, f := fixture(t)
	f.status = recorded(t, "status.json")
	if err := m.Action("connect"); err != nil {
		t.Fatal(err)
	}
	changes := m.store.Changes()
	f.take()
	choice := Settings{AcceptRoutes: true, ServeRoutes: true}
	if err := m.Set(choice); err != nil {
		t.Fatal(err)
	}
	calls := f.take()
	if len(find(calls, "netbird down")) != 1 || len(find(calls, "netbird up")) != 1 || !strings.Contains(find(calls, "netbird up")[0], "--disable-server-routes=false") {
		t.Fatalf("no reconnect with the new options: %v", calls)
	}
	runtime, _ := os.ReadFile(filepath.Join(m.runtime.Dir(), "runtime"))
	if !strings.Contains(string(runtime), "NETBIRD_SERVE_ROUTES=true") || !strings.Contains(string(runtime), "NB_STATE_DIR="+filepath.Join(m.mountRoot, "data/.titanos/netbird")) {
		t.Fatal(string(runtime))
	}
	if m.store.Changes() != changes || m.pending == nil {
		t.Fatal("trial persisted early")
	}
	if !errors.Is(m.Set(Settings{}), ErrPending) || !errors.Is(m.Action("disconnect"), ErrPending) || !errors.Is(m.SetKey(testKey), ErrPending) {
		t.Fatal("overlapping change")
	}
	if err := m.Undo(); err != nil {
		t.Fatal(err)
	}
	calls = f.take()
	if m.settings.ServeRoutes || m.pending != nil || !strings.Contains(find(calls, "netbird up")[0], "--disable-server-routes=true") {
		t.Fatalf("rollback failed %v", calls)
	}
	if err := m.Set(choice); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(m.store)
	restored.Disabled = true
	if err := restored.Load(); err != nil || !restored.settings.ServeRoutes {
		t.Fatalf("persistence %v", err)
	}
}

func TestDisconnectedTrialOnlyPreparesTheHost(t *testing.T) {
	m, f := fixture(t)
	if err := m.Set(Settings{AcceptDNS: true}); err != nil {
		t.Fatal(err)
	}
	if calls := f.take(); len(find(calls, "netbird up")) != 0 || len(find(calls, "/usr/libexec/titanos-netbird-runtime apply")) != 1 {
		t.Fatalf("calls %v", calls)
	}
	m.Undo()
}

func TestNewManagementServerNeedsKey(t *testing.T) {
	m, f := fixture(t)
	f.status = recorded(t, "status.json")
	m.SetKey(testKey)
	if err := m.Action("connect"); err != nil {
		t.Fatal(err)
	}
	m.SetKey("")
	if err := m.Set(Settings{ManagementURL: "https://nb.example.org"}); !errors.Is(err, ErrSetupKey) || m.pending != nil {
		t.Fatal("new server without a key", err)
	}
	m.SetKey(testKey)
	f.take()
	if err := m.Set(Settings{ManagementURL: "https://nb.example.org:33073"}); err != nil {
		t.Fatal(err)
	}
	up := find(f.calls, "netbird up")
	if len(up) != 1 || !strings.Contains(up[0], "--management-url=https://nb.example.org:33073") || !strings.Contains(up[0], "--setup-key-file=/dev/stdin") {
		t.Fatalf("calls %v", f.calls)
	}
	m.Undo()
}

func TestFailedApplyAndFailedRollback(t *testing.T) {
	m, f := fixture(t)
	f.fail = true
	if err := m.Set(Settings{AcceptDNS: true}); err == nil {
		t.Fatal("failure hidden")
	}
	if m.settings.AcceptDNS || m.pending == nil || m.store.Changes() != 0 {
		t.Fatal("failed rollback must be retried without saving")
	}
	f.fail = false
	if err := m.Undo(); err != nil || m.pending != nil {
		t.Fatal("rollback did not recover", err)
	}
	// A reconnect that fails with the new options keeps the old choice; the
	// failed rollback reconnect is retried.
	f.status = recorded(t, "status.json")
	m.connect = true
	f.failUp = true
	if err := m.Set(Settings{AcceptRoutes: true}); err == nil || m.settings.AcceptRoutes {
		t.Fatal("failed reconnect kept")
	}
	f.failUp = false
	m.Undo()
}

func TestConfirmStorageFailureKeepsRollback(t *testing.T) {
	m, _ := fixture(t)
	m.Set(Settings{AcceptDNS: true})
	os.MkdirAll(filepath.Join(m.store.Dir(), settingsFile), 0700)
	if err := m.Confirm(); err == nil || m.pending == nil {
		t.Fatal("bad save confirmed")
	}
	m.Undo()
	if m.settings.AcceptDNS {
		t.Fatal("rollback after failed save")
	}
}

func TestExpiredChangeCannotBeConfirmed(t *testing.T) {
	m, _ := fixture(t)
	m.Set(Settings{AcceptDNS: true})
	m.deadline = time.Now().Add(-time.Second)
	if err := m.Confirm(); !errors.Is(err, ErrPending) {
		t.Fatal(err)
	}
	m.Undo()
}

func TestActivateRestoresTheSavedConnection(t *testing.T) {
	m, f := fixture(t)
	// Not wanted: the daemon stays disconnected.
	f.take()
	if err := m.Activate("netbird"); err != nil {
		t.Fatal(err)
	}
	if calls := f.take(); len(find(calls, "netbird up")) != 0 {
		t.Fatalf("connected without being asked: %v", calls)
	}
	m.SetKey(testKey)
	if err := m.Action("connect"); err != nil {
		t.Fatal(err)
	}
	// A titand restart with the daemon still connected with these settings
	// changes nothing.
	f.status = recorded(t, "status.json")
	f.take()
	if err := m.Activate("netbird"); err != nil {
		t.Fatal(err)
	}
	if calls := f.take(); len(find(calls, "netbird up")) != 0 || len(find(calls, "netbird down")) != 0 {
		t.Fatalf("needless reconnect: %v", calls)
	}
	// After a boot (nothing applied yet) or an unconfirmed trial, it
	// reconnects with the saved choice.
	m.runtime.Remove("applied")
	if err := m.Activate("netbird"); err != nil {
		t.Fatal(err)
	}
	if calls := f.take(); len(find(calls, "netbird up")) != 1 {
		t.Fatalf("not restored: %v", calls)
	}
	// An unreachable management server is no failed activation.
	m.runtime.Remove("applied")
	f.failUp = true
	if err := m.Activate("netbird"); err != nil || m.Status().Error == "" {
		t.Fatal("reconnect failure must stay visible, not fail the plugin", err)
	}
	if m.Activate("tailscale") != nil || m.Prepare("tailscale", "data") != nil {
		t.Fatal("other plugins are not NetBird's business")
	}
}

func TestStatusFromRecordedOutput(t *testing.T) {
	m, f := fixture(t)
	f.status = recorded(t, "status.json")
	s := m.Status()
	if !s.Active || s.State != "Connected" || s.Version != "0.81.5" || s.FQDN != "titan.netbird.cloud" || len(s.IPs) != 2 || s.IPs[0] != "100.92.10.5" || !s.ManagementConnected || s.PeersConnected != 2 || s.PeersTotal != 4 {
		t.Fatalf("%+v", s)
	}
	f.status = "<html>"
	if s := m.Status(); s.Active || s.Error == "" {
		t.Fatalf("invalid output accepted %+v", s)
	}
	f.fail = true
	if s := m.Status(); s.Active || s.State != "inactive" {
		t.Fatalf("missing daemon %+v", s)
	}
	before := m.store.Changes()
	m.Status()
	if m.store.Changes() != before {
		t.Fatal("status wrote config")
	}
}

func TestDisabledAndUnsafeVolume(t *testing.T) {
	m, f := fixture(t)
	f.take()
	m.Disabled = true
	if err := m.Set(Settings{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := m.Action("connect"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := m.SetKey(testKey); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	m.Status()
	m.Peers()
	if len(f.calls) != 0 {
		t.Fatal("development mode ran commands", f.calls)
	}
	m.Disabled = false
	if err := m.Prepare("netbird", "../else"); err == nil {
		t.Fatal("unsafe volume")
	}
	os.RemoveAll(filepath.Join(m.mountRoot, "data/.titanos/netbird"))
	os.Symlink(t.TempDir(), filepath.Join(m.mountRoot, "data/.titanos/netbird"))
	if err := m.Prepare("netbird", "data"); err == nil {
		t.Fatal("symlink state path")
	}
	if err := m.Action("unknown"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestStateRejectsWritableAncestors(t *testing.T) {
	for _, directory := range []string{".titanos", ".titanos/netbird"} {
		t.Run(directory, func(t *testing.T) {
			m, _ := fixture(t)
			if err := os.Chmod(filepath.Join(m.mountRoot, "data", directory), 0777); err != nil {
				t.Fatal(err)
			}
			if err := m.Prepare("netbird", "data"); err == nil {
				t.Fatal("accepted externally writable private state directory")
			}
		})
	}
}

func TestInvalidStoredSettingsAreReported(t *testing.T) {
	store := config.Open(t.TempDir())
	store.WriteJSON(settingsFile, map[string]any{"managementUrl": "ftp://x"})
	m := NewManager(store)
	m.Disabled = true
	if err := m.Load(); !errors.Is(err, ErrInvalid) || m.Status().Error == "" {
		t.Fatal(err)
	}
}
