package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// uiHandler serves the web UI. The UI is a single-page app with client-side
// routing, so every path that is not a file gets index.html.
func uiHandler(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
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
