package startup

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestReadyWaitsForSharesAndExtra(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	settled := false
	var applied time.Time
	m := NewManager(nil)
	m.now = func() time.Time { return now }
	m.Settled = func() bool { return settled }
	m.SharesApplied = func() time.Time { return applied }
	_ = m.Save(Settings{Extra: 30})

	applied = now.Add(-time.Minute) // applied before the volumes settled: does not count
	if m.Ready() {
		t.Fatal("ready before the volumes settled")
	}
	settled = true
	if m.Ready() {
		t.Fatal("ready before the shares were applied after settling")
	}
	now = now.Add(5 * time.Second)
	applied = now
	if m.Ready() {
		t.Fatal("ready before the extra wait")
	}
	now = now.Add(31 * time.Second)
	if !m.Ready() {
		t.Fatal("not ready after the extra wait")
	}
	applied = time.Time{} // later failures do not close it again
	if !m.Ready() {
		t.Fatal("closed again")
	}
}

func TestReadyGivesUpOnFailingShares(t *testing.T) {
	now := time.Now()
	m := NewManager(nil)
	m.now = func() time.Time { return now }
	m.Settled = func() bool { return true }
	m.SharesApplied = func() time.Time { return time.Time{} }
	if m.Ready() {
		t.Fatal("ready without shares")
	}
	now = now.Add(giveUp + time.Second)
	if !m.Ready() {
		t.Fatal("never starts when the shares keep failing")
	}
}

func TestNoWaitAndValidation(t *testing.T) {
	m := NewManager(nil)
	m.Settled = func() bool { return false }
	_ = m.Save(Settings{NoWait: true})
	if !m.Ready() {
		t.Fatal("NoWait must not wait")
	}
	for _, bad := range []Settings{
		{Extra: -1},
		{Extra: MaxWait + 1},
		{Order: []Entry{{Kind: "lxc", Name: "x"}}},
		{Order: []Entry{{Kind: "vm", Name: ""}}},
		{Order: []Entry{{Kind: "vm", Name: "a", Wait: 9999}}},
		{Order: []Entry{{Kind: "vm", Name: "a"}, {Kind: "vm", Name: "a"}}},
	} {
		if err := m.Save(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestSettingsPersist(t *testing.T) {
	store := config.Open(filepath.Join(t.TempDir(), "config"))
	m := NewManager(store)
	want := Settings{Extra: 20, Order: []Entry{{Kind: "vm", Name: "ha", Wait: 30}, {Kind: "app", Name: "immich", Wait: 10}}}
	if err := m.Save(want); err != nil {
		t.Fatal(err)
	}
	got := NewManager(store).Settings()
	if got.Extra != 20 || len(got.Order) != 2 || got.Order[0].Name != "ha" || got.Order[1].Wait != 10 {
		t.Fatalf("read back: %+v", got)
	}
}

func TestRunStartsInOrderOnceAndGivesBack(t *testing.T) {
	store := config.Open(filepath.Join(t.TempDir(), "config"))
	m := NewManager(store)
	m.Settled = func() bool { return true }
	m.SharesApplied = func() time.Time { return time.Now() }
	_ = m.Save(Settings{Order: []Entry{
		{Kind: "vm", Name: "ha", Wait: 1},
		{Kind: "app", Name: "photos"},
		{Kind: "app", Name: "old", Off: true},
	}})
	_ = m.Hold(map[string]Policy{"c1": {Name: "unless-stopped"}})
	var got []string
	var at []time.Time
	var restored map[string]Policy
	var engine atomic.Bool
	done := filepath.Join(t.TempDir(), "run", "done")
	steps := Steps{
		EngineReady: func() bool { return engine.Load() },
		StartVM: func(_ context.Context, n string) error {
			got = append(got, "vm:"+n)
			at = append(at, time.Now())
			return nil
		},
		StartApp: func(_ context.Context, n string) error {
			got = append(got, "app:"+n)
			at = append(at, time.Now())
			return nil
		},
		Restore:  func(_ context.Context, h map[string]Policy) error { restored = h; return nil },
		DoneFile: done,
	}
	go func() { time.Sleep(300 * time.Millisecond); engine.Store(true) }()
	m.Run(context.Background(), steps)
	if len(got) != 2 || got[0] != "vm:ha" || got[1] != "app:photos" {
		t.Fatalf("order: %v", got)
	}
	if at[1].Sub(at[0]) < time.Second {
		t.Errorf("no wait after the VM: %v", at[1].Sub(at[0]))
	}
	if restored["c1"].Name != "unless-stopped" || len(m.Held()) != 0 {
		t.Errorf("policies: %+v, left %+v", restored, m.Held())
	}
	// A second run in the same boot (titand restarted) starts nothing.
	got = nil
	m.Run(context.Background(), steps)
	if len(got) != 0 {
		t.Errorf("started twice: %v", got)
	}
}
