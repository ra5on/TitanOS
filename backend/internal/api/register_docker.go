package api

// registerDocker registers the routes of Docker, the services and the app proxy.
func (s *Server) registerDocker(mux *routeMux) {
	// Docker. Whoever controls containers controls the machine: administrators only.
	mux.HandleFunc("GET /api/v1/docker", s.requireAdmin(s.handleDocker))
	mux.HandleFunc("GET /api/v1/docker/networks", s.requireAdmin(s.handleLANNetworks))
	mux.HandleFunc("POST /api/v1/docker/networks", s.requireHeavyAppAdmin(s.handleLANCreate))
	mux.HandleFunc("DELETE /api/v1/docker/networks/{name}", s.requireAppAdmin(s.handleLANRemove))
	// Images, volumes and the build cache (PLAN 67-1, docker_resources.go).
	mux.HandleFunc("GET /api/v1/docker/images", s.requireAdmin(s.handleDockerImages))
	mux.HandleFunc("DELETE /api/v1/docker/images/{id}", s.requireAppAdmin(s.handleDockerImageRemove))
	mux.HandleFunc("POST /api/v1/docker/images/prune", s.requireAppAdmin(s.handleDockerImagesPrune))
	mux.HandleFunc("GET /api/v1/docker/volumes", s.requireAdmin(s.handleDockerVolumes))
	mux.HandleFunc("DELETE /api/v1/docker/volumes/{name}", s.requireAppAdmin(s.handleDockerVolumeRemove))
	mux.HandleFunc("POST /api/v1/docker/volumes/prune", s.requireAppAdmin(s.handleDockerVolumesPrune))
	mux.HandleFunc("POST /api/v1/docker/builder/prune", s.requireAppAdmin(s.handleDockerBuilderPrune))
	mux.HandleFunc("GET /api/v1/docker/df", s.requireAdmin(s.handleDockerDiskUsage))
	// All engine networks; plain bridge networks are created and deleted
	// here, the LAN networks above keep their own routes.
	mux.HandleFunc("GET /api/v1/docker/networks/all", s.requireAdmin(s.handleDockerAllNetworks))
	mux.HandleFunc("POST /api/v1/docker/networks/bridge", s.requireAppAdmin(s.handleDockerBridgeCreate))
	mux.HandleFunc("DELETE /api/v1/docker/networks/bridge/{name}", s.requireAppAdmin(s.handleDockerBridgeRemove))
	mux.HandleFunc("POST /api/v1/docker/networks/prune", s.requireAppAdmin(s.handleDockerNetworksPrune))
	mux.HandleFunc("PUT /api/v1/docker/settings", s.requireAppAdmin(s.handleDockerSettings))
	mux.HandleFunc("GET /api/v1/services/state", s.requireUser(s.handleServicesState))
	mux.HandleFunc("GET /api/v1/services", s.requireAdmin(s.handleServices))
	mux.HandleFunc("PUT /api/v1/services/{name}", s.requireAdmin(s.handleServiceSet))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/{action}", s.requireAppAdmin(s.handleContainerAction))
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", s.requireAppAdmin(s.handleContainerRemove))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", s.requireAdmin(s.handleContainerLogs))
	mux.HandleFunc("POST /api/v1/docker/apps", s.requireHeavyAppAdmin(s.handleAppCreate))
	mux.HandleFunc("GET /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/network", s.requireHeavyAppAdmin(s.handleLANAssign))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}", s.requireAppAdmin(s.handleAppRemove))
	mux.HandleFunc("PUT /api/v1/docker/apps/{name}", s.requireAppAdmin(s.handleAppSetCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/update", s.requireHeavyAppAdmin(s.handleAppUpdate))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}/job", s.requireAppAdmin(s.handleAppJobDismiss))
	mux.HandleFunc("POST /api/v1/docker/updates/check", s.requireAppAdmin(s.handleUpdatesCheck))
	mux.HandleFunc("GET /api/v1/docker/auto-update", s.requireAdmin(s.handleAutoUpdates))
	mux.HandleFunc("PUT /api/v1/docker/auto-update", s.requireAppAdmin(s.handleAutoUpdatesSet))
	mux.HandleFunc("POST /api/v1/docker/updates/apply", s.requireHeavyAppAdmin(s.handleUpdatesApply))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/console", s.requireAppConsole(s.handleContainerConsole))
	mux.HandleFunc("GET /api/v1/docker/proxy", s.requireAdmin(s.handleAppProxy))
	mux.HandleFunc("PUT /api/v1/docker/proxy", s.requireAppAdmin(s.handleAppProxyDomain))
	mux.HandleFunc("PUT /api/v1/docker/proxy/apps/{app}", s.requireAppAdmin(s.handleAppProxyRoute))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/apps/{app}", s.requireAppAdmin(s.handleAppProxyRemove))
	mux.HandleFunc("POST /api/v1/docker/proxy/apps/{app}/tailnet", s.requireAppAdmin(s.handleAppTailnet))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/apps/{app}/tailnet", s.requireAppAdmin(s.handleAppTailnetRemove))
	mux.HandleFunc("POST /api/v1/docker/proxy/apps/{app}/tailnet/check", s.requireAppAdmin(s.handleAppTailnetCheck))
	mux.HandleFunc("PUT /api/v1/docker/proxy/targets/{id}", s.requireAppAdmin(s.handleAppProxyTarget))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/targets/{id}", s.requireAppAdmin(s.handleAppProxyTargetRemove))
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
	mux.HandleFunc("GET /api/v1/apps/mine", s.requireUser(s.handleAppsMine))
	mux.HandleFunc("POST /api/v1/apps/{app}/window", s.requireUser(s.handleAppWindow))
	mux.HandleFunc("GET /api/v1/apps/{app}/open", s.handleAppOpen)
}
