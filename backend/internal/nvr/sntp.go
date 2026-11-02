package nvr

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"
)

// SNTPServer answers cameras that ask the NAS for the time (UDP 123). Only
// addresses of the home network get an answer, and each source is limited,
// so the port cannot be used for amplification.
type SNTPServer struct {
	// Addr is the listen address; empty means ":123".
	Addr string
	// Allow decides who is answered; nil means AllowedAddr.
	Allow func(netip.Addr) bool
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	mu   sync.Mutex
	conn net.PacketConn
	seen map[netip.Addr]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

const ntpEpochOffset = 2208988800 // seconds from 1900 to 1970

func (s *SNTPServer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SNTPServer) allowed(a netip.Addr) bool {
	if s.Allow != nil {
		return s.Allow(a.Unmap())
	}
	return AllowedAddr(a)
}

// take reports whether the source may ask now: five at once, then one a second.
func (s *SNTPServer) take(a netip.Addr, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[netip.Addr]*bucket{}
	}
	if len(s.seen) > 4096 {
		s.seen = map[netip.Addr]*bucket{}
	}
	b := s.seen[a]
	if b == nil {
		b = &bucket{tokens: 5, last: now}
		s.seen[a] = b
	}
	b.tokens = min(5, b.tokens+now.Sub(b.last).Seconds())
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Listen opens the port. Call Serve to answer.
func (s *SNTPServer) Listen() (net.Addr, error) {
	addr := s.Addr
	if addr == "" {
		addr = ":123"
	}
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	return conn.LocalAddr(), nil
}

// Serve answers until ctx ends or Close is called.
func (s *SNTPServer) Serve(ctx context.Context) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return
	}
	go func() { <-ctx.Done(); conn.Close() }()
	buf := make([]byte, 128)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		ap, err := netip.ParseAddrPort(from.String())
		if err != nil || n < 48 || !s.allowed(ap.Addr()) {
			continue
		}
		rx := s.now()
		if !s.take(ap.Addr().Unmap(), rx) {
			continue
		}
		mode := buf[0] & 7
		if mode != 3 { // client
			continue
		}
		resp := make([]byte, 48)
		resp[0] = 0<<6 | (buf[0]>>3&7)<<3 | 4 // no warning, same version, server
		resp[1] = 3                           // stratum: the NAS follows its own time source
		resp[2] = buf[2]
		resp[3] = 0xEC                // precision about 1 ms
		copy(resp[24:32], buf[40:48]) // originate = the client's transmit
		putNTP(resp[16:24], rx)       // reference
		putNTP(resp[32:40], rx)       // receive
		putNTP(resp[40:48], s.now())  // transmit
		if _, err := conn.WriteTo(resp, from); err != nil {
			slog.Debug("nvr: time answer not sent", "err", err)
		}
	}
}

// Close stops the server.
func (s *SNTPServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
	}
}

func putNTP(b []byte, t time.Time) {
	sec := uint64(t.Unix()) + ntpEpochOffset
	frac := uint64(t.Nanosecond()) << 32 / 1e9
	binary.BigEndian.PutUint32(b[0:4], uint32(sec))
	binary.BigEndian.PutUint32(b[4:8], uint32(frac))
}

// ntpTime reads a 64-bit NTP timestamp.
func ntpTime(b []byte) time.Time {
	sec := int64(binary.BigEndian.Uint32(b[0:4])) - ntpEpochOffset
	frac := int64(binary.BigEndian.Uint32(b[4:8]))
	return time.Unix(sec, frac*1e9>>32)
}
