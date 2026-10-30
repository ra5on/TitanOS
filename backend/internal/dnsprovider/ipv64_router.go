package dnsprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// --- Cloud Router ---

// IPv64Peer is a WireGuard peer of a gateway (never with its config).
type IPv64Peer struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	State           string  `json:"state"`
	Disabled        bool    `json:"disabled"`
	SiteToSite      bool    `json:"site_to_site"`
	IPv4            *string `json:"ipv4"`
	IPv6            *string `json:"ipv6"`
	DNSServer       *string `json:"dns_server"`
	Endpoint        *string `json:"endpoint"`
	EndpointPort    *int    `json:"endpoint_port"`
	Keepalive       string  `json:"keepalive"`
	PublicKey       *string `json:"public_key"`
	HasPresharedKey bool    `json:"has_preshared_key"`
	CurrentEndpoint *string `json:"current_endpoint"`
	LastHandshake   *string `json:"last_handshake"`
	TrafficBytes    struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"traffic_bytes"`
	AllowedIPs []struct {
		ID      int64  `json:"id"`
		Network string `json:"network"`
	} `json:"allowed_ips,omitempty"`
}

// IPv64Gateway is a WireGuard gateway of a router.
type IPv64Gateway struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Disabled       bool   `json:"disabled"`
	TrafficLimited bool   `json:"traffic_limited"`
	Server         *struct {
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		Location string `json:"location"`
	} `json:"server"`
	ListenPort *int    `json:"listen_port"`
	IPv4       *string `json:"ipv4"`
	IPv6       *string `json:"ipv6"`
	PublicKey  *string `json:"public_key"`
	OSPF       *struct {
		AreaID      string `json:"area_id"`
		OSPFVersion int    `json:"ospf_version"`
		Cost        int    `json:"cost"`
	} `json:"ospf"`
	TrafficBytes struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"traffic_bytes"`
	CreatedAt *time.Time  `json:"created_at"`
	Peers     []IPv64Peer `json:"peers,omitempty"`
}

// IPv64PublicIP is a public address of a router.
type IPv64PublicIP struct {
	ID        int64      `json:"id"`
	IP        string     `json:"ip"`
	Type      string     `json:"type"`
	Disabled  bool       `json:"disabled"`
	State     string     `json:"state"`
	CreatedAt *time.Time `json:"created_at"`
}

// IPv64Router is a Cloud Router.
type IPv64Router struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	VRF            string          `json:"vrf"`
	TrafficLimited bool            `json:"traffic_limited"`
	Gateways       []IPv64Gateway  `json:"gateways"`
	PublicIPs      []IPv64PublicIP `json:"public_ips,omitempty"`
}

// IPv64Route is a static route of a router.
type IPv64Route struct {
	ID          int64   `json:"id"`
	Destination string  `json:"destination"`
	Gateway     string  `json:"gateway"`
	Distance    int     `json:"distance"`
	Peer        *string `json:"peer"`
	System      bool    `json:"system"`
	Disabled    bool    `json:"disabled"`
	State       string  `json:"state"`
}

// IPv64RouterRule is a firewall rule of a router.
type IPv64RouterRule struct {
	ID          int64   `json:"id"`
	Chain       string  `json:"chain"`
	Action      string  `json:"action"`
	Family      string  `json:"family"`
	Source      *string `json:"source"`
	Destination *string `json:"destination"`
	Disabled    bool    `json:"disabled"`
	State       string  `json:"state"`
}

// Router enums.
var (
	IPv64RuleChains  = []string{"forward", "input", "output"}
	IPv64RuleActions = []string{"accept", "drop"}
)

func routerPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("router")
	}
	return "/cloud-routers/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

func validName100(name string) bool {
	return len(name) <= 100 && !strings.ContainsAny(name, "\r\n")
}

// Routers lists the Cloud Routers.
func (c *IPv64) Routers(ctx context.Context) ([]IPv64Router, error) {
	list := []IPv64Router{}
	err := c.call(ctx, http.MethodGet, "/cloud-routers", nil, nil, &list)
	return list, err
}

// Router reads a router with gateways, peers and addresses.
func (c *IPv64) Router(ctx context.Context, token string) (IPv64Router, error) {
	path, err := routerPath(token)
	if err != nil {
		return IPv64Router{}, err
	}
	var r IPv64Router
	err = c.call(ctx, http.MethodGet, path, nil, nil, &r)
	return r, err
}

// CreateRouter makes a router with its first gateway (202).
func (c *IPv64) CreateRouter(ctx context.Context, name string) (IPv64Router, error) {
	if !validName100(name) {
		return IPv64Router{}, invalid("name")
	}
	var r IPv64Router
	err := c.call(ctx, http.MethodPost, "/cloud-routers", nil, map[string]string{"name": name}, &r)
	return r, err
}

// RenameRouter renames a router.
func (c *IPv64) RenameRouter(ctx context.Context, token, name string) (IPv64Router, error) {
	path, err := routerPath(token)
	if err != nil || !validName100(name) || name == "" {
		return IPv64Router{}, invalid("router or name")
	}
	var r IPv64Router
	err = c.call(ctx, http.MethodPatch, path, nil, map[string]string{"name": name}, &r)
	return r, err
}

// CreateGateway adds a gateway to a router (202).
func (c *IPv64) CreateGateway(ctx context.Context, token, name string) (IPv64Gateway, error) {
	path, err := routerPath(token, "/gateways")
	if err != nil || !validName100(name) {
		return IPv64Gateway{}, invalid("router or name")
	}
	var g IPv64Gateway
	err = c.call(ctx, http.MethodPost, path, nil, map[string]string{"name": name}, &g)
	return g, err
}

// Routes lists the routes of a router.
func (c *IPv64) Routes(ctx context.Context, token string) ([]IPv64Route, error) {
	path, err := routerPath(token, "/routes")
	if err != nil {
		return nil, err
	}
	list := []IPv64Route{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// RouteInput is a new static route into a network behind a peer.
type RouteInput struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Peer        string `json:"peer"`
	Distance    int    `json:"distance,omitempty"`
}

// Validate checks the network, the next hop of the same family and distance.
func (in RouteInput) Validate() error {
	dest, err := netip.ParsePrefix(in.Destination)
	if err != nil {
		return invalid("destination is a network in CIDR")
	}
	hop, err := netip.ParseAddr(in.Gateway)
	if err != nil || hop.Is4() != dest.Addr().Is4() {
		return invalid("gateway is an address of the same family")
	}
	if !validRef(in.Peer) {
		return invalid("peer")
	}
	if in.Distance != 0 && (in.Distance < 1 || in.Distance > 255) {
		return invalid("distance is 1 to 255")
	}
	return nil
}

// CreateRoute adds a route (202).
func (c *IPv64) CreateRoute(ctx context.Context, token string, in RouteInput) (IPv64Route, error) {
	if err := in.Validate(); err != nil {
		return IPv64Route{}, err
	}
	path, err := routerPath(token, "/routes")
	if err != nil {
		return IPv64Route{}, err
	}
	var r IPv64Route
	err = c.call(ctx, http.MethodPost, path, nil, in, &r)
	return r, err
}

// RouterItem switches a route, rule or public IP on or off (kind routes,
// firewall, public-ips) or, with remove, deletes it.
func (c *IPv64) RouterItem(ctx context.Context, token, kind string, id int64, disabled *bool) (json.RawMessage, error) {
	if !slices.Contains([]string{"routes", "firewall", "public-ips"}, kind) {
		return nil, invalid("kind")
	}
	path, err := routerPath(token, "/", kind, "/", strconv.FormatInt(id, 10))
	if err != nil {
		return nil, err
	}
	method, body := http.MethodDelete, any(nil)
	if disabled != nil {
		method, body = http.MethodPatch, map[string]bool{"disabled": *disabled}
	}
	answer, err := c.Do(ctx, method, path, nil, body)
	return answer.Data, err
}

// RouterRules lists the firewall rules of a router.
func (c *IPv64) RouterRules(ctx context.Context, token string) ([]IPv64RouterRule, error) {
	path, err := routerPath(token, "/firewall")
	if err != nil {
		return nil, err
	}
	list := []IPv64RouterRule{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// RouterRuleInput is a new firewall rule of a router.
type RouterRuleInput struct {
	Chain       string `json:"chain"`
	Action      string `json:"action"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
}

func ipOrCIDR(text string) (netip.Addr, bool) {
	if p, err := netip.ParsePrefix(text); err == nil {
		return p.Addr(), true
	}
	a, err := netip.ParseAddr(text)
	return a, err == nil
}

// Validate checks chain, action and that source and destination share a family.
func (in RouterRuleInput) Validate() error {
	if !slices.Contains(IPv64RuleChains, in.Chain) || !slices.Contains(IPv64RuleActions, in.Action) {
		return invalid("chain or action")
	}
	if in.Source == "" && in.Destination == "" {
		return invalid("source or destination is required")
	}
	var families []bool
	for _, text := range []string{in.Source, in.Destination} {
		if text == "" {
			continue
		}
		addr, ok := ipOrCIDR(text)
		if !ok {
			return invalid("source and destination are IPs or CIDR")
		}
		families = append(families, addr.Is4())
	}
	if len(families) == 2 && families[0] != families[1] {
		return invalid("source and destination of different families")
	}
	return nil
}

// CreateRouterRule adds a firewall rule (202).
func (c *IPv64) CreateRouterRule(ctx context.Context, token string, in RouterRuleInput) (IPv64RouterRule, error) {
	if err := in.Validate(); err != nil {
		return IPv64RouterRule{}, err
	}
	path, err := routerPath(token, "/firewall")
	if err != nil {
		return IPv64RouterRule{}, err
	}
	var r IPv64RouterRule
	err = c.call(ctx, http.MethodPost, path, nil, in, &r)
	return r, err
}

// PublicIPs lists the public addresses of a router.
func (c *IPv64) PublicIPs(ctx context.Context, token string) ([]IPv64PublicIP, error) {
	path, err := routerPath(token, "/public-ips")
	if err != nil {
		return nil, err
	}
	list := []IPv64PublicIP{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// CreatePublicIPv6 asks for the one public IPv6 of a router (202).
func (c *IPv64) CreatePublicIPv6(ctx context.Context, token string) (IPv64PublicIP, error) {
	path, err := routerPath(token, "/public-ips")
	if err != nil {
		return IPv64PublicIP{}, err
	}
	var ip IPv64PublicIP
	err = c.call(ctx, http.MethodPost, path, nil, nil, &ip)
	return ip, err
}

func gatewayPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("gateway")
	}
	return "/gateways/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

// Gateway reads a gateway with its peers.
func (c *IPv64) Gateway(ctx context.Context, token string) (IPv64Gateway, error) {
	path, err := gatewayPath(token)
	if err != nil {
		return IPv64Gateway{}, err
	}
	var g IPv64Gateway
	err = c.call(ctx, http.MethodGet, path, nil, nil, &g)
	return g, err
}

// GatewayInput is a PATCH of a gateway; addresses in CIDR, "" removes one.
type GatewayInput struct {
	Name     *string `json:"name,omitempty"`
	IPv4     *string `json:"ipv4,omitempty"`
	IPv6     *string `json:"ipv6,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

// Validate checks name and addresses.
func (in GatewayInput) Validate() error {
	if in.Name != nil && (!validName100(*in.Name) || *in.Name == "") {
		return invalid("name")
	}
	for _, field := range []struct {
		value *string
		v4    bool
	}{{in.IPv4, true}, {in.IPv6, false}} {
		if field.value == nil || *field.value == "" {
			continue
		}
		p, err := netip.ParsePrefix(*field.value)
		if err != nil || p.Addr().Is4() != field.v4 {
			return invalid("gateway addresses are in CIDR of their family")
		}
	}
	return nil
}

// ChangeGateway changes a gateway.
func (c *IPv64) ChangeGateway(ctx context.Context, token string, in GatewayInput) (IPv64Gateway, error) {
	if err := in.Validate(); err != nil {
		return IPv64Gateway{}, err
	}
	path, err := gatewayPath(token)
	if err != nil {
		return IPv64Gateway{}, err
	}
	var g IPv64Gateway
	err = c.call(ctx, http.MethodPatch, path, nil, in, &g)
	return g, err
}

// GatewayAction is delete, rotate-keys or reset-traffic of a gateway.
func (c *IPv64) GatewayAction(ctx context.Context, token, action string) (IPv64Gateway, error) {
	method, rest := http.MethodPost, "/"+action
	switch action {
	case "delete":
		method, rest = http.MethodDelete, ""
	case "rotate-keys", "reset-traffic":
	default:
		return IPv64Gateway{}, invalid("action")
	}
	path, err := gatewayPath(token, rest)
	if err != nil {
		return IPv64Gateway{}, err
	}
	var g IPv64Gateway
	err = c.call(ctx, method, path, nil, nil, &g)
	return g, err
}

// CreatePeer adds a peer; keys and addresses are given by IPv64 (202).
func (c *IPv64) CreatePeer(ctx context.Context, gateway, name string) (IPv64Peer, error) {
	path, err := gatewayPath(gateway, "/peers")
	if err != nil || !validName100(name) {
		return IPv64Peer{}, invalid("gateway or name")
	}
	var p IPv64Peer
	err = c.call(ctx, http.MethodPost, path, nil, map[string]string{"name": name}, &p)
	return p, err
}

func peerPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("peer")
	}
	return "/peers/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

// Peer reads a peer with its AllowedIPs.
func (c *IPv64) Peer(ctx context.Context, token string) (IPv64Peer, error) {
	path, err := peerPath(token)
	if err != nil {
		return IPv64Peer{}, err
	}
	var p IPv64Peer
	err = c.call(ctx, http.MethodGet, path, nil, nil, &p)
	return p, err
}

// DeletePeer removes a peer (202).
func (c *IPv64) DeletePeer(ctx context.Context, token string) error {
	path, err := peerPath(token)
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, path, nil, nil, nil)
}

// SetPeerPublicKey hands IPv64 only the public key of a pair made on the
// NAS: the private key never leaves it, the config holds none.
func (c *IPv64) SetPeerPublicKey(ctx context.Context, token, publicKey string) (IPv64Peer, error) {
	path, err := peerPath(token, "/keys")
	if err != nil {
		return IPv64Peer{}, err
	}
	if !validWGKey(publicKey) {
		return IPv64Peer{}, invalid("public key")
	}
	var p IPv64Peer
	err = c.call(ctx, http.MethodPut, path, nil, map[string]string{"public_key": publicKey}, &p)
	return p, err
}

// PeerConfig reads the WireGuard configuration of a peer (for titand only).
func (c *IPv64) PeerConfig(ctx context.Context, token string) (string, error) {
	path, err := peerPath(token, "/config")
	if err != nil {
		return "", err
	}
	var out struct {
		Config string `json:"config"`
	}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &out)
	return out.Config, err
}

var wgKeyRE = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=$`)

func validWGKey(key string) bool { return wgKeyRE.MatchString(key) }
