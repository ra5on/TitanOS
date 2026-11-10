package api

// registerFiles registers the routes of the file manager.
func (s *Server) registerFiles(mux *routeMux) {
	// The file manager. Every request names a root the account has access to.
	s.filesArea().register(mux, s.guards())
	mux.HandleFunc("POST /api/v1/files/write-document", s.requireUser(s.handleWriteDocument))
	mux.HandleFunc("POST /api/v1/files/edit-document", s.requireUser(s.handleEditDocument))
	mux.HandleFunc("POST /api/v1/files/move-many", s.requireUser(s.handleMoveMany))
}

// registerUploads registers the routes of uploads into the file manager.
func (s *Server) registerUploads(mux *routeMux) {
	s.filesArea().registerUploads(mux, s.guards())
}
