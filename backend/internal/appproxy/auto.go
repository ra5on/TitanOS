package appproxy

// Automatic HTTPS names (PLAN 61): with the proxy on, every Docker app
// with a WebUI gets a name of its own, so that the desktop can show it in
// a window. These routes live in RAM only and follow the containers: they
// are never written to the stick, a removed app loses its name, and a
// route of the app's own in proxy.json (even one switched off) always
// wins. "Automatisch für alle Apps mit WebUI" switches them off
// (Settings.NoAuto, stored only when changed).

import (
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// webUIPort reads the port of a WebUI label: "8096", "8096/admin" or
// "http://[host]:8096/…".
var webUIPort = regexp.MustCompile(`^(?:(\d{1,5})(?:[/?#].*)?|(?i:https?)://\[host\]:(\d{1,5})(?:[/?#].*)?)$`)

// WebUIPort is the port of a container's WebUI label, or 0 if the label
// names none (empty, or a full address elsewhere).
func WebUIPort(label string) int {
	m := webUIPort.FindStringSubmatch(strings.TrimSpace(label))
	if m == nil {
		return 0
	}
	port, err := strconv.Atoi(m[1] + m[2])
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

// SetAutoApps tells the manager which apps have a WebUI now and on which
// port of the NAS (app → port); see the wiring, which asks Docker. Only a
// change of the resulting names publishes anew.
func (m *Manager) SetAutoApps(ports map[string]int) {
	m.mu.Lock()
	m.autoInput = maps.Clone(ports)
	changed := m.recomputeAuto()
	m.mu.Unlock()
	if changed {
		m.changed()
	}
}

// recomputeAuto makes the automatic routes from the input and the
// settings; m.mu is held. It tells whether they changed.
func (m *Manager) recomputeAuto() bool {
	next := m.computeAuto(m.settings, m.autoInput)
	if maps.EqualFunc(next, m.auto, func(a, b Route) bool { return a.Name == b.Name && a.Port == b.Port }) {
		return false
	}
	m.auto = next
	m.proxies = map[string]*appHandler{}
	return true
}

func (m *Manager) computeAuto(s Settings, input map[string]int) map[string]Route {
	out := map[string]Route{}
	var taken []string
	for _, routes := range []map[string]Route{s.Apps, s.Targets} {
		for _, r := range routes {
			taken = append(taken, names(r)...)
		}
	}
	apps := slices.Sorted(maps.Keys(input))
	sort.Strings(apps)
	for _, app := range apps {
		port := input[app]
		if _, own := s.Apps[app]; own || !appRE.MatchString(app) || port < 1 || port > 65535 || slices.Contains(m.Reserved, port) {
			continue
		}
		name := freeLabel(DNSLabel(app), func(n string) bool { return slices.Contains(taken, n) })
		if slices.Contains(taken, name) {
			continue
		}
		taken = append(taken, name)
		out[app] = Route{Enabled: true, Name: name, Port: port, LocalOnly: true}
	}
	return out
}

// autoActive tells whether automatic routes answer; m.mu is held.
func (m *Manager) autoActive() bool {
	return !m.settings.Off && !m.settings.NoAuto
}

// changed publishes the names anew after the routes changed.
func (m *Manager) changed() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
	m.kickTailnet()
	if m.OnChange != nil {
		m.OnChange()
	}
}
