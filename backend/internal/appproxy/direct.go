package appproxy

// Direct windows (PLAN 75-14, "wie bei Kasm Workspaces"): an app that
// brings its own desktop stream (jlesage/noVNC, linuxserver Selkies,
// KasmVNC) is framed by the web UI under its own HTTPS name instead of
// being shown through Chromium on the NAS. The window only works when the
// browser reaches that name, which it can only find out by trying: the
// enter page tells the window by postMessage that it arrived; no message
// in time and the window falls back to the remote view.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"
)

// DirectHost is the app's name under the suffix the web UI is reached by
// (the browser resolves and trusts that one); false when the UI is reached
// another way (an IP address, a tunnel's own name).
func (m *Manager) DirectHost(app, uiHost string) (string, bool) {
	route, ok := m.Route(app)
	if !ok {
		return "", false
	}
	uiHost = hostOnly(uiHost)
	for _, suffix := range m.Suffixes() {
		if uiHost == suffix || strings.HasSuffix(uiHost, "."+suffix) {
			return route.Name + "." + suffix, true
		}
	}
	return "", false
}

// FrameSources are the origins the web UI may frame: the apps' names
// under every suffix, never more.
func (m *Manager) FrameSources() []string {
	port := ""
	if m.HTTPSPort != 0 && m.HTTPSPort != 443 {
		port = ":" + strconv.Itoa(m.HTTPSPort)
	}
	var out []string
	for _, suffix := range m.Suffixes() {
		out = append(out, "https://*."+suffix+port)
	}
	return out
}

// enteredFrame answers the enter page of a direct window: it tells the
// window (only the web UI's origins) that the app is reached, then goes on
// to the app's start page.
func (m *Manager) enteredFrame(w http.ResponseWriter) {
	nonce := make([]byte, 16)
	rand.Read(nonce)
	n := base64.RawStdEncoding.EncodeToString(nonce)
	origins := m.frameOrigins()
	list, _ := json.Marshal(origins)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+n+"'; base-uri 'none'; form-action 'none'; "+frameAncestors(origins))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><script nonce="` + html.EscapeString(n) + `">` +
		`for (const o of ` + string(list) + `) { try { parent.postMessage({ type: 'titanos-direct', ok: true }, o); } catch (e) {} }` +
		`location.replace('/');</script>`))
}

// directQuery marks the enter address of a direct window.
const directQuery = "frame"
