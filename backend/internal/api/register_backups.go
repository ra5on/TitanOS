package api

// registerBackups registers the routes of backups, the backup target and replication.
func (s *Server) registerBackups(mux *routeMux) {
	// Backups of shares. They hold everyone's files: administrators only.
	mux.HandleFunc("POST /api/v1/backups/smb/discover", s.requireAdmin(s.handleSMBDiscover))
	mux.HandleFunc("PUT /api/v1/backups/smb", s.requireAdmin(s.backupJobOr("smb", s.handleSMBSave)))
	mux.HandleFunc("DELETE /api/v1/backups/smb/{target}", s.requireAdmin(s.handleSMBDelete))
	mux.HandleFunc("GET /api/v1/backups/progress", s.requireAdmin(s.handleUSBBackupProgress))
	mux.HandleFunc("GET /api/v1/backups", s.requireAdmin(s.handleBackups))
	mux.HandleFunc("POST /api/v1/backups", s.requireAdmin(s.handleBackupCreate))
	mux.HandleFunc("GET /api/v1/backups/target", s.requireAdmin(s.handleBackupTarget))
	mux.HandleFunc("PUT /api/v1/backups/target", s.requireAdmin(s.backupJobOr("target", s.handleBackupTargetSet)))
	mux.HandleFunc("POST /api/v1/backups/target/sources", s.requireAdmin(s.handleBackupTargetPair))
	mux.HandleFunc("DELETE /api/v1/backups/target/sources/{name}", s.requireAdmin(s.handleBackupTargetRemove))
	mux.HandleFunc("GET /api/v1/replication", s.requireAdmin(s.handleReplication))
	mux.HandleFunc("POST /api/v1/replication", s.requireAdmin(s.handleReplicationSave))
	mux.HandleFunc("PUT /api/v1/replication/{id}", s.requireAdmin(s.handleReplicationSave))
	mux.HandleFunc("DELETE /api/v1/replication/{id}", s.requireAdmin(s.handleReplicationRemove))
	mux.HandleFunc("POST /api/v1/replication/{id}/run", s.requireHeavyAdmin(s.handleReplicationRun))
	mux.HandleFunc("POST /api/v1/replication/received/restore", s.requireHeavyAdmin(s.handleReplicaRestore))
	mux.HandleFunc("GET /api/v1/backups/folders", s.requireAdmin(s.handleBackupFolders))
	mux.HandleFunc("POST /api/v1/backups/ssh-key", s.requireAdmin(s.handleBackupKey))
	mux.HandleFunc("PUT /api/v1/backups/{id}", s.requireAdmin(s.handleBackupUpdate))
	mux.HandleFunc("DELETE /api/v1/backups/{id}", s.requireAdmin(s.handleBackupDelete))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots", s.requireAdmin(s.handleBackupSnapshots))
	mux.HandleFunc("GET /api/v1/backups/{id}/password", s.requireAdmin(s.handleBackupPassword))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", s.requireHeavyAdmin(s.handleBackupRestore))
	mux.HandleFunc("POST /api/v1/backups/{id}/check", s.requireHeavyAdmin(s.handleBackupCheck))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots/{snapshot}/files", s.requireAdmin(s.handleBackupFiles))
	mux.HandleFunc("POST /api/v1/backups/{id}/{action}", s.requireAdmin(s.handleBackupAction))
}
