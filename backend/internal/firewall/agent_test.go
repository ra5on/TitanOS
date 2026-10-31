package firewall

import (
	"context"
	"errors"
	"testing"
)

func TestInProcessChanges(t *testing.T) {
	m, _, store := newTestManager(t)
	cfg := Defaults()
	cfg.Enabled = true
	store.WriteJSON(settingsFile, toFile(cfg))
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	off := Defaults()
	if err := m.CheckProposal(off); !errors.Is(err, ErrSwitchManual) {
		t.Fatalf("switching off allowed: %v", err)
	}
	if err := m.ChangeInProcess(context.Background(), off); !errors.Is(err, ErrSwitchManual) || !m.Enabled() {
		t.Fatalf("switched off: %v", err)
	}
	closed := withPolicies(cfg, Home, ToNAS, Policy{Action: Deny, Protocol: "any", Service: "webui"})
	if err := m.CheckProposal(closed); !errors.Is(err, ErrClosesWebUI) {
		t.Fatalf("closing the web UI allowed: %v", err)
	}
	// A rule for one sender is the trial's business, not this check's.
	one := withPolicies(cfg, Home, ToNAS, Policy{Action: Deny, Protocol: "tcp", Ports: "443", Source: "192.168.1.20"})
	if err := m.CheckProposal(one); err != nil {
		t.Fatal(err)
	}
	// A rule with a sender that covers a whole network of the NAS or the
	// VPN shuts out just as much as one without.
	m.HomeNetworks = func() []string { return []string{"192.168.1.0/24"} }
	for name, wide := range map[string]Config{
		"home network":  withPolicies(cfg, Home, ToNAS, Policy{Action: Deny, Protocol: "tcp", Ports: "443", Source: "192.168.0.0/16"}),
		"own network":   withPolicies(cfg, Home, ToNAS, Policy{Action: Deny, Protocol: "any", Service: "webui", Source: "192.168.1.0/24"}),
		"tailscale/vpn": withPolicies(cfg, VPN, ToNAS, Policy{Action: Deny, Protocol: "any", Service: "webui", Source: "100.64.0.0/10"}),
	} {
		if err := m.CheckProposal(wide); !errors.Is(err, ErrClosesWebUI) {
			t.Errorf("%s shut out: %v", name, err)
		}
	}
	if err := m.CheckProposal(one); err != nil {
		t.Fatal(err)
	}
	ssh := withPolicies(cfg, Internet, ToNAS, Policy{Action: Allow, Protocol: "any", Service: "ssh", Comment: "agent"})
	if err := m.ChangeInProcess(context.Background(), ssh); err != nil {
		t.Fatal(err)
	}
	status := m.Status("")
	if status.Pending == nil || status.Pending.Cell(Internet, ToNAS).Policies[0].Service != "ssh" || status.Config.Cell(Internet, ToNAS).Policies[0].Service == "ssh" {
		t.Fatalf("not on trial: %+v", status.Pending)
	}
	if err := m.CheckProposal(ssh); !errors.Is(err, ErrPending) {
		t.Fatalf("second proposal during trial: %v", err)
	}
	if err := m.Revert(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckProposal(withPolicies(cfg, Home, ToNAS, Policy{Action: "maybe", Protocol: "any"})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid policy: %v", err)
	}
}

func TestInProcessChangesWhileOff(t *testing.T) {
	m, _, store := newTestManager(t)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := withPolicies(Defaults(), Home, ToNAS, Policy{Action: Deny, Protocol: "tcp", Ports: "8080"})
	if err := m.ChangeInProcess(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	status := m.Status("")
	if status.Pending != nil || status.Config.Enabled || status.Config.Cell(Home, ToNAS).Policies[0].Ports != "8080" {
		t.Fatalf("rule of a switched-off firewall not stored: %+v", status.Config.Cell(Home, ToNAS))
	}
	var file fileConfig
	if err := store.ReadJSON(settingsFile, &file); err != nil || file.Enabled {
		t.Fatalf("stored: %v %+v", err, file.Enabled)
	}
	enabled := Defaults()
	enabled.Enabled = true
	if err := m.CheckProposal(enabled); !errors.Is(err, ErrSwitchManual) {
		t.Fatalf("switching on allowed: %v", err)
	}
}
