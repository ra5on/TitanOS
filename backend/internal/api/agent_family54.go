package api

// Gruppe „Gemeinschaft und Familie“ (PLAN 54-G): Backup-Buddy, Skill-
// Katalog, Familien-Tagebuch. Stub of the skeleton; the group fills this
// file (and new agent_family54_*.go files).

type agentBuddy struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (b agentBuddy) view() agentBuddy { return b }

type agentCatalog struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (c agentCatalog) view() agentCatalog { return c }

type agentDiaryNAS struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (d agentDiaryNAS) view() agentDiaryNAS { return d }

type agentDiaryMine struct{}

func (d agentDiaryMine) view() agentDiaryMine { return d }

func (s *Server) checkFamilyNAS(f *agentFeatures, old agentFeatures) error { return nil }
func (s *Server) checkDiaryMine(user string, d *agentDiaryMine, old agentDiaryMine) error {
	return nil
}
func (s *Server) registerAgentFamily54(mux *routeMux) {}
func (s *Server) agentFamily54Tick(now agentNow)      {}
func (b *agentToolbox) family54Tools() []*agentTool   { return nil }
