package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const lanFile = "docker/lan-networks.json"

type LANNetwork struct {
	Name        string `json:"name"`
	Parent      string `json:"parent"`
	Driver      string `json:"driver"`
	Subnet      string `json:"subnet"`
	Gateway     string `json:"gateway"`
	Range       string `json:"range"`
	HostAccess  bool   `json:"hostAccess"`
	HostAddress string `json:"hostAddress,omitempty"`
	Error       string `json:"error,omitempty"`
}
type engineLAN struct {
	ID      string `json:"Id"`
	Driver  string
	Options map[string]string
	Labels  map[string]string
	IPAM    struct {
		Config []struct{ Subnet, Gateway, IPRange string }
	}
	Containers map[string]any
}

func lanName(name string) string { return "titanos-lan-" + name }
func lanHostName(name string) string {
	h := sha256.Sum256([]byte(name))
	return "tl" + hex.EncodeToString(h[:6])
}
func (m *Manager) LANNetworks() []LANNetwork {
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	list := slices.Clone(m.lan)
	if list == nil {
		list = []LANNetwork{}
	}
	for i := range list {
		list[i].Error = m.lanErrors[list[i].Name]
	}
	return list
}
func validateLAN(n LANNetwork, address string) error {
	if !appNameRE.MatchString(n.Name) || len(n.Name) > 24 || n.Parent == "" || !slices.Contains([]string{"ipvlan", "macvlan"}, n.Driver) {
		return ErrInvalid
	}
	s, err := netip.ParsePrefix(n.Subnet)
	pool, pe := netip.ParsePrefix(n.Range)
	g, ge := netip.ParseAddr(n.Gateway)
	if err != nil || pe != nil || ge != nil || !s.Addr().Is4() || s != s.Masked() || pool != pool.Masked() || s.Bits() < 8 || s.Bits() > 30 || !s.Contains(pool.Addr()) || pool.Bits() < s.Bits() || !usableLAN(s, g) || pool.Contains(g) {
		return fmt.Errorf("%w: subnet, gateway or container range", ErrInvalid)
	}
	if address != "" {
		p, e := netip.ParsePrefix(address)
		if e != nil || p.Masked() != s || pool.Contains(p.Addr()) {
			return fmt.Errorf("%w: pool conflicts with NAS address or subnet", ErrInvalid)
		}
		if p.Addr() == g {
			return fmt.Errorf("%w: NAS address cannot be the external gateway", ErrInvalid)
		}
	}
	if n.HostAccess {
		h, e := netip.ParseAddr(n.HostAddress)
		if e != nil || !usableLAN(s, h) || h == g || pool.Contains(h) {
			return fmt.Errorf("%w: choose a free host address outside the container range", ErrInvalid)
		}
		if p, e := netip.ParsePrefix(address); e == nil && p.Addr() == h {
			return ErrInvalid
		}
	} else if n.HostAddress != "" {
		return ErrInvalid
	}
	return nil
}
func usableLAN(p netip.Prefix, a netip.Addr) bool {
	return p.Contains(a) && a.Is4() && a != p.Masked().Addr() && p.Contains(a.Next())
}

func (n LANNetwork) ValidateParentAddress(address string) error { return validateLAN(n, address) }

func (m *Manager) CreateLAN(ctx context.Context, n LANNetwork) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	ready := m.loaded && m.settings.Enabled && m.applied != ""
	m.mu.Unlock()
	if !ready {
		return ErrNotRunning
	}
	if m.NetworkResolver == nil {
		return ErrInvalid
	}
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	dev, address, ok := m.NetworkResolver(n.Parent)
	if !ok {
		return fmt.Errorf("%w: confirm the NAS network first", ErrInvalid)
	}
	if n.Driver == "" {
		n.Driver = "ipvlan"
	}
	if err := validateLAN(n, address); err != nil {
		return err
	}
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	if len(m.lan) >= 32 {
		return ErrInvalid
	}
	for _, old := range m.lan {
		if old.Name == n.Name {
			return ErrExists
		}
		a, _ := netip.ParsePrefix(old.Range)
		b, _ := netip.ParsePrefix(n.Range)
		if a.Overlaps(b) {
			return fmt.Errorf("%w: container address ranges overlap", ErrInvalid)
		}
	}
	n.Error = ""
	next := append(slices.Clone(m.lan), n)
	if err := m.store.WriteJSON(lanFile, next); err != nil {
		return err
	}
	m.lan = next
	return m.ensureLAN(ctx, n, dev)
}

func (m *Manager) ensureLAN(ctx context.Context, n LANNetwork, dev string) (err error) {
	if m.lanErrors == nil {
		m.lanErrors = map[string]string{}
	}
	defer func() {
		if err != nil {
			m.lanErrors[n.Name] = err.Error()
		} else {
			delete(m.lanErrors, n.Name)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var existing engineLAN
	err = m.api.getJSON(ctx, "/networks/"+url.PathEscape(lanName(n.Name)), nil, &existing)
	if err == nil {
		if existing.Labels["io.titanos.lan"] != n.Name || existing.Options["parent"] != dev || existing.Driver != n.Driver || len(existing.IPAM.Config) != 1 || existing.IPAM.Config[0].Subnet != n.Subnet || existing.IPAM.Config[0].Gateway != n.Gateway || existing.IPAM.Config[0].IPRange != n.Range {
			return fmt.Errorf("%w: existing network differs; it will not be replaced automatically", ErrInvalid)
		}
	} else {
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != 404 {
			return err
		}
		options := map[string]string{"parent": dev}
		if n.Driver == "ipvlan" {
			options["ipvlan_mode"] = "l2"
		} else {
			options["macvlan_mode"] = "bridge"
		}
		ipam := map[string]any{"Subnet": n.Subnet, "Gateway": n.Gateway, "IPRange": n.Range}
		if n.HostAccess {
			ipam["AuxiliaryAddresses"] = map[string]string{"titanos-host": n.HostAddress}
		}
		body := map[string]any{"Name": lanName(n.Name), "CheckDuplicate": true, "Driver": n.Driver, "Options": options, "Labels": map[string]string{"io.titanos.lan": n.Name, "io.titanos.parent": n.Parent}, "IPAM": map[string]any{"Driver": "default", "Config": []any{ipam}}}
		if err = m.api.sendJSON(ctx, http.MethodPost, "/networks/create", body, nil); err != nil {
			return err
		}
	}
	if n.HostAccess {
		return m.ensureLANHost(ctx, n, dev)
	}
	return nil
}

func (m *Manager) ensureLANHost(ctx context.Context, n LANNetwork, parent string) error {
	// A separate endpoint is necessary because ipvlan/macvlan isolate the
	// host's original endpoint. Only a local route to the reserved pool is added.
	name := lanHostName(n.Name)
	var links []struct {
		LinkIndex int    `json:"link_index"`
		LinkName  string `json:"link"`
		Address   string
		LinkInfo  struct {
			Kind string `json:"info_kind"`
		} `json:"linkinfo"`
	}
	var text string
	out, err := m.runner.Run(ctx, "", "ip", "-j", "-d", "link", "show", "dev", name)
	text = out
	if err != nil {
		if _, err = m.runner.Run(ctx, "", "ip", "link", "add", name, "link", parent, "type", n.Driver, "mode", map[string]string{"ipvlan": "l2", "macvlan": "bridge"}[n.Driver]); err != nil {
			return err
		}
	} else {
		if err = json.Unmarshal([]byte(text), &links); err != nil || len(links) != 1 || links[0].LinkInfo.Kind != n.Driver {
			return fmt.Errorf("%w: host endpoint is not the expected link", ErrInvalid)
		}
		out, e := m.runner.Run(ctx, "", "ip", "-j", "link", "show", "dev", parent)
		var parents []struct {
			Index int `json:"ifindex"`
		}
		if e != nil || json.Unmarshal([]byte(out), &parents) != nil || len(parents) != 1 || (parents[0].Index != links[0].LinkIndex && links[0].LinkName != parent) {
			return fmt.Errorf("%w: host endpoint belongs to another parent", ErrInvalid)
		}
	}
	for _, args := range [][]string{{"address", "replace", n.HostAddress + "/32", "dev", name}, {"link", "set", name, "up"}, {"route", "replace", n.Range, "dev", name, "src", n.HostAddress}} {
		if _, err := m.runner.Run(ctx, "", "ip", args...); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ReconcileLAN(ctx context.Context) {
	if m.Disabled || m.NetworkResolver == nil {
		return
	}
	m.mu.Lock()
	running := m.applied != ""
	m.mu.Unlock()
	if !running {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return
		}
		defer release()
	}
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	for _, n := range m.lan {
		dev, _, ok := m.NetworkResolver(n.Parent)
		if !ok {
			m.lanErrors[n.Name] = "NAS network is unavailable or on trial"
			continue
		}
		m.ensureLAN(ctx, n, dev)
	}
}

func (m *Manager) RemoveLAN(ctx context.Context, name string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	m.lanUsers.Lock()
	defer m.lanUsers.Unlock()
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	i := slices.IndexFunc(m.lan, func(n LANNetwork) bool { return n.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	// Saved Compose files count even when every container is stopped.
	m.mu.Lock()
	apps := m.knownApps()
	m.mu.Unlock()
	for _, app := range apps {
		data, err := m.store.Read(composeFile(app))
		if err != nil {
			return err
		}
		if strings.Contains(string(data), lanName(name)) {
			return fmt.Errorf("%w: a saved application still references this network", ErrBusy)
		}
		// Environment interpolation can hide the actual external network name.
		// Resolve through Compose without logging or exposing its secret output.
		resolved, err := m.runner.Run(ctx, "", "docker", m.compose(m.current().Volume, app, "config", "--format", "json")...)
		if err != nil {
			return fmt.Errorf("%w: could not verify saved application network dependencies", ErrBusy)
		}
		var document map[string]any
		if json.Unmarshal([]byte(resolved), &document) != nil {
			return fmt.Errorf("%w: unreadable saved application network dependencies", ErrBusy)
		}
		definitions, _ := document["networks"].(map[string]any)
		for _, v := range definitions {
			definition, _ := v.(map[string]any)
			if definition["name"] == lanName(name) {
				return fmt.Errorf("%w: a saved application still references this network", ErrBusy)
			}
		}
	}
	var existing engineLAN
	err := m.api.getJSON(ctx, "/networks/"+url.PathEscape(lanName(name)), nil, &existing)
	if err == nil {
		if existing.Labels["io.titanos.lan"] != name || len(existing.Containers) > 0 {
			return ErrBusy
		}
		res, e := m.api.do(ctx, http.MethodDelete, "/networks/"+url.PathEscape(existing.ID), nil)
		if e != nil {
			return e
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	} else {
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != 404 {
			return err
		}
	}
	n := m.lan[i]
	if n.HostAccess {
		dev := lanHostName(n.Name)
		if out, err := m.runner.Run(ctx, "", "ip", "-j", "-d", "link", "show", "dev", dev); err == nil {
			var links []struct {
				LinkIndex int    `json:"link_index"`
				LinkName  string `json:"link"`
				LinkInfo  struct {
					Kind string `json:"info_kind"`
				} `json:"linkinfo"`
			}
			parent, _, ok := m.NetworkResolver(n.Parent)
			if !ok || json.Unmarshal([]byte(out), &links) != nil || len(links) != 1 || links[0].LinkInfo.Kind != n.Driver {
				return ErrInvalid
			}
			out, err = m.runner.Run(ctx, "", "ip", "-j", "link", "show", "dev", parent)
			var parents []struct {
				Index int `json:"ifindex"`
			}
			if err != nil || json.Unmarshal([]byte(out), &parents) != nil || len(parents) != 1 || (parents[0].Index != links[0].LinkIndex && links[0].LinkName != parent) {
				return ErrInvalid
			}
			if _, err := m.runner.Run(ctx, "", "ip", "link", "delete", dev); err != nil {
				return err
			}
		}
	}
	next := slices.Delete(slices.Clone(m.lan), i, i+1)
	if err := m.store.WriteJSON(lanFile, next); err != nil {
		return err
	}
	m.lan = next
	delete(m.lanErrors, name)
	return nil
}

// AssignLAN changes only the selected service and keeps unrelated private
// networks and other services intact. Static addresses must be in its pool.
func AssignLAN(text, service string, n LANNetwork, address string) (string, error) {
	p, e := netip.ParsePrefix(n.Range)
	ip, ie := netip.ParseAddr(address)
	sub, se := netip.ParsePrefix(n.Subnet)
	if e != nil || se != nil || address != "" && (ie != nil || !p.Contains(ip) || !usableLAN(sub, ip)) {
		return "", ErrInvalid
	}
	var doc map[string]any
	if yaml.Unmarshal([]byte(text), &doc) != nil {
		return "", ErrInvalid
	}
	services, _ := doc["services"].(map[string]any)
	target, _ := services[service].(map[string]any)
	if target == nil || target["network_mode"] != nil {
		return "", ErrInvalid
	}
	key := "titanos_lan_" + strings.ReplaceAll(n.Name, "-", "_")
	networks, _ := doc["networks"].(map[string]any)
	if networks == nil {
		networks = map[string]any{}
		doc["networks"] = networks
	}
	networks[key] = map[string]any{"external": true, "name": lanName(n.Name)}
	attached := map[string]any{}
	switch old := target["networks"].(type) {
	case nil:
		attached["default"] = nil
	case []any:
		for _, v := range old {
			if s, ok := v.(string); ok {
				attached[s] = nil
			}
		}
	case map[string]any:
		for k, v := range old {
			attached[k] = v
		}
	default:
		return "", ErrInvalid
	}
	oldKeys := []string{}
	for k := range attached {
		definition, _ := networks[k].(map[string]any)
		name, _ := definition["name"].(string)
		if strings.HasPrefix(name, "titanos-lan-") {
			oldKeys = append(oldKeys, k)
			delete(attached, k)
		}
	}
	attached[key] = map[string]any{}
	if address != "" {
		attached[key] = map[string]any{"ipv4_address": address}
	}
	target["networks"] = attached
	for _, oldKey := range oldKeys {
		if oldKey == key {
			continue
		}
		used := false
		for _, v := range services {
			service, _ := v.(map[string]any)
			switch references := service["networks"].(type) {
			case map[string]any:
				if _, ok := references[oldKey]; ok {
					used = true
				}
			case []any:
				for _, value := range references {
					if value == oldKey {
						used = true
					}
				}
			}
		}
		if !used {
			delete(networks, oldKey)
		}
	}
	if _, ok := attached["default"]; ok {
		if _, exists := networks["default"]; !exists {
			networks["default"] = nil
		}
	}
	out, err := yaml.Marshal(doc)
	return string(out), err
}

func (m *Manager) validateLANCompose(text string) error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return ErrInvalid
	}
	defined, _ := doc["networks"].(map[string]any)
	services, _ := doc["services"].(map[string]any)
	known := m.LANNetworks()
	seen := map[string]bool{}
	for key, value := range defined {
		def, _ := value.(map[string]any)
		name, _ := def["name"].(string)
		if !strings.HasPrefix(name, "titanos-lan-") {
			continue
		}
		i := slices.IndexFunc(known, func(n LANNetwork) bool { return lanName(n.Name) == name })
		if i < 0 {
			return fmt.Errorf("%w: unknown TitanOS LAN network", ErrInvalid)
		}
		pool, _ := netip.ParsePrefix(known[i].Range)
		sub, _ := netip.ParsePrefix(known[i].Subnet)
		for _, v := range services {
			s, _ := v.(map[string]any)
			nets, _ := s["networks"].(map[string]any)
			setting, _ := nets[key].(map[string]any)
			if setting == nil {
				continue
			}
			address, _ := setting["ipv4_address"].(string)
			if address == "" {
				continue
			}
			ip, e := netip.ParseAddr(address)
			if e != nil || !pool.Contains(ip) || !usableLAN(sub, ip) || seen[name+":"+address] {
				return fmt.Errorf("%w: static LAN address outside pool or duplicated", ErrInvalid)
			}
			seen[name+":"+address] = true
		}
	}
	return nil
}
