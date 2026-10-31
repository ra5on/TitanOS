package dnsprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IPv64.net, as its OpenAPI description (ipv64.net/api/v2/openapi.json),
// ipv64.net/dyndns_updater_api and ipv64.net/healthcheck_updater_api
// describe it:
//
//   - API v2 https://ipv64.net/api/v2 with the account API key as Bearer
//     token. Answers {"data": …}, errors are problem+json (RFC 9457). A
//     token bucket of 300 per account (10 a second); 429 carries
//     Retry-After. Used for domains, records (TXT of the DNS challenge),
//     DynDNS updates (POST /domains/{d}/dyndns, not counted against the
//     daily update limit), healthchecks and integrations.
//   - The DynDNS2 updater https://ipv64.net/nic/update with a domain update
//     token, when there is no API key or the IPv6 prefix is to be sent too.
//     48 updates a day by default (per plan), at most 3 requests within 10
//     seconds; many failures lock the token for 10 minutes.
//   - Healthcheck pings https://ipv64.net/health with the healthcheck token,
//     72 a day by default, at most 3 within 10 seconds.
//
// Tokens only ever go into the Authorization header.
//
// API v2 is covered field by field after its OpenAPI description, one file
// per area: ipv64_account.go (account, log, API keys, blocklist token),
// ipv64_domains.go (domains and records), ipv64_dyndns.go (the Provider
// methods, updates and the public address), ipv64_checks.go (healthchecks
// with events and statistics), ipv64_integrations.go, ipv64_failover.go
// (DNS failover), ipv64_pages.go (status and offline pages), ipv64_cdn.go
// (CDN with reverse proxy and portmapper) and ipv64_router.go (Cloud Router
// with gateways and peers).
//
// The types of the full API carry the field names of the API (snake_case),
// so the web UI can be checked against the same description. The older,
// smaller views (IPv64Domain, IPv64Record, IPv64Healthcheck,
// IPv64Integration, IPv64Account) keep the camelCase names the DynDNS page
// of the web UI reads; they are made from the same requests. What is secret
// stays here: healthcheck tokens become an ID (a hash, as for the dynamic DNS
// page), push URLs, domain update tokens, secrets of integrations and the
// WireGuard configuration of peers are never part of these types.

// IPv64Zones are the zones DynDNS names can be made under ("nas.ipv64.de"),
// from the description of POST /domains.
var IPv64Zones = []string{"ipv64.net", "ipv64.de", "any64.de", "api64.de", "dns64.de", "dyndns64.de", "dynipv6.de", "eth64.de",
	"home64.de", "iot64.de", "lan64.de", "nas64.de", "root64.de", "route64.de", "srv64.de", "tcp64.de", "udp64.de", "vpn64.de", "wan64.de"}

// IPv64RecordTypes are the record types the API takes.
var IPv64RecordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "TLSA", "CAA"}

// IPv64 is a client of IPv64.net.
type IPv64 struct {
	client *http.Client
	base   string
	// key is the API key, update the domain update token; either may be empty.
	key    string
	update string
	// prefix6 is sent as ipv6prefix to the updater ("auto" or a prefix).
	prefix6 string
}

// NewIPv64 returns a client for an API key and/or a domain update token.
func NewIPv64(apiKey, updateToken string, opts Options) *IPv64 {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBase[IPv64Net]
	}
	return &IPv64{client: client, base: strings.TrimSuffix(base, "/"), key: apiKey, update: updateToken}
}

// ipv64Error turns a problem+json answer into an APIError.
func ipv64Error(status int, header http.Header, data []byte) error {
	var problem struct {
		Detail     string `json:"detail"`
		Code       string `json:"code"`
		RetryAfter int    `json:"retry_after"`
	}
	_ = json.Unmarshal(data, &problem) // a value that cannot be read leaves the zero value, which the code handles
	e := &APIError{Provider: "IPv64", Status: status, Message: problem.Detail, Code: problem.Code}
	if e.Message == "" {
		e.Message = short(data)
	}
	e.RetryAfter = time.Duration(problem.RetryAfter) * time.Second
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		e.RetryAfter = time.Duration(seconds) * time.Second
	}
	if status == http.StatusTooManyRequests && e.RetryAfter == 0 {
		e.RetryAfter = 10 * time.Second
	}
	return e
}

func (c *IPv64) roundTrip(req *http.Request, client *http.Client) (int, http.Header, []byte, error) {
	req.Header.Set("User-Agent", "TitanOS")
	res, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, nil, nil, fmt.Errorf("IPv64: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return res.StatusCode, res.Header, nil, fmt.Errorf("IPv64: %w", err)
	}
	return res.StatusCode, res.Header, data, nil
}

// call sends one request to API v2 and decodes data into out.
func (c *IPv64) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	answer, err := c.Do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if out != nil && answer.Status != http.StatusNoContent {
		if len(answer.Data) == 0 {
			return &APIError{Provider: "IPv64", Message: "unexpected answer"}
		}
		if err := json.Unmarshal(answer.Data, out); err != nil {
			return &APIError{Provider: "IPv64", Message: "unexpected answer"}
		}
	}
	return nil
}

// IPv64Answer is a successful answer of API v2: the envelope {data, meta}
// and the status (202 means the change is carried out in the background).
type IPv64Answer struct {
	Status int
	Data   json.RawMessage
	Meta   json.RawMessage
}

// Do sends one request to API v2 with the API key. Every request goes
// through the token bucket of the account (ipv64LimiterFor), which also follows the
// RateLimit-Remaining and Retry-After headers of the answers.
func (c *IPv64) Do(ctx context.Context, method, path string, query url.Values, body any) (IPv64Answer, error) {
	var data []byte
	contentType := ""
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return IPv64Answer{}, err
		}
		data, contentType = encoded, "application/json"
	}
	return c.send(ctx, method, path, query, data, contentType)
}

func (c *IPv64) send(ctx context.Context, method, path string, query url.Values, data []byte, contentType string) (IPv64Answer, error) {
	if c.key == "" {
		return IPv64Answer{}, &APIError{Provider: "IPv64", Message: "this needs the API key of the account"}
	}
	address := c.base + "/api/v2" + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	limiter := ipv64LimiterFor(c.key)
	if err := limiter.wait(ctx); err != nil {
		return IPv64Answer{}, err
	}
	var reader io.Reader
	if data != nil {
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return IPv64Answer{}, errors.New("IPv64: request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	status, header, answer, err := c.roundTrip(req, c.client)
	if err != nil {
		return IPv64Answer{}, err
	}
	limiter.observe(header, status)
	if status >= 300 {
		return IPv64Answer{}, ipv64Error(status, header, answer)
	}
	out := IPv64Answer{Status: status}
	if status != http.StatusNoContent && len(bytes.TrimSpace(answer)) > 0 {
		var envelope struct {
			Data json.RawMessage `json:"data"`
			Meta json.RawMessage `json:"meta"`
		}
		if err := json.Unmarshal(answer, &envelope); err != nil {
			return IPv64Answer{}, &APIError{Provider: "IPv64", Message: "unexpected answer"}
		}
		out.Data, out.Meta = envelope.Data, envelope.Meta
	}
	return out, nil
}

// ipv64Limiters keep the API v2 requests of this NAS under the limits of
// IPv64: a token bucket of 300 requests per account refilled with 10 a
// second (from the documentation, section "Limits"). An answer with fewer
// remaining requests (RateLimit-Remaining) lowers the bucket; a 429 holds
// the requests of that account back for Retry-After seconds. The buckets
// are keyed by a hash of the API key, so the key itself is not kept here.
var (
	ipv64LimitersMu sync.Mutex
	ipv64Limiters   = map[[sha256.Size]byte]*tokenBucket{}
)

// maxIPv64Limiters bounds the map; a NAS has a handful of accounts at most,
// keys that were replaced just fall out when it is full.
const maxIPv64Limiters = 64

// ipv64LimiterFor returns the token bucket of the account of an API key.
func ipv64LimiterFor(key string) *tokenBucket {
	id := sha256.Sum256([]byte(key))
	ipv64LimitersMu.Lock()
	defer ipv64LimitersMu.Unlock()
	if b, ok := ipv64Limiters[id]; ok {
		return b
	}
	if len(ipv64Limiters) >= maxIPv64Limiters {
		clear(ipv64Limiters)
	}
	b := newTokenBucket(300, 10)
	ipv64Limiters[id] = b
	return b
}

type tokenBucket struct {
	mu       sync.Mutex
	capacity float64
	rate     float64
	tokens   float64
	last     time.Time
	hold     time.Time
	now      func() time.Time
}

func newTokenBucket(capacity, rate float64) *tokenBucket {
	return &tokenBucket{capacity: capacity, rate: rate, tokens: capacity, now: time.Now}
}

func (b *tokenBucket) refill(now time.Time) {
	if !b.last.IsZero() {
		b.tokens = min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	}
	b.last = now
}

// reserve takes one token and says how long to wait before using it.
func (b *tokenBucket) reserve() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.refill(now)
	wait := time.Duration(0)
	if b.hold.After(now) {
		wait = b.hold.Sub(now)
	}
	b.tokens--
	if b.tokens < 0 {
		wait = max(wait, time.Duration(-b.tokens/b.rate*float64(time.Second)))
	}
	return wait
}

// wait takes a token, sleeping as long as needed (at most a minute).
func (b *tokenBucket) wait(ctx context.Context) error {
	wait := b.reserve()
	if wait <= 0 {
		return nil
	}
	if wait > time.Minute {
		return &APIError{Provider: "IPv64", Status: http.StatusTooManyRequests, Message: "too many requests to IPv64, try again later", RetryAfter: wait}
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// observe follows the rate limit headers of an answer.
func (b *tokenBucket) observe(header http.Header, status int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.refill(now)
	if remaining, err := strconv.Atoi(header.Get("RateLimit-Remaining")); err == nil && remaining >= 0 {
		b.tokens = min(b.tokens, float64(remaining))
	}
	if status == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(header.Get("Retry-After"))
		if err != nil || seconds <= 0 {
			seconds = 10
		}
		b.hold = now.Add(time.Duration(seconds) * time.Second)
		b.tokens = min(b.tokens, 0)
	}
}

// --- helpers ---

var refRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validRef accepts the IDs and tokens of IPv64 in a path.
func validRef(ref string) bool { return refRE.MatchString(ref) }

// ValidIPv64Ref tells whether an ID or token may go into a path.
func ValidIPv64Ref(ref string) bool { return validRef(ref) }

func decodeData(data json.RawMessage, out any) error {
	if len(data) == 0 {
		return &APIError{Provider: "IPv64", Message: "unexpected answer"}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &APIError{Provider: "IPv64", Message: fmt.Sprintf("unexpected answer: %v", err)}
	}
	return nil
}
