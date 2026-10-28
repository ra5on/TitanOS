package firewall

import (
	"slices"
	"sort"
)

// Settings file and the way back to older titand.
//
// /config/firewall/settings.json keeps the settings of version 1 (zones
// home/VPN/internet × services, apps, own rules) at the top level, as older
// titand read them, and version 2 under "v2". An older titand (A/B fallback
// to a system before the zones) ignores "v2" and gets a view of the
// built-in zones computed from version 2 (project). When it saves the file
// itself, it writes only its own fields: "v2" is gone, and the next newer
// titand migrates what the older one stored (migrate) instead of bringing
// back settings the user changed in the meantime.

// Zones says from which of the zones of version 1 something is allowed.
type Zones struct {
	Home     bool `json:"home"`
	VPN      bool `json:"vpn"`
	Internet bool `json:"internet"`
}

// Has tells whether a zone is allowed.
func (z Zones) Has(zone Zone) bool {
	switch zone {
	case Home:
		return z.Home
	case VPN:
		return z.VPN
	case Internet:
		return z.Internet
	}
	return false
}

func (z *Zones) set(zone Zone, on bool) {
	switch zone {
	case Home:
		z.Home = on
	case VPN:
		z.VPN = on
	case Internet:
		z.Internet = on
	}
}

// Rule is an own rule of version 1, checked in order before the services.
type Rule struct {
	Action   string `json:"action"`
	Protocol string `json:"protocol"`
	Ports    string `json:"ports,omitempty"`
	Source   string `json:"source,omitempty"`
	// Zone limits the rule to one zone; empty means all.
	Zone     Zone   `json:"zone,omitempty"`
	Comment  string `json:"comment,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// legacyConfig is the settings file of version 1.
type legacyConfig struct {
	Enabled   bool             `json:"enabled"`
	Services  map[string]Zones `json:"services"`
	Apps      map[string]Zones `json:"apps"`
	NewApps   Zones            `json:"newApps"`
	Rules     []Rule           `json:"rules"`
	AutoBlock AutoBlock        `json:"autoBlock"`
}

// zoneModel is the part of version 2 an older titand does not know.
type zoneModel struct {
	Zones []ZoneDef `json:"zones"`
	Cells []Cell    `json:"cells"`
}

// fileConfig is settings.json.
type fileConfig struct {
	legacyConfig
	V2 *zoneModel `json:"v2,omitempty"`
}

// maxLegacyRules is what an older titand accepts.
const maxLegacyRules = 100

// emptyFile is what a file of version 1 leaves out means.
func emptyFile() fileConfig {
	return fileConfig{legacyConfig: legacyConfig{NewApps: local}}
}

// fromFile reads the file: version 2 if there, else version 1 migrated.
func fromFile(f fileConfig) Config {
	if f.V2 == nil {
		return migrate(f.legacyConfig)
	}
	return normal(Config{Enabled: f.Enabled, Zones: f.V2.Zones, Cells: f.V2.Cells, AutoBlock: f.AutoBlock})
}

// toFile writes version 2 and the view of it for older titand.
func toFile(cfg Config) fileConfig {
	return fileConfig{legacyConfig: project(cfg), V2: &zoneModel{Zones: cfg.Zones, Cells: cfg.Cells}}
}

// migrate turns settings of version 1 into zones and cells that filter
// exactly the same: own rules first in each cell of their zone (to the NAS
// and to the apps), then the allowed services, then the apps with an own
// setting; what is left follows the default (services: blocked, apps: "new
// apps"). The zone Docker did not exist: its networks counted as home
// network, and their traffic to other zones was not filtered at all. For a
// firewall that was on, the Docker zone gets the cells of the home network
// and allows everything to other zones, so nothing changes; for one that
// was off, the defaults of a new installation.
func migrate(old legacyConfig) Config {
	cfg := Config{Enabled: old.Enabled, AutoBlock: old.AutoBlock}
	services := map[string]Zones{}
	for _, s := range Services(nil) {
		services[s.ID] = s.Default
	}
	for id, z := range old.Services {
		if _, ok := services[id]; ok {
			services[id] = z
		}
	}
	apps := make([]string, 0, len(old.Apps))
	for key := range old.Apps {
		apps = append(apps, key)
	}
	sort.Strings(apps)
	newApps := old.NewApps
	for _, zone := range legacyZones {
		var rules []Policy
		for _, r := range old.Rules {
			if r.Zone != "" && r.Zone != zone {
				continue
			}
			rules = append(rules, Policy{Action: r.Action, Protocol: r.Protocol, Ports: r.Ports, Source: r.Source, Comment: r.Comment, Disabled: r.Disabled})
		}
		nas := Cell{From: zone, To: ToNAS, Default: Deny, Policies: slices.Clone(rules)}
		for _, s := range Services(nil) {
			if services[s.ID].Has(zone) {
				nas.Policies = append(nas.Policies, Policy{Action: Allow, Protocol: "any", Service: s.ID})
			}
		}
		toApps := Cell{From: zone, To: ToApps, Default: Deny, Policies: slices.Clone(rules)}
		if newApps.Has(zone) {
			toApps.Default = Allow
		}
		for _, key := range apps {
			action := Deny
			if old.Apps[key].Has(zone) {
				action = Allow
			}
			toApps.Policies = append(toApps.Policies, Policy{Action: action, Protocol: "any", App: key})
		}
		if nas.Policies == nil {
			nas.Policies = []Policy{}
		}
		if toApps.Policies == nil {
			toApps.Policies = []Policy{}
		}
		cfg.Cells = append(cfg.Cells, nas, toApps)
		if zone == Home && old.Enabled {
			for _, c := range []Cell{nas, toApps} {
				c.From = Docker
				c.Policies = slices.Clone(c.Policies)
				cfg.Cells = append(cfg.Cells, c)
			}
		}
	}
	if old.Enabled {
		// Out of Docker networks nothing was filtered, not even between
		// them: titanos-firewall-open accepts first in FORWARD and in
		// DOCKER-USER, before Docker's own isolation.
		for _, to := range append(slices.Clone(legacyZones), Docker) {
			cfg.Cells = append(cfg.Cells, Cell{From: Docker, To: string(to), Policies: []Policy{}, Default: Allow})
		}
	}
	return normal(cfg)
}

// project is the view of version 2 an older titand understands: for the
// built-in zones home, VPN and internet, the services and apps that their
// cells allow and the own policies of their cells to the NAS (as rules of
// that zone). Own zones, the Docker zone and cells between zones do not
// exist there; their traffic falls into home network, VPN or internet by
// address. So nobody loses the web UI by going back, the web UI counts as
// allowed from the home network (and VPN) if an own zone allows it.
func project(cfg Config) legacyConfig {
	old := legacyConfig{Enabled: cfg.Enabled, Services: map[string]Zones{}, Apps: map[string]Zones{}, Rules: []Rule{}, AutoBlock: cfg.AutoBlock}
	services := Services(nil)
	for _, s := range services {
		var z Zones
		for _, zone := range legacyZones {
			z.set(zone, serviceAllowed(cfg.Cell(zone, ToNAS), s.ID))
		}
		old.Services[s.ID] = z
	}
	for _, zone := range cfg.Zones {
		if !zone.Builtin && serviceAllowed(cfg.Cell(zone.ID, ToNAS), "webui") {
			z := old.Services["webui"]
			z.Home, z.VPN = true, true
			old.Services["webui"] = z
		}
	}
	keys := map[string]bool{}
	for _, zone := range legacyZones {
		c := cfg.Cell(zone, ToApps)
		old.NewApps.set(zone, c.Default == Allow)
		for _, p := range c.Policies {
			if p.App != "" {
				keys[p.App] = true
			}
		}
	}
	for key := range keys {
		var z Zones
		for _, zone := range legacyZones {
			z.set(zone, appAllowed(cfg.Cell(zone, ToApps), key))
		}
		old.Apps[key] = z
	}
	for _, zone := range legacyZones {
		for _, p := range cfg.Cell(zone, ToNAS).Policies {
			if p.Service != "" || len(old.Rules) >= maxLegacyRules {
				continue
			}
			old.Rules = append(old.Rules, Rule{Action: p.Action, Protocol: p.Protocol, Ports: p.Ports, Source: p.Source, Zone: zone, Comment: p.Comment, Disabled: p.Disabled})
		}
	}
	return old
}

// serviceAllowed reads a service from a cell the way version 1 knew it:
// the first enabled policy for this service without a source, else the
// default.
func serviceAllowed(c Cell, id string) bool {
	for _, p := range c.Policies {
		if !p.Disabled && p.Service == id && p.Source == "" {
			return p.Action == Allow
		}
	}
	return c.Default == Allow
}

// appAllowed reads an app from a cell: the first enabled policy for this
// app without a source, else the default.
func appAllowed(c Cell, key string) bool {
	for _, p := range c.Policies {
		if !p.Disabled && p.App == key && p.Source == "" {
			return p.Action == Allow
		}
	}
	return c.Default == Allow
}
