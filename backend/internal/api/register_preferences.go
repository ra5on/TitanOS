package api

// registerPreferences registers the routes of the preferences and backgrounds of each account.
func (s *Server) registerPreferences(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/preferences", s.requireUser(s.handleGetPreferences))
	mux.HandleFunc("PUT /api/v1/preferences/{kind}", s.requireUser(s.handlePutPreferences))
	mux.HandleFunc("GET /api/v1/background/{id}", s.requireUser(s.handleGetDashboardImage))
	mux.HandleFunc("GET /api/v1/background", s.requireUser(s.handleGetBackground))
	mux.HandleFunc("PUT /api/v1/background", s.requireUser(s.handlePutBackground))
	mux.HandleFunc("DELETE /api/v1/background", s.requireUser(s.handleDeleteBackground))
}
