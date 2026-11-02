package nvr

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestAllowedAddr(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"192.168.20.41", true},
		{"10.0.0.7", true},
		{"172.16.5.5", true},
		{"fd12:3456::1", true},
		{"100.64.1.2", true},         // Tailscale
		{"::ffff:192.168.1.5", true}, // mapped
		{"127.0.0.1", false},
		{"::1", false},
		{"0.0.0.0", false},
		{"169.254.169.254", false}, // link-local, cloud metadata
		{"224.0.0.1", false},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
	} {
		if got := AllowedAddr(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.addr, got, tc.want)
		}
	}
}

type fakeResolver map[string][]string

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, a := range f[host] {
		out = append(out, netip.MustParseAddr(a))
	}
	return out, nil
}

func TestResolveRefusesNamesWithOneBadAddress(t *testing.T) {
	r := fakeResolver{"cam.lan": {"192.168.1.9"}, "rebind.example": {"192.168.1.9", "127.0.0.1"}, "far.example": {"93.184.216.34"}}
	if addrs, err := Resolve(context.Background(), r, "cam.lan"); err != nil || len(addrs) != 1 {
		t.Fatalf("cam.lan: %v %v", addrs, err)
	}
	for _, host := range []string{"rebind.example", "far.example", "127.0.0.1", "8.8.8.8"} {
		if _, err := Resolve(context.Background(), r, host); !errors.Is(err, ErrTargetNotAllowed) {
			t.Errorf("%s: %v", host, err)
		}
	}
	if addrs, err := Resolve(context.Background(), r, "192.168.1.9"); err != nil || addrs[0].String() != "192.168.1.9" {
		t.Errorf("literal: %v %v", addrs, err)
	}
}

func TestStreamURLEncodesCredentials(t *testing.T) {
	got := StreamURL("rtsp", "192.168.1.9", 554, "/Streaming/Channels/101", "viewer", "p@ss:w/rd#1")
	want := "rtsp://viewer:p%40ss%3Aw%2Frd%231@192.168.1.9:554/Streaming/Channels/101"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if q := StreamURL("rtsp", "10.0.0.2", 554, "/cam/realmonitor?channel=1&subtype=1", "u", "p"); !strings.HasSuffix(q, "/cam/realmonitor?channel=1&subtype=1") {
		t.Errorf("query kept: %s", q)
	}
	if v6 := StreamURL("rtsp", "fd12::1", 554, "/s", "", ""); v6 != "rtsp://[fd12::1]:554/s" {
		t.Errorf("v6: %s", v6)
	}
}

func TestRedactHidesPassword(t *testing.T) {
	got := Redact("rtsp://viewer:geheim@192.168.1.9:554/stream1")
	if strings.Contains(got, "geheim") || !strings.Contains(got, "viewer:REDACTED@") {
		t.Fatalf("not redacted: %s", got)
	}
	if Redact("kein link") != "kein link" || Redact("rtsp://192.168.1.9/x") != "rtsp://192.168.1.9/x" {
		t.Error("address without password changed")
	}
}

func TestAuthBudgetStopsBeforeLockout(t *testing.T) {
	b := NewAuthBudget(DefaultAuthAttempts)
	for i := 0; i < DefaultAuthAttempts; i++ {
		if !b.Allow() {
			t.Fatalf("attempt %d refused too early", i)
		}
		b.Record(OutcomeUnauthorized)
	}
	if b.Allow() || b.Rejected() != 3 {
		t.Fatalf("budget not used up: allow=%v rejected=%d", b.Allow(), b.Rejected())
	}
	l := NewAuthBudget(10)
	l.Record(OutcomeOther) // a wrong path says nothing about the password
	if !l.Allow() {
		t.Fatal("other outcome counted")
	}
	l.Record(OutcomeLocked)
	if l.Allow() || !l.Locked() {
		t.Fatal("search continued after a lock")
	}
	if NewAuthBudget(0).Allow() == false {
		t.Fatal("budget below one must still allow one attempt")
	}
}

func TestCandidatesForPutsTheMakerFirstAndKeepsAll(t *testing.T) {
	list := CandidatesFor("reolink")
	if list[0].Vendor != "reolink" || list[0].Path != "/h264Preview_01_main" {
		t.Fatalf("first: %+v", list[0])
	}
	if last := list[len(list)-1]; last.Vendor != "generic" && last.Role != "sub" {
		t.Errorf("generic paths must come last: %+v", last)
	}
	seen := map[string]bool{}
	for _, c := range list {
		key := c.Role + c.Path
		if seen[key] {
			t.Errorf("duplicate %s", key)
		}
		seen[key] = true
	}
	for _, v := range Vendors {
		if v.Main == "" || v.RTSPPort == 0 {
			t.Errorf("%s: incomplete", v.Name)
		}
	}
	if CandidatesFor("")[0].Vendor == "generic" {
		t.Error("generic first without a maker")
	}
}
