package api

// registerStorage registers the routes of volumes, disks, SMART, disk tests and snapshots.
func (s *Server) registerStorage(mux *routeMux) {
	// Everyone may see the volumes; only administrators may change disks.
	s.operationsRoutes(mux)
	s.storageArea().register(mux, s.guards())
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireHeavyAdmin(s.handleDeleteVolume))
	s.storageCryptArea().register(mux, s.guards())
}
