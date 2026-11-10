package api

// registerTerminal registers the routes of shells, login settings and confirmations.
func (s *Server) registerTerminal(mux *routeMux) {
	// Shells on the NAS and in containers.
	terminalArea{terminal: s.opts.Terminal, serveConsole: s.serveConsole}.register(mux, s.requireAdmin)
	loginSettingsArea{sessions: s.sessions}.register(mux, s.requireAdmin)
	mux.HandleFunc("POST /api/v1/session/confirm", s.requireUser(s.handleConfirm))
	mux.HandleFunc("GET /api/v1/session/confirmed", s.requireUser(s.handleConfirmed))
}
