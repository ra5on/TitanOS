package vm

import (
	"bufio"
	"context"
	"encoding/xml"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// SetMedium puts an installation medium into the CD drive of a VM, or
// takes it out with an empty path. A running VM gets the change at once,
// like a disc put into a drive; it is kept for the next start as well.
func (m *Manager) SetMedium(ctx context.Context, name, path string) error {
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	if m.pendingSnapshot(vm) {
		return ErrSnapshotRecovery
	}
	real, err := m.checkISO(path)
	if err != nil {
		return err
	}
	// A VM without disks starts from its medium only.
	if real == "" && len(vm.Disks) == 0 {
		return ErrInvalid
	}
	if m.pid(name) != 0 {
		// --update with a file changes the disc, without one it ejects;
		// --force opens a drive the guest has locked.
		args := []string{"--quiet", "change-media", name, cdromTarget(vm)}
		if real != "" {
			args = append(args, real)
		}
		if _, err := m.virsh(ctx, append(args, "--update", "--live", "--force")...); err != nil {
			return err
		}
	}
	vm.ISO = real
	if err := m.persistVM(vm); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[name] = vm
	m.mu.Unlock()
	return nil
}

// Addresses are the network addresses of a running VM and where they
// come from: "agent" (the guest agent in the VM said so) or "arp" (the
// NAS saw the VM's card under this address on the bridge).
type Addresses struct {
	Addresses []string `json:"addresses"`
	Source    string   `json:"source"`
	// Agent is AgentConnected or AgentMissing, empty when libvirt does
	// not say (no channel for the agent, older VMs taken over).
	Agent string `json:"agent"`
}

// States of the QEMU guest agent in a running VM. Without the agent a
// guest only gets the ACPI power button when it should shut down, and some
// systems (Linux Mint, desktops in general) answer that with a question on
// their screen instead of shutting down (23 Nov 2026).
const (
	AgentConnected = "connected"
	AgentMissing   = "missing"
)

type cachedAddresses struct {
	at    time.Time
	value Addresses
}

// addressTTL keeps several open pages from asking the guest agent over
// and over; it answers slowly when it is not there.
const addressTTL = 20 * time.Second

// Addresses returns the addresses of a running VM. Without the guest
// agent, a VM on the bridge is still found in the neighbour table of the
// NAS; a VM with outgoing connections only has no address in the network.
func (m *Manager) Addresses(ctx context.Context, name string) (Addresses, error) {
	vm, err := m.get(name)
	if err != nil {
		return Addresses{}, err
	}
	if m.Disabled {
		return Addresses{Addresses: []string{}}, nil
	}
	if m.pid(name) == 0 {
		return Addresses{}, ErrNotRunning
	}
	m.mu.Lock()
	if m.addresses == nil {
		m.addresses = map[string]cachedAddresses{}
	}
	cached, ok := m.addresses[name]
	m.mu.Unlock()
	if ok && time.Since(cached.at) < addressTTL {
		return cached.value, nil
	}
	result := Addresses{Addresses: []string{}}
	ask, cancel := context.WithTimeout(ctx, 6*time.Second)
	out, err := m.runner.Run(ask, "", "virsh", "--quiet", "domifaddr", name, "--source", "agent")
	cancel()
	if err == nil {
		if found := agentAddresses(out, vm.MAC); len(found) > 0 {
			result = Addresses{Addresses: found, Source: "agent"}
		}
	}
	result.Agent = m.agentState(ctx, name)
	if result.Source == "agent" {
		result.Agent = AgentConnected
	}
	if result.Source == "" && vm.Network == "bridge" {
		if found := m.neighbours(vm.MAC); len(found) > 0 {
			result = Addresses{Addresses: found, Source: "arp"}
		}
	}
	m.mu.Lock()
	m.addresses[name] = cachedAddresses{at: time.Now(), value: result}
	m.mu.Unlock()
	return result, nil
}

// agentAddresses reads the table of "virsh domifaddr": a line per
// address, the name and MAC only on the first line of a card.
//
//	Name       MAC address          Protocol     Address
//	-------------------------------------------------------------------------------
//	enp1s0     52:54:00:12:34:56    ipv4         192.168.1.42/24
//	-          -                    ipv6         fd00::42/64
func agentAddresses(out, mac string) []string {
	found := []string{}
	ours := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || strings.HasPrefix(fields[0], "---") || fields[0] == "Name" {
			continue
		}
		if fields[1] != "-" {
			ours = strings.EqualFold(fields[1], mac)
		}
		if !ours {
			continue
		}
		if address := usable(strings.Split(fields[3], "/")[0]); address != "" {
			found = append(found, address)
		}
	}
	return found
}

// neighbours looks up a MAC address in the IPv4 neighbour table of the NAS.
func (m *Manager) neighbours(mac string) []string {
	file, err := os.Open(filepath.Join(m.procDir, "net/arp"))
	if err != nil {
		return nil
	}
	defer file.Close()
	found := []string{}
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		// IP address, HW type, Flags, HW address, Mask, Device
		fields := strings.Fields(lines.Text())
		if len(fields) < 6 || !strings.EqualFold(fields[3], mac) || fields[2] == "0x0" {
			continue
		}
		if address := usable(fields[0]); address != "" {
			found = append(found, address)
		}
	}
	return found
}

// usable drops addresses nobody can reach the VM under: loopback and
// link-local ones.
func usable(text string) string {
	ip := net.ParseIP(text)
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// agentState reads from libvirt whether the guest agent has opened its
// channel in the VM. libvirt tracks that itself: the guest is not asked,
// so a VM without the agent costs no waiting.
func (m *Manager) agentState(ctx context.Context, name string) string {
	ask, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := m.runner.Run(ask, "", "virsh", "--quiet", "dumpxml", name)
	if err != nil {
		return ""
	}
	return agentChannelState(out)
}

// agentChannelState finds the channel of the guest agent in the live XML of
// a domain: state "connected" while the agent runs in the guest.
func agentChannelState(domainXML string) string {
	var domain struct {
		Channels []struct {
			Target struct {
				Name  string `xml:"name,attr"`
				State string `xml:"state,attr"`
			} `xml:"target"`
		} `xml:"devices>channel"`
	}
	if xml.Unmarshal([]byte(domainXML), &domain) != nil {
		return ""
	}
	for _, channel := range domain.Channels {
		if channel.Target.Name != "org.qemu.guest_agent.0" {
			continue
		}
		if channel.Target.State == "connected" {
			return AgentConnected
		}
		return AgentMissing
	}
	return ""
}

// WithoutAgent lists the running VMs whose guest agent is not there. The
// restart dialog names them: they may not shut down when asked.
func (m *Manager) WithoutAgent(ctx context.Context) []string {
	if m.Disabled {
		return nil
	}
	names := []string{}
	for name := range m.RunningNames() {
		// A VM still starting has not had the time to start its agent.
		if m.pid(name) != 0 && m.agentState(ctx, name) == AgentMissing {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
