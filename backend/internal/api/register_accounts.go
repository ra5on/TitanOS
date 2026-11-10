package api

// registerUsers registers the routes of the accounts of the NAS.
func (s *Server) registerUsers(mux *routeMux) {
	s.usersArea().register(mux, s.requireAdmin, s.requireUser)
}

// registerAccount registers the routes of one's own account: sessions, tokens, 2FA, passkeys, SSH keys and notifications.
func (s *Server) registerAccount(mux *routeMux) {
	au := s.authArea()
	// One's own account.
	mux.HandleFunc("GET /api/v1/account/sessions", s.requireUser(au.handleAccountSessions))
	s.tokensArea().register(mux, s.requireUser)
	mux.HandleFunc("GET /api/v1/openapi.json", s.requireUser(s.handleOpenAPI))
	mux.HandleFunc("DELETE /api/v1/account/sessions/{id}", s.requireUser(au.handleAccountSessionEnd))
	mux.HandleFunc("DELETE /api/v1/account/sessions", s.requireUser(au.handleAccountSessionsEndOthers))
	mux.HandleFunc("POST /api/v1/account/2fa/begin", s.requireUser(au.handleTwoFactorBegin))
	mux.HandleFunc("GET /api/v1/account/passkeys", s.requireUser(au.handlePasskeys))
	mux.HandleFunc("POST /api/v1/account/passkeys/begin", s.requireUser(au.handlePasskeyBegin))
	mux.HandleFunc("POST /api/v1/account/passkeys", s.requireUser(au.handlePasskeyFinish))
	mux.HandleFunc("DELETE /api/v1/account/passkeys/{id}", s.requireUser(au.handlePasskeyDelete))
	mux.HandleFunc("POST /api/v1/account/passkeys/confirm", s.requireUser(au.handlePasskeyConfirmBegin))
	mux.HandleFunc("POST /api/v1/account/2fa/enable", s.requireUser(au.handleTwoFactorEnable))
	mux.HandleFunc("POST /api/v1/account/2fa/disable", s.requireUser(au.handleTwoFactorDisable))
	mux.HandleFunc("POST /api/v1/account/2fa/recovery", s.requireUser(au.handleTwoFactorRecovery))
	accountKeysArea{users: s.opts.Users, ssh: s.opts.SSH, reconfirm: s.authArea().reconfirm}.register(mux, s.requireUser)
	accountNotifyArea{notify: s.opts.Notify}.register(mux, s.requireUser)
}
