package cluster

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// Header carries the signature of a request between nodes:
//
//	node=<id>, gen=<generation>, time=<unix>, nonce=<hex>, user=<account>, sig=<base64>
//
// The signature is an HMAC-SHA256 with the secret of the cluster over
// method, path with query, and the fields before it. The body is not part
// of it: the connection to a node is TLS with a pinned certificate, so
// nobody in between can change it, and uploads and consoles stream.
const Header = "X-Titanos-Cluster"

// skew is how far the clocks of two nodes may differ.
const skew = 2 * time.Minute

// Identity is who sent a signed request: the node, and the account on
// whose behalf, empty for requests of the node itself.
type Identity struct {
	Node string
	User string
}

type identityKey struct{}

// FromContext returns the identity of a request that came signed from
// another node.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

func signature(secret, method, target, node string, gen int, at int64, nonce, user string) string {
	key, _ := hex.DecodeString(secret)
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%s\n%s\n%s\n%d\n%d\n%s\n%s", method, target, node, gen, at, nonce, user)
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

// sign adds the signature of this node to r, on behalf of user.
func (m *Manager) sign(r *http.Request, user string) error {
	m.mu.Lock()
	secret, gen, node := m.secrets.Secret, 0, m.self.ID
	if m.state != nil {
		gen = m.state.Generation
	}
	m.mu.Unlock()
	if secret == "" {
		return ErrNoCluster
	}
	nonce, err := randomHex(12)
	if err != nil {
		return err
	}
	at := time.Now().Unix()
	sig := signature(secret, r.Method, r.URL.RequestURI(), node, gen, at, nonce, user)
	r.Header.Set(Header, fmt.Sprintf("node=%s, gen=%d, time=%d, nonce=%s, user=%s, sig=%s", node, gen, at, nonce, user, sig))
	return nil
}

func parseHeader(value string) map[string]string {
	fields := map[string]string{}
	for part := range strings.SplitSeq(value, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			fields[k] = v
		}
	}
	return fields
}

// Verify checks the signature of a request from another node and the
// address it came from. It returns ErrUnauthorized for every failure, so
// that the answer tells nothing about which check failed.
func (m *Manager) Verify(r *http.Request) (Identity, error) {
	f := parseHeader(r.Header.Get(Header))
	m.mu.Lock()
	state, secret := m.state, m.secrets.Secret
	internet := state != nil && state.Internet
	m.mu.Unlock()
	if state == nil || secret == "" || !m.allowedPeer(r.RemoteAddr, internet) {
		return Identity{}, ErrUnauthorized
	}
	gen, err1 := strconv.Atoi(f["gen"])
	at, err2 := strconv.ParseInt(f["time"], 10, 64)
	if err1 != nil || err2 != nil || gen != state.Generation || len(f["nonce"]) != 24 {
		return Identity{}, ErrUnauthorized
	}
	if d := time.Since(time.Unix(at, 0)); d > skew || d < -skew {
		return Identity{}, ErrUnauthorized
	}
	known := false
	for _, n := range state.Nodes {
		known = known || n.ID == f["node"] && n.ID != m.ID()
	}
	want := signature(secret, r.Method, r.URL.RequestURI(), f["node"], gen, at, f["nonce"], f["user"])
	if !known || !hmac.Equal([]byte(want), []byte(f["sig"])) {
		return Identity{}, ErrUnauthorized
	}
	// The nonce last: a forged request must not use up a real one.
	if !m.nonces.fresh(f["node"]+"/"+f["nonce"], time.Now()) {
		return Identity{}, ErrUnauthorized
	}
	return Identity{Node: f["node"], User: f["user"]}, nil
}

// allowedPeer reports whether a node may talk to this one from remote:
// from the home network, Tailscale, NetBird or another VPN, and from the
// internet only when the cluster allows it.
func (m *Manager) allowedPeer(remote string, internet bool) bool {
	addrPort, err := netip.ParseAddrPort(remote)
	if err != nil {
		return false
	}
	addr := addrPort.Addr().Unmap()
	if addr.IsLoopback() {
		// Only development and tests run several nodes on one machine.
		return m.Insecure || m.Loopback
	}
	return netguard.Local(addr) || internet && netguard.Public(addr)
}

// nonces remembers the nonces of the last skew, so that a request cannot
// be sent twice.
type nonces struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newNonces() *nonces { return &nonces{seen: map[string]time.Time{}} }

func (n *nonces) fresh(key string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.seen) > 4096 {
		for k, at := range n.seen {
			if now.Sub(at) > 2*skew {
				delete(n.seen, k)
			}
		}
	}
	if _, ok := n.seen[key]; ok {
		return false
	}
	n.seen[key] = now
	return true
}

// WithIdentity returns ctx carrying id, for the handlers behind Verify.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}
