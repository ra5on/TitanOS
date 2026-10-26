package api

import "net/http"

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	paths := map[string]any{}
	add := func(method, path, scope string) {
		ops, _ := paths[path].(map[string]any)
		if ops == nil {
			ops = map[string]any{}
			paths[path] = ops
		}
		operation := map[string]any{"summary": method + " " + path, "x-titanos-scope": scope, "security": []any{map[string]any{"bearerAuth": []string{}}}, "responses": map[string]any{"200": map[string]any{"description": "Successful response (JSON unless documented otherwise)"}, "401": map[string]any{"description": "Invalid or revoked token"}, "403": map[string]any{"description": "Scope or current account permissions deny access"}}}
		if method == "post" || method == "put" {
			operation["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}}
		}
		ops[method] = operation
	}
	for _, path := range []string{"system", "storage", "network", "plugins", "openapi.json"} {
		add("get", "/"+path, "system:read")
	}
	for _, path := range []string{"shares", "files/roots", "files/list", "files/download", "files/properties", "files/text", "files/jobs"} {
		add("get", "/"+path, "files:read")
	}
	for _, path := range []string{"mkdir", "copy", "rename", "create", "delete"} {
		add("post", "/files/"+path, "files:write")
	}
	add("put", "/files/text", "files:write")
	add("get", "/docker", "docker:read")
	add("get", "/vms", "vms:read")
	for _, path := range []string{"/files/list", "/files/download", "/files/properties", "/files/text"} {
		ops := paths[path].(map[string]any)
		op := ops["get"].(map[string]any)
		op["parameters"] = []any{map[string]any{"name": "root", "in": "query", "required": true, "schema": map[string]any{"type": "string"}}, map[string]any{"name": "path", "in": "query", "schema": map[string]any{"type": "string", "default": ""}}}
	}
	for _, action := range []string{"start", "stop", "restart"} {
		add("post", "/docker/containers/{id}/"+action, "docker:write")
	}
	for _, action := range []string{"start", "shutdown", "reboot"} {
		add("post", "/vms/{name}/"+action, "vms:write")
	}
	add("get", "/docker/containers/{id}/logs", "docker:read")
	add("post", "/docker/apps/{name}/network", "docker:write")
	text := map[string]any{"type": "string"}
	list := map[string]any{"type": "array", "items": text}
	boolean := map[string]any{"type": "boolean", "default": false}
	body := func(method, path string, required []string, props map[string]any) {
		op := paths[path].(map[string]any)[method].(map[string]any)
		op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": required, "properties": props}}}}
	}
	for _, path := range []string{"mkdir", "create"} {
		body("post", "/files/"+path, []string{"root", "path"}, map[string]any{"root": text, "path": text})
	}
	body("post", "/files/rename", []string{"root", "from", "to"}, map[string]any{"root": text, "from": text, "to": text})
	body("post", "/files/delete", []string{"root", "paths"}, map[string]any{"root": text, "paths": list, "permanent": boolean, "versions": map[string]any{"type": "object", "additionalProperties": text}})
	body("post", "/files/copy", []string{"root", "paths", "toRoot", "toDir"}, map[string]any{"root": text, "paths": list, "toRoot": text, "toDir": text, "move": boolean, "conflict": map[string]any{"type": "string", "enum": []string{"rename", "skip", "replace"}, "default": "rename"}})
	body("put", "/files/text", []string{"root", "path", "content", "version"}, map[string]any{"root": text, "path": text, "content": text, "version": map[string]any{"type": "string", "description": "Version returned by GET /files/text; a changed source is rejected."}})
	body("post", "/docker/apps/{name}/network", []string{"service", "network"}, map[string]any{"service": text, "network": text, "address": text})
	paths["/files/download"].(map[string]any)["get"].(map[string]any)["description"] = "Streams the authorized file. Supports HTTP Range. The path is relative to the selected root."
	for path, v := range paths {
		ops := v.(map[string]any)
		for _, v := range ops {
			op := v.(map[string]any)
			op["responses"].(map[string]any)["2XX"] = map[string]any{"description": "Success; mutating operations may return 201, 202 or 204. A 202 job must be polled to its final state."}
		}
		for _, name := range []string{"id", "name"} {
			if containsPlaceholder(path, name) {
				for _, v := range ops {
					op := v.(map[string]any)
					params, _ := op["parameters"].([]any)
					op["parameters"] = append(params, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
				}
			}
		}
	}
	writeJSON(w, 200, map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "TitanOS public API", "version": s.opts.Version, "description": "Personal scoped tokens. Account and share permissions are evaluated on every request. Tokens cannot manage credentials, sessions, shells, network confirmation or updates. Last-used timestamps are in memory and reset on service restart."}, "servers": []any{map[string]any{"url": "/api/v1"}}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}}}})
}

func containsPlaceholder(path, name string) bool {
	for i := 0; i+len(name)+2 <= len(path); i++ {
		if path[i:i+len(name)+2] == "{"+name+"}" {
			return true
		}
	}
	return false
}
