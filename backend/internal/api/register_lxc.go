package api

// registerLXC registers the routes of system containers (LXC).
func (s *Server) registerLXC(mux *routeMux) {
	// System containers (LXC). Like VMs: administrators only.
	lxcArea{lxc: s.opts.LXC, storage: s.opts.Storage, network: s.opts.Network, monitor: s.opts.Monitor, serveConsole: s.serveConsole}.register(mux, s.requireAdmin, s.requireAppAdmin, s.requireHeavyAppAdmin, s.requireAppConsole)
}
