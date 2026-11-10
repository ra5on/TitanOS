package api

// registerBackups registers the routes of backups, the backup target and replication.
func (s *Server) registerBackups(mux *routeMux) {
	// Backups of shares. They hold everyone's files: administrators only.
	s.backupsArea().register(mux, s.guards())
	s.backupTargetArea().register(mux, s.guards(), s.backupsArea().backupJobOr)
	s.replicationArea().register(mux, s.guards())
}
