package dnsprovider

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

// IPv64IntegrationTypes are the kinds of integrations that can be created.
var IPv64IntegrationTypes = []string{"telegram", "gotify", "ntfy", "pushover", "discord", "webhook", "email", "sms", "app"}

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

// IntegrationInput is the body of POST /integrations.
type IntegrationInput struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Default      *bool  `json:"default,omitempty"`
	ChatID       string `json:"chat_id,omitempty"`
	APIToken     string `json:"api_token,omitempty"`
	ServerURL    string `json:"server_url,omitempty"`
	AppToken     string `json:"app_token,omitempty"`
	UserToken    string `json:"user_token,omitempty"`
	Device       string `json:"device,omitempty"`
	Priority     *int   `json:"priority,omitempty"`
	PriorityDown *int   `json:"priority_down,omitempty"`
	PriorityUp   *int   `json:"priority_up,omitempty"`
	WebhookURL   string `json:"webhook_url,omitempty"`
	PingUser     string `json:"ping_user,omitempty"`
	PingGroup    string `json:"ping_group,omitempty"`
	URLDown      string `json:"url_down,omitempty"`
	URLUp        string `json:"url_up,omitempty"`
	Email        string `json:"email,omitempty"`
	Number       string `json:"number,omitempty"`
	CountryCode  string `json:"country_code,omitempty"`
	DeviceToken  string `json:"device_token,omitempty"`
}

func inRange(v *int, low, high int) bool { return v == nil || (*v >= low && *v <= high) }

// Validate checks the fields each type needs (POST /integrations).
func (in IntegrationInput) Validate() error {
	if !slices.Contains(IPv64IntegrationTypes, in.Type) {
		return invalid("type")
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || strings.ContainsAny(in.Name, "\r\n") {
		return invalid("name")
	}
	for _, text := range []string{in.ChatID, in.APIToken, in.AppToken, in.UserToken, in.Device, in.PingUser, in.PingGroup, in.Email, in.Number, in.CountryCode, in.DeviceToken} {
		if len(text) > 512 || strings.ContainsAny(text, "\r\n") {
			return invalid("field too long")
		}
	}
	need := func(ok bool, field string) error {
		if !ok {
			return invalid("%s is required for %s", field, in.Type)
		}
		return nil
	}
	urlOK := func(text string) bool { return text == "" || httpURL(text) }
	if !urlOK(in.ServerURL) || !urlOK(in.WebhookURL) || !urlOK(in.URLDown) || !urlOK(in.URLUp) {
		return invalid("URLs begin with http:// or https://")
	}
	switch in.Type {
	case "telegram":
		if err := need(in.ChatID != "", "chat_id"); err != nil {
			return err
		}
		return need(in.APIToken != "", "api_token")
	case "gotify":
		if err := need(in.ServerURL != "" && in.AppToken != "", "server_url and app_token"); err != nil {
			return err
		}
		if !inRange(in.Priority, 0, 10) {
			return invalid("priority is 0 to 10")
		}
	case "ntfy":
		if err := need(in.ServerURL != "", "server_url"); err != nil {
			return err
		}
		if !inRange(in.PriorityDown, 1, 5) || !inRange(in.PriorityUp, 1, 5) {
			return invalid("priorities are 1 to 5")
		}
	case "pushover":
		if err := need(in.UserToken != "" && in.AppToken != "", "user_token and app_token"); err != nil {
			return err
		}
		if !inRange(in.Priority, -2, 1) {
			return invalid("priority is -2 to 1")
		}
	case "discord":
		return need(in.WebhookURL != "", "webhook_url")
	case "webhook":
		return need(in.URLDown != "" || in.URLUp != "", "url_down or url_up")
	case "email":
		return need(strings.Count(in.Email, "@") == 1 && !strings.ContainsAny(in.Email, " <>"), "email")
	case "sms":
		if err := need(in.Number != "", "number"); err != nil {
			return err
		}
		if in.CountryCode != "" && in.CountryCode != "0049" {
			return invalid("country_code is 0049")
		}
	case "app":
		return need(in.DeviceToken != "", "device_token")
	}
	return nil
}

// CreateIntegration adds an integration; e-mail and SMS wait for a
// confirmation link.
func (c *IPv64) CreateIntegration(ctx context.Context, in IntegrationInput) (IPv64IntegrationInfo, error) {
	if in.Type == "sms" && in.CountryCode == "" {
		in.CountryCode = "0049"
	}
	if err := in.Validate(); err != nil {
		return IPv64IntegrationInfo{}, err
	}
	var i IPv64IntegrationInfo
	err := c.call(ctx, http.MethodPost, "/integrations", nil, in, &i)
	i.strip()
	return i, err
}

// ChangeIntegration renames an integration or changes its preselection.
func (c *IPv64) ChangeIntegration(ctx context.Context, id int, name *string, preselect *bool) (IPv64IntegrationInfo, error) {
	body := map[string]any{}
	if name != nil {
		if strings.TrimSpace(*name) == "" || len(*name) > 100 {
			return IPv64IntegrationInfo{}, invalid("name")
		}
		body["name"] = *name
	}
	if preselect != nil {
		body["default"] = *preselect
	}
	var i IPv64IntegrationInfo
	err := c.call(ctx, http.MethodPatch, "/integrations/"+strconv.Itoa(id), nil, body, &i)
	i.strip()
	return i, err
}

// DeleteIntegration removes an integration.
func (c *IPv64) DeleteIntegration(ctx context.Context, id int) error {
	return c.call(ctx, http.MethodDelete, "/integrations/"+strconv.Itoa(id), nil, nil, nil)
}

// TestIntegration sends a test message (one per 30 seconds and integration).
func (c *IPv64) TestIntegration(ctx context.Context, id int) error {
	return c.call(ctx, http.MethodPost, "/integrations/"+strconv.Itoa(id)+"/test", nil, nil, nil)
}
