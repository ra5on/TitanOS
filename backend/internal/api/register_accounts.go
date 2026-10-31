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
	mux.HandleFunc("GET /api/v1/account/tokens", s.requireUser(s.handleTokens))
	mux.HandleFunc("POST /api/v1/account/tokens", s.requireUser(s.handleTokenCreate))
	mux.HandleFunc("DELETE /api/v1/account/tokens/{id}", s.requireUser(s.handleTokenRevoke))
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
	mux.HandleFunc("GET /api/v1/account/ssh-keys", s.requireUser(s.handleAccountKeys))
	mux.HandleFunc("POST /api/v1/account/ssh-keys", s.requireUser(s.handleAccountKeyAdd))
	mux.HandleFunc("DELETE /api/v1/account/ssh-keys/{id}", s.requireUser(s.handleAccountKeyRemove))
	mux.HandleFunc("GET /api/v1/account/notifications", s.requireUser(s.handleAccountNotify))
	mux.HandleFunc("PUT /api/v1/account/notifications", s.requireUser(s.handleAccountNotifyChange))
	mux.HandleFunc("POST /api/v1/account/notifications/test", s.requireUser(s.handleAccountNotifyTest))
}
