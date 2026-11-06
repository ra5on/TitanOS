package api

// Gruppe „Schutz“ (PLAN 54-S): Ransomware-Wächter, Notfallhandbuch,
// Notfallzugang. Stub of the skeleton; the group fills this file (and new
// agent_protect_*.go files).

type agentGuard struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (g agentGuard) view() agentGuard { return g }

type agentHandbook struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (h agentHandbook) view() agentHandbook { return h }

type agentEmergency struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (e agentEmergency) view() agentEmergency { return e }

func (s *Server) checkProtect(f *agentFeatures, old agentFeatures) error { return nil }
func (s *Server) registerAgentProtect(mux *routeMux)                     {}
func (s *Server) agentProtectTick(now agentNow)                          {}
func (b *agentToolbox) protectTools() []*agentTool                       { return nil }
