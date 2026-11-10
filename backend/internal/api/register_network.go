package api

// registerNetwork registers the routes of the network and the firewall.
func (s *Server) registerNetwork(mux *routeMux) {
	// How the NAS is connected. Changes are on trial until confirmed.
	s.networkArea().register(mux, s.guards())
	// The packet filter; changes are on trial like network changes.
	firewallArea{firewall: s.opts.Firewall}.register(mux, s.requireAdmin, s.clusterLocal)
}

// registerWireGuard registers the routes of the WireGuard server.
func (s *Server) registerWireGuard(mux *routeMux) {
	wireGuardArea{wireGuard: s.opts.WireGuard}.register(mux, s.requireAdmin)
}
