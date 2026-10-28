package network

import (
	"context"
	"errors"
	"testing"
)

func TestVLANOptionOffByDefaultAndSaved(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(context.Background()); st.VLANs || st.VLANsInUse {
		t.Fatalf("VLANs on by default: %+v", st)
	}
	if err := m.SetOptions(Options{VLANs: true}); err != nil {
		t.Fatal(err)
	}
	// A new manager on the same stick reads the saved choice.
	again := NewManager(m.store)
	again.dir, again.sys, again.runner = m.dir, m.sys, m.runner
	if err := again.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !again.Status(context.Background()).VLANs {
		t.Fatal("saved VLAN option lost")
	}
	if err := again.SetOptions(Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestVLANOptionCannotHideUsedVLANs(t *testing.T) {
	m, _ := newTestManager(t)
	m.Load(context.Background())
	m.mu.Lock()
	m.active.Networks = []LogicalNetwork{{ID: "admin", Parent: "enp1s0", VLAN: 10, Management: true, IPv4: "dhcp"}}
	m.mu.Unlock()
	st := m.Status(context.Background())
	if !st.VLANs || !st.VLANsInUse {
		t.Fatalf("used VLAN not shown: %+v", st)
	}
	if err := m.SetOptions(Options{}); !errors.Is(err, ErrVLANsInUse) {
		t.Fatalf("switched off with a VLAN in use: %v", err)
	}
}
