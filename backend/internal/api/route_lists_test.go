package api

import (
	"regexp"
	"strings"
	"testing"
)

// A {parameter} in a route of the public API must not stand for a path the
// mux gives to a more specific handler: the route list would then allow
// that handler with the scope of another one. POST /plugins/{name}/{action}
// once reached POST /plugins/{name}/update as "switch a plugin on or off".
func TestRouteListsDoNotShadowOtherHandlers(t *testing.T) {
	ts := newTestServer(t)
	param := regexp.MustCompile(`\{[^}]+\}`)

	type listed struct{ list, method, path string }
	var routes []listed
	for _, r := range publicRoutes {
		routes = append(routes, listed{"publicRoutes", r.Method, r.Path})
	}

	// Shared on purpose: backupJobOr hands a body with a destination on to
	// the backup job of that (reserved, older) name.
	shared := map[string]bool{
		"PUT /backups/{id} PUT /api/v1/backups/smb":    true,
		"PUT /backups/{id} PUT /api/v1/backups/target": true,
	}
	for _, pattern := range ts.server.patterns {
		method, full, ok := strings.Cut(pattern, " ")
		if !ok {
			continue
		}
		p, ok := strings.CutPrefix(full, "/api/v1")
		if !ok {
			continue
		}
		// A path this pattern serves; a parameter is never one of the
		// literal words of another pattern.
		concrete := param.ReplaceAllString(strings.TrimSuffix(p, "{$}"), "x1")
		for _, r := range routes {
			if r.method != method || r.path == p || shared[r.method+" "+r.path+" "+pattern] {
				continue
			}
			if _, ok := matchPattern(r.path, concrete); ok {
				t.Errorf("%s: %s %s also matches %s, which the mux serves as %q", r.list, r.method, r.path, concrete, pattern)
			}
		}
	}
}
