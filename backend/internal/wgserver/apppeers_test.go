package wgserver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	_, public, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return public
}

func TestAppPeerNeedsTheServerOn(t *testing.T) {
	m, _, _ := fixture(t)
	if _, err := m.SetAppPeer(context.Background(), "alex", "dev1", "Pixel", newKey(t)); !errors.Is(err, ErrOff) {
		t.Fatalf("before switching on: %v", err)
	}
	if _, ok := m.AppServer(); ok || m.Ready() {
		t.Fatal("server ready while off")
	}
}

func TestAppPeerMakeAnewRemoveAndKeys(t *testing.T) {
	m, f, store := fixture(t)
	ctx := context.Background()
	if err := m.Set(ctx, Settings{Enabled: true, Endpoint: "nas.example.org", DNS: "192.168.178.1"}); err != nil {
		t.Fatal(err)
	}
	manual, _, err := m.AddPeer(ctx, "Pixel 10 Pro XL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetAppPeer(ctx, "alex", "dev1", "Pixel", "not a key"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := m.SetAppPeer(ctx, "alex", "dev1", "Pixel", manual.PublicKey); !errors.Is(err, ErrKeyUsed) {
		t.Fatalf("key of another device: %v", err)
	}
	server, ok := m.AppServer()
	if !ok || server.Address != "10.254.13.1" || server.Port != DefaultPort || server.Endpoint != "nas.example.org" ||
		len(server.DNS) != 1 || server.DNS[0] != "192.168.178.1" || len(server.HomeNetworks) != 2 || server.Keepalive != 25 {
		t.Fatalf("server %+v", server)
	}
	if _, err := m.SetAppPeer(ctx, "alex", "dev1", "Pixel", server.PublicKey); !errors.Is(err, ErrKeyUsed) {
		t.Fatalf("server's key: %v", err)
	}
	key := newKey(t)
	// The phone has the name of a device of the page: " (App)" makes it unique.
	peer, err := m.SetAppPeer(ctx, "alex", "dev1", "pixel 10 pro xl\"", key)
	if err != nil {
		t.Fatal(err)
	}
	if peer.Name != "pixel 10 pro xl (App)" || peer.Address != "10.254.13.3/32" || peer.Preshared == "" || !peer.App() {
		t.Fatalf("peer %+v", peer)
	}
	if !strings.Contains(f.conf, "PublicKey = "+key) {
		t.Fatalf("not on the interface: %s", f.conf)
	}
	// Make anew: same address, new key and preshared key, still one device.
	again := newKey(t)
	renewed, err := m.SetAppPeer(ctx, "alex", "dev1", "pixel 10 pro xl", again)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Address != peer.Address || renewed.Preshared == peer.Preshared || len(m.Settings().Peers) != 2 {
		t.Fatalf("renewed %+v, peers %+v", renewed, m.Settings().Peers)
	}
	if strings.Contains(f.conf, key) || !strings.Contains(f.conf, again) {
		t.Fatalf("old key still on the interface: %s", f.conf)
	}
	// Stored like the other devices, with the owner; no private key exists.
	data, _ := store.Read(settingsFile)
	if !strings.Contains(string(data), `"account": "alex"`) || !strings.Contains(string(data), `"device": "dev1"`) {
		t.Fatalf("stored %s", data)
	}
	// Another app gets the next address; another account cannot take the key.
	if _, err := m.SetAppPeer(ctx, "sam", "dev9", "Pixel", again); !errors.Is(err, ErrKeyUsed) {
		t.Fatalf("key of alex's phone: %v", err)
	}
	second, err := m.SetAppPeer(ctx, "sam", "dev9", "", newKey(t))
	if err != nil || second.Address != "10.254.13.4/32" || second.Name != "Android" {
		t.Fatalf("second %+v %v", second, err)
	}
	if got, ok := m.AppPeer("alex", "dev1"); !ok || got.PublicKey != again {
		t.Fatalf("AppPeer %+v %v", got, ok)
	}
	if removed, err := m.RemoveAppPeer(ctx, "alex", "dev1"); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if removed, err := m.RemoveAppPeer(ctx, "alex", "dev1"); err != nil || removed {
		t.Fatalf("remove twice: %v %v", removed, err)
	}
	if strings.Contains(f.conf, again) {
		t.Fatal("removed device still on the interface")
	}
	if _, ok := m.AppPeer("alex", "dev1"); ok {
		t.Fatal("still there")
	}
	// The page lists it and the status says whose it is.
	st := m.Status()
	if len(st.Peers) != 2 || st.Peers[1].Account != "sam" || !st.Peers[1].Active || st.Peers[0].Account != "" {
		t.Fatalf("status %+v", st.Peers)
	}
}

func TestAppPeersFollowTheirApps(t *testing.T) {
	m, f, store := fixture(t)
	ctx := context.Background()
	if err := m.Set(ctx, Settings{Enabled: true, Endpoint: "nas.example.org"}); err != nil {
		t.Fatal(err)
	}
	live := map[string]bool{"alex/dev1": true, "sam/dev2": true}
	known := true
	m.AppPeerLive = func(account, device string) (bool, bool) { return live[account+"/"+device], known }
	m.AppPeerCached = m.AppPeerLive
	k1, k2 := newKey(t), newKey(t)
	if _, err := m.SetAppPeer(ctx, "alex", "dev1", "Pixel", k1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetAppPeer(ctx, "sam", "dev2", "Pixel", k2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.AddPeer(ctx, "Laptop"); err != nil {
		t.Fatal(err)
	}
	// Ended lazily (ran out, account locked): out of the tunnel at the next
	// check, nothing written.
	live["sam/dev2"] = false
	before, _ := store.Read(settingsFile)
	// The timer judges from RAM only: AppPeerLive (which may read /config)
	// is not asked, neither for comparing nor for the interface.
	liveFunc, asked := m.AppPeerLive, 0
	m.AppPeerLive = func(account, device string) (bool, bool) { asked++; return liveFunc(account, device) }
	if err := m.Recheck(ctx); err != nil {
		t.Fatal(err)
	}
	if m.AppPeerLive = liveFunc; asked != 0 {
		t.Fatalf("recheck asked AppPeerLive %d times", asked)
	}
	if strings.Contains(f.conf, k2) || !strings.Contains(f.conf, k1) {
		t.Fatalf("interface %s", f.conf)
	}
	if after, _ := store.Read(settingsFile); string(after) != string(before) {
		t.Fatal("recheck wrote the stick")
	}
	calls := len(f.calls)
	if err := m.Recheck(ctx); err != nil || len(f.calls) != calls {
		t.Fatalf("recheck without change ran %v", f.calls[calls:])
	}
	if st := m.Status(); st.Peers[1].Active {
		t.Fatalf("status %+v", st.Peers)
	}
	// Not readable now: nothing is removed.
	known = false
	if n, err := m.PruneAppPeers(ctx); err != nil || n != 0 {
		t.Fatalf("prune while unknown: %d %v", n, err)
	}
	known = true
	if n, err := m.PruneAppPeers(ctx); err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	peers := m.Settings().Peers
	if len(peers) != 2 || peers[0].Account != "alex" || peers[1].Name != "Laptop" {
		t.Fatalf("after prune %+v", peers)
	}
	if n, err := m.PruneAppPeers(ctx); err != nil || n != 0 {
		t.Fatalf("prune again: %d %v", n, err)
	}
}
