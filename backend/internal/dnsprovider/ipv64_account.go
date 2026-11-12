package dnsprovider

import (
	"context"
	"net/http"
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

// --- blocklist ---
