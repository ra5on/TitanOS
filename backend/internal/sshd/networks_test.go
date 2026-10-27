package sshd

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

func TestNormalizeNetworks(t *testing.T) {
	got, err := NormalizeNetworks([]string{" 192.168.1.17/24 ", "10.0.0.1", "fd7a:115c:a1e0::1/48", "2001:db8::1", "::ffff:192.168.5.0/120", "192.168.1.0/24", ""})
	want := []string{"192.168.1.0/24", "10.0.0.1/32", "fd7a:115c:a1e0::/48", "2001:db8::1/128", "192.168.5.0/24"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("got %v %v, want %v", got, err, want)
	}
	for _, bad := range []string{"192.168.1.0/33", "nas.local", "fe80::1%eth0", "*", "10.0.0.0/8 *@x", "::ffff:1.2.3.4/90"} {
		if _, err := NormalizeNetworks([]string{bad}); !errors.Is(err, ErrInvalidNetwork) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	many := make([]string, maxNetworks+1)
	for i := range many {
		many[i] = netip.AddrFrom4([4]byte{10, 0, byte(i), 0}).String() + "/24"
	}
	if _, err := NormalizeNetworks(many); !errors.Is(err, ErrTooManyNetworks) {
		t.Errorf("too many: %v", err)
	}
	if got, err := NormalizeNetworks(nil); err != nil || len(got) != 0 {
		t.Errorf("empty: %v %v", got, err)
	}
}

func TestCovers(t *testing.T) {
	list := []string{"192.168.1.0/24", "100.64.0.0/10", "fd7a:115c:a1e0::/48"}
	for address, want := range map[string]bool{
		"192.168.1.20":           true,
		"::ffff:192.168.1.20":    true,
		"100.101.102.103":        true,
		"fd7a:115c:a1e0:ab::1":   true,
		"192.168.2.1":            false,
		"127.0.0.1":              false,
		"2001:db8::1":            false,
		"not an address":         false,
		"fd7a:115c:a1e0::1%eth0": true,
	} {
		if Covers(list, address) != want {
			t.Errorf("%s: want %v", address, want)
		}
	}
	if !Covers(nil, "203.0.113.9") {
		t.Error("an empty list covers everything")
	}
}

func TestNetworksConfig(t *testing.T) {
	if networksConfig(nil) != "" {
		t.Error("no list, no restriction")
	}
	got := networksConfig([]string{"192.168.1.0/24", "fd7a:115c:a1e0::/48"})
	if !strings.Contains(got, "\nAllowUsers *@192.168.1.0/24 *@fd7a:115c:a1e0::/48\n") {
		t.Errorf("config: %q", got)
	}
}

func TestHomeNetworks(t *testing.T) {
	p := netip.MustParsePrefix
	got := homeNetworks([]iface{
		{name: "lo", up: true, addrs: []netip.Prefix{p("127.0.0.1/8"), p("::1/128")}},
		{name: "br0", up: true, addrs: []netip.Prefix{p("192.168.1.20/24"), p("fe80::1/64"), p("2001:db8:1::20/64")}},
		{name: "eth1", up: false, addrs: []netip.Prefix{p("10.1.0.2/16")}},
		{name: "docker0", up: true, addrs: []netip.Prefix{p("172.17.0.1/16")}},
		{name: "tailscale0", up: true, addrs: []netip.Prefix{p("100.70.1.2/32")}},
		{name: "wt0", up: true, addrs: []netip.Prefix{p("100.80.1.2/16")}},
		{name: "vlan20", up: true, addrs: []netip.Prefix{p("192.168.20.5/24"), p("192.168.1.21/24")}},
	})
	want := []string{"192.168.1.0/24", "2001:db8:1::/64", "192.168.20.0/24"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// sshdChecker lets sshd -t fail on demand.
type sshdChecker struct {
	*fakeSystem
	refuse bool
}

func (c *sshdChecker) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "sshd" && c.refuse {
		c.calls = append(c.calls, "sshd "+strings.Join(args, " "))
		return "bad option", &run.CommandError{Command: "sshd -t", Err: errors.New("exit status 255")}
	}
	return c.fakeSystem.Run(ctx, stdin, name, args...)
}

func TestNetworksAppliedWithCheckAndReload(t *testing.T) {
	m, system, _ := newManager(t)
	checker := &sshdChecker{fakeSystem: system}
	m.runner = checker
	users := []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{userKey(t)}}}
	file := filepath.Join(m.etcDir, networksFile)
	m.SetEnabled(true)
	if _, err := m.Apply(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err == nil || system.ran("sshd -t") != 0 || system.ran("systemctl reload") != 0 {
		t.Fatalf("no list wrote a restriction: %v", system.calls)
	}

	if _, err := m.SetNetworks([]string{"192.168.1.0/24", "x"}); !errors.Is(err, ErrInvalidNetwork) {
		t.Fatalf("invalid list: %v", err)
	}
	saved, err := m.SetNetworks([]string{"192.168.1.7/24"})
	if err != nil || !slices.Equal(saved, []string{"192.168.1.0/24"}) {
		t.Fatal(saved, err)
	}
	// Enabled stays as it was; the list is in /config.
	var settings Settings
	m.store.ReadJSON(settingsFile, &settings)
	if !settings.Enabled || !slices.Equal(settings.AllowedNetworks, []string{"192.168.1.0/24"}) {
		t.Errorf("settings: %+v", settings)
	}
	if _, err := m.Apply(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "AllowUsers *@192.168.1.0/24\n") {
		t.Errorf("drop-in: %q", data)
	}
	if system.ran("sshd -t") != 1 || system.ran("systemctl reload ssh.service") != 1 {
		t.Errorf("calls: %v", system.calls)
	}
	// Nothing changed: no check, no reload.
	m.Apply(context.Background(), users)
	if system.ran("sshd -t") != 1 || system.ran("systemctl reload") != 1 {
		t.Errorf("unchanged list touched sshd: %v", system.calls)
	}
	// Switching SSH off and on keeps the list.
	m.SetEnabled(false)
	m.SetEnabled(true)
	if got := m.Status().AllowedNetworks; !slices.Equal(got, []string{"192.168.1.0/24"}) {
		t.Errorf("list after switching: %v", got)
	}

	// sshd refuses: the last good file stays, sshd is not reloaded.
	checker.refuse = true
	m.SetNetworks([]string{"10.0.0.0/8"})
	starts := system.ran("systemctl start")
	if _, err := m.Apply(context.Background(), users); err == nil {
		t.Fatal("a refused configuration was applied")
	}
	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "*@192.168.1.0/24") {
		t.Errorf("last good file not kept: %q", data)
	}
	if status := m.Status(); !strings.Contains(status.NetworksError, "bad option") || system.ran("systemctl start") != starts || system.ran("systemctl reload") != 1 {
		t.Errorf("status %+v, calls %v", status, system.calls)
	}

	// The empty list lifts the restriction.
	checker.refuse = false
	m.SetNetworks(nil)
	if _, err := m.Apply(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err == nil || m.Status().NetworksError != "" || system.ran("systemctl reload") != 2 {
		t.Errorf("restriction left: %v", system.calls)
	}
}

// The local console clears the list straight in /config; other settings stay.
func TestClearNetworks(t *testing.T) {
	m, _, _ := newManager(t)
	if changed, err := ClearNetworks(m.store); changed || err != nil {
		t.Fatalf("nothing to clear: %v %v", changed, err)
	}
	m.SetEnabled(true)
	m.SetNetworks([]string{"192.168.1.0/24"})
	if changed, err := ClearNetworks(m.store); !changed || err != nil {
		t.Fatalf("clear: %v %v", changed, err)
	}
	var settings Settings
	m.store.ReadJSON(settingsFile, &settings)
	if !settings.Enabled || len(settings.AllowedNetworks) != 0 {
		t.Errorf("settings: %+v", settings)
	}
	raw, _ := m.store.Read(settingsFile)
	if strings.Contains(string(raw), "allowedNetworks") {
		t.Errorf("empty list still written: %s", raw)
	}
}

// Older titand reads ssh.json with only "enabled"; the new field must not
// trip it, and a file of an older titand must load here.
func TestSettingsCompatible(t *testing.T) {
	m, _, _ := newManager(t)
	m.store.Write(settingsFile, []byte(`{"enabled":true}`))
	if err := m.Load(); err != nil || !m.Status().Enabled || len(m.Status().AllowedNetworks) != 0 {
		t.Fatalf("old file: %v %+v", err, m.Status())
	}
	m.SetNetworks([]string{"10.0.0.0/8"})
	type oldSettings struct {
		Enabled bool `json:"enabled"`
	}
	var old oldSettings
	if err := m.store.ReadJSON(settingsFile, &old); err != nil || !old.Enabled {
		t.Errorf("as an older titand reads it: %+v %v", old, err)
	}
}
