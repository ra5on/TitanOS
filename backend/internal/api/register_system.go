package api

// registerPower registers the routes of the system overview and restart or shutdown.
func (s *Server) registerPower(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/system", s.requireUser(s.handleSystem))
	mux.HandleFunc("POST /api/v1/system/power", s.requireAdmin(s.handlePower))
	mux.HandleFunc("GET /api/v1/system/power", s.requireAdmin(s.handlePowerStatus))
	mux.HandleFunc("DELETE /api/v1/system/power", s.requireAdmin(s.handlePowerCancel))
	mux.HandleFunc("POST /api/v1/system/power/now", s.requireAdmin(s.handlePowerNow))
	// Task manager (PLAN 61): owners and administrators, never tokens/MCP.
	processesArea{procs: s.opts.Processes, docker: s.opts.Docker, names: &s.containerNames}.register(mux, s.requireAdmin, s.browserOnly)
}

// registerMaintenance registers the routes of the backup of the stick, logs, the activity log, diagnostics and the about page.
func (s *Server) registerMaintenance(mux *routeMux) {
	configBackupArea{backup: s.opts.ConfigBackup, storage: s.opts.Storage, confirmed: s.confirmed}.register(mux, s.requireAdmin)
	diagnoseArea{diagnose: s.opts.Diagnose, logKeep: s.opts.LogKeep, users: s.opts.Users}.register(mux, s.requireAdmin)
	mux.HandleFunc("GET /api/v1/activity", s.requireAdmin(s.handleActivity))
	mux.HandleFunc("PUT /api/v1/activity", s.requireAdmin(s.handleActivitySettings))
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
	mux.HandleFunc("PUT /api/v1/system/hostname", s.requireAdmin(s.handleHostname))
}
