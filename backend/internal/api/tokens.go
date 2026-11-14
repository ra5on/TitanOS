package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

type apiTokenIdentity struct{}

// tokenError is why a Bearer header is not accepted.
type tokenError struct {
	status        int
	code, message string
}

// tokensArea is the personal API tokens: their check for every request and
// their management by the account; an area with narrow dependencies
// (PLAN 8a, see fansArea). tokens are the tokens of the server, users the
// accounts they belong to; reconfirm asks for the password again before a
// token is made.
type tokensArea struct {
	users     *auth.Users
	tokens    *auth.Tokens
	reconfirm reconfirmFunc
}

// tokensArea is the area of the server; the handler chain and the MCP
// server check tokens through it.
func (s *Server) tokensArea() tokensArea {
	return tokensArea{users: s.opts.Users, tokens: s.tokens, reconfirm: s.authArea().reconfirm}
}

func (a tokensArea) register(mux *routeMux, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/account/tokens", user(a.handleTokens))
	mux.HandleFunc("POST /api/v1/account/tokens", user(a.handleTokenCreate))
	mux.HandleFunc("DELETE /api/v1/account/tokens/{id}", user(a.handleTokenRevoke))
}

// tokenAccount checks a Bearer header: a token that exists, of an account
// that still exists unchanged and is not locked.
func (a tokensArea) tokenAccount(header string) (auth.APIToken, auth.User, *tokenError) {
	if !strings.HasPrefix(header, "Bearer ") {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "expected a Bearer token"}
	}
	token, ok := a.tokens.Lookup(strings.TrimPrefix(header, "Bearer "))
	if !ok {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "invalid or revoked API token"}
	}
	user, ok := a.users.Get(token.User)
	if !ok || !user.Created.Equal(token.AccountCreated) || token.AccountKey != auth.TokenAccountKey(user) {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "token account no longer exists"}
	}
	if user.Disabled {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "the account of this token is locked"}
	}
	return token, user, nil
}

func (a tokensArea) bearerAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		// WebDAV checks its own Basic sign-in (webdav.go), the MCP server
		// its token (mcp.go), Assist the key of Home Assistant
		// (agent_assist.go).
		if header == "" || r.URL.Path == davPrefix || strings.HasPrefix(r.URL.Path, davPrefix+"/") || r.URL.Path == mcpPath || AssistPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token, user, failed := a.tokenAccount(header)
		if failed != nil {
			writeError(w, failed.status, failed.code, failed.message)
			return
		}
		scope := publicScope(r.Method, r.URL.Path)
		if scope == "" || !slices.Contains(token.Scopes, scope) {
			writeError(w, 403, "token_scope_required", "API token does not allow this endpoint")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiTokenIdentity{}, user.Name)))
	})
}

func (a tokensArea) handleTokens(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"tokens": a.tokens.List(currentUser(r)), "scopes": auth.TokenScopes})
}
func (a tokensArea) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
		// Password and Code confirm it like switching two-factor login
		// off: a token is another way into the account.
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	if !a.reconfirm(w, currentUser(r), req.Password, req.Code) {
		return
	}
	token, plain, err := a.tokens.Create(currentUser(r), req.Name, req.Scopes)
	if err != nil {
		writeError(w, 400, "invalid_api_token", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"token": token, "secret": plain})
}
func (a tokensArea) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if err := a.tokens.Revoke(currentUser(r).Name, r.PathValue("id")); err != nil {
		writeError(w, 404, "api_token_not_found", err.Error())
		return
	}
	w.WriteHeader(204)
}
