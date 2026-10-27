package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

type apiTokenIdentity struct{}

// Public routes are an explicit contract. A token never opens account, shell,
// credential, update, network-confirmation or assistant endpoints.
func publicScope(method, path string) string {
	if method == "GET" {
		switch path {
		case "/api/v1/system", "/api/v1/storage", "/api/v1/network", "/api/v1/plugins", "/api/v1/openapi.json":
			return "system:read"
		case "/api/v1/shares", "/api/v1/files/roots", "/api/v1/files/list", "/api/v1/files/download", "/api/v1/files/properties", "/api/v1/files/text", "/api/v1/files/jobs":
			return "files:read"
		case "/api/v1/docker":
			return "docker:read"
		case "/api/v1/vms":
			return "vms:read"
		}
		if strings.HasPrefix(path, "/api/v1/docker/containers/") && strings.HasSuffix(path, "/logs") {
			return "docker:read"
		}
	}
	if method == "POST" {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "docker" && parts[3] == "containers" && slices.Contains([]string{"start", "stop", "restart"}, parts[5]) {
			return "docker:write"
		}
		if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "vms" && slices.Contains([]string{"start", "shutdown", "reboot"}, parts[4]) {
			return "vms:write"
		}
		if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "docker" && parts[3] == "apps" && parts[5] == "network" {
			return "docker:write"
		}
		if slices.Contains([]string{"mkdir", "copy", "rename", "create", "delete"}, strings.TrimPrefix(path, "/api/v1/files/")) {
			return "files:write"
		}
	}
	if method == "PUT" && path == "/api/v1/files/text" {
		return "files:write"
	}
	return ""
}

func (s *Server) bearerAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, 401, "unauthorized", "expected a Bearer token")
			return
		}
		token, ok := s.tokens.Lookup(strings.TrimPrefix(header, "Bearer "))
		if !ok {
			writeError(w, 401, "unauthorized", "invalid or revoked API token")
			return
		}
		user, ok := s.opts.Users.Get(token.User)
		if !ok || !user.Created.Equal(token.AccountCreated) || token.AccountKey != auth.TokenAccountKey(user) {
			writeError(w, 401, "unauthorized", "token account no longer exists")
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
