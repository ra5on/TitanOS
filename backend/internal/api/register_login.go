package api

// registerLogin registers the routes that need no login (health, session,
// setup, login and logout, the look of the login page, WebDAV, MCP) and the
// settings of WebDAV, MCP and the login page.
func (s *Server) registerLogin(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/session", s.handleSession)
	// Whoever sets up first owns the NAS: only from the home network.
	mux.HandleFunc("POST /api/v1/setup", setupLocal(s.handleSetup))
	// A new stick may take the configuration from a backup instead.
	mux.HandleFunc("GET /api/v1/setup/backups", setupLocal(s.handleSetupBackups))
	mux.HandleFunc("POST /api/v1/setup/restore", setupLocal(s.handleSetupRestore))
	mux.HandleFunc("POST /api/v1/setup/restore/upload", setupLocal(s.handleSetupRestoreUpload))
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/login/passkey/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/v1/login/passkey", s.handlePasskeyLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)
	// The account picker of the login page (login_accounts.go).
	mux.HandleFunc("GET /api/v1/login/accounts", s.handleLoginAccounts)
	// PLAN 74: the photos of the accounts named there, under the same gate.
	mux.HandleFunc("GET /api/v1/login/pictures/{id}", s.handleLoginPicture)
	mux.HandleFunc("GET /api/v1/account/picture", s.requireUser(s.handleAccountPicture))
	mux.HandleFunc("GET /api/v1/account/picture/{id}", s.requireUser(s.handleAccountPicturePhoto))
	mux.HandleFunc("PUT /api/v1/account/picture", s.requireUser(s.handlePutAccountPicture))
	mux.HandleFunc("DELETE /api/v1/account/picture", s.requireUser(s.handleDeleteAccountPicture))
	// The lock screen (session_lock.go): requireUser lets a locked session through to these two.
	mux.HandleFunc("POST /api/v1/session/lock", s.requireUser(s.handleSessionLock))
	mux.HandleFunc("POST /api/v1/session/unlock", s.requireUser(s.handleSessionUnlock))
	s.loginLookArea().register(mux, s.requireAdmin)
	s.appIconArea().register(mux, s.requireAdmin)
	s.davArea().register(mux, s.requireAdmin, s.requireUser)
	// The MCP server checks its own token (mcp.go).
	mux.HandleFunc(mcpPath, s.handleMCP)
	mux.HandleFunc("GET /api/v1/system/mcp", s.requireUser(s.handleMCPSettings))
	mux.HandleFunc("PUT /api/v1/system/mcp", s.requireAdmin(s.handleMCPSettingsSet))
	mux.HandleFunc("GET /api/v1/system/startup", s.requireAdmin(s.handleStartup))
	mux.HandleFunc("PUT /api/v1/system/startup", s.requireAdmin(s.handlePutStartup))
}
