package api

// registerNotifications registers the routes of notifications and their settings.
func (s *Server) registerNotifications(mux *routeMux) {
	a := notifyArea{notify: s.opts.Notify, monitor: s.opts.Monitor, prefs: s.prefs}
	a.register(mux, s.requireAdmin)
	if s.opts.WebPush != nil && s.opts.Notify != nil && s.opts.Users != nil && s.opts.Store != nil {
		s.opts.WebPush.SetBadge(func(name string) *int {
			user, ok := s.opts.Users.Get(name)
			if !ok || user.Disabled || !user.Role.CanAdminister() {
				return nil
			}
			return a.badge(name)
		})
	}
}

// registerHealthReport registers the routes of the regular health report.
func (s *Server) registerHealthReport(mux *routeMux) {
	healthReportArea{report: s.opts.HealthReport}.register(mux, s.requireAdmin)
}

// registerPush registers the routes of push notifications to browsers.
func (s *Server) registerPush(mux *routeMux) {
	pushArea{push: s.opts.WebPush, notify: s.opts.Notify}.register(mux, s.requireUser)
}
