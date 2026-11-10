package api

// registerNotifications registers the routes of notifications and their settings.
func (s *Server) registerNotifications(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/notifications", s.requireAdmin(s.handleNotifications))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", s.requireAdmin(s.handleDismissNotifications))
	mux.HandleFunc("GET /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettings))
	mux.HandleFunc("PUT /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettingsChange))
	mux.HandleFunc("POST /api/v1/notifications/test", s.requireAdmin(s.handleNotifyTest))
	mux.HandleFunc("GET /api/v1/notifications/temperatures", s.requireAdmin(s.handleTemperatureAlarms))
	mux.HandleFunc("PUT /api/v1/notifications/temperatures", s.requireAdmin(s.handleTemperatureAlarmsChange))
}

// registerHealthReport registers the routes of the regular health report.
func (s *Server) registerHealthReport(mux *routeMux) {
	healthReportArea{report: s.opts.HealthReport}.register(mux, s.requireAdmin)
}

// registerPush registers the routes of push notifications to browsers.
func (s *Server) registerPush(mux *routeMux) {
	pushArea{push: s.opts.WebPush, notify: s.opts.Notify}.register(mux, s.requireUser)
}
