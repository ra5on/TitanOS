package api

// registerVMs registers the routes of virtual machines, their imports and taking them over.
func (s *Server) registerVMs(mux *routeMux) {
	// Virtual machines. Like containers: administrators only.
	mux.HandleFunc("GET /api/v1/vms", s.requireAdmin(s.handleVMs))
	mux.HandleFunc("GET /api/v1/vms/imports", s.requireAdmin(s.handleMediaImports))
	mux.HandleFunc("POST /api/v1/vms/imports", s.requireHeavyAppAdmin(s.handleMediaImport))
	mux.HandleFunc("POST /api/v1/vms/virtio", s.requireHeavyAppAdmin(s.handleVirtIO))
	mux.HandleFunc("PUT /api/v1/vms/imports/{id}/data", s.requireHeavyAppAdmin(s.handleMediaChunk))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/verify", s.requireHeavyAppAdmin(s.handleMediaUploadVerify))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/{action}", s.requireAppAdmin(s.handleMediaControl))
	mux.HandleFunc("POST /api/v1/vms", s.requireHeavyAppAdmin(s.handleVMCreate))
	mux.HandleFunc("GET /api/v1/vms/media", s.requireAdmin(s.handleVMMedia))
	mux.HandleFunc("GET /api/v1/vms/host", s.requireAdmin(s.handleVMHost))
	// Taking a VM over from the copy of its settings on a volume.
	mux.HandleFunc("GET /api/v1/vms/adopt", s.requireAdmin(s.handleVMAdoptable))
	mux.HandleFunc("POST /api/v1/vms/adopt", s.requireAppAdmin(s.handleVMAdopt))
}

// registerVMDetails registers the routes of snapshots, consoles and actions of single virtual machines.
func (s *Server) registerVMDetails(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/vms/{name}/snapshots", s.requireAdmin(s.handleVMSnapshots))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots", s.requireHeavyAppAdmin(s.handleVMSnapshotCreate))
	mux.HandleFunc("PUT /api/v1/vms/{name}/snapshot-schedule", s.requireAppAdmin(s.handleVMSnapshotSchedule))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshot-recovery", s.requireHeavyAppAdmin(s.handleVMSnapshotRecover))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots/{snapshot}/restore", s.requireHeavyAppAdmin(s.handleVMSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/vms/{name}/snapshots/{snapshot}", s.requireHeavyAppAdmin(s.handleVMSnapshotDelete))
	mux.HandleFunc("POST /api/v1/vms/{name}/grow", s.requireHeavyAppAdmin(s.handleVMGrow))
	mux.HandleFunc("GET /api/v1/vms/{name}/serial", s.requireAppConsole(s.handleVMSerial))
	mux.HandleFunc("PUT /api/v1/vms/{name}", s.requireAppAdmin(s.handleVMUpdate))
	mux.HandleFunc("DELETE /api/v1/vms/{name}", s.requireAppAdmin(s.handleVMDelete))
	mux.HandleFunc("GET /api/v1/vms/{name}/xml", s.requireAdmin(s.handleVMXML))
	mux.HandleFunc("GET /api/v1/vms/{name}/addresses", s.requireAdmin(s.handleVMAddresses))
	mux.HandleFunc("PUT /api/v1/vms/{name}/medium", s.requireAppAdmin(s.handleVMMedium))
	mux.HandleFunc("GET /api/v1/vms/{name}/screen", s.requireAdmin(s.handleVMScreen))
	mux.HandleFunc("POST /api/v1/vms/{name}/{action}", s.requireAppAdmin(s.handleVMAction))
}
