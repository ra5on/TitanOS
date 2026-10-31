package api

// registerPlugins registers the routes of plugins, Tailscale and NetBird.
func (s *Server) registerPlugins(mux *routeMux) {
	// Plugins become part of the system: administrators only.
	mux.HandleFunc("GET /api/v1/plugins/tailscale", s.requireAdmin(s.handleTailscale))
	mux.HandleFunc("GET /api/v1/plugins/tailscale/devices", s.requireAdmin(s.handleTailscaleDevices))
	mux.HandleFunc("PUT /api/v1/plugins/tailscale", s.requireAdmin(s.handleTailscaleSettings))
	mux.HandleFunc("POST /api/v1/plugins/tailscale/control/{action}", s.requireAdmin(s.handleTailscaleAction))
	mux.HandleFunc("GET /api/v1/plugins/netbird", s.requireAdmin(s.handleNetbird))
	mux.HandleFunc("GET /api/v1/plugins/netbird/peers", s.requireAdmin(s.handleNetbirdPeers))
	mux.HandleFunc("PUT /api/v1/plugins/netbird", s.requireAdmin(s.handleNetbirdSettings))
	mux.HandleFunc("PUT /api/v1/plugins/netbird/setup-key", s.requireAdmin(s.handleNetbirdSetupKey))
	mux.HandleFunc("POST /api/v1/plugins/netbird/control/{action}", s.requireAdmin(s.handleNetbirdAction))
	mux.HandleFunc("GET /api/v1/plugins", s.requireAdmin(s.handlePlugins))
	mux.HandleFunc("GET /api/v1/plugins/progress", s.requireAdmin(s.handlePluginProgress))
	mux.HandleFunc("DELETE /api/v1/plugins/progress/{id}", s.requireAdmin(s.handlePluginProgressForget))
	mux.HandleFunc("GET /api/v1/plugins/releases", s.requireAdmin(s.handlePluginOldReleases))
	mux.HandleFunc("POST /api/v1/plugins/cleanup", s.requireAdmin(s.handlePluginCleanup))
	mux.HandleFunc("POST /api/v1/plugins/nvidia/updates", s.requireAdmin(s.handleDriverUpdate))
	mux.HandleFunc("POST /api/v1/plugins/catalog", s.requireAdmin(s.handlePluginCatalog))
	mux.HandleFunc("POST /api/v1/plugins/recommendations/{name}/dismiss", s.requireAdmin(s.handlePluginDismiss))
	mux.HandleFunc("DELETE /api/v1/plugins/recommendations/dismissed", s.requireAdmin(s.handlePluginRestoreDismissed))
	mux.HandleFunc("GET /api/v1/plugins/sources", s.requireAdmin(s.handlePluginSources))
	mux.HandleFunc("POST /api/v1/plugins/sources", s.requireAdmin(s.handlePluginSourceAdd))
	mux.HandleFunc("DELETE /api/v1/plugins/sources/{id}", s.requireAdmin(s.handlePluginSourceRemove))
	mux.HandleFunc("POST /api/v1/plugins/external/inspect", s.requireAdmin(s.handlePluginInspect))
	mux.HandleFunc("POST /api/v1/plugins/external", s.requireHeavyAdmin(s.handlePluginExternal))
	mux.HandleFunc("POST /api/v1/plugins", s.requireHeavyAdmin(s.handlePluginInstall))
	mux.HandleFunc("POST /api/v1/plugins/{name}/update", s.requireHeavyAdmin(s.handlePluginUpdate))
	mux.HandleFunc("POST /api/v1/plugins/{name}/{action}", s.requireAdmin(s.handlePluginAction))
	mux.HandleFunc("DELETE /api/v1/plugins/{name}", s.requireAdmin(s.handlePluginRemove))
}
