package api

// registerLXC registers the routes of system containers (LXC).
func (s *Server) registerLXC(mux *routeMux) {
	// System containers (LXC). Like VMs: administrators only.
	mux.HandleFunc("GET /api/v1/lxc", s.requireAdmin(s.handleLXC))
	mux.HandleFunc("GET /api/v1/lxc/images", s.requireAdmin(s.handleLXCImages))
	mux.HandleFunc("POST /api/v1/lxc", s.requireHeavyAppAdmin(s.handleLXCCreate))
	mux.HandleFunc("PUT /api/v1/lxc/{name}", s.requireAppAdmin(s.handleLXCUpdate))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}", s.requireAppAdmin(s.handleLXCDelete))
	mux.HandleFunc("GET /api/v1/lxc/{name}/config", s.requireAdmin(s.handleLXCConfig))
	mux.HandleFunc("GET /api/v1/lxc/{name}/console", s.requireAppConsole(s.handleLXCConsole))
	mux.HandleFunc("GET /api/v1/lxc/{name}/snapshots", s.requireAdmin(s.handleLXCSnapshots))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots", s.requireHeavyAppAdmin(s.handleLXCSnapshotCreate))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots/{snapshot}/restore", s.requireHeavyAppAdmin(s.handleLXCSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}/snapshots/{snapshot}", s.requireHeavyAppAdmin(s.handleLXCSnapshotDelete))
	mux.HandleFunc("POST /api/v1/lxc/{name}/{action}", s.requireAppAdmin(s.handleLXCAction))
}
