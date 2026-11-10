package api

// registerTerminal registers the routes of shells, login settings and confirmations.
func (s *Server) registerTerminal(mux *routeMux) {
	au := s.authArea()
	// Shells on the NAS and in containers.
	terminalArea{terminal: s.opts.Terminal, serveConsole: s.serveConsole}.register(mux, s.requireAdmin)
	loginSettingsArea{sessions: s.sessionStore}.register(mux, s.requireAdmin)
	mux.HandleFunc("POST /api/v1/session/confirm", s.requireUser(au.handleConfirm))
	mux.HandleFunc("GET /api/v1/session/confirmed", s.requireUser(au.handleConfirmed))
}
