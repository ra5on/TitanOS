package api

// registerShares registers the routes of the shares.
func (s *Server) registerShares(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/shares", s.requireUser(s.handleListShares))
	mux.HandleFunc("POST /api/v1/shares", s.requireAdmin(s.handleCreateShare))
	mux.HandleFunc("PUT /api/v1/shares/{name}", s.requireAdmin(s.handleUpdateShare))
	mux.HandleFunc("DELETE /api/v1/shares/{name}", s.requireAdmin(s.handleDeleteShare))
	mux.HandleFunc("POST /api/v1/shares/{name}/unlock", s.requireAdmin(s.handleShareUnlock))
	mux.HandleFunc("GET /api/v1/shares/{name}/delete-data", s.requireAdmin(s.handleShareDataPreview))
	mux.HandleFunc("POST /api/v1/shares/{name}/delete-data", s.requireHeavyAdmin(s.handleShareDataDelete))
}
