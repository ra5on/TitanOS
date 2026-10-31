package api

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Tools of the areas added on 29.10.2026 (decision of the owner): plugins,
// network with firewall, more of the backups and the automatic updates of
// Docker apps. Every target is checked against the current state when the
// card is planned and again right before it runs (the plan is made anew
// and compared). The plans are pure functions of a status, so that their
// levels can be tested without a real NAS. See docs/MCP.md.
//
// Not offered: installing plugins from a file or an address (only official
// plugins by name from the signed catalog), changing the network,
// switching the firewall on or off, confirming a change on trial.

func (s *Server) areaTools() []toolcatalog.Tool {
	mounted := func() []string { return s.opts.Storage.Mounted() }
	return []toolcatalog.Tool{
		// plugins
		reading("plugins_list", "Installed plugins (version, on/off, active, official or community, problem, an update found in a share) and official plugins that can be installed from the signed catalog of this TitanOS version. refresh loads the catalog anew from the release and looks for plugin files in the shares.",
			schema(map[string]any{"refresh": map[string]any{"type": "boolean", "description": "Load the catalog anew (takes a moment)."}}), true,
			func(ctx context.Context, args json.RawMessage) (any, error) {
				var req struct{ Refresh bool }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				if req.Refresh {
					// A failure shows as the state of the catalog below,
					// never as "no plugins".
					_ = s.opts.Plugins.RefreshCatalog(ctx, false)
					s.opts.Plugins.DetectHardware()
					s.opts.Plugins.ScanShares(mounted())
				}
				return pluginsView(s.opts.Plugins.Status(mounted())), nil
			}),
		actionTool("plugin_install", "Propose to install an official plugin by its name from plugins_list (installable true). Community plugins, files and addresses are never installed over MCP. A plugin becomes part of the system with full rights.",
			map[string]any{"name": str("Name as plugins_list shows it."), "volume": str("Volume for the plugin files; only needed while no plugin is installed yet."),
				"activate": map[string]any{"type": "boolean", "description": "Switch it on right away (default true)."}}, []string{"name"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct {
					Name, Volume string
					Activate     *bool
				}
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planPluginInstall(s.opts.Plugins.Status(mounted()), mounted(), req.Name, req.Volume, req.Activate)
			}),
		actionTool("plugin_switch", "Propose to switch an installed plugin on or off.",
			map[string]any{"name": str("Name as plugins_list shows it."), "action": enum("What to do.", "enable", "disable")}, []string{"name", "action"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct{ Name, Action string }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planPluginSwitch(s.opts.Plugins.Status(mounted()), req.Name, req.Action)
			}),
		actionTool("plugin_update", "Propose to replace an installed plugin with the newer version plugins_list names under update (found in a share). A version that is not official needs the strongest confirmation.",
			map[string]any{"name": str("Name as plugins_list shows it.")}, []string{"name"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct{ Name string }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planPluginUpdate(s.opts.Plugins.Status(mounted()), req.Name)
			}),
		actionTool("plugin_remove", "Propose to remove an installed plugin: it is switched off and its files are deleted.",
			map[string]any{"name": str("Name as plugins_list shows it.")}, []string{"name"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct{ Name string }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planPluginRemove(s.opts.Plugins.Status(mounted()), req.Name)
			}),
		// network and firewall
		reading("network_diagnostics", "Check one network of the NAS: link, addresses, gateway, name resolution. Without network, lists the networks to check. Reads only; changes of the network are made by a person in the web UI.",
			schema(map[string]any{"network": str("ID of the network from the list.")}), false, s.toolNetworkDiagnostics),
		reading("firewall_status", "The firewall: on or off, a change on trial, zones, the cells (source zone × destination nas, apps or a zone) with their numbered rules and default, services, Docker apps, blocked addresses and recently dropped packets.",
			nil, true, noArgs(func(ctx context.Context) (any, error) { return firewallView(s.opts.Firewall.Status("")), nil })),
		actionTool("firewall_rule_add", "Propose to add a rule to one cell of the firewall. The first matching rule of a cell decides. A change of a switched-on firewall runs 2 minutes on trial; a person confirms it in the web UI or it goes back. Switching the firewall on or off is not possible here.",
			map[string]any{
				"from":        str("Source zone ID from firewall_status (home, vpn, internet, docker or an own zone)."),
				"to":          str("Destination: nas, apps, or a zone ID (only for zones with networks behind the NAS)."),
				"action":      enum("Allow or deny.", firewall.Allow, firewall.Deny),
				"service":     str("A service ID from firewall_status (cells to nas only); leave out for ports."),
				"app":         str("A Docker app key from firewall_status (cells to apps only)."),
				"protocol":    enum("Protocol for ports; ignored for service and app.", "tcp", "udp", "any"),
				"ports":       str("A port (22) or range (8000-8100); empty means all."),
				"source":      str("Only this sender address or network; empty means anyone in the zone."),
				"destination": str("Only this destination address or network (cells to a zone only)."),
				"comment":     map[string]any{"type": "string", "maxLength": 80, "description": "Short note shown with the rule."},
				"position":    map[string]any{"type": "integer", "minimum": 0, "description": "Where in the cell, 0 is first; at the end if left out."},
			}, []string{"from", "to", "action"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req ruleRequest
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				a, cfg, err := planFirewallRuleAdd(s.opts.Firewall.Status(""), req)
				if err != nil {
					return nil, err
				}
				return a, firewallProposalError(s.opts.Firewall.CheckProposal(cfg))
			}),
		actionTool("firewall_rule_remove", "Propose to remove one rule of a cell of the firewall, by its number in firewall_status. A change of a switched-on firewall runs 2 minutes on trial; a person confirms it in the web UI.",
			map[string]any{"from": str("Source zone ID."), "to": str("Destination: nas, apps or a zone ID."),
				"index": map[string]any{"type": "integer", "minimum": 0, "description": "Number of the rule in the cell, as firewall_status shows it."}}, []string{"from", "to", "index"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct {
					From, To string
					Index    int
				}
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				a, cfg, err := planFirewallRuleRemove(s.opts.Firewall.Status(""), req.From, req.To, req.Index)
				if err != nil {
					return nil, err
				}
				return a, firewallProposalError(s.opts.Firewall.CheckProposal(cfg))
			}),
		actionTool("firewall_unblock", "Propose to release an address the automatic blocking locked out after failed logins.",
			map[string]any{"address": str("Address as firewall_status lists it under blocked.")}, []string{"address"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct{ Address string }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planFirewallUnblock(s.opts.Firewall.Status(""), req.Address)
			}),
		// backups
		reading("backup_snapshots", "The saved states of a backup job (ID, time, paths, size), newest last.",
			schema(map[string]any{"job": str("ID as backup_status shows it.")}, "job"), false,
			func(ctx context.Context, args json.RawMessage) (any, error) {
				var req struct{ Job string }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				list, err := s.opts.Backup.Snapshots(ctx, req.Job)
				if err != nil {
					return nil, toolcatalog.Errorf("saved states of %q unavailable: %v", req.Job, err)
				}
				return map[string]any{"job": req.Job, "snapshots": list, "observedAt": time.Now()}, nil
			}),
		reading("backup_files", "One folder of a saved state of a backup job, folders first, pages of 500.",
			schema(map[string]any{"job": str("ID as backup_status shows it."), "snapshot": str("ID from backup_snapshots."),
				"path":   str("Absolute folder in the saved state, e.g. /mnt/tank/media/Photos; / for the top."),
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Next page from nextOffset."}}, "job", "snapshot", "path"), true,
			func(ctx context.Context, args json.RawMessage) (any, error) {
				var req struct {
					Job, Snapshot, Path string
					Offset              int
				}
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				entries, total, cut, err := s.opts.Backup.Files(bounded, req.Job, req.Snapshot, req.Path, req.Offset)
				if err != nil {
					return nil, toolcatalog.Errorf("folder of the saved state unavailable: %v", err)
				}
				if entries == nil {
					entries = []backup.Entry{}
				}
				res := map[string]any{"entries": entries, "total": total, "cut": cut}
				if next := req.Offset + len(entries); next < total {
					res["nextOffset"] = next
				}
				return res, nil
			}),
		actionTool("backup_check", "Propose to check the repository of a backup job in the background (restic check); readData reads back that percent of the data. The result is lastCheck/checkError in backup_status.",
			map[string]any{"job": str("ID as backup_status shows it."), "readData": map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "description": "Percent of the data to read back, 0 checks the structure only."}}, []string{"job"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct {
					Job      string
					ReadData int
				}
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planBackupCheck(s.opts.Backup.List(), req.Job, req.ReadData)
			}),
		actionTool("backup_restore_files", "Propose to restore files and folders of a saved state into a new folder restored/<job>-<time> on a volume. Nothing is overwritten; the shares stay as they are.",
			map[string]any{"job": str("ID as backup_status shows it."), "snapshot": str("ID from backup_snapshots."), "volume": str("Volume for the new folder."),
				"paths": map[string]any{"type": "array", "minItems": 1, "maxItems": 200, "items": map[string]any{"type": "string"}, "description": "Absolute paths in the saved state from backup_files."}}, []string{"job", "snapshot", "volume", "paths"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct {
					Job, Snapshot, Volume string
					Paths                 []string
				}
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planBackupRestore(s.opts.Backup.List(), s.opts.Storage.Mounted(), req.Job, req.Snapshot, req.Volume, req.Paths)
			}),
		actionTool("backup_schedule", "Propose to pause the schedule of a backup job (no more automatic runs) or to resume it.",
			map[string]any{"job": str("ID as backup_status shows it."), "action": enum("What to do.", "pause", "resume")}, []string{"job", "action"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct{ Job, Action string }
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planBackupSchedule(s.opts.Backup.List(), req.Job, req.Action)
			}),
		// automatic updates of Docker apps
		reading("docker_auto_updates", "Which apps update by themselves, the window (weekdays, time in the time zone of the NAS), the next window and the last result per app.",
			nil, false, noArgs(func(ctx context.Context) (any, error) { return s.opts.Docker.AutoUpdates(), nil })),
		actionTool("docker_auto_update", "Propose to switch the automatic update of one app on or off, optionally with a new window for all apps.",
			map[string]any{"app": str("Name of the app (Compose project) as docker_list shows it."), "enabled": map[string]any{"type": "boolean", "description": "true updates the app by itself."},
				"time":     str("New time of the window, HH:MM; the current one if left out."),
				"weekdays": map[string]any{"type": "array", "maxItems": 7, "items": map[string]any{"type": "integer", "minimum": 0, "maximum": 6}, "description": "New weekdays of the window, 0 is Sunday; empty means every day; the current ones if left out."}},
			[]string{"app", "enabled"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				var req struct {
					App      string
					Enabled  bool
					Time     *string
					Weekdays *[]int
				}
				if err := toolcatalog.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				return planAutoUpdate(s.opts.Docker.AutoUpdates().Settings, s.opts.Docker.Status(ctx).Apps, req.App, req.Enabled, req.Time, req.Weekdays)
			}),
	}
}

// --- plugins

type pluginItem struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Installed   bool     `json:"installed"`
	Enabled     bool     `json:"enabled"`
	Active      bool     `json:"active"`
	Official    bool     `json:"official"`
	Installable bool     `json:"installable"`
	Problem     string   `json:"problem,omitempty"`
	Update      any      `json:"update,omitempty"`
	Devices     []string `json:"devices,omitempty"`
}

// pluginsView is what the model reads: no paths of files in shares, the
// description in English if there is one.
func pluginsView(st plugins.Status) map[string]any {
	list := []pluginItem{}
	for _, p := range st.Plugins {
		item := pluginItem{Name: p.Name, Title: p.Title, Version: p.Version, Installed: p.Installed, Enabled: p.Enabled, Active: p.Active,
			Official: p.Official, Installable: officialDownload(p), Problem: p.Problem}
		item.Description = p.Description["en"]
		if item.Description == "" {
			item.Description = p.Description["de"]
		}
		if r := []rune(item.Description); len(r) > 400 {
			item.Description = string(r[:400]) + " …"
		}
		if p.Update != nil {
			item.Update = map[string]any{"version": p.Update.Version, "official": p.Update.Official}
		}
		for _, d := range p.Devices {
			item.Devices = append(item.Devices, d.Name)
		}
		if !p.Installed && !item.Installable {
			// A loose file in a share: shown, never installed over MCP.
			item.Problem = strings.TrimSpace(item.Problem + " not_installable_over_mcp")
		}
		list = append(list, item)
	}
	recommended := []string{}
	for _, r := range st.Recommendations {
		recommended = append(recommended, r.Plugin)
	}
	return map[string]any{"volume": st.Volume, "kernel": st.Kernel, "catalog": st.Catalog, "plugins": list, "recommended": recommended, "observedAt": time.Now()}
}

// officialDownload is a plugin of the signed release catalog that is not
// installed: the only kind MCP installs.
func officialDownload(p plugins.Plugin) bool {
	return !p.Installed && p.Download && p.Official
}

func findPlugin(st plugins.Status, name string) (plugins.Plugin, bool) {
	i := slices.IndexFunc(st.Plugins, func(p plugins.Plugin) bool { return p.Name == name })
	if i < 0 {
		return plugins.Plugin{}, false
	}
	return st.Plugins[i], true
}

func planPluginInstall(st plugins.Status, mounted []string, name, volume string, activate *bool) (*toolcatalog.Action, error) {
	p, ok := findPlugin(st, name)
	switch {
	case !ok:
		return nil, toolcatalog.Errorf("no plugin %q; plugins_list (refresh true) shows the catalog", name)
	case p.Installed:
		return nil, toolcatalog.Errorf("plugin %s is installed already", name)
	case !officialDownload(p):
		return nil, toolcatalog.CodedErrorf("plugin_not_official", "plugin %s is not an official plugin of the release catalog; the assistant installs only those, the plugin page offers the rest", name)
	}
	if st.Volume != "" && slices.Contains(mounted, st.Volume) {
		if volume != "" && volume != st.Volume {
			return nil, toolcatalog.Errorf("plugins live on volume %s", st.Volume)
		}
		volume = st.Volume
	}
	if !slices.Contains(mounted, volume) {
		return nil, toolcatalog.Errorf("choose a mounted volume for the plugin files: %v", mounted)
	}
	on := activate == nil || *activate
	key := "plugin.install"
	if !on {
		key = "plugin.installOff"
	}
	body := map[string]any{"name": p.Name, "volume": volume, "activate": on}
	return withLevel(card(key, map[string]string{"name": p.Name, "title": p.Title, "version": p.Version, "volume": volume}, "POST", "/plugins", body), toolcatalog.LevelDanger, "", ""), nil
}

func planPluginSwitch(st plugins.Status, name, action string) (*toolcatalog.Action, error) {
	p, ok := findPlugin(st, name)
	if !ok || !p.Installed {
		return nil, toolcatalog.Errorf("no installed plugin %q; plugins_list shows them", name)
	}
	if action != "enable" && action != "disable" {
		return nil, toolcatalog.Errorf("action must be enable or disable")
	}
	if (action == "enable") == p.Enabled {
		return nil, toolcatalog.Errorf("plugin %s is already %sd", name, action)
	}
	return withLevel(card("plugin."+action, map[string]string{"name": p.Name, "title": p.Title}, "POST", "/plugins/"+esc(p.Name)+"/"+action, nil), toolcatalog.LevelDanger, "", ""), nil
}

func planPluginUpdate(st plugins.Status, name string) (*toolcatalog.Action, error) {
	p, ok := findPlugin(st, name)
	if !ok || !p.Installed {
		return nil, toolcatalog.Errorf("no installed plugin %q; plugins_list shows them", name)
	}
	if p.Update == nil {
		return nil, toolcatalog.Errorf("no newer version of %s was found in a share; official versions come with TitanOS updates", name)
	}
	params := map[string]string{"name": p.Name, "title": p.Title, "from": p.Version, "version": p.Update.Version}
	body := map[string]any{"path": p.Update.Path}
	if p.Update.Official {
		return withLevel(card("plugin.update", params, "POST", "/plugins/"+esc(p.Name)+"/update", body), toolcatalog.LevelDanger, "", ""), nil
	}
	// Not signed: the plugin page asks for the name as well.
	return withLevel(card("plugin.updateCommunity", params, "POST", "/plugins/"+esc(p.Name)+"/update", body), toolcatalog.LevelDestroy, p.Name, "body:confirm"), nil
}

func planPluginRemove(st plugins.Status, name string) (*toolcatalog.Action, error) {
	p, ok := findPlugin(st, name)
	if !ok || !p.Installed {
		return nil, toolcatalog.Errorf("no installed plugin %q; plugins_list shows them", name)
	}
	return withLevel(card("plugin.remove", map[string]string{"name": p.Name, "title": p.Title}, "DELETE", "/plugins/"+esc(p.Name), nil), toolcatalog.LevelDestroy, p.Name, ""), nil
}

// --- network

func (s *Server) toolNetworkDiagnostics(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct{ Network string }
	if err := toolcatalog.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	type item struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Management bool   `json:"management"`
	}
	known := []item{}
	for _, n := range s.opts.Network.Connections() {
		known = append(known, item{n.ID, n.Name, n.Management})
	}
	if req.Network == "" {
		return map[string]any{"networks": known}, nil
	}
	checks, err := s.opts.Network.Diagnostics(ctx, req.Network)
	switch {
	case errors.Is(err, network.ErrInvalid):
		return nil, toolcatalog.Errorf("no network %q; networks: %v", req.Network, known)
	case err != nil:
		return nil, toolcatalog.Errorf("diagnostics unavailable: %v", err)
	}
	failed := 0
	for _, c := range checks {
		if !c.OK {
			failed++
		}
	}
	return map[string]any{"network": req.Network, "checks": checks, "failed": failed, "observedAt": time.Now()}, nil
}

// --- firewall

type ruleView struct {
	Index int `json:"index"`
	firewall.Policy
}

type cellView struct {
	From    firewall.Zone `json:"from"`
	To      string        `json:"to"`
	Default string        `json:"default"`
	Rules   []ruleView    `json:"rules"`
}

func cellsView(cfg firewall.Config) []cellView {
	list := []cellView{}
	for _, c := range cfg.Cells {
		v := cellView{From: c.From, To: c.To, Default: c.Default, Rules: []ruleView{}}
		for i, p := range c.Policies {
			v.Rules = append(v.Rules, ruleView{i, p})
		}
		list = append(list, v)
	}
	return list
}

func firewallView(st firewall.Status) map[string]any {
	zones := []map[string]any{}
	for _, z := range st.Config.Zones {
		zones = append(zones, map[string]any{"id": z.ID, "name": z.Name, "builtin": z.Builtin, "networks": z.Networks, "sources": z.Sources, "hostsNetworks": st.Config.Hosted(z.ID)})
	}
	services := []string{}
	for _, svc := range st.Services {
		services = append(services, svc.ID)
	}
	recent := st.Recent[:min(len(st.Recent), 30)]
	out := map[string]any{"enabled": st.Config.Enabled, "active": st.Active, "zones": zones, "cells": cellsView(st.Config), "services": services, "apps": st.Apps,
		"autoBlock": st.Config.AutoBlock, "blocked": st.Blocked, "recent": recent, "homeNetworks": st.HomeNetworks, "observedAt": time.Now()}
	if st.Error != "" {
		out["error"] = st.Error
	}
	if st.Pending != nil {
		out["pending"] = map[string]any{"seconds": st.PendingSeconds, "enabled": st.Pending.Enabled, "cells": cellsView(*st.Pending),
			"note": "On trial: a person confirms it in the web UI (Einstellungen › Firewall) or it goes back by itself."}
	}
	return out
}

type ruleRequest struct {
	From, To, Action, Service, App, Protocol, Ports, Source, Destination, Comment string
	Position                                                                      *int
}

// firewallTarget finds a cell of the settings in force; a change on trial
// first has to be confirmed or undone in the web UI.
func firewallTarget(st firewall.Status, from, to string) (firewall.Config, int, error) {
	if st.Pending != nil {
		return firewall.Config{}, 0, toolcatalog.CodedErrorf("firewall_pending", "a firewall change is on trial; a person confirms or undoes it in the web UI first")
	}
	cfg := st.Config
	if _, ok := cfg.Zone(firewall.Zone(from)); !ok {
		return cfg, 0, toolcatalog.Errorf("no zone %q; firewall_status lists the zones", from)
	}
	if to != firewall.ToNAS && to != firewall.ToApps {
		if _, ok := cfg.Zone(firewall.Zone(to)); !ok || !cfg.Hosted(firewall.Zone(from)) {
			return cfg, 0, toolcatalog.Errorf("no cell %s → %s: destinations are nas, apps, and zones only from zones with networks behind the NAS", from, to)
		}
	}
	i := slices.IndexFunc(cfg.Cells, func(c firewall.Cell) bool { return string(c.From) == from && c.To == to })
	if i < 0 {
		cfg.Cells = append(cfg.Cells, cfg.Cell(firewall.Zone(from), to))
		i = len(cfg.Cells) - 1
	}
	return cfg, i, nil
}

// copyConfig gives a plan its own slices to change.
func copyConfig(cfg firewall.Config) firewall.Config {
	data, _ := json.Marshal(cfg)
	var out firewall.Config
	_ = json.Unmarshal(data, &out)
	return out
}

func describeRule(p firewall.Policy) string {
	what := []string{p.Action}
	switch {
	case p.Service != "":
		what = append(what, "service "+p.Service)
	case p.App != "":
		what = append(what, "app "+p.App)
	default:
		ports := p.Ports
		if ports == "" {
			ports = "all ports"
		}
		what = append(what, p.Protocol+" "+ports)
	}
	if p.Source != "" {
		what = append(what, "from "+p.Source)
	}
	if p.Destination != "" {
		what = append(what, "to "+p.Destination)
	}
	if p.Disabled {
		what = append(what, "(disabled)")
	}
	return strings.Join(what, " ")
}

func firewallCard(key string, cfg firewall.Config, params map[string]string) *toolcatalog.Action {
	if cfg.Enabled {
		params["trial"] = "2"
	}
	return withLevel(card(key, params, "PUT", "/firewall", cfg), toolcatalog.LevelDanger, "", "")
}

func planFirewallRuleAdd(st firewall.Status, req ruleRequest) (*toolcatalog.Action, firewall.Config, error) {
	cfg, i, err := firewallTarget(st, req.From, req.To)
	if err != nil {
		return nil, cfg, err
	}
	cfg = copyConfig(cfg)
	p := firewall.Policy{Action: req.Action, Protocol: req.Protocol, Ports: strings.TrimSpace(req.Ports), Service: req.Service, App: req.App,
		Source: strings.TrimSpace(req.Source), Destination: strings.TrimSpace(req.Destination), Comment: strings.TrimSpace(req.Comment)}
	if p.Protocol == "" {
		p.Protocol = "any"
	}
	switch {
	case p.Service != "" && req.To != firewall.ToNAS, p.App != "" && req.To != firewall.ToApps:
		return nil, cfg, toolcatalog.Errorf("a service belongs to cells to nas, an app to cells to apps")
	case p.Destination != "" && (req.To == firewall.ToNAS || req.To == firewall.ToApps):
		return nil, cfg, toolcatalog.Errorf("a destination address is only for cells to a zone")
	case p.Service != "" && !slices.ContainsFunc(st.Services, func(s firewall.Service) bool { return s.ID == p.Service }):
		return nil, cfg, toolcatalog.Errorf("no service %q; firewall_status lists the services", p.Service)
	}
	policies := cfg.Cells[i].Policies
	at := len(policies)
	if req.Position != nil {
		at = min(*req.Position, len(policies))
	}
	cfg.Cells[i].Policies = slices.Insert(slices.Clone(policies), at, p)
	params := map[string]string{"from": req.From, "to": req.To, "rule": describeRule(p), "position": strconv.Itoa(at)}
	return firewallCard("firewall.ruleAdd", cfg, params), cfg, nil
}

func planFirewallRuleRemove(st firewall.Status, from, to string, index int) (*toolcatalog.Action, firewall.Config, error) {
	cfg, i, err := firewallTarget(st, from, to)
	if err != nil {
		return nil, cfg, err
	}
	cfg = copyConfig(cfg)
	policies := cfg.Cells[i].Policies
	if index < 0 || index >= len(policies) {
		return nil, cfg, toolcatalog.Errorf("cell %s → %s has %d rules; firewall_status shows their numbers", from, to, len(policies))
	}
	params := map[string]string{"from": from, "to": to, "rule": describeRule(policies[index]), "position": strconv.Itoa(index)}
	cfg.Cells[i].Policies = slices.Delete(slices.Clone(policies), index, index+1)
	return firewallCard("firewall.ruleRemove", cfg, params), cfg, nil
}

func planFirewallUnblock(st firewall.Status, address string) (*toolcatalog.Action, error) {
	address = strings.TrimSpace(address)
	if !slices.ContainsFunc(st.Blocked, func(b firewall.Block) bool { return b.Address == address }) {
		return nil, toolcatalog.Errorf("%q is not blocked; firewall_status lists the blocked addresses", address)
	}
	return withLevel(card("firewall.unblock", map[string]string{"address": address}, "POST", "/firewall/unblock", map[string]string{"address": address}), toolcatalog.LevelDanger, "", ""), nil
}

func firewallProposalError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, firewall.ErrSwitchManual):
		return toolcatalog.CodedErrorf("firewall_switch_manual", "switching the firewall on or off is only done by a person in the web UI")
	case errors.Is(err, firewall.ErrClosesWebUI):
		return toolcatalog.CodedErrorf("firewall_closes_webui", "the change would close the web UI for the home network or the VPN; not proposed")
	case errors.Is(err, firewall.ErrPending):
		return toolcatalog.CodedErrorf("firewall_pending", "a firewall change is on trial; a person confirms or undoes it in the web UI first")
	case errors.Is(err, firewall.ErrDisabled):
		return toolcatalog.CodedErrorf("read_only", "the firewall cannot be changed in development mode")
	}
	return toolcatalog.Errorf("%v", err)
}

// --- backups

func findJob(views []backup.View, id string) (backup.View, bool) {
	i := slices.IndexFunc(views, func(v backup.View) bool { return v.Job.ID == id })
	if i < 0 {
		return backup.View{}, false
	}
	return views[i], true
}

func planBackupCheck(views []backup.View, id string, readData int) (*toolcatalog.Action, error) {
	view, ok := findJob(views, id)
	if !ok {
		return nil, toolcatalog.Errorf("no backup job %q; backup_status shows them", id)
	}
	if readData < 0 || readData > 100 {
		return nil, toolcatalog.Errorf("readData is 0 to 100")
	}
	return card("backup.check", map[string]string{"name": view.Job.ID, "readData": strconv.Itoa(readData)}, "POST", "/backups/"+esc(view.Job.ID)+"/check", map[string]any{"readData": readData}), nil
}

func planBackupRestore(views []backup.View, mounted []string, id, snapshot, volume string, paths []string) (*toolcatalog.Action, error) {
	view, ok := findJob(views, id)
	switch {
	case !ok:
		return nil, toolcatalog.Errorf("no backup job %q; backup_status shows them", id)
	case strings.TrimSpace(snapshot) == "":
		return nil, toolcatalog.Errorf("snapshot is required; backup_snapshots lists them")
	case !slices.Contains(mounted, volume):
		return nil, toolcatalog.Errorf("no mounted volume %q; volumes: %v", volume, mounted)
	case len(paths) == 0 || len(paths) > 200:
		return nil, toolcatalog.Errorf("give 1 to 200 paths")
	}
	clean := []string{}
	for _, p := range paths {
		if !strings.HasPrefix(p, "/") || slices.Contains(strings.Split(p, "/"), "..") || path.Clean(p) != p {
			return nil, toolcatalog.CodedErrorf("invalid_path", "paths are absolute paths of the saved state without ..: %q", p)
		}
		clean = append(clean, p)
	}
	params := map[string]string{"name": view.Job.ID, "snapshot": snapshot, "volume": volume, "count": strconv.Itoa(len(clean))}
	return card("backup.restoreFiles", params, "POST", "/backups/"+esc(view.Job.ID)+"/restore", map[string]any{"snapshot": snapshot, "volume": volume, "paths": clean}), nil
}

func planBackupSchedule(views []backup.View, id, action string) (*toolcatalog.Action, error) {
	view, ok := findJob(views, id)
	if !ok {
		return nil, toolcatalog.Errorf("no backup job %q; backup_status shows them", id)
	}
	if action != "pause" && action != "resume" {
		return nil, toolcatalog.Errorf("action must be pause or resume")
	}
	on := action == "resume"
	if view.Job.Enabled == on {
		return nil, toolcatalog.Errorf("the schedule of %s is already %s", id, map[bool]string{true: "running", false: "paused"}[on])
	}
	// The job as the page has it, without secrets: an empty secret keeps
	// the stored one for the same destination.
	job := view.Job
	job.Enabled = on
	return card("backup."+action, map[string]string{"name": job.ID}, "PUT", "/backups/"+esc(job.ID), job), nil
}

// --- automatic updates of Docker apps

func planAutoUpdate(current docker.AutoUpdateSettings, apps []string, app string, on bool, at *string, weekdays *[]int) (*toolcatalog.Action, error) {
	known := slices.Contains(apps, app)
	listed := slices.Contains(current.Apps, app)
	if !known && !(listed && !on) {
		return nil, toolcatalog.Errorf("no app %q; docker_list shows the app of each container", app)
	}
	next := docker.AutoUpdateSettings{Apps: slices.DeleteFunc(slices.Clone(current.Apps), func(a string) bool { return a == app }), Weekdays: slices.Clone(current.Weekdays), Time: current.Time}
	if on {
		next.Apps = append(next.Apps, app)
	}
	slices.Sort(next.Apps)
	if next.Apps == nil {
		next.Apps = []string{}
	}
	if at != nil {
		next.Time = strings.TrimSpace(*at)
	}
	if weekdays != nil {
		next.Weekdays = slices.Clone(*weekdays)
		slices.Sort(next.Weekdays)
		next.Weekdays = slices.Compact(next.Weekdays)
	}
	if next.Weekdays == nil {
		next.Weekdays = []int{}
	}
	if len(next.Time) != 5 || next.Time[2] != ':' {
		return nil, toolcatalog.Errorf("time is HH:MM")
	}
	if listed == on && at == nil && weekdays == nil {
		return nil, toolcatalog.Errorf("the automatic update of %s is already %s", app, map[bool]string{true: "on", false: "off"}[on])
	}
	key := "docker.autoUpdateOff"
	if on {
		key = "docker.autoUpdateOn"
	}
	days := []string{}
	for _, d := range next.Weekdays {
		days = append(days, strconv.Itoa(d))
	}
	params := map[string]string{"name": app, "time": next.Time, "weekdays": strings.Join(days, ",")}
	return card(key, params, "PUT", "/docker/auto-update", next), nil
}
