package api

// registerNotifications registers the routes of notifications and their settings.
func (s *Server) registerNotifications(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/notifications", s.requireAdmin(s.handleNotifications))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", s.requireAdmin(s.handleDismissNotifications))
	mux.HandleFunc("GET /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettings))
	mux.HandleFunc("PUT /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettingsChange))
	mux.HandleFunc("POST /api/v1/notifications/test", s.requireAdmin(s.handleNotifyTest))
}

// registerHealthReport registers the routes of the regular health report.
func (s *Server) registerHealthReport(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/health-report", s.requireAdmin(s.handleHealthReport))
	mux.HandleFunc("PUT /api/v1/health-report", s.requireAdmin(s.handleHealthReportSet))
	mux.HandleFunc("POST /api/v1/health-report/send", s.requireAdmin(s.handleHealthReportSend))
}

// registerPush registers the routes of push notifications to browsers.
func (s *Server) registerPush(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/notifications/push", s.requireAdmin(s.handlePushDevices))
	mux.HandleFunc("POST /api/v1/notifications/push", s.requireAdmin(s.handlePushSubscribe))
	mux.HandleFunc("DELETE /api/v1/notifications/push/{id}", s.requireAdmin(s.handlePushRemove))
	mux.HandleFunc("POST /api/v1/notifications/push/{id}/test", s.requireAdmin(s.handlePushTest))
}
