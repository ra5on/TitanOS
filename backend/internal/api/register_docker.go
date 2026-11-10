package api

// registerDocker registers the routes of Docker, the services and the app proxy.
func (s *Server) registerDocker(mux *routeMux) {
	// Docker. Whoever controls containers controls the machine: administrators only.
	s.dockerArea().register(mux, s.guards())
	mux.HandleFunc("GET /api/v1/services/state", s.requireUser(s.handleServicesState))
	mux.HandleFunc("GET /api/v1/services", s.requireAdmin(s.handleServices))
	mux.HandleFunc("PUT /api/v1/services/{name}", s.requireAdmin(s.handleServiceSet))
	appProxyArea{proxy: s.opts.AppProxy, docker: s.opts.Docker}.register(mux, s.requireAdmin, s.requireAppAdmin)
	s.registerDockerOpens(mux)
}

// registerAppStore registers the routes of the app store.
func (s *Server) registerAppStore(mux *routeMux) {
	// The app store: templates to install as Docker apps.
	mux.HandleFunc("GET /api/v1/apps", s.requireAdmin(s.handleApps))
	mux.HandleFunc("POST /api/v1/apps/refresh", s.requireAdmin(s.handleAppsRefresh))
	mux.HandleFunc("POST /api/v1/apps/sources", s.requireAdmin(s.handleSourceAdd))
	mux.HandleFunc("DELETE /api/v1/apps/sources/{id}", s.requireAdmin(s.handleSourceRemove))
	mux.HandleFunc("GET /api/v1/apps/sources/{source}/apps/{app}", s.requireAdmin(s.handleAppTemplate))
	mux.HandleFunc("GET /api/v1/apps/blank", s.requireAdmin(s.handleBlankTemplate))
	// Programme (PLAN 61): who may use which Docker app, and its window.
	mux.HandleFunc("GET /api/v1/apps/access", s.requireAdmin(s.handleAppAccess))
	mux.HandleFunc("PUT /api/v1/apps/access", s.requireAdmin(s.handleAppAccessSet))
	mux.HandleFunc("GET /api/v1/users/{name}/apps", s.requireAdmin(s.handleUserApps))
	mux.HandleFunc("PUT /api/v1/users/{name}/apps", s.requireAdmin(s.handleUserAppsSet))
	mux.HandleFunc("GET /api/v1/apps/mine", s.requireUser(s.handleAppsMine))
	mux.HandleFunc("POST /api/v1/apps/{app}/window", s.requireUser(s.handleAppWindow))
	mux.HandleFunc("GET /api/v1/apps/{app}/open", s.handleAppOpen)
}
