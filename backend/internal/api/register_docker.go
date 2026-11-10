package api

// registerDocker registers the routes of Docker, the services and the app proxy.
func (s *Server) registerDocker(mux *routeMux) {
	// Docker. Whoever controls containers controls the machine: administrators only.
	s.dockerArea().register(mux, s.guards())
	s.servicesArea().register(mux, s.guards())
	appProxyArea{proxy: s.opts.AppProxy, docker: s.opts.Docker}.register(mux, s.requireAdmin, s.requireAppAdmin)
	s.registerDockerOpens(mux)
}

// registerAppStore registers the routes of the app store.
func (s *Server) registerAppStore(mux *routeMux) {
	// The app store: templates to install as Docker apps.
	s.appStoreArea().register(mux, s.guards())
	s.appAccessArea().register(mux, s.guards())
}
