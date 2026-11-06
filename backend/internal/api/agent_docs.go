package api

// Gruppe „Dokumente und Fristen“ (PLAN 54-D): Dokumenten-Gedächtnis,
// Fristenwächter, Papierkram-Autopilot. Stub of the skeleton; the group
// fills this file (and new agent_docs_*.go files).

// agentDocsNAS: whether the functions are there at all (owners and
// administrators switch them on for the NAS).
type agentDocsNAS struct {
	Enabled   bool `json:"enabled,omitempty"`
	Deadlines bool `json:"deadlines,omitempty"`
	Paper     bool `json:"paper,omitempty"`
}

func (d agentDocsNAS) view() agentDocsNAS { return d }

// agentDocsMine: an account's folders and deadline mode.
type agentDocsMine struct{}

func (d agentDocsMine) view() agentDocsMine { return d }

// agentPaperMine: an account's inbox folder and mailbox.
type agentPaperMine struct{}

func (p agentPaperMine) view() agentPaperMine { return p }

func (s *Server) checkDocsNAS(d *agentDocsNAS, old agentDocsNAS) error                 { return nil }
func (s *Server) checkDocsMine(user string, d *agentDocsMine, old agentDocsMine) error { return nil }
func (s *Server) checkPaperMine(user string, p *agentPaperMine, old agentPaperMine) error {
	return nil
}
func (s *Server) registerAgentDocs(mux *routeMux) {}
func (s *Server) agentDocsTick(now agentNow)      {}
func (b *agentToolbox) docsTools() []*agentTool   { return nil }
