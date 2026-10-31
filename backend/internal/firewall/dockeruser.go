package firewall

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Docker isolates its bridge networks from each other in its own
// iptables(-nft) chains. The table inet titanos can only drop on top of
// that; to let one Docker network reach another, as a cell between zones
// allows, Docker's isolation must be passed in DOCKER-USER, the chain
// Docker leaves to the administrator. On TitanOS titanos-firewall-open
// already accepts first in FORWARD and DOCKER-USER, so the isolation does
// not apply as long as that rule is there; TITANOS-FWD keeps the open
// pairs working should it be missing, and the cells themselves are always
// enforced by inet titanos. titand keeps its own chain
// TITANOS-FWD there, jumped to from DOCKER-USER, and touches nothing else
// of Docker's tables: no rule of Docker's own chains, not DOCKER-USER
// itself beyond its jump. The exact policies of the cell stay in inet
// titanos, which still drops what they do not allow.

// dockerUserChain is titand's chain in the filter tables of iptables.
const dockerUserChain = "TITANOS-FWD"

// dockerPairs are the pairs of Docker bridges (from, to) between which a
// cell lets anything through.
func dockerPairs(cfg Config, networks []Network) [][2]string {
	if !cfg.Enabled {
		return nil
	}
	type bridge struct {
		device string
		zone   Zone
	}
	var bridges []bridge
	for _, m := range members(cfg, networks) {
		for _, n := range networks {
			if n.Kind != KindDocker || !n.assignable() {
				continue
			}
			for _, b := range m.bindings {
				if b.device == n.Device && !slices.ContainsFunc(bridges, func(o bridge) bool { return o.device == n.Device }) {
					bridges = append(bridges, bridge{n.Device, m.zone})
				}
			}
		}
	}
	var pairs [][2]string
	for _, a := range bridges {
		for _, b := range bridges {
			if a.device == b.device {
				continue
			}
			if cellLetsThrough(cfg.Cell(a.zone, string(b.zone))) {
				pairs = append(pairs, [2]string{a.device, b.device})
			}
		}
	}
	return pairs
}

// cellLetsThrough tells whether any traffic may pass a cell.
func cellLetsThrough(c Cell) bool {
	if c.Default == Allow {
		return true
	}
	return slices.ContainsFunc(c.Policies, func(p Policy) bool { return !p.Disabled && p.Action == Allow })
}

// dockerUserRules are the rules of TITANOS-FWD for the pairs.
func dockerUserRules(pairs [][2]string) [][]string {
	var rules [][]string
	for _, p := range pairs {
		rules = append(rules,
			[]string{"-i", p[0], "-o", p[1], "-j", "ACCEPT"},
			[]string{"-i", p[1], "-o", p[0], "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"})
	}
	return rules
}

func pairsKey(pairs [][2]string) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, p[0]+">"+p[1])
	}
	return strings.Join(parts, ",")
}

// syncDockerUser puts TITANOS-FWD in line with the pairs: filled and
// jumped to from DOCKER-USER, or gone without pairs. IPv4 must work
// (Docker always has DOCKER-USER there); IPv6 only where Docker manages
// ip6tables. The caller holds mu.
func (m *Manager) syncDockerUser(ctx context.Context, pairs [][2]string, check bool) error {
	key := pairsKey(pairs)
	if key == m.dockerUser && m.dockerUserErr == "" && !check {
		return nil
	}
	if len(pairs) == 0 {
		if m.dockerUser == "" && m.dockerUserErr == "" && m.dockerUserClean {
			return nil
		}
		for _, tool := range []string{"iptables", "ip6tables"} {
			m.removeDockerUser(ctx, tool)
		}
		m.dockerUser, m.dockerUserErr, m.dockerUserClean = "", "", true
		return nil
	}
	if check && key == m.dockerUser && m.dockerUserErr == "" {
		// Docker restarted or someone flushed DOCKER-USER: the jump is
		// gone. Only then load again.
		if _, err := m.runner.Run(ctx, "", "iptables", "-w", "-C", "DOCKER-USER", "-j", dockerUserChain); err == nil {
			return nil
		}
	}
	err := m.loadDockerUser(ctx, "iptables", pairs)
	if err == nil {
		_ = m.loadDockerUser(ctx, "ip6tables", pairs) // IPv6 is optional; the IPv4 rules decide
		m.dockerUser, m.dockerUserErr, m.dockerUserClean = key, "", false
		return nil
	}
	m.dockerUser, m.dockerUserErr, m.dockerUserClean = key, err.Error(), false
	return err
}

func (m *Manager) loadDockerUser(ctx context.Context, tool string, pairs [][2]string) error {
	run := func(args ...string) error {
		_, err := m.runner.Run(ctx, "", tool, append([]string{"-w"}, args...)...)
		return err
	}
	// Without DOCKER-USER, Docker is not running (yet): nothing to pass.
	if err := run("-L", "DOCKER-USER", "-n"); err != nil {
		return fmt.Errorf("%s: no DOCKER-USER chain (Docker not running?)", tool)
	}
	// The whole chain in one go: one process however many networks, and
	// never half of it.
	var script strings.Builder
	fmt.Fprintf(&script, "*filter\n:%s - [0:0]\n-F %s\n", dockerUserChain, dockerUserChain)
	for _, rule := range dockerUserRules(pairs) {
		fmt.Fprintf(&script, "-A %s %s\n", dockerUserChain, strings.Join(rule, " "))
	}
	script.WriteString("COMMIT\n")
	if _, err := m.runner.Run(ctx, script.String(), tool+"-restore", "-w", "--noflush"); err != nil {
		return fmt.Errorf("%s-restore: %v", tool, err)
	}
	if run("-C", "DOCKER-USER", "-j", dockerUserChain) != nil {
		if err := run("-I", "DOCKER-USER", "1", "-j", dockerUserChain); err != nil {
			return fmt.Errorf("%s: %v", tool, err)
		}
	}
	return nil
}

// removeDockerUser takes the jump and the chain away; errors (nothing
// there) do not matter.
func (m *Manager) removeDockerUser(ctx context.Context, tool string) {
	run := func(args ...string) error {
		_, err := m.runner.Run(ctx, "", tool, append([]string{"-w"}, args...)...)
		return err
	}
	if run("-L", dockerUserChain, "-n") != nil {
		return
	}
	for range 8 {
		if run("-D", "DOCKER-USER", "-j", dockerUserChain) != nil {
			break
		}
	}
	_ = run("-F", dockerUserChain) // cleanup, best effort: an absent chain is fine
	_ = run("-X", dockerUserChain) // cleanup, best effort: an absent chain is fine
}
