package api

// The account picker of the login page (PLAN 67, owner decisions of
// 7.11.2026): GET /api/v1/login/accounts names the accounts to tap on,
// without login. All accounts are named only when the owner left
// "showAccounts" on (default) and the request comes from the home network,
// or "showAccountsExternal" is on. Locked accounts are never named. The
// answer always has the same shape, so it tells nothing beyond the list.

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/sshd"
)

const (
	// A burst of loginAccountsBurst requests per address, then one per
	// loginAccountsEvery: a page load asks once.
	loginAccountsBurst = 20
	loginAccountsEvery = 3 * time.Second
	loginAccountsKeys  = 4096
)

type loginAccountsState struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
	// homeNetwork replaces the check of the address in tests.
	homeNetwork func(netip.Addr) bool
	now         func() time.Time
}

type loginBucket struct {
	tokens float64
	at     time.Time
}

// loginAccount is one entry of the picker. There are no avatars or display
// names in TitanOS; the page draws the initials.
type loginAccount struct {
	Name string `json:"name"`
}

// proxyHeaders are set by tunnels and reverse proxies (Cloudflare Tunnel,
// the app proxy, Tailscale Funnel, nginx …). A request carrying any of them
// came through one and counts as from outside, whatever its address.
var proxyHeaders = []string{"Cf-Connecting-Ip", "Cf-Ray", "Cf-Ipcountry", "True-Client-Ip", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "Forwarded", "Via"}

// fromHomeNetwork tells whether a request comes straight from a device in
// the home network. It fails closed: loopback and the NAS's own addresses
// are outside (a tunnel such as cloudflared on the NAS connects from
// there), so are Docker networks, VPNs (Tailscale, NetBird, WireGuard) and
// any request a proxy passed on.
func (s *Server) fromHomeNetwork(r *http.Request) bool {
	for _, h := range proxyHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	addr, err := netip.ParseAddr(remoteIP(r))
	if err != nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	if check := s.loginAccounts.homeNetwork; check != nil {
		return check(addr)
	}
	return inHomeNetwork(addr)
}

// inHomeNetwork: in a network of a physical interface of the NAS
// (sshd.HomeNetworks leaves out lo, Docker, VMs and VPNs), and not one of
// the NAS's own addresses.
func inHomeNetwork(addr netip.Addr) bool {
	if own, err := net.InterfaceAddrs(); err == nil {
		for _, a := range own {
			if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().Unmap() == addr {
				return false
			}
		}
	}
	for _, n := range sshd.HomeNetworks() {
		if p, err := netip.ParsePrefix(n); err == nil && p.Contains(addr) {
			return true
		}
	}
	return false
}

// allowLoginAccounts takes one request from the bucket of an address.
func (s *Server) allowLoginAccounts(ip string) (bool, time.Duration) {
	l := &s.loginAccounts
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	if l.buckets == nil {
		l.buckets = map[string]*loginBucket{}
	}
	b := l.buckets[ip]
	if b == nil {
		if len(l.buckets) >= loginAccountsKeys {
			for key, other := range l.buckets {
				if now.Sub(other.at) > loginAccountsBurst*loginAccountsEvery {
					delete(l.buckets, key)
				}
			}
			if len(l.buckets) >= loginAccountsKeys {
				// Full of busy addresses: refuse rather than grow.
				return false, loginAccountsEvery
			}
		}
		b = &loginBucket{tokens: loginAccountsBurst, at: now}
		l.buckets[ip] = b
	}
	b.tokens = min(loginAccountsBurst, b.tokens+now.Sub(b.at).Seconds()/loginAccountsEvery.Seconds())
	b.at = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) * float64(loginAccountsEvery))
	}
	b.tokens--
	return true, 0
}

func (s *Server) handleLoginAccounts(w http.ResponseWriter, r *http.Request) {
	// A cache (browser, Cloudflare) must never hand the home network's
	// answer to someone else.
	w.Header().Set("Cache-Control", "no-store")
	ip := remoteIP(r)
	// An address that is guessing passwords gets no names either.
	if wait := s.limiter.RetryAfter("ip:" + ip); wait > 0 {
		writeRateLimited(w, wait, "too many failed logins, try again later")
		return
	}
	// The bucket is per address, per /64 for IPv6 (passkeySource).
	if ok, wait := s.allowLoginAccounts(passkeySource(r)); !ok {
		writeRateLimited(w, wait, "too many requests, try again later")
		return
	}
	look, _ := s.loginAppearance()
	answer := struct {
		Style    string         `json:"style"`
		Accounts []loginAccount `json:"accounts"`
	}{Style: look.AccountsStyle, Accounts: []loginAccount{}}
	if answer.Style == "" {
		answer.Style = "circles"
	}
	if look.ShowAccounts && s.opts.Users.Loaded() && (look.ShowAccountsExternal || s.fromHomeNetwork(r)) {
		for _, user := range s.opts.Users.List() {
			if !user.Disabled {
				answer.Accounts = append(answer.Accounts, loginAccount{Name: user.Name})
			}
		}
		slices.SortFunc(answer.Accounts, func(a, b loginAccount) int {
			switch {
			case a.Name < b.Name:
				return -1
			case a.Name > b.Name:
				return 1
			}
			return 0
		})
	}
	writeJSON(w, http.StatusOK, answer)
}
