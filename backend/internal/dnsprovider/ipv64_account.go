package dnsprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// --- account ---

// IPv64Usage is how much of a limit is used; Limit nil is unlimited.
type IPv64Usage struct {
	Used  float64 `json:"used"`
	Limit *int    `json:"limit"`
}

// IPv64Account is GET /me without the e-mail address and the key.
type IPv64Account struct {
	Username   string                `json:"username"`
	Plan       string                `json:"plan"`
	ValidUntil *time.Time            `json:"validUntil,omitempty"`
	Usage      map[string]IPv64Usage `json:"usage"`
	KeyName    string                `json:"keyName,omitempty"`
}

// Account tells the plan and the use of its limits: the smaller view of Me
// the DynDNS page and the heartbeat read.
func (c *IPv64) Account(ctx context.Context) (IPv64Account, error) {
	me, err := c.Me(ctx)
	if err != nil {
		return IPv64Account{}, err
	}
	out := IPv64Account{Plan: me.Plan.Name, ValidUntil: me.Plan.ValidUntil, Usage: make(map[string]IPv64Usage, len(me.Usage)), KeyName: me.Auth.KeyName}
	if me.Username != nil {
		out.Username = *me.Username
	}
	for name, usage := range me.Usage {
		out.Usage[name] = IPv64Usage{Used: usage.Used, Limit: usage.Limit}
	}
	return out, nil
}

// IPv64UsageLimit is one entry of usage in /me.
type IPv64UsageLimit struct {
	Used    float64 `json:"used"`
	Limit   *int    `json:"limit"`
	Limited *bool   `json:"limited,omitempty"`
}

// IPv64Me is GET /me without the e-mail address and the account ID.
type IPv64Me struct {
	Username     *string    `json:"username"`
	RegisteredAt *time.Time `json:"registered_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	Plan         struct {
		ID         int        `json:"id"`
		Name       string     `json:"name"`
		ValidUntil *time.Time `json:"valid_until"`
	} `json:"plan"`
	Usage            map[string]IPv64UsageLimit `json:"usage"`
	LogRetentionDays int                        `json:"log_retention_days"`
	Auth             struct {
		Type    string `json:"type"`
		KeyID   *int64 `json:"key_id"`
		KeyName string `json:"key_name"`
	} `json:"auth"`
}

// Me reads the account, its plan and the use of its limits.
func (c *IPv64) Me(ctx context.Context) (IPv64Me, error) {
	var me IPv64Me
	err := c.call(ctx, http.MethodGet, "/me", nil, nil, &me)
	if me.Usage == nil {
		me.Usage = map[string]IPv64UsageLimit{}
	}
	return me, err
}

// IPv64LogEntry is an entry of the account log.
type IPv64LogEntry struct {
	ID       int64     `json:"id"`
	DomainID *int64    `json:"domain_id"`
	Title    string    `json:"title"`
	Message  string    `json:"message"`
	Time     time.Time `json:"time"`
}

// IPv64LogPage is one page of the log; NextBefore nil is the end.
type IPv64LogPage struct {
	Entries       []IPv64LogEntry `json:"entries"`
	NextBefore    *int64          `json:"next_before"`
	RetentionDays int             `json:"retention_days"`
}

// Logs reads the account log, newest first; before is the cursor (0 = from
// the newest), limit 1 to 500.
func (c *IPv64) Logs(ctx context.Context, limit int, before int64) (IPv64LogPage, error) {
	if limit < 1 || limit > 500 {
		return IPv64LogPage{}, invalid("limit is 1 to 500")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if before > 0 {
		query.Set("before", strconv.FormatInt(before, 10))
	}
	answer, err := c.Do(ctx, http.MethodGet, "/me/logs", query, nil)
	if err != nil {
		return IPv64LogPage{}, err
	}
	page := IPv64LogPage{Entries: []IPv64LogEntry{}}
	if err := decodeData(answer.Data, &page.Entries); err != nil {
		return IPv64LogPage{}, err
	}
	var meta struct {
		NextBefore    *int64 `json:"next_before"`
		RetentionDays int    `json:"retention_days"`
	}
	if len(answer.Meta) > 0 {
		_ = json.Unmarshal(answer.Meta, &meta) // a value that cannot be read leaves the zero value, which the code handles
	}
	page.NextBefore, page.RetentionDays = meta.NextBefore, meta.RetentionDays
	return page, nil
}

// IPv64APIKey is an API key of the account; the key itself is never listed.
type IPv64APIKey struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	TokenHint string     `json:"token_hint"`
	CreatedAt *time.Time `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Expired   bool       `json:"expired"`
	Uses      int        `json:"uses"`
}

// APIKeys lists the API keys of the account.
func (c *IPv64) APIKeys(ctx context.Context) ([]IPv64APIKey, error) {
	list := []IPv64APIKey{}
	err := c.call(ctx, http.MethodGet, "/api-keys", nil, nil, &list)
	return list, err
}

// MaxIPv64KeyLifetime is the longest validity POST /api-keys takes.
const MaxIPv64KeyLifetime = 315360000

// CreateAPIKey makes a key valid for expiresIn seconds (0 = unlimited) and
// returns it with the full key, which IPv64 shows this one time only.
func (c *IPv64) CreateAPIKey(ctx context.Context, name string, expiresIn int) (IPv64APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "\r\n") {
		return IPv64APIKey{}, "", invalid("name")
	}
	if expiresIn < 0 || expiresIn > MaxIPv64KeyLifetime {
		return IPv64APIKey{}, "", invalid("expires_in is 0 to %d seconds", MaxIPv64KeyLifetime)
	}
	var created struct {
		IPv64APIKey
		Token string `json:"token"`
	}
	err := c.call(ctx, http.MethodPost, "/api-keys", nil, map[string]any{"name": name, "expires_in": expiresIn}, &created)
	return created.IPv64APIKey, created.Token, err
}

// DeleteAPIKey removes a key (IPv64 refuses the key of the request itself).
func (c *IPv64) DeleteAPIKey(ctx context.Context, id int64) error {
	return c.call(ctx, http.MethodDelete, "/api-keys/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

// --- blocklist ---

// IPv64ListToken is GET /honeypot/list-token: the personal token of the
// v64 blocklist and the base address of its files.
type IPv64ListToken struct {
	Token string `json:"token"`
	Live  bool   `json:"live"`
	Base  string `json:"base"`
}

// ListToken reads (or creates) the list token. Only for titand.
func (c *IPv64) ListToken(ctx context.Context) (IPv64ListToken, error) {
	var t IPv64ListToken
	err := c.call(ctx, http.MethodGet, "/honeypot/list-token", nil, nil, &t)
	return t, err
}
