package api

import (
	"maps"
	"net/http"
	"slices"
	"strings"
)

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	paths := map[string]any{}
	for _, route := range publicRoutes {
		ops, _ := paths[route.Path].(map[string]any)
		if ops == nil {
			ops = map[string]any{}
			paths[route.Path] = ops
		}
		operation := map[string]any{"summary": route.Method + " " + route.Path, "description": route.En, "x-titanos-scope": route.Scope, "security": []any{map[string]any{"bearerAuth": []string{}}},
			"responses": map[string]any{
				"200": map[string]any{"description": "Successful response (JSON unless documented otherwise)"},
				"2XX": map[string]any{"description": "Success; mutating operations may return 201, 202 or 204. A 202 job must be polled to its final state."},
				"401": map[string]any{"description": "Invalid or revoked token"},
				"403": map[string]any{"description": "Scope or current account permissions deny access"}}}
		params := []any{}
		for _, name := range []string{"id", "name"} {
			if containsPlaceholder(route.Path, name) {
				params = append(params, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		for _, name := range slices.Sorted(maps.Keys(route.Query)) {
			params = append(params, map[string]any{"name": name, "in": "query", "required": slices.Contains(route.Required, name), "schema": route.Query[name]})
		}
		if len(params) > 0 {
			operation["parameters"] = params
		}
		if route.Body != nil {
			operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": route.Required, "properties": route.Body}}}}
		}
		ops[strings.ToLower(route.Method)] = operation
	}
	writeJSON(w, 200, map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "TitanOS public API", "version": s.opts.Version, "description": "Personal scoped tokens. Account and share permissions are evaluated on every request. Tokens cannot manage credentials, sessions, shells, network or firewall confirmation or updates. Last-used timestamps are in memory and reset on service restart. The same routes are offered as tools of the MCP server at /mcp (docs/MCP.md)."}, "servers": []any{map[string]any{"url": "/api/v1"}}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}}}})
}

func containsPlaceholder(path, name string) bool {
	for i := 0; i+len(name)+2 <= len(path); i++ {
		if path[i:i+len(name)+2] == "{"+name+"}" {
			return true
		}
	}
	return false
}
