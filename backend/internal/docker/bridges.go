package docker

// All networks of the engine for Docker › Netzwerke (PLAN 67-1): list,
// create plain bridge networks, delete and clean up unused ones. The LAN
// networks (macvlan/ipvlan, lan.go) keep their own routes; here they are
// listed with kind "lan" and not deletable.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Kinds of networks. Only "user" networks may be deleted here.
const (
	NetworkSystem  = "system"  // bridge, host, none of the engine
	NetworkLAN     = "lan"     // LAN networks of TitanOS (lan.go)
	NetworkTitanOS = "titanos" // other networks of TitanOS (remote view …)
	NetworkApp     = "app"     // networks of saved apps
	NetworkUser    = "user"    // everything else
)

var (
	bridgeNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
	// systemNetworks are the networks the engine brings along.
	systemNetworks = []string{"bridge", "host", "none"}
	// interfacePrefixes are the networks of the NAS's interfaces; a new
	// network must not overlap them. A variable for tests.
	interfacePrefixes = func() []netip.Prefix {
		var out []netip.Prefix
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return nil
		}
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil {
				out = append(out, netip.PrefixFrom(p.Addr().Unmap(), p.Bits()).Masked())
			}
		}
		return out
	}
)

// DockerNetwork is a network of the engine for Docker › Netzwerke.
type DockerNetwork struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Driver   string         `json:"driver"`
	Scope    string         `json:"scope"`
	Kind     string         `json:"kind"`
	Subnets  []string       `json:"subnets"`
	Gateways []string       `json:"gateways"`
	Internal bool           `json:"internal"`
	UsedBy   []ResourceUser `json:"usedBy"`
	// App is the saved app the network belongs to (Compose project or a
	// network its Compose file names).
	App       string `json:"app,omitempty"`
	Protected bool   `json:"protected"`
	CreatedAt string `json:"createdAt"`
}

// BridgeRequest creates a plain bridge network.
type BridgeRequest struct {
	Name string `json:"name"`
	// Subnet is optional (IPv4, private, /8 to /29); empty lets the engine
	// choose one.
	Subnet   string `json:"subnet"`
	Internal bool   `json:"internal"`
}

type rawNetwork struct {
	ID       string            `json:"Id"`
	Name     string            `json:"Name"`
	Driver   string            `json:"Driver"`
	Scope    string            `json:"Scope"`
	Internal bool              `json:"Internal"`
	Created  string            `json:"Created"`
	Labels   map[string]string `json:"Labels"`
	IPAM     struct {
		Config []struct{ Subnet, Gateway string }
	} `json:"IPAM"`
}

// mentions tells whether a Compose file names a network (as a whole word).
func mentions(text, name string) bool {
	word := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-'
	}
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		if (start == 0 || !word(text[start-1])) && (end == len(text) || !word(text[end])) {
			return true
		}
		i = start + 1
	}
}

// networkState is what the decisions need: the engine's networks, the
// containers on each, and the saved apps with their Compose files.
type networkState struct {
	networks []rawNetwork
	users    map[string][]ResourceUser // network ID → containers
	apps     []string
	compose  map[string]string
}

func (m *Manager) networkState(ctx context.Context) (networkState, error) {
	var st networkState
	if err := m.api.getJSON(ctx, "/networks", nil, &st.networks); err != nil {
		return st, mapAPIError(err)
	}
	var containers []struct {
		rawContainer
		NetworkSettings struct {
			Networks map[string]struct {
				NetworkID string `json:"NetworkID"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := m.api.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}}, &containers); err != nil {
		return st, mapAPIError(err)
	}
	byName := map[string]string{}
	for _, n := range st.networks {
		byName[n.Name] = n.ID
	}
	st.users = map[string][]ResourceUser{}
	for _, c := range containers {
		for name, n := range c.NetworkSettings.Networks {
			id := n.NetworkID
			if id == "" {
				id = byName[name] // stopped containers may not carry the ID
			}
			if id != "" {
				st.users[id] = append(st.users[id], userOf(c.rawContainer))
			}
		}
	}
	m.mu.Lock()
	st.apps = m.knownApps()
	m.mu.Unlock()
	st.compose = map[string]string{}
	for _, app := range st.apps {
		data, err := m.store.Read(composeFile(app))
		if err != nil {
			return st, err
		}
		st.compose[app] = string(data)
	}
	return st, nil
}

// classify gives a network its kind and, for app networks, the app.
func (st networkState) classify(n rawNetwork) (kind, app string) {
	switch {
	case slices.Contains(systemNetworks, n.Name):
		return NetworkSystem, ""
	case strings.HasPrefix(n.Name, "titanos-lan-"):
		return NetworkLAN, ""
	case strings.HasPrefix(n.Name, "titanos-") || strings.HasPrefix(n.Name, "titanos_") || n.Labels[ViewerLabel] != "":
		return NetworkTitanOS, ""
	}
	if project := n.Labels[composeProjectLabel]; project != "" && slices.Contains(st.apps, project) {
		return NetworkApp, project
	}
	for _, a := range st.apps {
		if mentions(st.compose[a], n.Name) {
			return NetworkApp, a
		}
	}
	return NetworkUser, ""
}

func (st networkState) item(n rawNetwork) DockerNetwork {
	kind, app := st.classify(n)
	item := DockerNetwork{ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Kind: kind, Subnets: []string{}, Gateways: []string{},
		Internal: n.Internal, UsedBy: st.users[n.ID], App: app, Protected: kind != NetworkUser, CreatedAt: n.Created}
	for _, c := range n.IPAM.Config {
		if c.Subnet != "" {
			item.Subnets = append(item.Subnets, c.Subnet)
		}
		if c.Gateway != "" {
			item.Gateways = append(item.Gateways, c.Gateway)
		}
	}
	if item.UsedBy == nil {
		item.UsedBy = []ResourceUser{}
	}
	return item
}

// AllNetworks lists every network of the engine.
func (m *Manager) AllNetworks(ctx context.Context) ([]DockerNetwork, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	st, err := m.networkState(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DockerNetwork, 0, len(st.networks))
	for _, n := range st.networks {
		out = append(out, st.item(n))
	}
	order := map[string]int{NetworkUser: 0, NetworkApp: 1, NetworkLAN: 2, NetworkSystem: 3, NetworkTitanOS: 4}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return order[out[i].Kind] < order[out[j].Kind]
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// CreateBridge creates a plain bridge network. A given subnet must be a
// private IPv4 network that overlaps no network of the engine, of the
// LAN networks or of the NAS's interfaces.
func (m *Manager) CreateBridge(ctx context.Context, req BridgeRequest) (DockerNetwork, error) {
	if err := m.ready(); err != nil {
		return DockerNetwork{}, err
	}
	lower := strings.ToLower(req.Name)
	if !bridgeNameRE.MatchString(req.Name) || slices.Contains(systemNetworks, lower) || strings.HasPrefix(lower, "titanos") {
		return DockerNetwork{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	var subnet netip.Prefix
	if req.Subnet != "" {
		p, err := netip.ParsePrefix(req.Subnet)
		if err != nil || !p.Addr().Is4() || p != p.Masked() || !p.Addr().IsPrivate() || p.Bits() < 8 || p.Bits() > 29 {
			return DockerNetwork{}, fmt.Errorf("%w: subnet (private IPv4, /8 to /29)", ErrInvalid)
		}
		subnet = p
	}
	// One change of networks at a time: the overlap check must hold.
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	st, err := m.networkState(ctx)
	if err != nil {
		return DockerNetwork{}, err
	}
	for _, n := range st.networks {
		if strings.EqualFold(n.Name, req.Name) {
			return DockerNetwork{}, ErrExists
		}
	}
	if subnet.IsValid() {
		var taken []netip.Prefix
		for _, n := range st.networks {
			for _, c := range n.IPAM.Config {
				if p, err := netip.ParsePrefix(c.Subnet); err == nil {
					taken = append(taken, p)
				}
			}
		}
		for _, l := range m.lan {
			if p, err := netip.ParsePrefix(l.Subnet); err == nil {
				taken = append(taken, p)
			}
		}
		taken = append(taken, interfacePrefixes()...)
		for _, p := range taken {
			if p.Overlaps(subnet) {
				return DockerNetwork{}, fmt.Errorf("%w: the subnet overlaps %s", ErrOverlap, p)
			}
		}
	}
	body := map[string]any{"Name": req.Name, "Driver": "bridge", "Internal": req.Internal, "CheckDuplicate": true, "Attachable": true}
	if subnet.IsValid() {
		body["IPAM"] = map[string]any{"Driver": "default", "Config": []map[string]string{{"Subnet": subnet.String()}}}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := m.api.sendJSON(ctx, http.MethodPost, "/networks/create", body, &created); err != nil {
		var api *apiError
		if errors.As(err, &api) && api.Status == http.StatusConflict {
			return DockerNetwork{}, ErrExists
		}
		if errors.As(err, &api) && api.Status < 500 {
			return DockerNetwork{}, fmt.Errorf("%w: %s", ErrInvalid, api.Message)
		}
		return DockerNetwork{}, mapAPIError(err)
	}
	var n rawNetwork
	if err := m.api.getJSON(ctx, "/networks/"+url.PathEscape(created.ID), nil, &n); err != nil {
		return DockerNetwork{ID: created.ID, Name: req.Name, Driver: "bridge", Kind: NetworkUser, Subnets: []string{}, Gateways: []string{}, UsedBy: []ResourceUser{}, Internal: req.Internal}, nil
	}
	return st.item(n), nil
}

// ErrOverlap refuses a subnet that is already used.
var ErrOverlap = errors.New("docker: the subnet is in use")

func (m *Manager) deleteNetwork(ctx context.Context, id string) error {
	res, err := m.api.do(ctx, http.MethodDelete, "/networks/"+url.PathEscape(id), nil)
	if err == nil {
		res.Body.Close()
		return nil
	}
	var api *apiError
	if errors.As(err, &api) && (api.Status == http.StatusConflict || api.Status == http.StatusForbidden) {
		return fmt.Errorf("%w: %s", ErrInUse, api.Message)
	}
	if notFound(err) {
		return nil
	}
	return mapAPIError(err)
}

// RemoveNetwork deletes a network no container uses that is neither the
// engine's, TitanOS's nor a saved app's. Never forced.
func (m *Manager) RemoveNetwork(ctx context.Context, name string) error {
	if !bridgeNameRE.MatchString(name) {
		return ErrNotFound
	}
	if err := m.cleanable(); err != nil {
		return err
	}
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	st, err := m.networkState(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(st.networks, func(n rawNetwork) bool { return n.Name == name || n.ID == name })
	if i < 0 {
		return ErrNotFound
	}
	item := st.item(st.networks[i])
	switch {
	case item.Protected:
		return ErrProtected
	case len(item.UsedBy) > 0:
		return ErrInUse
	}
	return m.deleteNetwork(ctx, item.ID)
}

// PruneNetworks deletes the unused user networks.
func (m *Manager) PruneNetworks(ctx context.Context) (PruneResult, error) {
	result := PruneResult{Deleted: []string{}, Failed: []PruneFailure{}}
	if err := m.cleanable(); err != nil {
		return result, err
	}
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	st, err := m.networkState(ctx)
	if err != nil {
		return result, err
	}
	for _, n := range st.networks {
		item := st.item(n)
		if item.Protected || len(item.UsedBy) > 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := m.deleteNetwork(ctx, n.ID); err != nil {
			result.Failed = append(result.Failed, PruneFailure{ID: n.Name, Error: err.Error()})
			continue
		}
		result.Deleted = append(result.Deleted, n.Name)
	}
	return result, nil
}
