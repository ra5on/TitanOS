package api

import "net/http"

// registerUsers registers the routes of the accounts of the NAS.
func (s *Server) registerUsers(mux *routeMux) {
	s.usersArea().register(mux, s.requireAdmin, s.requireUser)
}

// registerAccount registers the routes of one's own account: sessions, tokens, 2FA, passkeys, SSH keys and notifications.
func (s *Server) registerAccount(mux *routeMux) {
	au := s.authArea()
	// Tokens, 2FA, passkeys and SSH keys never go over MCP, the agent,
	// Home Assistant or a token (docs/rules/API.md "Nie über MCP"): only
	// a person at a web session manages them (Z-11, 1.03).
	web := func(h http.HandlerFunc) http.HandlerFunc { return s.requireUser(s.browserOnly(h)) }
	// One's own account.
	mux.HandleFunc("GET /api/v1/account/sessions", s.requireUser(au.handleAccountSessions))
	s.tokensArea().register(mux, web)
	mux.HandleFunc("GET /api/v1/openapi.json", s.requireUser(s.handleOpenAPI))
	mux.HandleFunc("DELETE /api/v1/account/sessions/{id}", s.requireUser(au.handleAccountSessionEnd))
	mux.HandleFunc("DELETE /api/v1/account/sessions", s.requireUser(au.handleAccountSessionsEndOthers))
	mux.HandleFunc("POST /api/v1/account/2fa/begin", web(au.handleTwoFactorBegin))
	mux.HandleFunc("GET /api/v1/account/passkeys", web(au.handlePasskeys))
	mux.HandleFunc("POST /api/v1/account/passkeys/begin", web(au.handlePasskeyBegin))
	mux.HandleFunc("POST /api/v1/account/passkeys", web(au.handlePasskeyFinish))
	mux.HandleFunc("DELETE /api/v1/account/passkeys/{id}", web(au.handlePasskeyDelete))
	mux.HandleFunc("POST /api/v1/account/passkeys/confirm", web(au.handlePasskeyConfirmBegin))
	mux.HandleFunc("POST /api/v1/account/2fa/enable", web(au.handleTwoFactorEnable))
	mux.HandleFunc("POST /api/v1/account/2fa/disable", web(au.handleTwoFactorDisable))
	mux.HandleFunc("POST /api/v1/account/2fa/recovery", web(au.handleTwoFactorRecovery))
	accountKeysArea{users: s.opts.Users, ssh: s.opts.SSH, reconfirm: s.authArea().reconfirm}.register(mux, web)
	accountNotifyArea{notify: s.opts.Notify}.register(mux, s.requireUser)
}
