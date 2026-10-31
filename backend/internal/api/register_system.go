package api

// registerPower registers the routes of the system overview and restart or shutdown.
func (s *Server) registerPower(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/system", s.requireUser(s.handleSystem))
	mux.HandleFunc("POST /api/v1/system/power", s.requireAdmin(s.handlePower))
	mux.HandleFunc("GET /api/v1/system/power", s.requireAdmin(s.handlePowerStatus))
	mux.HandleFunc("DELETE /api/v1/system/power", s.requireAdmin(s.handlePowerCancel))
	mux.HandleFunc("POST /api/v1/system/power/now", s.requireAdmin(s.handlePowerNow))
}

// registerMaintenance registers the routes of the backup of the stick, logs, the activity log, diagnostics and the about page.
func (s *Server) registerMaintenance(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/config-backup", s.requireAdmin(s.handleConfigBackup))
	mux.HandleFunc("PUT /api/v1/config-backup", s.requireAdmin(s.handleConfigBackupSettings))
	mux.HandleFunc("POST /api/v1/config-backup/run", s.requireAdmin(s.handleConfigBackupRun))
	mux.HandleFunc("GET /api/v1/config-backup/download", s.requireAdmin(s.handleConfigBackupDownload))
	mux.HandleFunc("GET /api/v1/logs", s.requireAdmin(s.handleLogs))
	mux.HandleFunc("GET /api/v1/logs/export", s.requireAdmin(s.handleLogsExport))
	mux.HandleFunc("GET /api/v1/activity", s.requireAdmin(s.handleActivity))
	mux.HandleFunc("PUT /api/v1/activity", s.requireAdmin(s.handleActivitySettings))
	mux.HandleFunc("GET /api/v1/diagnostics/bundle", s.requireAdmin(s.handleDiagnosticBundle))
	mux.HandleFunc("GET /api/v1/about", s.requireUser(s.handleAbout))
}

// registerUPS registers the routes of the UPS.
func (s *Server) registerUPS(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/ups", s.requireAdmin(s.handleUPS))
	mux.HandleFunc("PUT /api/v1/ups", s.requireAdmin(s.handleUPSChange))
	mux.HandleFunc("POST /api/v1/ups/scan", s.requireAdmin(s.handleUPSScan))
}

// registerUpdate registers the routes of updates.
func (s *Server) registerUpdate(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/update", s.requireAdmin(s.handleUpdate))
	mux.HandleFunc("GET /api/v1/update/progress", s.requireAdmin(s.handleUpdateProgress))
	mux.HandleFunc("PUT /api/v1/update/settings", s.requireAdmin(s.handleUpdateSettings))
	mux.HandleFunc("POST /api/v1/update/{action}", s.requireAdmin(s.handleUpdateAction))
}

// registerSystemSettings registers the routes of time zone and host name.
func (s *Server) registerSystemSettings(mux *routeMux) {
	mux.HandleFunc("PUT /api/v1/system/timezone", s.requireAdmin(s.handleTimezone))
	mux.HandleFunc("PUT /api/v1/system/hostname", s.requireAdmin(s.handleHostname))
}
