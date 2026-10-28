// Package dnsprovider changes DNS records at the services people in
// Germany and Europe use most: TXT records for the DNS challenge of Let's
// Encrypt and A/AAAA records for dynamic DNS.
//
// Every provider speaks its own HTTP API with the standard library only;
// RFC 2136 sends signed DNS updates. Tokens and passwords are secrets: they
// are only sent to the provider, never written to the log, never put on a
// command line and never part of an error message.
package dnsprovider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Types of provider.
const (
	Cloudflare = "cloudflare"
	DeSEC      = "desec"
	Hetzner    = "hetzner"
	IONOS      = "ionos"
	Netcup     = "netcup"
	INWX       = "inwx"
	DuckDNS    = "duckdns"
	// IPv64Net is IPv64.net: DNS API, DynDNS and healthchecks.
	IPv64Net = "ipv64"
	RFC2136  = "rfc2136"
	// DynDNS2 is the update protocol of dyndns.org that many services
	// speak (Strato, No-IP, all-inkl, …); it changes addresses only.
	DynDNS2 = "dyndns2"
)

// Purposes a provider is used for.
const (
	ForChallenge = "challenge"
	ForAddress   = "address"
)

// ErrInvalid means the settings of a provider are not usable.
var ErrInvalid = errors.New("dnsprovider: invalid settings")

// Config names a provider and how to sign in. Token, Password and Secret
// are secrets; everything else may be shown.
type Config struct {
	Type string `json:"type"`
	// Customer is the customer number at Netcup.
	Customer string `json:"customer,omitempty"`
	// User is the account at INWX or of a dyndns2 service.
	User string `json:"user,omitempty"`
	// Server is the name server of RFC 2136 (host or host:port) or the
	// update address of a dyndns2 service.
	Server string `json:"server,omitempty"`
	// Zone is the DNS zone for RFC 2136; other providers find it themselves.
	Zone string `json:"zone,omitempty"`
	// KeyName and Algorithm are the TSIG key of RFC 2136.
	KeyName   string `json:"keyName,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`

	// Token is the API token (Cloudflare, deSEC, Hetzner, DuckDNS), the API
	// key (IONOS as "prefix.secret", Netcup).
	Token string `json:"token,omitempty"`
	// Password is the API password of Netcup, the password of INWX or of a
	// dyndns2 service.
	Password string `json:"password,omitempty"`
	// Secret is the TSIG secret of RFC 2136, base64.
	Secret string `json:"secret,omitempty"`
	// UpdateToken is the domain update token of IPv64.net (DynDNS only).
	UpdateToken string `json:"updateToken,omitempty"`
	// IPv6Prefix is sent along to IPv64.net: "auto" or a prefix like
	// 2001:db8:1234::/56; empty sends none.
	IPv6Prefix string `json:"ipv6Prefix,omitempty"`
}

// Types lists the providers for a purpose.
func Types(purpose string) []string {
	all := []string{Cloudflare, DeSEC, Hetzner, IONOS, Netcup, INWX, DuckDNS, IPv64Net, RFC2136}
	if purpose == ForAddress {
		all = append(all, DynDNS2)
	}
	return all
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Public returns the settings without the secrets.
func (c Config) Public() Config {
	c.Token, c.Password, c.Secret, c.UpdateToken = "", "", "", ""
	return c
}

// Secrets says which secrets are set.
func (c Config) Secrets() map[string]bool {
	return map[string]bool{"token": c.Token != "", "password": c.Password != "", "secret": c.Secret != "", "updateToken": c.UpdateToken != ""}
}

// sameAccount tells whether two settings sign in to the same account, so
// that secrets left empty may be taken over.
func (c Config) sameAccount(o Config) bool {
	return c.Type == o.Type && c.Customer == o.Customer && c.User == o.User && c.Server == o.Server && c.KeyName == o.KeyName
}

// KeepSecrets fills secrets left empty from the stored settings, but only
// for the same provider and account: a token must not go to another one.
// Names in clear are dropped instead.
func (c Config) KeepSecrets(stored Config, clear []string) Config {
	if !c.sameAccount(stored) {
		return c
	}
	for _, name := range clear {
		switch name {
		case "token":
			stored.Token = ""
		case "password":
			stored.Password = ""
		case "secret":
			stored.Secret = ""
		case "updateToken":
			stored.UpdateToken = ""
		}
	}
	if c.Token == "" {
		c.Token = stored.Token
	}
	if c.Password == "" {
		c.Password = stored.Password
	}
	if c.Secret == "" {
		c.Secret = stored.Secret
	}
	if c.UpdateToken == "" {
		c.UpdateToken = stored.UpdateToken
	}
	return c
}

// Normalize trims the fields and fills defaults.
func (c Config) Normalize() Config {
	for _, field := range []*string{&c.Type, &c.Customer, &c.User, &c.Server, &c.Zone, &c.KeyName, &c.Algorithm, &c.Token, &c.Password, &c.Secret, &c.UpdateToken, &c.IPv6Prefix} {
		*field = strings.TrimSpace(*field)
	}
	c.Zone = strings.TrimSuffix(strings.ToLower(c.Zone), ".")
	c.KeyName = strings.TrimSuffix(strings.ToLower(c.KeyName), ".")
	if c.Type == RFC2136 && c.Algorithm == "" {
		c.Algorithm = "hmac-sha256"
	}
	if c.Type != RFC2136 {
		c.Zone, c.KeyName, c.Algorithm, c.Secret = "", "", "", ""
	}
	if c.Type != IPv64Net {
		c.UpdateToken, c.IPv6Prefix = "", ""
	}
	return c
}

// Validate checks the settings for a purpose. Secrets must be filled in
// already (see KeepSecrets).
func (c Config) Validate(purpose string) error {
	if !slices.Contains(Types(purpose), c.Type) {
		return invalid("unknown provider %q", c.Type)
	}
	for _, field := range []string{c.Customer, c.User, c.Server, c.Zone, c.KeyName, c.Algorithm, c.Token, c.Password, c.Secret, c.UpdateToken, c.IPv6Prefix} {
		if len(field) > 512 || strings.ContainsFunc(field, unicode.IsControl) {
			return invalid("a field is too long or holds control characters")
		}
	}
	need := func(value, name string) error {
		if value == "" {
			return invalid("%s missing", name)
		}
		return nil
	}
	switch c.Type {
	case Cloudflare, DeSEC, Hetzner, DuckDNS:
		return need(c.Token, "token")
	case IONOS:
		if err := need(c.Token, "API key"); err != nil {
			return err
		}
		if !strings.Contains(c.Token, ".") {
			return invalid("the API key of IONOS has the form prefix.secret")
		}
	case Netcup:
		if c.Customer == "" || strings.Trim(c.Customer, "0123456789") != "" {
			return invalid("customer number")
		}
		if err := need(c.Token, "API key"); err != nil {
			return err
		}
		return need(c.Password, "API password")
	case IPv64Net:
		if purpose == ForChallenge || c.Token == "" && c.UpdateToken == "" {
			if err := need(c.Token, "API key"); err != nil {
				return err
			}
		}
		if c.IPv6Prefix != "" && c.IPv6Prefix != "auto" {
			if prefix, err := netip.ParsePrefix(c.IPv6Prefix); err != nil || !prefix.Addr().Is6() {
				return invalid("IPv6 prefix")
			}
		}
	case INWX:
		if err := need(c.User, "user"); err != nil {
			return err
		}
		return need(c.Password, "password")
	case RFC2136:
		if _, err := serverAddress(c.Server); err != nil {
			return err
		}
		if !validName(c.Zone) {
			return invalid("zone")
		}
		if !validName(c.KeyName) {
			return invalid("key name")
		}
		if _, ok := tsigAlgorithms[c.Algorithm]; !ok {
			return invalid("algorithm")
		}
		if key, err := base64.StdEncoding.DecodeString(c.Secret); err != nil || len(key) == 0 {
			return invalid("the TSIG secret must be base64")
		}
	case DynDNS2:
		parsed, err := url.Parse(c.Server)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return invalid("update address")
		}
	}
	return nil
}

// Challenger sets the TXT records of the DNS challenge.
type Challenger interface {
	// SetTXT makes these the TXT records of a name (no trailing dot).
	SetTXT(ctx context.Context, fqdn string, values []string) error
	// ClearTXT removes the TXT records of a name again.
	ClearTXT(ctx context.Context, fqdn string) error
	// MultipleTXT tells whether a name can hold several TXT records at
	// once (a name and its wildcard need two).
	MultipleTXT() bool
}

// Updater points a host name at addresses.
type Updater interface {
	// SetAddresses sets the A record to v4 and the AAAA record to v6; an
	// address that is not valid leaves that record alone.
	SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error
}

// Options are for tests: another address for the API and another client.
type Options struct {
	Client  *http.Client
	BaseURL string
}

var defaultBase = map[string]string{
	Cloudflare: "https://api.cloudflare.com/client/v4",
	DeSEC:      "https://desec.io/api/v1",
	Hetzner:    "https://api.hetzner.cloud/v1",
	IONOS:      "https://api.hosting.ionos.com/dns/v1",
	Netcup:     "https://ccp.netcup.net/run/webservice/servers/endpoint.php?JSON",
	INWX:       "https://api.domrobot.com/jsonrpc/",
	DuckDNS:    "https://www.duckdns.org/update",
	IPv64Net:   "https://ipv64.net",
}

// New returns the provider for validated settings. It works as Challenger
// and as Updater, except dyndns2 which only updates addresses.
func New(c Config, opts Options) (any, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBase[c.Type]
	}
	api := &httpAPI{client: client, base: strings.TrimSuffix(base, "/")}
	switch c.Type {
	case Cloudflare:
		return &cloudflare{api: api, token: c.Token}, nil
	case DeSEC:
		return &desec{api: api, token: c.Token}, nil
	case Hetzner:
		return &hetzner{api: api, token: c.Token}, nil
	case IONOS:
		return &ionos{api: api, key: c.Token}, nil
	case Netcup:
		api.base = base // the query "?JSON" is part of it
		return &netcup{api: api, customer: c.Customer, key: c.Token, password: c.Password}, nil
	case INWX:
		api.base = base
		return &inwx{api: api, user: c.User, password: c.Password}, nil
	case DuckDNS:
		return &duckdns{api: api, token: c.Token}, nil
	case IPv64Net:
		p := NewIPv64(c.Token, c.UpdateToken, opts)
		p.prefix6 = c.IPv6Prefix
		return p, nil
	case RFC2136:
		server := c.Server
		if opts.BaseURL != "" {
			server = opts.BaseURL
		}
		addr, err := serverAddress(server)
		if err != nil {
			return nil, err
		}
		key, _ := base64.StdEncoding.DecodeString(c.Secret)
		return &rfc2136{server: addr, zone: c.Zone, keyName: c.KeyName, algorithm: c.Algorithm, secret: key}, nil
	case DynDNS2:
		return &dyndns2{api: api, address: c.Server, user: c.User, password: c.Password}, nil
	}
	return nil, invalid("unknown provider %q", c.Type)
}

// NewChallenger returns the provider for the DNS challenge.
func NewChallenger(c Config, opts Options) (Challenger, error) {
	p, err := New(c, opts)
	if err != nil {
		return nil, err
	}
	ch, ok := p.(Challenger)
	if !ok {
		return nil, invalid("%s cannot set TXT records", c.Type)
	}
	return ch, nil
}

// NewUpdater returns the provider for dynamic DNS.
func NewUpdater(c Config, opts Options) (Updater, error) {
	p, err := New(c, opts)
	if err != nil {
		return nil, err
	}
	return p.(Updater), nil
}

// PropagationDefault is how long to wait after setting a TXT record before
// Let's Encrypt is asked to look: some providers take minutes until all
// their name servers know a change.
func PropagationDefault(kind string) time.Duration {
	switch kind {
	case Cloudflare:
		return 15 * time.Second
	case IPv64Net:
		// SetTXT already waits until IPv64 has handed the records on.
		return 15 * time.Second
	case Netcup:
		return 8 * time.Minute
	case IONOS, INWX:
		return 2 * time.Minute
	case RFC2136:
		return 10 * time.Second
	}
	return time.Minute
}

// --- helpers shared by the HTTP APIs ---

// validName tells whether a DNS name (without trailing dot) is well formed.
func validName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

// ValidHost tells whether a host name may be given to a provider: lower
// case, at least two labels.
func ValidHost(name string) bool {
	return validName(name) && strings.Contains(name, ".")
}

// candidates are the names a zone could have for fqdn, longest first,
// without the top-level domain on its own.
func candidates(fqdn string) []string {
	labels := strings.Split(fqdn, ".")
	var out []string
	for i := 0; i < len(labels)-1; i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}

// relative returns the name relative to its zone, "@" for the zone itself.
func relative(fqdn, zone string) string {
	if fqdn == zone {
		return "@"
	}
	return strings.TrimSuffix(fqdn, "."+zone)
}

// inZone tells whether fqdn is the zone or below it.
func inZone(fqdn, zone string) bool {
	return fqdn == zone || strings.HasSuffix(fqdn, "."+zone)
}

func serverAddress(server string) (string, error) {
	if server == "" {
		return "", invalid("name server missing")
	}
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		host, port = strings.Trim(server, "[]"), "53"
	}
	if host == "" || strings.ContainsAny(host, " /") {
		return "", invalid("name server")
	}
	return net.JoinHostPort(host, port), nil
}

// APIError is an answer of a provider that says no. Its text comes from the
// provider and never holds a secret of TitanOS.
type APIError struct {
	Provider string
	Status   int
	Message  string
	// Code is a machine-readable reason, where the provider gives one.
	Code string
	// RetryAfter is how long the provider asks to wait (429).
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Message)
	}
	return e.Provider + ": " + e.Message
}

type httpAPI struct {
	client *http.Client
	base   string
}

// maxBody bounds what is read from a provider.
const maxBody = 1 << 20

// do sends a request and decodes a JSON answer into out. Errors never carry
// the address of the request: it may hold a token (DuckDNS, dyndns2).
func (h *httpAPI) do(ctx context.Context, name, method, address string, header http.Header, body, out any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: request could not be built", name)
	}
	for key, values := range header {
		req.Header[key] = values
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TitanOS")
	res, err := h.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, nil, fmt.Errorf("%s: %w", name, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return res.StatusCode, nil, fmt.Errorf("%s: %w", name, err)
	}
	if res.StatusCode >= 300 {
		return res.StatusCode, data, &APIError{Provider: name, Status: res.StatusCode, Message: short(data)}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return res.StatusCode, data, fmt.Errorf("%s: unexpected answer", name)
		}
	}
	return res.StatusCode, data, nil
}

// short makes a one-line excerpt of an answer for an error message.
func short(data []byte) string {
	text := strings.Join(strings.Fields(string(data)), " ")
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	if text == "" {
		text = "no details"
	}
	return text
}

// quote puts a TXT value in double quotes as zone files write it.
func quote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

// shortHash names a secret without showing it.
func shortHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:8])
}

// Combined is implemented by providers that may fill a missing address from
// where the request came from: both known addresses must go together.
type Combined interface {
	CombinedUpdate() bool
}

func decodeJSON(data []byte, v any) error { return json.Unmarshal(data, v) }

type addrRecord struct{ kind, value string }

// addressRecords lists the records to set: A for a valid v4, AAAA for v6.
func addressRecords(v4, v6 netip.Addr) []addrRecord {
	var out []addrRecord
	if v4.IsValid() {
		out = append(out, addrRecord{"A", v4.Unmap().String()})
	}
	if v6.IsValid() {
		out = append(out, addrRecord{"AAAA", v6.String()})
	}
	return out
}
