package webapps

import (
	"bytes"
	"errors"
	"html"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// HostScript is the TitanOS mediator in the app's document (part of the
// web UI: web/static/webapps/_host/host.js). "_host" is never an app id.
const HostScript = "/webapps/_host/host.js"

// maxIndex bounds index.html, which is read into memory to be rewritten.
const maxIndex = 1 << 20

// Headers of every file of an app (docs/WEBAPPS.md): the sandbox keeps
// the app away from the TitanOS origin even when a file is opened outside
// its frame; the sandboxed frame has the origin "null", so module scripts
// and .wasm need CORS (the files are public open-source code anyway).
const (
	sandboxPolicy = "sandbox allow-scripts allow-downloads allow-modals allow-popups; frame-ancestors 'self'"
	// trustedPolicy is the header of a trusted app (our own build): framed
	// only by the web UI, no sandbox.
	trustedPolicy = "frame-ancestors 'self'"
	cacheForever  = "public, max-age=31536000, immutable"
)

var mimeTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".wasm": "application/wasm", ".css": "text/css; charset=utf-8", ".json": "application/json",
	".map": "application/json", ".webmanifest": "application/manifest+json",
	".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif",
	".ttf": "font/ttf", ".otf": "font/otf", ".woff": "font/woff", ".woff2": "font/woff2",
	".txt": "text/plain; charset=utf-8", ".md": "text/plain; charset=utf-8",
	".wav": "audio/wav", ".mp3": "audio/mpeg", ".ogg": "audio/ogg",
}

func contentType(name string) string {
	if t, ok := mimeTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// ServeHTTP answers GET /webapps/{id}/{version}/{path...} without sign-in:
// only files of installed apps, never a listing, dot files or links.
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	id, version, name := r.PathValue("id"), r.PathValue("version"), r.PathValue("path")
	app, ok := Lookup(id)
	if !ok || !ValidVersion(version) {
		h.Set("Content-Security-Policy", sandboxPolicy)
		http.NotFound(w, r)
		return
	}
	if app.Trusted {
		h.Set("Content-Security-Policy", trustedPolicy)
	} else {
		h.Set("Content-Security-Policy", sandboxPolicy)
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Access-Control-Allow-Origin", "*")
	}
	if name == "" {
		name = "index.html"
	}
	rel, ok := cleanEntry(name)
	if !ok || strings.HasSuffix(name, "/") || hasDotPart(rel) || rel == "index.html.gz" || rel == "index.html.br" {
		http.NotFound(w, r)
		return
	}
	root, err := m.appRoot(id, version)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	if rel == "index.html" {
		m.serveIndex(w, r, root, app)
		return
	}
	file, info, err := openRegular(root, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	h.Set("Content-Type", contentType(rel))
	// The version is part of the address: no file under it ever changes.
	h.Set("Cache-Control", cacheForever)
	if compressible[strings.ToLower(path.Ext(rel))] || strings.HasSuffix(rel, ".wasm") {
		h.Add("Vary", "Accept-Encoding")
		for _, enc := range []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
			if !accepts(r.Header.Get("Accept-Encoding"), enc.token) {
				continue
			}
			if packed, packedInfo, err := openRegular(root, rel+enc.ext); err == nil {
				defer packed.Close()
				h.Set("Content-Encoding", enc.token)
				http.ServeContent(w, r, "", packedInfo.ModTime(), packed)
				return
			}
		}
	}
	http.ServeContent(w, r, "", info.ModTime(), file)
}

func hasDotPart(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// accepts reads Accept-Encoding (q=0 refuses).
func accepts(header, token string) bool {
	for _, item := range strings.Split(header, ",") {
		fields := strings.Split(item, ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), token) {
			continue
		}
		for _, p := range fields[1:] {
			if q := strings.ReplaceAll(strings.TrimSpace(p), " ", ""); q == "q=0" || q == "q=0.0" || q == "q=0.00" || q == "q=0.000" {
				return false
			}
		}
		return true
	}
	return false
}

func openRegular(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	st, err := root.Lstat(name)
	if err != nil || !st.Mode().IsRegular() {
		return nil, nil, os.ErrNotExist
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, os.ErrNotExist
	}
	return f, info, nil
}

func readLimited(root *os.Root, name string, limit int64) ([]byte, error) {
	f, _, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("webapps: index.html too large")
	}
	return data, nil
}

func (m *Manager) serveIndex(w http.ResponseWriter, r *http.Request, root *os.Root, app App) {
	data, err := readLimited(root, "index.html", maxIndex)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = RewriteIndex(data, app)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// The rewrite may change with titand: always ask again.
	h.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

var (
	canvasTag = regexp.MustCompile(`(?i)<canvas\b[^>]*>`)
	dataHost  = regexp.MustCompile(`(?i)\sdata-host\s*=`)
	scriptTag = regexp.MustCompile(`(?i)<script\b`)
	headEnd   = regexp.MustCompile(`(?i)</head\s*>`)
)

// RewriteIndex adds data-host="page" to the app's canvas (Craft bridge
// apps: the bridge in the same document) and the TitanOS mediator before
// the first script of the page (the app's module script runs after it).
func RewriteIndex(data []byte, app App) []byte {
	out := data
	if app.Bridge == "host" {
		if loc := canvasTag.FindIndex(out); loc != nil && !dataHost.Match(out[loc[0]:loc[1]]) {
			at := loc[0] + len("<canvas")
			out = append(append(append([]byte{}, out[:at]...), ` data-host="page"`...), out[at:]...)
		}
	}
	tag := []byte(`<script src="` + HostScript + `" data-app="` + html.EscapeString(app.ID) + `" data-bridge="` + html.EscapeString(app.Bridge) + `"></script>`)
	at := -1
	if loc := scriptTag.FindIndex(out); loc != nil {
		at = loc[0]
	}
	if loc := headEnd.FindIndex(out); loc != nil && (at < 0 || loc[0] < at) {
		at = loc[0]
	}
	if at < 0 {
		at = 0
	}
	return append(append(append([]byte{}, out[:at]...), tag...), out[at:]...)
}
