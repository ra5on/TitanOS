package network

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func (m *Manager) Diagnostics(ctx context.Context, id string) ([]Check, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	device, ok := m.Device(id, false)
	if !ok {
		return nil, ErrInvalid
	}
	var cfg LogicalNetwork
	for _, n := range m.Connections() {
		if n.ID == id {
			cfg = n
		}
	}
	checks := []Check{}
	run := func(name, command string, args ...string) {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err := m.runner.Run(bounded, "", command, args...)
		out = strings.TrimSpace(out)
		if len(out) > 2048 {
			out = out[:2048]
		}
		if err != nil && out == "" {
			out = err.Error()
		}
		checks = append(checks, Check{Name: name, OK: err == nil, Detail: out})
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := m.runner.Run(bounded, "", "ip", "-json", "address", "show", "dev", device)
	cancel()
	checks = append(checks, addressCheck(out, err))
	for _, gateway := range []string{cfg.Gateway, cfg.Gateway6} {
		if gateway != "" {
			run("Gateway "+gateway, "ping", "-c", "1", "-W", "2", "-I", device, gateway)
		}
	}
	run("DNS (example.org)", "resolvectl", "query", "--interface="+device, "example.org")
	return checks, nil
}

func addressCheck(out string, err error) Check {
	check := Check{Name: "Link / addresses", Detail: strings.TrimSpace(out)}
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	var links []struct {
		Flags     []string `json:"flags"`
		Addresses []struct {
			Local     string `json:"local"`
			Scope     string `json:"scope"`
			Tentative bool   `json:"tentative"`
		} `json:"addr_info"`
	}
	if json.Unmarshal([]byte(out), &links) == nil && len(links) == 1 && slices.Contains(links[0].Flags, "UP") && slices.Contains(links[0].Flags, "LOWER_UP") {
		for _, addr := range links[0].Addresses {
			if addr.Scope == "global" && addr.Local != "" && !addr.Tentative {
				check.OK = true
			}
		}
	}
	if !check.OK {
		check.Detail = "Link is down or has no usable global address. " + check.Detail
	}
	if len(check.Detail) > 2048 {
		check.Detail = check.Detail[:2048]
	}
	return check
}
