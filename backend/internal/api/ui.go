package api

import (
	"bytes"
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
)

// cspMeta finds the policy SvelteKit writes into index.html (see
// web/vite.config.ts), with the hash of its own start script.
var cspMeta = regexp.MustCompile(`(?is)<meta\s[^>]*http-equiv=["']content-security-policy["'][^>]*>`)
var cspContent = regexp.MustCompile(`(?is)\scontent=(?:"([^"]*)"|'([^']*)')`)

// cspExtra are directives a meta tag cannot set: who may frame the UI and
// where forms may go.
const cspExtra = "frame-ancestors 'self'; form-action 'self'"

// contentSecurityPolicy is the policy of index.html as a header, so that it
// also holds for what the meta tag cannot cover. It is the same policy as
// in the page (web/vite.config.ts): default-src 'self', with data:/blob:
// where the UI needs them and https pictures for app icons.
func contentSecurityPolicy(ui fs.FS) string {
	data, err := fs.ReadFile(ui, "index.html")
	if err == nil {
		if tag := cspMeta.Find(data); tag != nil {
			if m := cspContent.FindSubmatch(tag); m != nil {
				policy := strings.TrimRight(strings.TrimSpace(html.UnescapeString(string(m[1])+string(m[2]))), ";")
				if policy != "" && !strings.ContainsAny(policy, "\r\n") {
					return policy + "; " + cspExtra
				}
			}
		}
	}
	// Without the tag (development builds) the hash of the start script is
	// unknown; leave scripts to the page.
	return "object-src 'none'; base-uri 'self'; " + cspExtra
}

// uiHandler serves the web UI. The UI is a single-page app with client-side
// routing, so every path that is not a file gets index.html.
func uiHandler(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	csp := sync.OnceValue(func() string { return contentSecurityPolicy(ui) })
	agentIndex := sync.OnceValues(func() ([]byte, error) { return agentIndexHTML(ui) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || !isFile(ui, name) {
			// index.html references hashed files and must never be cached,
			// otherwise the browser keeps an old UI after an update.
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Security-Policy", csp())
			if name == "agent" || strings.HasPrefix(name, "agent/") {
				if data, err := agentIndex(); err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					if r.Method == http.MethodGet {
						_, _ = w.Write(data)
					}
					return
				}
			}
			http.ServeFileFS(w, r, ui, "index.html")
			return
		}

		if strings.HasSuffix(name, ".webmanifest") {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		if strings.HasPrefix(name, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

// agentIndexHTML is index.html for the KI-Agent's pages (PLAN 59): iOS
// takes the status bar of a home-screen app from the page as it loads, so
// the agent's picture behind the status bar has to be in the HTML, not only
// set by the page afterwards. Opened from a notification, the agent app
// otherwise starts with an opaque bar and its view slides down.
func agentIndexHTML(ui fs.FS) ([]byte, error) {
	data, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		return nil, err
	}
	data = bytes.Replace(data, []byte(`name="apple-mobile-web-app-status-bar-style" content="black"`), []byte(`name="apple-mobile-web-app-status-bar-style" content="black-translucent"`), 1)
	data = bytes.Replace(data, []byte(`content="width=device-width, initial-scale=1"`), []byte(`content="width=device-width, initial-scale=1, viewport-fit=cover"`), 1)
	return data, nil
}
