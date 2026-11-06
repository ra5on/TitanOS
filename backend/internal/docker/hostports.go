package docker

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
)

// HostPorts are all TCP ports the containers publish on the NAS, those of
// stopped containers included: GET /containers/json lists ports of
// running containers only, so every container is inspected for its port
// bindings. Random host ports appear only while a container runs and
// come from the list. ok is false while Docker does not answer.
func (m *Manager) HostPorts(ctx context.Context) (ports []int, ok bool) {
	m.mu.Lock()
	ready := m.applied != ""
	m.mu.Unlock()
	if m.Disabled || !ready {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	containers, err := m.api.containers(ctx)
	if err != nil {
		return nil, false
	}
	for _, c := range containers {
		for _, p := range c.Ports {
			if p.PublicPort > 0 && (p.Type == "tcp" || p.Type == "") {
				ports = append(ports, p.PublicPort)
			}
		}
		var inspect apiInspect
		if err := m.api.getJSON(ctx, "/containers/"+c.ID+"/json", nil, &inspect); err != nil {
			continue
		}
		for spec, bindings := range inspect.HostConfig.PortBindings {
			if _, proto, _ := strings.Cut(spec, "/"); proto != "" && proto != "tcp" {
				continue
			}
			for _, b := range bindings {
				ports = append(ports, portRange(b.HostPort)...)
			}
		}
	}
	slices.Sort(ports)
	return slices.Compact(ports), true
}

// portRange reads a host port of a binding: "8080", a range "8000-8010"
// or "" (a random port, none to note).
func portRange(text string) []int {
	from, to, isRange := strings.Cut(strings.TrimSpace(text), "-")
	first, err := strconv.Atoi(from)
	if err != nil || first < 1 || first > 65535 {
		return nil
	}
	last := first
	if isRange {
		if last, err = strconv.Atoi(to); err != nil || last < first || last > 65535 {
			return nil
		}
	}
	out := make([]int, 0, last-first+1)
	for p := first; p <= last; p++ {
		out = append(out, p)
	}
	return out
}
