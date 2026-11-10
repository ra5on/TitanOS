package api

// registerNetwork registers the routes of the network and the firewall.
func (s *Server) registerNetwork(mux *routeMux) {
	// How the NAS is connected. Changes are on trial until confirmed.
	mux.HandleFunc("GET /api/v1/network", s.requireAdmin(s.handleNetwork))
	mux.HandleFunc("POST /api/v1/network/diagnostics", s.requireAdmin(s.handleNetworkDiagnostics))
	mux.HandleFunc("PUT /api/v1/network", s.requireAdmin(s.clusterLocal(s.handleNetworkChange)))
	mux.HandleFunc("PUT /api/v1/network/options", s.requireAdmin(s.clusterLocal(s.handleNetworkOptions)))
	mux.HandleFunc("POST /api/v1/network/bridge", s.requireAdmin(s.clusterLocal(s.handleNetworkBridge)))
	mux.HandleFunc("POST /api/v1/network/confirm", s.requireAdmin(s.clusterLocal(s.handleNetworkConfirm)))
	mux.HandleFunc("POST /api/v1/network/revert", s.requireAdmin(s.handleNetworkRevert))
	// The packet filter; changes are on trial like network changes.
	firewallArea{firewall: s.opts.Firewall}.register(mux, s.requireAdmin, s.clusterLocal)
}

// registerWireGuard registers the routes of the WireGuard server.
func (s *Server) registerWireGuard(mux *routeMux) {
	wireGuardArea{wireGuard: s.opts.WireGuard}.register(mux, s.requireAdmin)
}
