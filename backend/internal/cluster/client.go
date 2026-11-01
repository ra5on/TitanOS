package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// transports keeps one HTTP transport per pinned certificate, so that
// connections to a node are reused.
var transports sync.Map // fingerprint+internet → *http.Transport

// transport returns the transport for connections to a node with the
// certificate fingerprint cert.
func (m *Manager) transport(cert string) *http.Transport {
	m.mu.Lock()
	internet := m.state != nil && m.state.Internet
	own, self := m.cert.tls, m.self.ID
	m.mu.Unlock()
	key := fmt.Sprintf("%s/%s/%t/%t/%t", self, cert, internet, m.Insecure, m.Loopback)
	if t, ok := transports.Load(key); ok {
		return t.(*http.Transport)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	t := &http.Transport{
		TLSClientConfig: pinned(cert, own),
		// Every dial checks the address it lands on, also of a host name.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if m.allowedTarget(ip, internet) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
				}
			}
			return nil, fmt.Errorf("cluster: %s is no address a node may be reached at", host)
		},
		ForceAttemptHTTP2:     false,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0, // long requests (consoles, backups) stream
	}
	actual, _ := transports.LoadOrStore(key, t)
	return actual.(*http.Transport)
}

func (m *Manager) allowedTarget(ip netip.Addr, internet bool) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return m.Insecure || m.Loopback
	}
	return netguard.Local(ip) || internet && netguard.Public(ip)
}

func (m *Manager) scheme() string {
	if m.Insecure {
		return "http"
	}
	return "https"
}

// candidates returns the addresses of n for p in the order to try: the
// one that worked last first, as long as it is still allowed.
func (m *Manager) candidates(n Node, p purpose) []string {
	order := m.ordered(n, p)
	m.mu.Lock()
	last := m.reach[p.key(n.ID)]
	m.mu.Unlock()
	if last == "" || !slices.Contains(order, last) {
		return order
	}
	out := []string{last}
	for _, a := range order {
		if a != last {
			out = append(out, a)
		}
	}
	return out
}

// reached remembers address as the way to key (a node, or a node for
// backups) and when that way is not the preferred one.
func (m *Manager) reached(key, address string) {
	m.mu.Lock()
	m.reach[key] = address
	m.mu.Unlock()
}

// call sends a signed request to node n on behalf of user ("" for the
// node itself) and decodes the JSON answer into out. It tries the
// addresses of n until one answers.
func (m *Manager) call(ctx context.Context, n Node, method, path, user string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	return m.tryAddresses(ctx, n, m.candidates(n, forManage), n.ID, func(ctx context.Context, address string) (*http.Response, error) {
		return m.sendSigned(ctx, n, address, method, path, user, body)
	}, out)
}

// sendSigned sends one signed request to address of node n.
func (m *Manager) sendSigned(ctx context.Context, n Node, address, method, path, user string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, m.scheme()+"://"+address+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := m.sign(req, user); err != nil {
		return nil, err
	}
	return (&http.Client{Transport: m.transport(n.Cert), Timeout: 30 * time.Second}).Do(req)
}

// tryAddresses runs send against addresses in turn. A connection error
// moves on to the next address; an answer, also an error, ends. The
// address that answered is remembered for key, unless key is empty.
func (m *Manager) tryAddresses(ctx context.Context, n Node, addresses []string, key string, send func(context.Context, string) (*http.Response, error), out any) error {
	lastErr := ErrUnreachable
	restarting := false
	for _, address := range addresses {
		res, err := send(ctx, address)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			restarting = restarting || refused(err)
			continue
		}
		if key != "" {
			m.reached(key, address)
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return peerError(res)
		}
		if out == nil {
			return nil
		}
		return json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(out)
	}
	slog.Debug("cluster: node not reached", "node", n.Name, "err", lastErr)
	if restarting {
		return fmt.Errorf("%w: %v", ErrRestarting, lastErr)
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, lastErr)
}

// PeerError is an error answer of another node.
type PeerError struct {
	Status  int
	Code    string
	Message string
}

func (e *PeerError) Error() string {
	return fmt.Sprintf("cluster: node answered %d %s: %s", e.Status, e.Code, e.Message)
}

func peerError(res *http.Response) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	if res.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w (%s)", ErrUnauthorized, body.Error.Message)
	}
	return &PeerError{Status: res.StatusCode, Code: body.Error.Code, Message: body.Error.Message}
}

// IsPeerError reports whether err is the answer code of another node.
func IsPeerError(err error, code string) bool {
	var pe *PeerError
	return errors.As(err, &pe) && pe.Code == code
}

// postJSON sends an unsigned request, for joining: the node has no secret
// yet, it proves itself with the join key in the body.
func (m *Manager) postJSON(ctx context.Context, n Node, address, path string, in any) (*http.Response, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", m.scheme()+"://"+address+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return (&http.Client{Transport: m.joinTransport(n.Cert), Timeout: 30 * time.Second}).Do(req)
}

// joinTransport is the transport while joining: this node is in no
// cluster yet and goes to every address the join information names, also
// on the internet; the pinned certificate makes sure it is the cluster.
func (m *Manager) joinTransport(cert string) *http.Transport {
	t := m.transport(cert).Clone()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if m.allowedTarget(ip, true) {
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			}
		}
		return nil, fmt.Errorf("cluster: %s is no address a node may be reached at", host)
	}
	return t
}
