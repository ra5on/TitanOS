package api

// registerLogin registers the routes that need no login (health, session,
// setup, login and logout, the look of the login page, WebDAV, MCP) and the
// settings of WebDAV, MCP and the login page.
func (s *Server) registerLogin(mux *routeMux) {
	au := s.authArea()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/session", au.handleSession)
	// Whoever sets up first owns the NAS: only from the home network.
	mux.HandleFunc("POST /api/v1/setup", setupLocal(au.handleSetup))
	// A new stick may take the configuration from a backup instead.
	mux.HandleFunc("GET /api/v1/setup/backups", setupLocal(au.handleSetupBackups))
	mux.HandleFunc("POST /api/v1/setup/restore", setupLocal(au.handleSetupRestore))
	mux.HandleFunc("POST /api/v1/setup/restore/upload", setupLocal(au.handleSetupRestoreUpload))
	mux.HandleFunc("POST /api/v1/login", au.handleLogin)
	mux.HandleFunc("POST /api/v1/login/passkey/begin", au.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/v1/login/passkey", au.handlePasskeyLogin)
	mux.HandleFunc("POST /api/v1/logout", au.handleLogout)
	// The account picker of the login page (login_accounts.go).
	mux.HandleFunc("GET /api/v1/login/accounts", au.handleLoginAccounts)
	// PLAN 74: the photos of the accounts named there, under the same gate.
	mux.HandleFunc("GET /api/v1/login/pictures/{id}", au.handleLoginPicture)
	mux.HandleFunc("GET /api/v1/account/picture", s.requireUser(au.handleAccountPicture))
	mux.HandleFunc("GET /api/v1/account/picture/{id}", s.requireUser(au.handleAccountPicturePhoto))
	mux.HandleFunc("PUT /api/v1/account/picture", s.requireUser(au.handlePutAccountPicture))
	mux.HandleFunc("DELETE /api/v1/account/picture", s.requireUser(au.handleDeleteAccountPicture))
	// The lock screen (session_lock.go): requireUser lets a locked session through to these two.
	mux.HandleFunc("POST /api/v1/session/lock", s.requireUser(au.handleSessionLock))
	mux.HandleFunc("POST /api/v1/session/unlock", s.requireUser(au.handleSessionUnlock))
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
