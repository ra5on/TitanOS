package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// sampleNetworks: the LAN bridge, a VLAN for IoT devices, the default
// Docker network, a user-defined one, a macvlan network and a VM NAT
// bridge.
func sampleNetworks() []Network {
	return []Network{
		{ID: "net:lan", Kind: KindNetwork, Name: "LAN", Device: "br0", Subnets: []string{"192.168.1.0/24", "2001:db8:1:2::/64"}},
		{ID: "net:iot", Kind: KindNetwork, Name: "IoT", Device: "tviot", Subnets: []string{"192.168.50.0/24"}},
		{ID: "docker:bridge", Kind: KindDocker, Name: "bridge", Device: "docker0", Subnets: []string{"172.17.0.0/16"}},
		{ID: "docker:immich_default", Kind: KindDocker, Name: "immich_default", Device: "br-0123456789ab", Subnets: []string{"172.18.0.0/16"}},
		{ID: "docker:media", Kind: KindDocker, Name: "media", Device: "br-ba9876543210", Subnets: []string{"172.19.0.0/16", "fd00:19::/64"}},
		{ID: "docker:titanos-lan-apps", Kind: KindDockerLAN, Name: "titanos-lan-apps", Parent: "tnapps", Subnets: []string{"192.168.30.0/24"}},
		{ID: "vm:virbr0", Kind: KindVM, Name: "virbr0", Device: "virbr0", Subnets: []string{"192.168.122.0/24"}},
	}
}

// sampleZones: an own zone for the IoT VLAN plus an office network, and
// one for the Docker network "media" (behind the NAS).
func sampleZones() Config {
	cfg := Defaults()
	cfg.Enabled = true
	cfg.Zones = append(cfg.Zones,
		ZoneDef{ID: "ziot", Name: "IoT", Networks: []string{"net:iot"}, Sources: []string{"10.20.0.0/16"}},
		ZoneDef{ID: "zmedia", Name: "Media", Networks: []string{"docker:media", "vm:gone"}},
	)
	cfg = normal(cfg)
	cfg = withPolicies(cfg, "ziot", ToNAS,
		Policy{Action: Allow, Service: "ping"},
		Policy{Action: Allow, Protocol: "tcp", Ports: "1883", Comment: "MQTT"})
	cfg = withPolicies(cfg, "zmedia", ToNAS, Policy{Action: Allow, Service: "nfs"})
	cfg = withPolicies(cfg, "zmedia", string(Home), Policy{Action: Deny, Protocol: "tcp", Ports: "22", Destination: "192.168.1.0/24", Comment: "no ssh"})
	c := cfg.Cell("zmedia", string(Docker))
	c.Policies = []Policy{{Action: Allow, Protocol: "tcp", Ports: "2283", Comment: "immich"}}
	cfg.setCell(c)
	c = cfg.Cell(Docker, string(Internet))
	c.Default = Deny
	cfg.setCell(c)
	return normal(cfg)
}

func zonesRuleset() ruleset {
	rs := sampleRuleset()
	rs.cfg = sampleZones()
	rs.networks = sampleNetworks()
	rs.admin = netip.Addr{}
	return rs
}

func TestRenderZonesGolden(t *testing.T) {
	cfg := sampleZones()
	if err := validate(cfg); err != nil {
		t.Fatal(err)
	}
	script := render(zonesRuleset())
	golden(t, "zones.nft", script)
	for _, want := range []string{
		// Own zones and Docker before VPN and home network, by interface and subnet.
		`iifname "tviot" ip saddr 192.168.50.0/24 goto ziot`,
		`ip saddr 10.20.0.0/16 goto ziot`,
		`iifname "br-ba9876543210" ip6 saddr fd00:19::/64 goto zmedia`,
		`iifname "docker0" ip saddr 172.17.0.0/16 goto docker`,
		`iifname "br-0123456789ab" ip saddr 172.18.0.0/16 goto app_docker`,
		// Traffic out of networks behind the NAS.
		`ct state new jump routed`,
		`iifname . oifname { "br-ba9876543210" . "br-ba9876543210", "docker0" . "docker0", "br-0123456789ab" . "br-0123456789ab" } accept comment "same network"`,
		`iifname "br-ba9876543210" goto from_zmedia`,
		`iifname { "docker0", "br-0123456789ab" } goto from_docker`,
		`oifname "tviot" goto fwd_zmedia_ziot`,
		`ip daddr 10.20.0.0/16 goto fwd_zmedia_ziot`,
		`ip daddr 192.168.1.0/24 tcp dport 22 goto drop_fwd_zmedia comment "no ssh"`,
		`tcp dport 2283 accept comment "immich"`,
		`log prefix "titanos-fw fwd-zmedia "`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("missing: %s", want)
		}
	}
	chain := func(name string) string {
		start := strings.Index(script, "\tchain "+name+" {")
		if start < 0 {
			t.Fatalf("no chain %s", name)
		}
		end := strings.Index(script[start:], "\n\t}\n")
		return script[start : start+end]
	}
	if !strings.HasSuffix(chain("fwd_docker_internet"), "goto drop_fwd_docker") || !strings.HasSuffix(chain("fwd_docker_home"), "accept") {
		t.Error("cells of the Docker zone")
	}
	if !strings.HasSuffix(chain("fwd_docker_docker"), "goto drop_fwd_docker") || !strings.HasSuffix(chain("fwd_zmedia_docker"), "goto drop_fwd_zmedia") {
		t.Error("Docker networks are kept apart by default")
	}
	// Docker zone, fresh defaults: to the NAS only the HTTPS names and DNS.
	docker := chain("docker")
	if !strings.Contains(docker, `tcp dport 443 accept comment "apps"`) || !strings.Contains(docker, `meta l4proto { tcp, udp } th dport 53 accept comment "DNS"`) || strings.Contains(docker, "smb") {
		t.Errorf("Docker zone to the NAS:\n%s", docker)
	}
	// The macvlan network, the VM bridge outside a zone and the missing
	// network have no rules.
	for _, absent := range []string{"tnapps", "virbr0", "192.168.30.0", "gone"} {
		if strings.Contains(script, absent) {
			t.Errorf("%s should not be in the rules", absent)
		}
	}
	// Without any network behind the NAS there is no routing chain.
	plain := sampleRuleset()
	if strings.Contains(render(plain), "routed") {
		t.Error("routed without networks behind the NAS")
	}
}

func TestClassifyOwnZones(t *testing.T) {
	cfg := sampleZones()
	c := newClassifier(cfg, sampleNetworks(), homeNetworks([]string{"192.168.1.0/24"}))
	for addr, want := range map[string]Zone{
		"192.168.50.7":  "ziot",
		"10.20.3.4":     "ziot",
		"172.19.0.5":    "zmedia",
		"fd00:19::5":    "zmedia",
		"172.17.0.2":    Docker,
		"172.18.0.9":    Docker,
		"192.168.1.20":  Home,
		"192.168.122.5": Home, // VM NAT outside a zone: by address
		"100.64.0.1":    VPN,
		"203.0.113.9":   Internet,
		"127.0.0.1":     Home,
	} {
		if got := c.zone(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
	// AllowHTTP follows the own zone: IoT has no web UI, Docker only the
	// HTTPS names of apps.
	m, _, _ := newTestManager(t)
	m.Load(context.Background())
	m.networks = sampleNetworks()
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	m.Confirm(context.Background())
	for _, c := range []struct {
		addr string
		app  bool
		want bool
	}{
		{"192.168.50.7:1", false, false},
		{"192.168.1.30:1", false, true},
		{"172.17.0.2:1", false, false},
		{"172.17.0.2:1", true, true},
	} {
		if got := m.AllowHTTP(c.addr, c.app); got != c.want {
			t.Errorf("%s app=%v: %v", c.addr, c.app, got)
		}
	}
	if z := m.Status("192.168.50.7").ClientZone; z != "ziot" {
		t.Errorf("client zone %s", z)
	}
}

// TestMigrationAndWayBack: a file of version 1 is migrated on load without
// a change in what is filtered, Docker networks keep the home network's
// cells, the stored file stays readable for an older titand, and a file an
// older titand wrote over it is migrated again.
func TestMigrationAndWayBack(t *testing.T) {
	m, nft, store := newTestManager(t)
	old := sampleLegacy()
	if err := store.WriteJSON(settingsFile, old); err != nil {
		t.Fatal(err)
	}
	m.Apps = func(context.Context) ([]App, bool) { return sampleRuleset().apps, true }
	m.Networks = func(context.Context) ([]Network, bool) { return sampleNetworks(), true }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.refresh(context.Background())
	status := m.Status("192.168.1.20")
	cfg := status.Config
	if !cfg.Enabled {
		t.Fatal("migrated settings must stay on")
	}
	for _, to := range []string{ToNAS, ToApps} {
		home, docker := cfg.Cell(Home, to), cfg.Cell(Docker, to)
		if docker.Default != home.Default || !slices.Equal(docker.Policies, home.Policies) {
			t.Errorf("Docker → %s should be the home network's cell: %+v", to, docker)
		}
	}
	// Nothing out of Docker networks was filtered before, not even between
	// them (titanos-firewall-open accepts before Docker's isolation).
	for _, to := range []Zone{Home, VPN, Internet, Docker} {
		if cfg.Cell(Docker, string(to)).Default != Allow {
			t.Errorf("Docker → %s must stay open after the migration", to)
		}
	}
	if !strings.HasSuffix(strings.Split(strings.Split(nft.last(), "chain fwd_docker_docker {")[1], "\n\t}")[0], "accept") {
		t.Error("Docker networks must keep reaching each other")
	}
	// Off before: the defaults of a new installation.
	off := sampleLegacy()
	off.Enabled = false
	if migrate(off).Cell(Docker, string(Docker)).Default != Deny {
		t.Error("a firewall that was off gets the new defaults")
	}
	if !strings.Contains(nft.last(), `iifname "docker0" ip saddr 172.17.0.0/16 goto docker`) || !strings.Contains(nft.last(), `tcp dport 22 accept comment "ssh"`) {
		t.Errorf("rules after migration:\n%s", nft.last())
	}
	if len(status.Networks) != len(sampleNetworks()) {
		t.Errorf("networks: %+v", status.Networks)
	}
	// Nothing was written by loading.
	var raw map[string]json.RawMessage
	store.ReadJSON(settingsFile, &raw)
	if _, ok := raw["v2"]; ok {
		t.Fatal("loading must not write the file")
	}

	// A change writes version 2 and the view for older titand.
	changed := withPolicies(cfg, Home, ToNAS, Policy{Action: Deny, Protocol: "tcp", Ports: "8080"})
	changed.Zones = append(changed.Zones, ZoneDef{ID: "ziot", Name: "IoT", Networks: []string{"net:iot"}})
	changed = withService(normal(changed), "ziot", "webui", true)
	if err := m.Change(context.Background(), changed, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(context.Background()); err != nil {
		t.Fatal(err)
	}
	var older legacyConfig
	if err := store.ReadJSON(settingsFile, &older); err != nil {
		t.Fatal(err)
	}
	if !older.Enabled || !older.Services["ssh"].Home || older.Services["ssh"].Internet || !older.Services["vpn"].Internet {
		t.Errorf("services for older titand: %+v", older.Services)
	}
	if !older.Apps["jellyfin"].Internet || older.Apps["container:pihole"].VPN || !older.NewApps.Home || older.NewApps.Internet {
		t.Errorf("apps for older titand: %+v %+v", older.Apps, older.NewApps)
	}
	if !slices.ContainsFunc(older.Rules, func(r Rule) bool { return r.Ports == "8080" && r.Zone == Home && r.Action == Deny }) {
		t.Errorf("rules for older titand: %+v", older.Rules)
	}
	if len(older.Rules) > maxLegacyRules {
		t.Error("too many rules for older titand")
	}
	// What the old version renders from the view equals the built-in
	// zones of version 2 (same migration back and forth).
	again := migrate(older)
	for _, zone := range legacyZones {
		for _, to := range []string{ToNAS, ToApps} {
			for _, id := range []string{"ssh", "webui", "smb"} {
				if to == ToNAS && serviceAllowed(again.Cell(zone, to), id) != serviceAllowed(m.Status("").Config.Cell(zone, to), id) {
					t.Errorf("%s %s differs in the view", zone, id)
				}
			}
		}
	}

	// The older titand saves the file itself: "v2" is gone. The newer one
	// takes the older one's settings, not its own stale zones.
	older.Services["ssh"] = Zones{}
	store.WriteJSON(settingsFile, older)
	next, _, _ := newTestManager(t)
	next.runner = nft
	next.store = store
	if err := next.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := next.Status("").Config
	if serviceAllowed(got.Cell(Home, ToNAS), "ssh") {
		t.Error("the older titand's change was lost")
	}
	if _, ok := got.Zone("ziot"); ok {
		t.Error("zones of version 2 must not come back over the older titand's settings")
	}
}

func TestDockerUser(t *testing.T) {
	m, nft, _ := newTestManager(t)
	m.Load(context.Background())
	m.networks = sampleNetworks()
	cfg := sampleZones()
	// Docker is not running yet: titand only looks for DOCKER-USER.
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	m.Confirm(context.Background())
	for _, call := range nft.ipt {
		if !strings.Contains(call, " -L ") {
			t.Errorf("without Docker nothing may change: %s", call)
		}
	}
	// zmedia → docker lets 2283 through: the bridges of the media zone to
	// the bridges of the Docker zone, while Docker is not running yet.
	nft.ipt = nil
	cfg = normal(withPolicies(cfg, Docker, string(Docker), Policy{Action: Allow, Protocol: "tcp", Ports: "5432"}))
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	m.Confirm(context.Background())
	if m.Status("").DockerError == "" {
		t.Error("without DOCKER-USER the error must show")
	}
	nft.docker = true
	nft.ipt = nil
	m.refresh(context.Background())
	if m.Status("").DockerError != "" {
		t.Fatalf("not loaded once Docker runs: %s", m.Status("").DockerError)
	}
	want := []string{
		"iptables -w -A TITANOS-FWD -i br-ba9876543210 -o docker0 -j ACCEPT",
		"iptables -w -A TITANOS-FWD -i docker0 -o br-ba9876543210 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
		"iptables -w -A TITANOS-FWD -i docker0 -o br-0123456789ab -j ACCEPT",
		"iptables -w -I DOCKER-USER 1 -j TITANOS-FWD",
	}
	for _, w := range want {
		if !slices.Contains(nft.ipt, w) {
			t.Errorf("missing %q in %q", w, nft.ipt)
		}
	}
	// Nothing of Docker's own tables but the jump in DOCKER-USER.
	restores := 0
	for _, call := range nft.ipt {
		fields := strings.Fields(call)
		if strings.HasSuffix(fields[0], "-restore") {
			restores++
			continue
		}
		chain := fields[3]
		if chain != dockerUserChain && chain != "DOCKER-USER" {
			t.Errorf("touched another chain: %s", call)
		}
		if chain == "DOCKER-USER" && !slices.Contains([]string{"-L", "-C", "-I", "-D"}, fields[2]) {
			t.Errorf("changed DOCKER-USER itself: %s", call)
		}
		if chain == "DOCKER-USER" && (fields[2] == "-I" || fields[2] == "-D") && fields[len(fields)-1] != dockerUserChain {
			t.Errorf("DOCKER-USER rule other than the jump: %s", call)
		}
	}
	if restores != 2 {
		t.Errorf("one restore per family, got %d", restores)
	}
	// Idempotent: a refresh only checks the jump.
	nft.ipt = nil
	m.refresh(context.Background())
	if len(nft.ipt) != 1 || !strings.Contains(nft.ipt[0], "-C DOCKER-USER") {
		t.Errorf("refresh should only check: %q", nft.ipt)
	}
	// Docker restarted and lost the jump: loaded again.
	nft.jump["iptables"] = false
	m.refresh(context.Background())
	if !nft.jump["iptables"] {
		t.Error("jump not restored")
	}
	// Switching off removes the jump and the chain.
	cfg.Enabled = false
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if nft.jump["iptables"] || nft.chains["iptables:"+dockerUserChain] {
		t.Errorf("not removed: %v %v", nft.jump, nft.chains)
	}
}

func TestRefreshFollowsNetworks(t *testing.T) {
	m, nft, store := newTestManager(t)
	cfg := Defaults()
	cfg.Enabled = true
	store.WriteJSON(settingsFile, toFile(cfg))
	networks := []Network{{ID: "docker:bridge", Kind: KindDocker, Name: "bridge", Device: "docker0", Subnets: []string{"172.17.0.0/16"}}}
	m.Networks = func(context.Context) ([]Network, bool) { return networks, true }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.refresh(context.Background())
	if !strings.Contains(nft.last(), `iifname "docker0"`) {
		t.Fatal("Docker network not taken over")
	}
	networks = append(networks, Network{ID: "docker:new", Kind: KindDocker, Name: "new", Device: "br-feedfeedfeed", Subnets: []string{"172.30.0.0/16"}})
	m.refresh(context.Background())
	if !strings.Contains(nft.last(), `iifname "br-feedfeedfeed" ip saddr 172.30.0.0/16 goto docker`) {
		t.Error("a new Docker network lands in the zone Docker")
	}
	m.Networks = func(context.Context) ([]Network, bool) { return nil, false }
	loads := len(nft.scripts)
	m.refresh(context.Background())
	if len(nft.scripts) != loads || !strings.Contains(nft.last(), "br-feedfeedfeed") {
		t.Error("networks lost while Docker did not answer")
	}
}

func TestParseForwardDrop(t *testing.T) {
	drop, ok := parseDrop("titanos-fw fwd-zmedia IN=br-ba9876543210 OUT=br0 SRC=172.19.0.5 DST=192.168.1.9 PROTO=TCP SPT=1 DPT=22")
	if !ok || !drop.Forward || drop.Zone != "zmedia" || drop.Destination != "192.168.1.9" || drop.Out != "br0" || drop.Port != 22 {
		t.Errorf("forward drop: %+v %v", drop, ok)
	}
	if drop, ok := parseDrop("titanos-fw docker IN=docker0 SRC=172.17.0.2 DST=172.17.0.1 PROTO=UDP DPT=137"); !ok || drop.Zone != "docker" {
		t.Errorf("docker drop: %+v", drop)
	}
	if _, ok := parseDrop("titanos-fw evil-zone IN=x SRC=1.2.3.4"); ok {
		t.Error("unknown zones are no drops")
	}
}

func TestSetAppZonesKeepsOrder(t *testing.T) {
	cfg := migrate(sampleLegacy())
	setAppZones(&cfg, "immich", Zones{Home: true})
	setAppZones(&cfg, "jellyfin", Zones{Home: true})
	if z := appZones(cfg, "immich"); !z.Home || z.VPN || z.Internet {
		t.Errorf("immich: %+v", z)
	}
	if z := appZones(cfg, "jellyfin"); z.Internet {
		t.Errorf("jellyfin: %+v", z)
	}
	if n := len(slices.DeleteFunc(slices.Clone(cfg.Cell(Home, ToApps).Policies), func(p Policy) bool { return p.App != "jellyfin" })); n != 1 {
		t.Errorf("jellyfin should have one policy, has %d", n)
	}
	if err := validate(normal(cfg)); err != nil {
		t.Error(err)
	}
	if errors.Is(validate(normal(cfg)), ErrInvalid) {
		t.Error("invalid")
	}
}
