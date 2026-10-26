package vm

import "strings"

func (m *Manager) namedBridge(network string) (string, bool) {
	if !strings.HasPrefix(network, "network:") || m.NetworkBridge == nil {
		return "", false
	}
	return m.NetworkBridge(strings.TrimPrefix(network, "network:"))
}
func (m *Manager) persistVM(v VM) error {
	if strings.HasPrefix(v.Network, "network:") {
		autostart := v.Autostart
		v.TopologyAutostart = &autostart
		v.Autostart = false
	} else {
		v.TopologyAutostart = nil
	}
	return m.store.WriteJSON(configDir+"/"+v.Name+".json", v)
}
func (m *Manager) NetworkReferences() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	refs := []string{}
	for _, v := range m.vms {
		if v.Network == "bridge" || strings.HasPrefix(v.Network, "network:") {
			refs = append(refs, v.Network)
		}
	}
	return refs
}
