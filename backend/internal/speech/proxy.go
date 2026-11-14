package speech

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Wyoming ports carry no password. titand therefore answers them
// itself and hands a connection through to the service's container only
// when its sender may use it (Settings.Reach): the Home Assistant set up in
// TitanOS and apps on this NAS, or every address of the home network. The
// containers themselves publish nothing.

// Seen is the last connection to a service.
type Seen struct {
	At   time.Time `json:"at"`
	From string    `json:"from"`
	OK   bool      `json:"ok"`
}

type gate struct {
	port int
	// target is the address of the service's container, "" while it is not there.
	target func() string
	// allow decides for a sender.
	allow func(ip net.IP) bool

	mu       sync.Mutex
	listener net.Listener
	err      string
	seen     *Seen
	conns    map[net.Conn]struct{}
}

// open starts listening; it is safe to call again while open.
func (g *gate) open() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.listener != nil {
		return
	}
	// IPv4 and IPv6 of the NAS; who gets through is decided per connection.
	l, err := net.Listen("tcp", ":"+strconv.Itoa(g.port))
	if err != nil {
		// Another program has the port (a Wyoming app of the user's own).
		g.err = err.Error()
		return
	}
	g.listener, g.err, g.conns = l, "", map[net.Conn]struct{}{}
	go g.serve(l)
}

func (g *gate) close() {
	g.mu.Lock()
	l, conns := g.listener, g.conns
	g.listener, g.conns, g.err = nil, nil, ""
	g.mu.Unlock()
	if l != nil {
		l.Close()
	}
	for c := range conns {
		c.Close()
	}
}

func (g *gate) state() (listening bool, err string, seen *Seen) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.listener != nil, g.err, g.seen
}

func (g *gate) serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go g.handle(conn)
	}
}

func (g *gate) handle(conn net.Conn) {
	defer conn.Close()
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	ip := net.ParseIP(strings.Split(host, "%")[0])
	target := g.target()
	ok := ip != nil && target != "" && g.allow(ip)
	g.mu.Lock()
	// A refused try does not replace an accepted connection of the last
	// minute: scanners of the network would hide that Home Assistant talks.
	if ok || g.seen == nil || !g.seen.OK || time.Since(g.seen.At) > time.Minute {
		g.seen = &Seen{At: time.Now(), From: host, OK: ok}
	}
	open := g.conns != nil
	if ok && open {
		g.conns[conn] = struct{}{}
	}
	g.mu.Unlock()
	if !ok || !open {
		if ip != nil && target != "" {
			slog.Info("speech: connection refused", "port", g.port, "from", host)
		}
		return
	}
	defer func() {
		g.mu.Lock()
		if g.conns != nil {
			delete(g.conns, conn)
		}
		g.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var d net.Dialer
	up, err := d.DialContext(ctx, "tcp", target)
	cancel()
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src)
		// Half-close, so the other side reads to its end.
		if c, ok := dst.(*net.TCPConn); ok {
			c.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(up, conn)
	go pipe(conn, up)
	<-done
	<-done
}

// homeNetwork reports whether an address is one of a home network or of
// this NAS: private IPv4 (also CGNAT, which Tailscale uses), unique local
// and link-local IPv6, loopback.
func homeNetwork(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

// dockerNetworks are the networks of Docker's bridges on this NAS: a
// connection from one of them is an app of this NAS (Home Assistant as a
// Docker app).
func dockerNetworks() []*net.IPNet {
	var out []*net.IPNet
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if !strings.HasPrefix(iface.Name, "docker") && !strings.HasPrefix(iface.Name, "br-") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				out = append(out, n)
			}
		}
	}
	return out
}
