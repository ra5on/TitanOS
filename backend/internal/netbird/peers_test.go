package netbird

import (
	"testing"
	"time"
)

// testdata/status.json is shaped like `netbird status --json` of 0.81.
func TestParsePeersFromRecordedStatus(t *testing.T) {
	got, ok := parsePeers([]byte(recorded(t, "status.json")))
	if !ok || !got.Active || got.State != "Connected" || len(got.Peers) != 4 {
		t.Fatalf("parse %+v %v", got, ok)
	}
	self, laptop, router, phone := got.Peers[0], got.Peers[1], got.Peers[2], got.Peers[3]
	if !self.Self || self.Name != "titan" || self.IPv4 != "100.92.10.5" || self.IPv6 != "fd4e:4269:7264:0:a5c::1" || self.Online == nil || !*self.Online || len(self.Networks) != 1 || self.LastSeen != nil {
		t.Fatalf("self %+v", self)
	}
	if laptop.ID != "Lp0tY2dhbGFjdGljc2Vh7l2Yk3n0aXNhZG1pbmJvb2s=" || laptop.Name != "alex-laptop" || laptop.FQDN != "alex-laptop.netbird.cloud" || laptop.Connection != "p2p" || laptop.Online == nil || !*laptop.Online {
		t.Fatalf("laptop %+v", laptop)
	}
	if laptop.LastSeen == nil || !laptop.LastSeen.Equal(time.Date(2026, 10, 26, 9, 59, 47, 118000000, time.UTC)) {
		t.Fatalf("last seen %v", laptop.LastSeen)
	}
	if router.Name != "office-router" || router.Connection != "relayed" || len(router.Networks) != 1 || router.Networks[0] != "10.20.0.0/16" {
		t.Fatalf("router %+v", router)
	}
	// Idle peers are not connected; values without a handshake stay unknown;
	// the entry with an invalid key is dropped.
	if phone.Name != "phone" || phone.Online == nil || *phone.Online || phone.Connection != "" || phone.LastSeen != nil || phone.IPv6 != "" || len(phone.Networks) != 0 {
		t.Fatalf("phone %+v", phone)
	}
}

func TestPeersWithoutConnectionHaveUnknownState(t *testing.T) {
	got, ok := parsePeers([]byte(`{"daemonStatus":"Idle","publicKey":"c2VsZg==","fqdn":"nas.netbird.cloud.","peers":{"details":[{"publicKey":"cGVlcg==","fqdn":"a.netbird.cloud","status":"Connected","connectionType":"P2P"}]}}`))
	if !ok || len(got.Peers) != 2 || got.Peers[1].Online != nil || got.Peers[1].Connection != "" || got.Peers[0].Online == nil || *got.Peers[0].Online || got.Peers[0].Name != "nas" {
		t.Fatalf("stale state %+v", got)
	}
	if _, ok := parsePeers([]byte(`<html>`)); ok {
		t.Fatal("invalid status accepted")
	}
	got, ok = parsePeers([]byte(recorded(t, "status-needslogin.json")))
	if !ok || got.State != "NeedsLogin" || len(got.Peers) != 0 {
		t.Fatalf("needs login %+v", got)
	}
	got, _ = parsePeers([]byte(`{"daemonStatus":"Connected","peers":{"details":[{"publicKey":"bad id\n"},{"publicKey":"cDI="},{"publicKey":"cDI="}]}}`))
	if len(got.Peers) != 1 {
		t.Fatalf("unsafe or duplicate IDs kept: %+v", got.Peers)
	}
}

func TestPeersInactiveAndCached(t *testing.T) {
	m, f := fixture(t)
	f.fail = true
	if p := m.Peers(); p.Active || p.State != "inactive" || p.Peers == nil {
		t.Fatalf("inactive %+v", p)
	}
	f.fail = false
	f.status = recorded(t, "status.json")
	f.take()
	if p := m.Peers(); p.Active {
		t.Fatal("cache bypassed")
	}
	m.peers.at = time.Now().Add(-peersTTL)
	if p := m.Peers(); !p.Active || len(p.Peers) != 4 || !p.Peers[0].Self {
		t.Fatalf("refreshed %+v", p)
	}
	m.Peers()
	if calls := f.take(); len(calls) != 1 || calls[0] != "netbird status --json" {
		t.Fatalf("calls %v", calls)
	}
	if m.store.Changes() != 0 {
		t.Fatal("listing peers wrote config")
	}
	f.status = "{}"
	m.peers.at = time.Time{}
	if p := m.Peers(); p.Active || p.State != "unknown" {
		t.Fatalf("unknown %+v", p)
	}
}
