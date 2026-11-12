package dnsprovider

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// IPv64Integration is an alert channel of the account, names only.
type IPv64Integration struct {
	ID     int    `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Integrations lists the alert channels by name only: the view of
// FullIntegrations the DynDNS page reads.
func (c *IPv64) Integrations(ctx context.Context) ([]IPv64Integration, error) {
	full, err := c.FullIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]IPv64Integration, 0, len(full))
	for _, i := range full {
		list = append(list, IPv64Integration{ID: i.ID, Type: i.Type, Name: i.Name, Status: i.Status})
	}
	return list, nil
}

// --- integrations ---

// IPv64IntegrationInfo is an integration without its secrets: of options
// only the plain settings are kept.
type IPv64IntegrationInfo struct {
	ID         int            `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	Default    *bool          `json:"default"`
	Options    map[string]any `json:"options"`
	CreatedAt  *time.Time     `json:"created_at"`
	LastUsedAt *time.Time     `json:"last_used_at"`
	Uses       int            `json:"uses"`
}

// ipv64PlainOptions are the options of integrations that are no secrets.
var ipv64PlainOptions = []string{"priority", "priority_down", "priority_up", "device", "ping_user", "ping_group", "email", "number", "country_code", "chat_id"}

func (i *IPv64IntegrationInfo) strip() {
	kept := map[string]any{}
	for key, value := range i.Options {
		if slices.Contains(ipv64PlainOptions, key) {
			kept[key] = value
		}
	}
	i.Options = kept
}

// FullIntegrations lists the integrations, never with include_secrets.
func (c *IPv64) FullIntegrations(ctx context.Context) ([]IPv64IntegrationInfo, error) {
	list := []IPv64IntegrationInfo{}
	err := c.call(ctx, http.MethodGet, "/integrations", url.Values{"include_secrets": {"false"}}, nil, &list)
	for i := range list {
		list[i].strip()
	}
	return list, err
}
