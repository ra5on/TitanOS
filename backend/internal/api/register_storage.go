package api

// registerStorage registers the routes of volumes, disks, SMART, disk tests and snapshots.
func (s *Server) registerStorage(mux *routeMux) {
	// Everyone may see the volumes; only administrators may change disks.
	s.operationsRoutes(mux)
	mux.HandleFunc("GET /api/v1/storage", s.requireUser(s.handleStorage))
	mux.HandleFunc("GET /api/v1/storage/disk-sleep", s.requireAdmin(s.handleDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/disk-sleep", s.requireAdmin(s.handleSetDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/quotas", s.requireAdmin(s.handleSetShareQuotas))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/sync-speed", s.requireAdmin(s.handleSetSyncSpeed))
	mux.HandleFunc("POST /api/v1/storage/rescan", s.requireAdmin(s.handleStorageRescan))
	mux.HandleFunc("GET /api/v1/storage/smart", s.requireAdmin(s.handleSmart))
	mux.HandleFunc("POST /api/v1/storage/smart/refresh", s.requireAdmin(s.handleSmartRefresh))
	mux.HandleFunc("GET /api/v1/storage/smart/{disk}", s.requireAdmin(s.handleSmartDisk))
	mux.HandleFunc("GET /api/v1/storage/smart/{disk}/history", s.requireAdmin(s.handleSmartHistory))
	mux.HandleFunc("POST /api/v1/storage/volumes", s.requireHeavyAdmin(s.handleCreateVolume))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireHeavyAdmin(s.handleDeleteVolume))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/disks", s.requireHeavyAdmin(s.handleAddVolumeDisk))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/disks/{disk}", s.requireHeavyAdmin(s.handleRemoveVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/replace", s.requireHeavyAdmin(s.handleReplaceVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/disks/{disk}/identify", s.requireAdmin(s.handleIdentifyDisk))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/profile", s.requireHeavyAdmin(s.handleBtrfsProfile))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/cache", s.requireHeavyAdmin(s.handleVolumeCache))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/cache", s.requireHeavyAdmin(s.handleVolumeCacheRemove))
	// Encrypted volumes: browser sessions only, never over MCP
	// catalog or the public API (storage_crypt.go).
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/unlock", s.requireAdmin(s.handleVolumeUnlock))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/lock", s.requireHeavyAdmin(s.handleVolumeLock))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/passwords", s.requireAdmin(s.handleVolumePasswords))
	mux.HandleFunc("GET /api/v1/storage/volumes/{name}/luks-header", s.requireAdmin(s.handleVolumeHeaderBackup))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/check", s.requireHeavyAdmin(s.handleCheckVolume))
	mux.HandleFunc("GET /api/v1/storage/volumes/{name}/snapshots", s.requireAdmin(s.handleSnapshots))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/snapshots", s.requireHeavyAdmin(s.handleSnapshotAction))
	mux.HandleFunc("GET /api/v1/storage/tests", s.requireAdmin(s.handleDiskTests))
	mux.HandleFunc("POST /api/v1/storage/tests", s.requireHeavyAdmin(s.handleDiskTestStart))
	mux.HandleFunc("DELETE /api/v1/storage/tests/{disk}", s.requireAdmin(s.handleDiskTestStop))
	mux.HandleFunc("POST /api/v1/storage/tests/{disk}/resume", s.requireHeavyAdmin(s.handleDiskTestResume))
	mux.HandleFunc("DELETE /api/v1/storage/test-notices", s.requireAdmin(s.handleDiskTestNotices))
}
