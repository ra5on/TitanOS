package api

// registerShares registers the routes of the shares.
func (s *Server) registerShares(mux *routeMux) {
	s.sharesArea().register(mux, s.guards())
}
