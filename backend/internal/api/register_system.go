package api

// registerPower registers the routes of the system overview and restart or shutdown.
func (s *Server) registerPower(mux *routeMux) {
	systemArea{monitor: s.opts.Monitor, version: s.opts.Version, network: s.opts.Network, accelerators: s.opts.Accelerators, notify: s.opts.Notify}.register(mux, s.requireUser)
	s.powerArea().register(mux, s.requireAdmin)
	// Task manager (PLAN 61): owners and administrators, never tokens/MCP.
	processesArea{procs: s.opts.Processes, docker: s.opts.Docker, names: &s.containerNames}.register(mux, s.requireAdmin, s.browserOnly)
}

// registerMaintenance registers the routes of the backup of the stick, logs, the activity log, diagnostics and the about page.
func (s *Server) registerMaintenance(mux *routeMux) {
	configBackupArea{backup: s.opts.ConfigBackup, storage: s.opts.Storage, confirmed: s.authArea().confirmed}.register(mux, s.requireAdmin)
	diagnoseArea{diagnose: s.opts.Diagnose, logKeep: s.opts.LogKeep, users: s.opts.Users}.register(mux, s.requireAdmin)
	activityArea{activity: s.opts.Activity}.register(mux, s.requireAdmin)
	aboutArea{version: s.opts.Version, shareDir: s.opts.ShareDir}.register(mux, s.requireUser)
}

// registerUPS registers the routes of the UPS.
func (s *Server) registerUPS(mux *routeMux) {
	upsArea{ups: s.opts.UPS}.register(mux, s.requireAdmin)
}

// registerUpdate registers the routes of updates.
func (s *Server) registerUpdate(mux *routeMux) {
	updateArea{update: s.opts.Update}.register(mux, s.requireAdmin)
}

// registerSystemSettings registers the routes of time zone and host name.
func (s *Server) registerSystemSettings(mux *routeMux) {
	timezoneArea{settings: s.opts.Settings}.register(mux, s.requireAdmin)
	mux.HandleFunc("PUT /api/v1/system/hostname", s.requireAdmin(s.networkArea().handleHostname))
}
