package tailscale

import (
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls  []string
	fail   bool
	status string
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.fail {
		return "", errors.New("command failed")
	}
	return f.status, nil
}
func fixture(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	m := NewManager(config.Open(t.TempDir()))
	m.runtime = config.Open(t.TempDir())
	m.mountRoot = t.TempDir()
	os.Mkdir(filepath.Join(m.mountRoot, "data"), 0700)
	f := &fakeRunner{}
	m.runner = f
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare("tailscale", "data"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m.timer != nil {
			m.timer.Stop()
		}
	})
	return m, f
}
func TestSettingsValidation(t *testing.T) {
	for _, s := range []Settings{{Routes: []string{"0.0.0.0/0"}}, {Routes: []string{"::/0"}}, {Routes: []string{"127.0.0.0/8"}}, {Routes: []string{"fe80::/64"}}, {Routes: []string{"192.168.1.0/24\nX=true"}}, {ExitNode: "8.8.8.8"}, {ExitNode: "100.64.1.2", AdvertiseExit: true}} {
		if _, err := normalize(s); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	s, err := normalize(Settings{Routes: []string{"192.168.1.3/24", "fd12::1/64", "192.168.1.0/24"}, ExitNode: "100.64.1.2"})
	if err != nil || len(s.Routes) != 2 || s.Routes[0] != "192.168.1.0/24" {
		t.Fatalf("normalize %+v %v", s, err)
	}
	if !strings.Contains(strings.Join(args(s), " "), "--exit-node-allow-lan-access=true") {
		t.Fatal("LAN access lost")
	}
}
func TestTrialConfirmAndUndo(t *testing.T) {
	m, _ := fixture(t)
	choice := Settings{Routes: []string{"192.168.10.0/24"}, AdvertiseExit: true}
	if err := m.Set(choice); err != nil {
		t.Fatal(err)
	}
	if m.store.Changes() != 0 || m.pending == nil {
		t.Fatal("trial persisted early")
	}
	if !errors.Is(m.Set(Settings{}), ErrPending) {
		t.Fatal("overlapping change")
	}
	if err := m.Undo(); err != nil {
		t.Fatal(err)
	}
	if m.settings.AdvertiseExit || m.pending != nil {
		t.Fatal("rollback failed")
	}
	if err := m.Set(choice); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(m.store)
	restored.Disabled = true
	if err := restored.Load(); err != nil || !restored.settings.AdvertiseExit {
		t.Fatalf("persistence %v", err)
	}
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
	m.Set(Settings{AcceptDNS: true})
	f.fail = true
	if err := m.Undo(); err == nil || m.pending == nil {
		t.Fatal("failed rollback lost")
	}
	f.fail = false
	if err := m.Undo(); err != nil {
		t.Fatal(err)
	}
	if m.settings.AcceptDNS {
		t.Fatal("rollback changed stored settings")
	}
}
func TestConfirmStorageFailureKeepsRollback(t *testing.T) {
	m, _ := fixture(t)
	m.Set(Settings{AcceptDNS: true})
	os.MkdirAll(filepath.Join(m.store.Dir(), "plugins/tailscale/settings.json"), 0700)
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
func TestStateAndLoginURL(t *testing.T) {
	m, f := fixture(t)
	f.status = `{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/test","TailscaleIPs":["100.64.1.2","fd7a:115c:a1e0::2"],"Peer":{"key":{"ID":"n1","DNSName":"exit.example.","TailscaleIPs":["100.64.1.3"],"ExitNodeOption":true,"Online":true}}}`
	s := m.Status()
	if !s.Active || s.AuthURL == "" || len(s.IPs) != 2 || len(s.ExitNodes) != 1 {
		t.Fatalf("%+v", s)
	}
	for _, url := range []string{"http://login.tailscale.com/a/x", "https://login.tailscale.com.evil/a/x", "javascript:alert(1)", "https://user@login.tailscale.com/a/x"} {
		if loginURL(url) != "" {
			t.Fatal("unsafe link", url)
		}
	}
	before := m.store.Changes()
	m.Status()
	if m.store.Changes() != before {
		t.Fatal("status wrote config")
	}
}
func TestDisabledAndUnsafeVolume(t *testing.T) {
	m, f := fixture(t)
	m.Disabled = true
	if err := m.Set(Settings{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := m.Action("connect"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatal("dev ran commands")
	}
	m.Disabled = false
	if err := m.Prepare("tailscale", "../else"); err == nil {
		t.Fatal("unsafe volume")
	}
	os.RemoveAll(filepath.Join(m.mountRoot, "data/.titanos/tailscale"))
	os.Symlink(t.TempDir(), filepath.Join(m.mountRoot, "data/.titanos/tailscale"))
	if err := m.Prepare("tailscale", "data"); err == nil {
		t.Fatal("symlink state path")
	}
}

func TestDaemonRestartReappliesOnlySavedRouting(t *testing.T) {
	m, f := fixture(t)
	if err := m.Set(Settings{AcceptDNS: true}); err != nil {
		t.Fatal(err)
	}
	m.timer.Stop()
	restored := NewManager(m.store)
	restored.runtime, restored.mountRoot, restored.runner = m.runtime, m.mountRoot, f
	if err := restored.Load(); err != nil {
		t.Fatal(err)
	}
	if err := restored.Prepare("tailscale", "data"); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	if err := restored.Activate("tailscale"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.runtime.Dir(), "runtime"))
	if err != nil || !strings.Contains(string(data), "TAILSCALE_ACCEPT_DNS=false") || len(f.calls) != 1 {
		t.Fatal("unconfirmed state survived restart", string(data), f.calls, err)
	}
}

func TestStateRejectsWritableAncestors(t *testing.T) {
	for _, directory := range []string{".titanos", ".titanos/tailscale"} {
		t.Run(directory, func(t *testing.T) {
			m, _ := fixture(t)
			path := filepath.Join(m.mountRoot, "data", directory)
			if err := os.Chmod(path, 0777); err != nil {
				t.Fatal(err)
			}
			if err := m.Prepare("tailscale", "data"); err == nil {
				t.Fatal("accepted externally writable private state directory")
			}
		})
	}
}
