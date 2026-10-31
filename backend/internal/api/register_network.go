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
	mux.HandleFunc("GET /api/v1/firewall", s.requireAdmin(s.handleFirewall))
	mux.HandleFunc("PUT /api/v1/firewall", s.requireAdmin(s.clusterLocal(s.handleFirewallChange)))
	mux.HandleFunc("POST /api/v1/firewall/confirm", s.requireAdmin(s.clusterLocal(s.handleFirewallConfirm)))
	mux.HandleFunc("POST /api/v1/firewall/revert", s.requireAdmin(s.handleFirewallRevert))
	mux.HandleFunc("PUT /api/v1/firewall/autoblock", s.requireAdmin(s.handleFirewallAutoBlock))
	mux.HandleFunc("POST /api/v1/firewall/unblock", s.requireAdmin(s.handleFirewallUnblock))
	mux.HandleFunc("DELETE /api/v1/firewall/recent", s.requireAdmin(s.handleFirewallClearRecent))
}

// registerWireGuard registers the routes of the WireGuard server.
func (s *Server) registerWireGuard(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/network/wireguard", s.requireAdmin(s.handleWireGuard))
	mux.HandleFunc("PUT /api/v1/network/wireguard", s.requireAdmin(s.handleWireGuardSet))
	mux.HandleFunc("POST /api/v1/network/wireguard/peers", s.requireAdmin(s.handleWireGuardAdd))
	mux.HandleFunc("POST /api/v1/network/wireguard/peers/remove", s.requireAdmin(s.handleWireGuardRemove))
}
