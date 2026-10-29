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

// tokenAccount checks a Bearer header: a token that exists, of an account
// that still exists unchanged and is not locked.
func (s *Server) tokenAccount(header string) (auth.APIToken, auth.User, *tokenError) {
	if !strings.HasPrefix(header, "Bearer ") {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "expected a Bearer token"}
	}
	token, ok := s.tokens.Lookup(strings.TrimPrefix(header, "Bearer "))
	if !ok {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "invalid or revoked API token"}
	}
	user, ok := s.opts.Users.Get(token.User)
	if !ok || !user.Created.Equal(token.AccountCreated) || token.AccountKey != auth.TokenAccountKey(user) {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "token account no longer exists"}
	}
	if user.Disabled {
		return auth.APIToken{}, auth.User{}, &tokenError{401, "unauthorized", "the account of this token is locked"}
	}
	return token, user, nil
}

func (s *Server) bearerAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		// WebDAV checks its own Basic sign-in (webdav.go), the MCP server
		// its token (mcp.go).
		if header == "" || r.URL.Path == davPrefix || strings.HasPrefix(r.URL.Path, davPrefix+"/") || r.URL.Path == mcpPath {
			next.ServeHTTP(w, r)
			return
		}
		token, user, failed := s.tokenAccount(header)
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

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"tokens": s.tokens.List(currentUser(r).Name), "scopes": auth.TokenScopes})
}
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	token, plain, err := s.tokens.Create(currentUser(r), req.Name, req.Scopes)
	if err != nil {
		writeError(w, 400, "invalid_api_token", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"token": token, "secret": plain})
}
func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.tokens.Revoke(currentUser(r).Name, r.PathValue("id")); err != nil {
		writeError(w, 404, "api_token_not_found", err.Error())
		return
	}
	w.WriteHeader(204)
}
