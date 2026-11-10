package api

// registerNotifications registers the routes of notifications and their settings.
func (s *Server) registerNotifications(mux *routeMux) {
	notifyArea{notify: s.opts.Notify, monitor: s.opts.Monitor}.register(mux, s.requireAdmin)
}

// registerHealthReport registers the routes of the regular health report.
func (s *Server) registerHealthReport(mux *routeMux) {
	healthReportArea{report: s.opts.HealthReport}.register(mux, s.requireAdmin)
}

// registerPush registers the routes of push notifications to browsers.
func (s *Server) registerPush(mux *routeMux) {
	pushArea{push: s.opts.WebPush, notify: s.opts.Notify}.register(mux, s.requireUser)
}
