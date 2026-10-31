package api

// registerTerminal registers the routes of shells, login settings and confirmations.
func (s *Server) registerTerminal(mux *routeMux) {
	// Shells on the NAS and in containers.
	mux.HandleFunc("GET /api/v1/terminal", s.requireAdmin(s.handleTerminal))
	mux.HandleFunc("PUT /api/v1/terminal", s.requireAdmin(s.handleTerminalSettings))
	mux.HandleFunc("GET /api/v1/system/login", s.requireAdmin(s.handleLoginSettings))
	mux.HandleFunc("PUT /api/v1/system/login", s.requireAdmin(s.handleSetLoginSettings))
	mux.HandleFunc("GET /api/v1/terminal/shell", s.requireAdmin(s.handleShell))
	mux.HandleFunc("POST /api/v1/session/confirm", s.requireUser(s.handleConfirm))
	mux.HandleFunc("GET /api/v1/session/confirmed", s.requireUser(s.handleConfirmed))
}
