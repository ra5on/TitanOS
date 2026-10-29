package vm

import "strings"

func (m *Manager) namedBridge(network string) (string, bool) {
	if !strings.HasPrefix(network, "network:") || m.NetworkBridge == nil {
		return "", false
	}
	return m.NetworkBridge(strings.TrimPrefix(network, "network:"))
}

// storedVM is a VM the way it is written down: on a named network older
// titand versions see no autostart (they do not know the network).
func storedVM(v VM) VM {
	if strings.HasPrefix(v.Network, "network:") {
		autostart := v.Autostart
		v.TopologyAutostart = &autostart
		v.Autostart = false
	} else {
		v.TopologyAutostart = nil
	}
	return v
}

// persistVM writes the settings of a VM to /config and then the copy in
// its folder on the volume (see volumecopy.go). Only /config counts: a copy
// that cannot be written is logged, never an error.
func (m *Manager) persistVM(v VM) error {
	if err := m.store.WriteJSON(configDir+"/"+v.Name+".json", storedVM(v)); err != nil {
		return err
	}
	m.saveVolumeCopy(v)
	return nil
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
