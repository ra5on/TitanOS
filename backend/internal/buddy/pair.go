package buddy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pairing: this NAS shows a one-time code; the person enters it, with the
// address of this NAS, on the other one. That NAS sends a pairing request
// with the code, its name and address, the id it gave this NAS and the
// token this NAS will sign in with there; the answer carries the same the
// other way. Each side keeps only the hash of the token it hands out.

const (
	// CodeTTL is how long a code may be used.
	CodeTTL = 10 * time.Minute
	// maxTries ends a code after so many wrong attempts.
	maxTries = 5
	// codeAlphabet leaves out 0/O, 1/I.
	codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codeLen      = 9
)

var (
	ErrNoCode   = errors.New("buddy: no valid pairing code")
	hostForm    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	errAddress  = errors.New("buddy: address: a host name or IP address, optionally with :port")
	errAnswered = errors.New("buddy: the other NAS refused the pairing")
)

// PairRequest is what the NAS that enters the code sends.
type PairRequest struct {
	Code string `json:"code"`
	// Name and Address of the sending NAS (Address as this NAS reaches it).
	Name    string `json:"name"`
	Address string `json:"address"`
	// ID is the name the sender gave this NAS; Token what this NAS signs
	// in with there; Quota what the sender grants this NAS (bytes).
	ID    string `json:"id"`
	Token string `json:"token"`
	Quota int64  `json:"quota"`
	// ProtectDays is the protection period of the sender: data this NAS
	// keeps there cannot be deleted before (older versions send 0).
	ProtectDays int `json:"protectDays,omitempty"`
}

// PairResponse is the answer, the same the other way.
type PairResponse struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	Token       string `json:"token"`
	Quota       int64  `json:"quota"`
	ProtectDays int    `json:"protectDays,omitempty"`
}

// Codes keeps at most one pairing code, in the RAM only.
type Codes struct {
	// Now is the clock (tests).
	Now func() time.Time

	mu      sync.Mutex
	code    string
	expires time.Time
	tries   int
	meta    any
}

// Issue makes a new code (the old one ends) and keeps meta for Redeem.
func (c *Codes) Issue(meta any) (string, time.Time, error) {
	raw := make([]byte, codeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	var b strings.Builder
	for _, v := range raw {
		b.WriteByte(codeAlphabet[int(v)%len(codeAlphabet)])
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.code, c.expires, c.tries, c.meta = b.String(), c.now().Add(CodeTTL), 0, meta
	return Display(c.code), c.expires, nil
}

func (c *Codes) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Active returns the code shown right now, if any.
func (c *Codes) Active() (string, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.code == "" || !c.now().Before(c.expires) {
		return "", time.Time{}, false
	}
	return Display(c.code), c.expires, true
}

// Cancel ends the code.
func (c *Codes) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.code, c.meta = "", nil
}

// Redeem uses the code once. A wrong code counts; after maxTries the code
// ends, so it cannot be guessed.
func (c *Codes) Redeem(code string) (any, error) {
	given := Normalize(code)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.code == "" || !c.now().Before(c.expires) {
		c.code, c.meta = "", nil
		return nil, ErrNoCode
	}
	if subtle.ConstantTimeCompare([]byte(given), []byte(c.code)) != 1 {
		c.tries++
		if c.tries >= maxTries {
			c.code, c.meta = "", nil
		}
		return nil, ErrNoCode
	}
	meta := c.meta
	c.code, c.meta = "", nil
	return meta, nil
}

// Normalize drops dashes and spaces and upper-cases a code.
func Normalize(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

// Display writes a code in groups of three: K7Q-4MZ-92X.
func Display(code string) string {
	if len(code) != codeLen {
		return code
	}
	return code[:3] + "-" + code[3:6] + "-" + code[6:]
}

// CheckAddress checks an address a person entered: host or host:port.
func CheckAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	host, port := address, ""
	if h, p, err := net.SplitHostPort(address); err == nil {
		host, port = h, p
	}
	if ip := net.ParseIP(host); ip == nil && !hostForm.MatchString(host) {
		return "", errAddress
	}
	if port == "" {
		port = fmt.Sprint(DefaultPort)
	} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", errAddress
	}
	return net.JoinHostPort(host, port), nil
}

// BaseURL is the address of a partner's server.
func BaseURL(address string) string { return "http://" + address + "/v1" }

func (s *Server) servePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.Pair == nil {
		http.Error(w, "pairing is not open", http.StatusNotFound)
		return
	}
	var req PairRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxPairBody)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	res, err := s.Pair(req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrNoCode) {
			status = http.StatusForbidden
		}
		slog.Warn("buddy pairing refused", "from", r.RemoteAddr, "err", err)
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// Client sends a pairing request to the NAS at address.
type Client struct {
	HTTP *http.Client
}

// Pair sends req to the partner and returns its answer.
func (c Client) Pair(ctx context.Context, address string, req PairRequest) (PairResponse, error) {
	body, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL(address)+"/pair", bytes.NewReader(body))
	if err != nil {
		return PairResponse{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(hr)
	if err != nil {
		return PairResponse{}, fmt.Errorf("buddy: the other NAS is not reachable: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, maxPairBody))
	if res.StatusCode != http.StatusOK {
		return PairResponse{}, fmt.Errorf("%w (%d): %s", errAnswered, res.StatusCode, strings.TrimSpace(string(data)))
	}
	var out PairResponse
	if err := json.Unmarshal(data, &out); err != nil || !ValidID(out.ID) || len(out.Token) < 32 || out.Quota < 0 {
		return PairResponse{}, errors.New("buddy: the other NAS answered something unexpected")
	}
	return out, nil
}

// Listener keeps one HTTP server per address.
type Listener struct {
	mu      sync.Mutex
	servers map[string]*http.Server
}

// Reconcile listens on exactly want (host:port) with handler.
func (l *Listener) Reconcile(want []string, handler http.Handler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.servers == nil {
		l.servers = map[string]*http.Server{}
	}
	for address, server := range l.servers {
		if !slices.Contains(want, address) {
			_ = server.Close() // best effort; nothing more can be done
			delete(l.servers, address)
		}
	}
	for _, address := range want {
		if _, ok := l.servers[address]; ok {
			continue
		}
		ln, err := net.Listen("tcp", address)
		if err != nil {
			slog.Warn("buddy cannot listen", "address", address, "err", err)
			continue
		}
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute}
		l.servers[address] = server
		go func() { _ = server.Serve(ln) }() // ends with Close
		slog.Info("buddy listening", "address", address)
	}
}

// Addresses are where the listener answers now.
func (l *Listener) Addresses() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []string{}
	for a := range l.servers {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// TailscaleOnly keeps the Tailscale addresses: 100.64.0.0/10 and fd7a:115c:a1e0::/48.
func TailscaleOnly(addresses []string) []string {
	var out []string
	for _, a := range addresses {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 || strings.HasPrefix(strings.ToLower(a), "fd7a:115c:a1e0:") {
			out = append(out, a)
		}
	}
	return out
}
