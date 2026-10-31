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
	// The look of the login page is public; changing it is not.
	mux.HandleFunc("GET /api/v1/login-appearance", s.handleLoginAppearance)
	mux.HandleFunc("GET /icons/{file}", s.handleAppIconFile)
	mux.HandleFunc("/dav/", s.handleWebDAV)
	mux.HandleFunc("/dav", s.handleWebDAV)
	// The MCP server checks its own token (mcp.go).
	mux.HandleFunc(mcpPath, s.handleMCP)
	mux.HandleFunc("GET /api/v1/system/mcp", s.requireUser(s.handleMCPSettings))
	mux.HandleFunc("PUT /api/v1/system/mcp", s.requireAdmin(s.handleMCPSettingsSet))
	mux.HandleFunc("GET /api/v1/system/webdav", s.requireAdmin(s.handleWebDAVSettings))
	mux.HandleFunc("PUT /api/v1/system/webdav", s.requireAdmin(s.handleWebDAVSettingsSet))
	mux.HandleFunc("GET /api/v1/account/webdav", s.requireUser(s.handleAccountWebDAV))
	mux.HandleFunc("POST /api/v1/account/webdav", s.requireUser(s.handleAccountWebDAVNew))
	mux.HandleFunc("DELETE /api/v1/account/webdav", s.requireUser(s.handleAccountWebDAVDelete))
	mux.HandleFunc("GET /api/v1/app-icon", s.requireAdmin(s.handleAppIcon))
	mux.HandleFunc("PUT /api/v1/app-icon", s.requireAdmin(s.handlePutAppIcon))
	mux.HandleFunc("GET /api/v1/login-appearance/background/{id}", s.handleLoginBackground)
	mux.HandleFunc("PUT /api/v1/login-appearance", s.requireAdmin(s.handlePutLoginAppearance))
	mux.HandleFunc("DELETE /api/v1/login-appearance", s.requireAdmin(s.handleDeleteLoginAppearance))
}
