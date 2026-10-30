package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// --- CDN ---

// IPv64CDNMapping is a port of the portmapper.
type IPv64CDNMapping struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ListenPort int     `json:"listen_port"`
	Transport  string  `json:"transport"`
	TargetPort int     `json:"target_port"`
	Record     string  `json:"record"`
	FQDN       *string `json:"fqdn"`
}

// IPv64CDN is a domain in the CDN, with its mappings in the detail view.
type IPv64CDN struct {
	ID                string   `json:"id"`
	Domain            *string  `json:"domain"`
	DomainID          int64    `json:"domain_id"`
	Active            bool     `json:"active"`
	Pending           []string `json:"pending"`
	DNSRecordsEnabled bool     `json:"dns_records_enabled"`
	Server            *struct {
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		IPv4     string `json:"ipv4"`
		IPv6     string `json:"ipv6"`
	} `json:"server"`
	Proxy struct {
		State    string  `json:"state"`
		Frontend *string `json:"frontend"`
		Backend  struct {
			Scheme string `json:"scheme"`
			Port   int    `json:"port"`
		} `json:"backend"`
		Websocket   bool   `json:"websocket"`
		Certificate string `json:"certificate"`
		Caching     struct {
			Enabled     bool `json:"enabled"`
			TimeMinutes int  `json:"time_minutes"`
			SizeMB      int  `json:"size_mb"`
		} `json:"caching"`
		AccessList struct {
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"`
		} `json:"access_list"`
	} `json:"proxy"`
	Portmapper struct {
		State string `json:"state"`
	} `json:"portmapper"`
	TrafficMB struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"traffic_mb"`
	CreatedAt *time.Time        `json:"created_at"`
	Mappings  []IPv64CDNMapping `json:"mappings,omitempty"`
}

// CDN enums.
var (
	IPv64CDNFrontends  = []string{"http", "https_redirect", "http_and_https"}
	IPv64CDNSchemes    = []string{"http", "https"}
	IPv64AccessModes   = []string{"allowlist", "denylist"}
	IPv64CDNRecordMode = []string{"full", "other_family_only"}
	IPv64Transports    = []string{"tcp", "udp"}
)

// CDNInput is a PATCH of the proxy settings.
type CDNInput struct {
	Frontend      *string `json:"frontend,omitempty"`
	BackendScheme *string `json:"backend_scheme,omitempty"`
	BackendPort   *int    `json:"backend_port,omitempty"`
	Websocket     *bool   `json:"websocket,omitempty"`
	Caching       *struct {
		Enabled     *bool `json:"enabled,omitempty"`
		TimeMinutes *int  `json:"time_minutes,omitempty"`
		SizeMB      *int  `json:"size_mb,omitempty"`
	} `json:"caching,omitempty"`
	AccessList *struct {
		Enabled *bool   `json:"enabled,omitempty"`
		Mode    *string `json:"mode,omitempty"`
	} `json:"access_list,omitempty"`
}

// Validate checks the enums and ranges.
func (in CDNInput) Validate() error {
	if in.Frontend != nil && !slices.Contains(IPv64CDNFrontends, *in.Frontend) {
		return invalid("frontend")
	}
	if in.BackendScheme != nil && !slices.Contains(IPv64CDNSchemes, *in.BackendScheme) {
		return invalid("backend_scheme")
	}
	if !inRange(in.BackendPort, 1, 65535) {
		return invalid("backend_port is 1 to 65535")
	}
	if in.Caching != nil {
		if !inRange(in.Caching.TimeMinutes, 1, 600) {
			return invalid("caching time is 1 to 600 minutes")
		}
		if in.Caching.SizeMB != nil && *in.Caching.SizeMB < 1 {
			return invalid("caching size is at least 1 MB")
		}
	}
	if in.AccessList != nil && in.AccessList.Mode != nil && !slices.Contains(IPv64AccessModes, *in.AccessList.Mode) {
		return invalid("access_list mode")
	}
	return nil
}

func cdnPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("CDN")
	}
	return "/cdn/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

func (c *IPv64) cdn(ctx context.Context, method, token string, rest string, body any) (IPv64CDN, int, error) {
	path, err := cdnPath(token, rest)
	if err != nil {
		return IPv64CDN{}, 0, err
	}
	answer, err := c.Do(ctx, method, path, nil, body)
	if err != nil {
		return IPv64CDN{}, 0, err
	}
	var out IPv64CDN
	if answer.Status != http.StatusNoContent {
		err = decodeData(answer.Data, &out)
	}
	if out.Pending == nil {
		out.Pending = []string{}
	}
	return out, answer.Status, err
}

// CDNs lists the domains in the CDN.
func (c *IPv64) CDNs(ctx context.Context) ([]IPv64CDN, error) {
	list := []IPv64CDN{}
	err := c.call(ctx, http.MethodGet, "/cdn", nil, nil, &list)
	for i := range list {
		if list[i].Pending == nil {
			list[i].Pending = []string{}
		}
	}
	return list, err
}

// CDN reads one CDN domain with its mappings.
func (c *IPv64) CDN(ctx context.Context, token string) (IPv64CDN, error) {
	out, _, err := c.cdn(ctx, http.MethodGet, token, "", nil)
	return out, err
}

// CreateCDN takes a domain (by ID or name) into the CDN.
func (c *IPv64) CreateCDN(ctx context.Context, domain string) (IPv64CDN, error) {
	if !ValidHost(domain) && !isNumber(domain, 1<<31) {
		return IPv64CDN{}, invalid("domain")
	}
	var out IPv64CDN
	err := c.call(ctx, http.MethodPost, "/cdn", nil, map[string]string{"domain": domain}, &out)
	if out.Pending == nil {
		out.Pending = []string{}
	}
	return out, err
}

// ChangeCDN changes the proxy settings of a CDN domain.
func (c *IPv64) ChangeCDN(ctx context.Context, token string, in CDNInput) (IPv64CDN, error) {
	if err := in.Validate(); err != nil {
		return IPv64CDN{}, err
	}
	out, _, err := c.cdn(ctx, http.MethodPatch, token, "", in)
	return out, err
}

// DeleteCDN takes a domain out of the CDN (202; first switch all records off).
func (c *IPv64) DeleteCDN(ctx context.Context, token string) (IPv64CDN, error) {
	out, _, err := c.cdn(ctx, http.MethodDelete, token, "", nil)
	return out, err
}

// IPv64CDNActions are the POST actions of a CDN domain.
var IPv64CDNActions = []string{"proxy/enable", "proxy/disable", "portmapper/enable", "portmapper/disable", "redeploy", "reset-traffic", "move", "renew-certificate"}

// CDNAction runs one of IPv64CDNActions; most answer 202 with pending.
func (c *IPv64) CDNAction(ctx context.Context, token, action string) (IPv64CDN, int, error) {
	if !slices.Contains(IPv64CDNActions, action) {
		return IPv64CDN{}, 0, invalid("action")
	}
	return c.cdn(ctx, http.MethodPost, token, "/"+action, nil)
}

// CDNRecords lists A and AAAA records of the domain with their CDN state.
func (c *IPv64) CDNRecords(ctx context.Context, token string) ([]IPv64FullRecord, error) {
	path, err := cdnPath(token, "/records")
	if err != nil {
		return nil, err
	}
	list := []IPv64FullRecord{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// CDNRecord routes a record through the CDN (mode full or
// other_family_only) or back to direct delivery (mode "").
func (c *IPv64) CDNRecord(ctx context.Context, token, record, mode string) (IPv64FullRecord, error) {
	if !validRef(record) {
		return IPv64FullRecord{}, invalid("record")
	}
	action, body := "/disable", any(nil)
	if mode != "" {
		if !slices.Contains(IPv64CDNRecordMode, mode) {
			return IPv64FullRecord{}, invalid("mode")
		}
		action, body = "/enable", map[string]string{"mode": mode}
	}
	path, err := cdnPath(token, "/records/", url.PathEscape(record), action)
	if err != nil {
		return IPv64FullRecord{}, err
	}
	var r IPv64FullRecord
	err = c.call(ctx, http.MethodPost, path, nil, body, &r)
	return r, err
}

// MappingInput is a new port of the portmapper.
type MappingInput struct {
	Record     string `json:"record"`
	TargetPort int    `json:"target_port"`
	Transport  string `json:"transport,omitempty"`
	Name       string `json:"name,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
}

// Validate checks ports and transport; ListenPort 0 lets IPv64 choose.
func (in MappingInput) Validate() error {
	if !validRef(in.Record) {
		return invalid("record")
	}
	if in.TargetPort < 1 || in.TargetPort > 65535 {
		return invalid("target_port is 1 to 65535")
	}
	if in.Transport != "" && !slices.Contains(IPv64Transports, in.Transport) {
		return invalid("transport is tcp or udp")
	}
	if in.ListenPort != 0 && (in.ListenPort < 50000 || in.ListenPort > 62000) {
		return invalid("listen_port is 50000 to 62000")
	}
	if len(in.Name) > 100 || strings.ContainsAny(in.Name, "\r\n") {
		return invalid("name")
	}
	return nil
}

// Mappings lists the ports of the portmapper.
func (c *IPv64) Mappings(ctx context.Context, token string) ([]IPv64CDNMapping, error) {
	path, err := cdnPath(token, "/mappings")
	if err != nil {
		return nil, err
	}
	list := []IPv64CDNMapping{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// CreateMapping adds a port.
func (c *IPv64) CreateMapping(ctx context.Context, token string, in MappingInput) (IPv64CDNMapping, error) {
	if err := in.Validate(); err != nil {
		return IPv64CDNMapping{}, err
	}
	path, err := cdnPath(token, "/mappings")
	if err != nil {
		return IPv64CDNMapping{}, err
	}
	var m IPv64CDNMapping
	err = c.call(ctx, http.MethodPost, path, nil, in, &m)
	return m, err
}

// DeleteMapping removes a port.
func (c *IPv64) DeleteMapping(ctx context.Context, token, mapping string) error {
	if !validRef(mapping) {
		return invalid("mapping")
	}
	path, err := cdnPath(token, "/mappings/", url.PathEscape(mapping))
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, path, nil, nil, nil)
}

// IPv64AccessEntry is an address of the access list.
type IPv64AccessEntry struct {
	ID      int64  `json:"id"`
	Address string `json:"address"`
}

// AccessList reads the access list of the reverse proxy.
func (c *IPv64) AccessList(ctx context.Context, token string) ([]IPv64AccessEntry, error) {
	path, err := cdnPath(token, "/access-list")
	if err != nil {
		return nil, err
	}
	list := []IPv64AccessEntry{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// AddAccess adds an address or network (CIDR).
func (c *IPv64) AddAccess(ctx context.Context, token, address string) (IPv64AccessEntry, error) {
	if _, err := netip.ParsePrefix(address); err != nil {
		if _, err := netip.ParseAddr(address); err != nil {
			return IPv64AccessEntry{}, invalid("address is an IP or a network in CIDR")
		}
	}
	path, err := cdnPath(token, "/access-list")
	if err != nil {
		return IPv64AccessEntry{}, err
	}
	var e IPv64AccessEntry
	err = c.call(ctx, http.MethodPost, path, nil, map[string]string{"address": address}, &e)
	return e, err
}

// RemoveAccess removes an entry of the access list.
func (c *IPv64) RemoveAccess(ctx context.Context, token string, id int64) error {
	path, err := cdnPath(token, "/access-list/", strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, path, nil, nil, nil)
}
