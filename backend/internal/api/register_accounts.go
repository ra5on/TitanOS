package api

// registerUsers registers the routes of the accounts of the NAS.
func (s *Server) registerUsers(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PUT /api/v1/users/{name}", s.requireAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{name}", s.requireAdmin(s.handleDeleteUser))
	mux.HandleFunc("POST /api/v1/users/{name}/lock", s.requireAdmin(s.handleLockUser(true)))
	mux.HandleFunc("POST /api/v1/users/{name}/unlock", s.requireAdmin(s.handleLockUser(false)))
	// Everyone may change their own password.
	mux.HandleFunc("POST /api/v1/users/{name}/password", s.requireUser(s.handleSetPassword))
}

// registerAccount registers the routes of one's own account: sessions, tokens, 2FA, passkeys, SSH keys and notifications.
func (s *Server) registerAccount(mux *routeMux) {
	// One's own account.
	mux.HandleFunc("GET /api/v1/account/sessions", s.requireUser(s.handleAccountSessions))
	s.tokensArea().register(mux, s.requireUser)
	mux.HandleFunc("GET /api/v1/openapi.json", s.requireUser(s.handleOpenAPI))
	mux.HandleFunc("DELETE /api/v1/account/sessions/{id}", s.requireUser(s.handleAccountSessionEnd))
	mux.HandleFunc("DELETE /api/v1/account/sessions", s.requireUser(s.handleAccountSessionsEndOthers))
	mux.HandleFunc("POST /api/v1/account/2fa/begin", s.requireUser(s.handleTwoFactorBegin))
	mux.HandleFunc("GET /api/v1/account/passkeys", s.requireUser(s.handlePasskeys))
	mux.HandleFunc("POST /api/v1/account/passkeys/begin", s.requireUser(s.handlePasskeyBegin))
	mux.HandleFunc("POST /api/v1/account/passkeys", s.requireUser(s.handlePasskeyFinish))
	mux.HandleFunc("DELETE /api/v1/account/passkeys/{id}", s.requireUser(s.handlePasskeyDelete))
	mux.HandleFunc("POST /api/v1/account/passkeys/confirm", s.requireUser(s.handlePasskeyConfirmBegin))
	mux.HandleFunc("POST /api/v1/account/2fa/enable", s.requireUser(s.handleTwoFactorEnable))
	mux.HandleFunc("POST /api/v1/account/2fa/disable", s.requireUser(s.handleTwoFactorDisable))
	mux.HandleFunc("POST /api/v1/account/2fa/recovery", s.requireUser(s.handleTwoFactorRecovery))
	accountKeysArea{users: s.opts.Users, ssh: s.opts.SSH, reconfirm: s.reconfirm}.register(mux, s.requireUser)
	accountNotifyArea{notify: s.opts.Notify}.register(mux, s.requireUser)
}
