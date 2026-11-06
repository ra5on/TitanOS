package api

// Gruppe „Vorausschau“ (PLAN 54-V): Was-wäre-wenn, Hardware-Berater. Stub
// of the skeleton; the group fills this file (and new agent_foresight_*.go
// files).

type agentWhatIf struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (w agentWhatIf) view() agentWhatIf { return w }

type agentAdvisor struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (a agentAdvisor) view() agentAdvisor { return a }

func (s *Server) checkForesight(f *agentFeatures, old agentFeatures) error { return nil }
func (s *Server) registerAgentForesight(mux *routeMux)                     {}
func (s *Server) agentForesightTick(now agentNow)                          {}
func (b *agentToolbox) foresightTools() []*agentTool                       { return nil }
