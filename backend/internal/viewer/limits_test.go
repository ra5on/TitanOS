package viewer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdmissionLimits(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	m.MemTotal = func() (uint64, error) { return 64 << 30, nil }
	var leases []*Lease
	for i := range PerAccount {
		lease, err := m.Open(t.Context(), "anna", fmt.Sprintf("app%d", i))
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	if _, err := m.Open(t.Context(), "anna", "one-more"); !errors.Is(err, ErrLimit) {
		t.Fatalf("fifth view of an account: %v", err)
	}
	// The same app again is a takeover, not a new view.
	again, err := m.Open(t.Context(), "anna", "app0")
	if err != nil {
		t.Fatal(err)
	}
	leases = append(leases, again)
	// Memory: every view keeps its limit reserved beyond what it uses now.
	// Four views using 300 MB each still claim 4 × 724 MB.
	reserved := uint64(PerAccount) * (PerView - 300<<20)
	m.MemAvailable = func() (uint64, error) { return Reserve + reserved + PerView - 1, nil }
	if _, err := m.Open(t.Context(), "ben", "immich"); !errors.Is(err, ErrMemory) {
		t.Fatalf("without room: %v", err)
	}
	m.MemAvailable = func() (uint64, error) { return Reserve + reserved + PerView, nil }
	lease, err := m.Open(t.Context(), "ben", "immich")
	if err != nil {
		t.Fatalf("with room: %v", err)
	}
	leases = append(leases, lease)
	// Overall cap from the RAM of the NAS: (8 GiB − 2 GiB) / 1 GiB = 6 views.
	m.MemAvailable = func() (uint64, error) { return 64 << 30, nil }
	m.MemTotal = func() (uint64, error) { return 8 << 30, nil }
	if _, err := m.Open(t.Context(), "carl", "immich"); err != nil {
		t.Fatalf("sixth view: %v", err)
	}
	if _, err := m.Open(t.Context(), "dora", "immich"); !errors.Is(err, ErrMemory) {
		t.Fatalf("seventh view: %v", err)
	}
	for _, l := range leases {
		l.Close(false)
	}
}

func TestStaleContainerIsStoppedBeforeDial(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	m.Policy = func(origin string) ([]byte, error) { return []byte(origin), nil }
	lease, err := m.Open(t.Context(), "anna", "immich")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	policy := engine.created[0].Policy
	engine.mu.Lock()
	engine.stale = map[string]bool{"v1": true} // another address now
	engine.mu.Unlock()
	if err := lease.Check(t.Context()); !errors.Is(err, ErrStale) {
		t.Fatalf("stale: %v", err)
	}
	if len(engine.removed) != 1 || len(engine.networks) != 0 {
		t.Fatalf("not stopped: removed %v networks %v", engine.removed, engine.networks)
	}
	if _, err := os.Stat(filepath.Dir(policy)); !os.IsNotExist(err) {
		t.Fatal("policy folder kept")
	}
	// Opened again: a new container.
	again, err := m.Open(t.Context(), "anna", "immich")
	if err != nil || len(engine.created) != 2 {
		t.Fatalf("reopen: %v", err)
	}
	again.Close(false)
}

func TestStopWhereAndProfiles(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	a, _ := m.Open(t.Context(), "anna", "immich")
	b, _ := m.Open(t.Context(), "ben", "immich")
	a.Close(true) // waiting for its window
	m.StopWhere(func(user, app string) bool { return user == "anna" })
	if len(engine.removed) != 1 || len(m.Sessions(t.Context(), "")) != 1 {
		t.Fatalf("removed %v", engine.removed)
	}
	select {
	case <-b.Ended:
		t.Fatal("ben's view ended")
	default:
	}
	profile := filepath.Join(m.MountRoot, "data/.titanos/viewer/profiles/anna")
	if _, err := os.Stat(profile); err != nil {
		t.Fatal(err)
	}
	// A link in place of the account's folder is not followed.
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o600)
	os.Symlink(outside, filepath.Join(m.MountRoot, "data/.titanos/viewer/profiles/eve"))
	if err := m.DeleteProfiles("eve"); err == nil {
		t.Fatal("link followed")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("removed outside the profiles")
	}
	if err := m.DeleteProfiles("anna"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("profiles kept")
	}
	b.Close(false)
}

func TestHostGuardScript(t *testing.T) {
	var got string
	runner := runnerFunc(func(stdin string, args ...string) {
		got = strings.Join(args, " ") + "\n" + stdin
	})
	if err := HostGuard(runner)(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nft -f -", "delete table inet titanos_viewer", "hook input",
		`iifname "tvw*" ct state established,related accept`, `iifname "tvw*" drop`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if b := BridgeName("anna", "immich"); len(b) != 15 || !strings.HasPrefix(b, BridgePrefix) || b == BridgeName("anna", "immich2") {
		t.Fatalf("bridge %q", b)
	}
}

type runnerFunc func(stdin string, args ...string)

func (f runnerFunc) Run(_ context.Context, stdin, name string, args ...string) (string, error) {
	f(stdin, append([]string{name}, args...)...)
	return "", nil
}
