package appproxy

// The browser gate (PLAN 61): a page a browser loads from an app's HTTPS
// name needs the TitanOS login and the account's right to the app. The
// web UI hands out a short token (POST /api/v1/apps/{app}/window); the
// browser brings it to /.titanos/enter on the app's name and gets a cookie
// for that name. Everything that is not a page load in a browser (API
// calls of phone and TV apps, which have their own login, scripts, images,
// WebSockets) passes unchanged: the gate guards the app's pages, the app's
// own login stays the guard of its API.

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EnterPath is where a browser brings its window token on an app's name.
const EnterPath = "/.titanos/enter"

// gateCookie holds the browser's pass for one app's name. Its name starts
// like the TitanOS login, so the proxy never hands it to the app and the
// app cannot set it (stripSessionCookies, dropSessionSetCookies).
const gateCookie = "__Host-titanos_app"

// Gatekeeper decides who gets through the gate; package appaccess.
type Gatekeeper interface {
	// Gated tells whether page loads of an app need the login.
	Gated(app string) bool
	// Enter takes a window token and returns the cookie value and its life.
	Enter(token, app string) (cookie string, ttl time.Duration, ok bool)
	// Admit checks a cookie against the account's current right.
	Admit(cookie, app string) bool
}

// pageLoad tells whether a request is a browser loading a page (a tab or
// a frame). Browsers since 2020 say so in Sec-Fetch-*; without those
// headers a GET that asks for HTML counts.
func pageLoad(r *http.Request) bool {
	mode := strings.ToLower(r.Header.Get("Sec-Fetch-Mode"))
	dest := strings.ToLower(r.Header.Get("Sec-Fetch-Dest"))
	if mode == "websocket" {
		return false
	}
	if mode == "navigate" || mode == "nested-navigate" {
		return true
	}
	switch dest {
	case "document", "iframe", "frame":
		return true
	}
	if mode != "" || dest != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		return false
	}
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

// gateApp is the Docker app a route key belongs to, or "" (targets
// outside Docker have no rights and no gate).
func gateApp(key string) string {
	app, ok := strings.CutPrefix(key, "app:")
	if !ok {
		return ""
	}
	return app
}

// gate answers the requests the gate takes itself and tells whether the
// request may go on to the app.
func (m *Manager) gate(w http.ResponseWriter, r *http.Request, app string) bool {
	if m.Access == nil || app == "" {
		return true
	}
	if r.URL.Path == EnterPath {
		m.enter(w, r, app)
		return false
	}
	if !m.Access.Gated(app) || !pageLoad(r) {
		return true
	}
	if cookie, err := r.Cookie(gateCookie); err == nil && m.Access.Admit(cookie.Value, app) {
		return true
	}
	m.refuse(w, r, app)
	return false
}

// enter takes the window token, sets the cookie and sends the browser on
// to the app's start page.
func (m *Manager) enter(w http.ResponseWriter, r *http.Request, app string) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	cookie, ttl, ok := m.Access.Enter(r.URL.Query().Get("t"), app)
	if !ok {
		m.refuse(w, r, app)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: gateCookie, Value: cookie, Path: "/", MaxAge: int(ttl / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteNoneMode, Partitioned: true,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// refuse answers a page load without a valid pass: a short page in German
// and English that leads to TitanOS, which opens the app again.
func (m *Manager) refuse(w http.ResponseWriter, r *http.Request, app string) {
	link := m.uiOrigin(hostOnly(r.Host)) + "/api/v1/apps/" + url.PathEscape(app) + "/open?host=" + url.QueryEscape(hostOnly(r.Host))
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; "+frameAncestors(m.frameOrigins()))
	w.WriteHeader(http.StatusForbidden)
	if r.Method == http.MethodHead {
		return
	}
	href := html.EscapeString(link)
	_, _ = w.Write([]byte(`<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>TitanOS</title>` +
		`<style>body{font-family:system-ui,sans-serif;margin:0;min-height:100vh;display:grid;place-items:center;background:#0f1419;color:#e6e8eb}main{max-width:28rem;padding:24px}h1{font-size:1.25rem;margin:0 0 8px}p{margin:0 0 16px;line-height:1.5;color:#b8bec6}a{display:inline-block;padding:8px 14px;border-radius:8px;background:#3b82f6;color:#fff;text-decoration:none}hr{border:0;border-top:1px solid #2a3139;margin:24px 0}@media (prefers-color-scheme:light){body{background:#f5f6f8;color:#14181d}p{color:#4a525c}hr{border-top-color:#d6dae0}a{background:#1d4ed8}}</style></head><body><main>` +
		`<h1>Diese App über TitanOS öffnen</h1><p>Diese App ist durch die TitanOS-Anmeldung geschützt. Melde dich bei TitanOS an und öffne sie dort.</p><p><a href="` + href + `" target="_top" rel="noreferrer">TitanOS öffnen</a></p><hr>` +
		`<h1 lang="en">Open this app through TitanOS</h1><p lang="en">This app is protected by the TitanOS login. Sign in to TitanOS and open it there.</p><p><a lang="en" href="` + href + `" target="_top" rel="noreferrer">Open TitanOS</a></p>` +
		`</main></body></html>`))
}

// uiOrigin picks the origin of the web UI closest to an app's host name:
// the one sharing the longest tail of labels (immich.nas.local → the
// NAS's .local name), else the first.
func (m *Manager) uiOrigin(host string) string {
	origins := m.frameOrigins()
	if len(origins) == 0 {
		return "https://" + host
	}
	best, score := origins[0], 0
	appLabels := strings.Split(host, ".")
	for _, origin := range origins {
		h := strings.TrimPrefix(origin, "https://")
		if i := strings.LastIndex(h, ":"); i > 0 && !strings.HasSuffix(h, "]") {
			h = h[:i]
		}
		labels := strings.Split(h, ".")
		n := 0
		for n < len(labels) && n < len(appLabels) && labels[len(labels)-1-n] == appLabels[len(appLabels)-1-n] {
			n++
		}
		if n > score {
			best, score = origin, n
		}
	}
	return best
}

// EnterURL is the address that lets a browser in to an app with a window
// token: on host if it is one of the app's names (its device in the
// tailnet included), else on the app's main address. ok is false if the
// app has no name that answers.
func (m *Manager) EnterURL(app, host, token string) (string, bool) {
	base := ""
	if host = hostOnly(host); host != "" {
		if key, _, ok := m.match(host); ok && key == "app:"+app {
			base = m.hostURL(host)
		} else if key, ok := m.tailnetMatch(host); ok && key == "app:"+app {
			if _, enabled := m.routeByKey(key); enabled {
				base = "https://" + host
			}
		}
	}
	if base == "" {
		base = m.URL(app)
	}
	if base == "" {
		return "", false
	}
	return base + EnterPath + "?t=" + url.QueryEscape(token), true
}
