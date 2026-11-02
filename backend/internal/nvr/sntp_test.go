package nvr

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

func startSNTP(t *testing.T, allow func(netip.Addr) bool, now time.Time) net.Addr {
	t.Helper()
	s := &SNTPServer{Addr: "127.0.0.1:0", Allow: allow, Now: func() time.Time { return now }}
	addr, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(t.Context())
	t.Cleanup(s.Close)
	return addr
}

func ask(t *testing.T, addr net.Addr) ([]byte, bool) {
	t.Helper()
	c, err := net.Dial("udp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := make([]byte, 48)
	req[0] = 0<<6 | 4<<3 | 3
	putNTP(req[40:48], time.Unix(1000, 0))
	c.Write(req)
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	return buf[:n], err == nil
}

func TestSNTPAnswersWithOurTime(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 500_000_000, time.UTC)
	resp, ok := ask(t, startSNTP(t, func(a netip.Addr) bool { return a.IsLoopback() }, now))
	if !ok || len(resp) != 48 {
		t.Fatalf("no answer: %d bytes", len(resp))
	}
	if resp[0]&7 != 4 || resp[1] != 3 {
		t.Fatalf("mode/stratum: %x %d", resp[0], resp[1])
	}
	if got := ntpTime(resp[40:48]); got.Sub(now).Abs() > 5*time.Millisecond {
		t.Fatalf("transmit time %v, want %v", got, now)
	}
	if got := ntpTime(resp[24:32]); got.Unix() != 1000 {
		t.Fatalf("originate not echoed: %v", got)
	}
}

func TestSNTPIgnoresOutsiders(t *testing.T) {
	// Default rule: loopback is not part of the home network, so no answer.
	if _, ok := ask(t, startSNTP(t, nil, time.Now())); ok {
		t.Fatal("answered a source outside the home network")
	}
}

func TestSNTPLimitsEachSource(t *testing.T) {
	addr := startSNTP(t, func(a netip.Addr) bool { return a.IsLoopback() }, time.Now())
	answered := 0
	for i := 0; i < 20; i++ {
		if _, ok := ask(t, addr); ok {
			answered++
		}
	}
	if answered >= 20 || answered < 3 {
		t.Fatalf("answered %d of 20", answered)
	}
}
