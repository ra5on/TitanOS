package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// twoNodes returns A (admin) and B (member) in one cluster.
func twoNodes(t *testing.T) (*testNode, *testNode) {
	t.Helper()
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	if err := b.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func TestWayOf(t *testing.T) {
	n := Node{Extra: []string{"nas.example.org:443"}, Ways: map[string]string{"10.9.0.2:443": WayWireGuard}}
	for address, want := range map[string]string{
		"192.168.1.20:443":        WayHome,
		"100.83.25.99:443":        WayTailscale,
		"[fd7a:115c:a1e0::1]:443": WayTailscale,
		"10.9.0.2:443":            WayWireGuard,
		"nas.example.org:443":     WayExtra,
		"thor.tail.ts.net:443":    WayExtra,
		"203.0.113.7:443":         WayInternet,
	} {
		if got := wayOf(n, address); got != want {
			t.Errorf("wayOf(%s) = %s, want %s", address, got, want)
		}
	}
	ip := netip.MustParseAddr("10.9.0.2")
	for iface, want := range map[string]string{"wg0": WayWireGuard, "titanoswg": WayWireGuard, "tailscale0": WayTailscale, "wt0": WayNetBird, TunnelInterface: WayTunnel, "eth0": WayHome} {
		if got := wayOfInterface(iface, ip); got != want {
			t.Errorf("wayOfInterface(%s) = %s, want %s", iface, got, want)
		}
	}
}

func TestWaysOrderAndOnly(t *testing.T) {
	m := NewManager(nil)
	n := Node{ID: "b", Addresses: []string{"192.168.1.30:443", "100.83.25.99:443", "10.77.0.2:443"},
		Ways: map[string]string{"10.77.0.2:443": WayTunnel}}
	m.state = &State{Nodes: []Node{n}}
	if got := m.ordered(n, forManage); !slices.Equal(got, n.Addresses) {
		t.Fatalf("automatic keeps the order: %v", got)
	}
	m.state.Ways = Ways{Manage: WayTailscale}
	if got := m.ordered(n, forManage); got[0] != "100.83.25.99:443" || len(got) != 3 {
		t.Fatalf("tailscale first: %v", got)
	}
	m.state.Ways = Ways{Manage: WayTailscale, Only: true, Backup: WayHome}
	if got := m.ordered(n, forManage); !slices.Equal(got, []string{"100.83.25.99:443"}) {
		t.Fatalf("only tailscale: %v", got)
	}
	if got := m.ordered(n, forBackup); !slices.Equal(got, []string{"192.168.1.30:443"}) {
		t.Fatalf("backups only on the home network: %v", got)
	}
	// The exception for the node wins.
	m.state.Routes = map[string]Route{"b": {Manage: WayTunnel}}
	if got := m.ordered(n, forManage); got[0] != "10.77.0.2:443" || len(got) != 3 {
		t.Fatalf("exception: tunnel first: %v", got)
	}
	// The last good address stays first only while it is allowed.
	m.state.Routes = nil
	m.state.Ways = Ways{Manage: WayHome, Only: true}
	m.reach["b"] = "100.83.25.99:443"
	if got := m.candidates(n, forManage); !slices.Equal(got, []string{"192.168.1.30:443"}) {
		t.Fatalf("forbidden last address still tried: %v", got)
	}
	if err := (Ways{Manage: "carrier-pigeon"}).check(); err == nil {
		t.Fatal("unknown way accepted")
	}
}

func TestWaysReachTheOthersAndReturnToPreferred(t *testing.T) {
	ctx := context.Background()
	a, b := twoNodes(t)
	if err := a.m.SetWays(ctx, Ways{Manage: WayHome, Return: 1, Alert: 30}); err != nil {
		t.Fatal(err)
	}
	if err := a.m.SetRoute(ctx, b.m.ID(), Route{Backup: WayTailscale}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "ways on B", func() bool {
		v := b.m.View()
		return v.Ways.Manage == WayHome && v.Ways.Alert == 30 && v.Routes[b.m.ID()].Backup == WayTailscale
	})
	if err := b.m.SetWays(ctx, Ways{}); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("member changed the ways: %v", err)
	}

	// B answers on two addresses: its own, named Tailscale, and the same
	// port written another way, named home network. A reaches it on
	// Tailscale and goes back to the home network.
	addr := b.srv.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	home := net.JoinHostPort("::ffff:127.0.0.1", port)
	a.m.mu.Lock()
	i := slices.IndexFunc(a.m.state.Nodes, func(n Node) bool { return n.ID == b.m.ID() })
	a.m.state.Nodes[i].Addresses = []string{home, addr}
	a.m.state.Nodes[i].Ways = map[string]string{home: WayHome, addr: WayTailscale}
	a.m.reach[b.m.ID()] = addr
	a.m.mu.Unlock()
	view := func() NodeView {
		for _, v := range a.m.Overview(ctx) {
			if v.ID == b.m.ID() {
				return v
			}
		}
		return NodeView{}
	}
	if v := view(); v.Way != WayTailscale || !v.Fallback || v.Preferred != WayHome || v.Millis == 0 || !v.Compatible {
		t.Fatalf("overview on the fallback: %+v", v)
	}
	a.m.returnToPreferred(ctx)
	a.m.mu.Lock()
	got := a.m.reach[b.m.ID()]
	a.m.mu.Unlock()
	if got != home {
		t.Fatalf("not back on the home network: %s", got)
	}

	probes := a.m.Probes(ctx)
	if len(probes) != 1 || len(probes[0].Probes) != 2 {
		t.Fatalf("probes %+v", probes)
	}
	for _, p := range probes[0].Probes {
		if !p.OK || p.Used != (p.Address == home) {
			t.Fatalf("probe %+v", p)
		}
	}
}

func TestOtherVersionIsManaged(t *testing.T) {
	ctx := context.Background()
	a, b := twoNodes(t)
	b.mu.Lock()
	b.version = "0.9.0"
	b.mu.Unlock()
	req := httptest.NewRequest("GET", "/api/v1/cluster/nodes/x/api/docker", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	a.m.Proxy(rec, req, b.m.ID(), "alex", "docker")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/api/v1/docker") {
		t.Fatalf("other version not managed: %d %s", rec.Code, rec.Body)
	}
	for _, v := range a.m.Overview(ctx) {
		if !v.Compatible {
			t.Fatalf("node of another version shown incompatible: %+v", v)
		}
	}
}

func TestProtocolLevels(t *testing.T) {
	if !Compatible(0) || !Compatible(Protocol) || Compatible(Protocol+1) {
		t.Fatal("levels")
	}
	for path, method := range map[string]string{"update": "GET", "update/install": "POST", "system": "GET", "system/power": "POST", "logs": "GET"} {
		if !limited(method, path) {
			t.Errorf("%s %s should reach a node of another level", method, path)
		}
	}
	for path, method := range map[string]string{"docker": "GET", "system/ssh": "PUT", "logs": "DELETE", "system": "PUT", "shares": "GET"} {
		if limited(method, path) {
			t.Errorf("%s %s should not reach a node of another level", method, path)
		}
	}
}

func TestHandOverAndTakeOver(t *testing.T) {
	ctx := context.Background()
	a, b := twoNodes(t)
	c := newTestNode(t, "nas-c", map[string]string{"carl": "password-of-c"})
	if err := c.m.Join(ctx, a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "C everywhere", func() bool { return len(b.m.View().Nodes) == 3 })
	if err := b.m.HandOver(ctx, b.m.ID()); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("member took the role: %v", err)
	}
	if err := a.m.HandOver(ctx, b.m.ID()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "B admin everywhere", func() bool {
		return a.m.View().Admin == b.m.ID() && b.m.View().Admin == b.m.ID() && c.m.View().Admin == b.m.ID()
	})
	if !b.m.IsAdmin() || !a.m.Manages() || c.m.Manages() {
		t.Fatal("B admin, A still manages, C member")
	}
	// The admin node may hand on to any node; while it answers, nobody
	// takes the role.
	if err := b.m.SetManage(ctx, c.m.ID(), true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "C manages", func() bool { return c.m.Manages() })
	if err := c.m.HandOver(ctx, c.m.ID()); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("took the role from an admin node that answers: %v", err)
	}
	b.srv.Close()
	if err := c.m.HandOver(ctx, c.m.ID()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "C admin on A", func() bool { return a.m.View().Admin == c.m.ID() })
	// B comes back with the old role: the higher handover wins there.
	b.m.mu.Lock()
	gen := b.m.state.AdminGen
	b.m.mu.Unlock()
	msg := func() membership {
		c.m.mu.Lock()
		defer c.m.mu.Unlock()
		return c.m.membershipLocked(c.m.state, c.m.secrets)
	}()
	if msg.AdminGen <= gen {
		t.Fatalf("admin generation %d not above %d", msg.AdminGen, gen)
	}
	if err := b.m.takeMembership(c.m.ID(), msg); err != nil {
		t.Fatal(err)
	}
	if b.m.View().Admin != c.m.ID() {
		t.Fatal("old admin node kept the role")
	}
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	conf  string
}

func (f *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name == "wg" && len(args) == 3 {
		raw, _ := os.ReadFile(args[2])
		f.conf = string(raw)
	}
	if name == "ip" && slices.Equal(args, []string{"link", "show", "dev", TunnelInterface}) {
		return "", errors.New("no such device")
	}
	return "", nil
}

func TestTunnelOfTheCluster(t *testing.T) {
	ctx := context.Background()
	tunnelDir = t.TempDir()
	a, b := twoNodes(t)
	ra, rb := &fakeRunner{}, &fakeRunner{}
	a.m.Runner, b.m.Runner = ra, rb
	if err := a.m.SetTunnel(ctx, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "tunnel on B", func() bool { return b.m.View().Tunnel.Enabled })
	va := a.m.View().Tunnel
	if va.Network != "10.77.0.0/24" || va.Address == "" || va.Port != TunnelPort {
		t.Fatalf("tunnel %+v", va)
	}
	// Each node makes its key and tells the others; then both have a peer.
	a.m.applyTunnel(ctx)
	b.m.applyTunnel(ctx)
	eventually(t, "keys known", func() bool {
		for _, n := range a.m.View().Nodes {
			if n.TunnelKey == "" {
				return false
			}
		}
		return true
	})
	a.m.applyTunnel(ctx)
	ra.mu.Lock()
	conf, calls := ra.conf, strings.Join(ra.calls, "\n")
	ra.mu.Unlock()
	bAddr := a.m.View().Nodes[1].Addresses[0]
	host, _, _ := net.SplitHostPort(bAddr)
	if !strings.Contains(conf, "[Peer]") || !strings.Contains(conf, "Endpoint = "+net.JoinHostPort(host, "51821")) || !strings.Contains(conf, "AllowedIPs = 10.77.0.") {
		t.Fatalf("configuration:\n%s", conf)
	}
	if !strings.Contains(calls, "ip link add dev titanoscl type wireguard") || !strings.Contains(calls, "ip address add "+va.Address+"/24 dev titanoscl") {
		t.Fatalf("commands:\n%s", calls)
	}
	if strings.Contains(calls, "PrivateKey") {
		t.Fatal("key on the command line")
	}
	if entries, _ := os.ReadDir(tunnelDir); len(entries) != 0 {
		t.Fatal("configuration with the key left in /run")
	}
	var self self
	if err := a.store.ReadJSON(nodeFile, &self); err != nil || !validKey(self.TunnelKey) {
		t.Fatalf("key not kept: %v", err)
	}
	if err := a.m.SetTunnel(ctx, false); err != nil {
		t.Fatal(err)
	}
	a.m.applyTunnel(ctx)
	ra.mu.Lock()
	last := ra.calls[len(ra.calls)-1]
	ra.mu.Unlock()
	if last != "ip link del dev titanoscl" {
		t.Fatalf("tunnel not taken down: %s", last)
	}
	if pickTunnelNetwork([]string{"10.77.0.5:443"}) != "10.78.77.0/24" {
		t.Fatal("tunnel network overlaps an own address")
	}
}

func TestUnreachableNodeIsReported(t *testing.T) {
	ctx := context.Background()
	a, b := twoNodes(t)
	var mu sync.Mutex
	var notes []string
	a.m.Notify = func(kind, subject string) {
		mu.Lock()
		notes = append(notes, kind+" "+subject)
		mu.Unlock()
	}
	b.srv.Close()
	a.m.watchNodes(ctx)
	a.m.mu.Lock()
	a.m.down[b.m.ID()] = time.Now().Add(-11 * time.Minute)
	a.m.mu.Unlock()
	a.m.watchNodes(ctx)
	a.m.watchNodes(ctx)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(notes, []string{"cluster_node_unreachable nas-b"}) {
		t.Fatalf("notices %v", notes)
	}
}

func TestUpdateAllNodes(t *testing.T) {
	ctx := context.Background()
	updatePoll = 20 * time.Millisecond
	a, b := twoNodes(t)
	b.api = func(w http.ResponseWriter, r *http.Request, id Identity) bool {
		switch r.URL.Path {
		case "/api/v1/update/check":
			json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0", "available": map[string]string{"version": "1.1.0"}})
		case "/api/v1/update/install":
			if id.User != "alex" {
				http.Error(w, "who?", http.StatusForbidden)
				return true
			}
			go func() {
				time.Sleep(50 * time.Millisecond)
				b.mu.Lock()
				b.version = "1.1.0"
				b.mu.Unlock()
			}()
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			return false
		}
		return true
	}
	selfDone := make(chan struct{})
	a.m.Update = func(context.Context) (string, string, error) {
		close(selfDone)
		return "1.0.0", "1.1.0", nil
	}
	if err := a.m.UpdateAll(ctx, "alex", []string{a.m.ID(), b.m.ID()}, false); err != nil {
		t.Fatal(err)
	}
	if err := a.m.UpdateAll(ctx, "alex", []string{b.m.ID()}, false); !errors.Is(err, ErrRunning) {
		t.Fatalf("second run: %v", err)
	}
	eventually(t, "run done", func() bool { r := a.m.UpdateRunState(); return r != nil && !r.Running })
	<-selfDone
	r := a.m.UpdateRunState()
	if r.Nodes[0].ID != b.m.ID() || r.Nodes[0].State != "done" || r.Nodes[0].To != "1.1.0" {
		t.Fatalf("B: %+v", r.Nodes[0])
	}
	if r.Nodes[1].ID != a.m.ID() || r.Nodes[1].State != "restarting" {
		t.Fatalf("this node last: %+v", r.Nodes[1])
	}
}
