package api

// registerPIM registers T-Kalender and Kontakte (TECHNIK 3, 4.1): for now
// the device passwords. Every route for the web session of this node only,
// none in publicRoutes or the tool catalog. /caldav and /carddav follow
// here (davsync, step 6), before the catch-all of the web UI.
func (s *Server) registerPIM(mux *routeMux) {
	s.pimArea().register(mux, s.guards())
}
