package api

// registerStorage registers the routes of volumes, disks, SMART, disk tests and snapshots.
func (s *Server) registerStorage(mux *routeMux) {
	// Everyone may see the volumes; only administrators may change disks.
	s.operationsRoutes(mux)
	s.storageArea().register(mux, s.guards())
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireHeavyAdmin(s.handleDeleteVolume))
	// Encrypted volumes: browser sessions only, never over MCP
	// catalog or the public API (storage_crypt.go).
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/unlock", s.requireAdmin(s.handleVolumeUnlock))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/lock", s.requireHeavyAdmin(s.handleVolumeLock))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/passwords", s.requireAdmin(s.handleVolumePasswords))
	mux.HandleFunc("GET /api/v1/storage/volumes/{name}/luks-header", s.requireAdmin(s.handleVolumeHeaderBackup))
}
