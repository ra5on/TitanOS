package api

// registerVMs registers the routes of virtual machines, their imports and taking them over.
func (s *Server) registerVMs(mux *routeMux) {
	// Virtual machines. Like containers: administrators only.
	s.vmsArea().register(mux, s.guards())
	mediaArea{media: s.opts.Media, admitOperation: s.admitOperation}.register(mux, s.requireAdmin, s.requireAppAdmin, s.requireHeavyAppAdmin)
}

// registerVMDetails registers the routes of snapshots, consoles and actions of single virtual machines.
func (s *Server) registerVMDetails(mux *routeMux) {
	s.vmsArea().registerDetails(mux, s.guards())
}
