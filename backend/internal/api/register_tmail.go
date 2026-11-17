package api

// registerMail registers T-Mail (docs/TMAIL.md): every route for the web
// session of this node only, none in publicRoutes or the tool catalog.
func (s *Server) registerMail(mux *routeMux) {
	s.tmailArea().register(mux, s.guards())
}
