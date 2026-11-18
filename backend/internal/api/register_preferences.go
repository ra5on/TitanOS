package api

// registerPreferences registers the routes of the preferences and backgrounds of each account.
func (s *Server) registerPreferences(mux *routeMux) {
	prefsArea{store: s.opts.Store, prefs: s.prefs, calendarChanged: s.reminders.replan}.register(mux, s.requireUser)
}
