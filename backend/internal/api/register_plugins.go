package api

// registerPlugins registers the routes of plugins, Tailscale and NetBird.
func (s *Server) registerPlugins(mux *routeMux) {
	// Plugins become part of the system: administrators only.
	tailscaleArea{tailscale: s.opts.Tailscale, plugins: s.opts.Plugins}.register(mux, s.requireAdmin)
	netbirdArea{netbird: s.opts.Netbird, plugins: s.opts.Plugins}.register(mux, s.requireAdmin)
	pluginsArea{plugins: s.opts.Plugins, storage: s.opts.Storage, update: s.opts.Update, tailscale: s.opts.Tailscale, netbird: s.opts.Netbird}.register(mux, s.requireAdmin, s.requireHeavyAdmin)
}
