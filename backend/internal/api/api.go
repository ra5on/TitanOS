// Package api is the HTTP interface of titand: the JSON API under /api/v1
// and the static files of the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

// Options are the parts the API works with.
type Options struct {
	// UI holds the files of the web UI with index.html at its root.
	UI      fs.FS
	Store   *config.Store
	Users   *auth.Users
	Monitor *system.Monitor
	Storage *storage.Manager
	Shares  *shares.Manager
	Files   *files.Manager
	Docker  *docker.Manager
	Catalog *catalog.Manager
	Power   system.Power
	Version string
	// SecureCookies is on whenever the API is served over HTTPS.
	SecureCookies bool
}

// Server answers all HTTP requests.
type Server struct {
	opts     Options
	sessions *auth.Sessions
	limiter  *auth.Limiter
	prefs    *preferences
	handler  http.Handler

	loadMu      sync.Mutex
	lastLoadTry time.Time
}

// New returns the handler for all requests.
func New(opts Options) *Server {
	s := &Server{
		opts:     opts,
		sessions: auth.NewSessions(),
		limiter:  auth.NewLimiter(),
		prefs:    newPreferences(opts.Store),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/session", s.handleSession)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)

	mux.HandleFunc("GET /api/v1/preferences", s.requireUser(s.handleGetPreferences))
	mux.HandleFunc("PUT /api/v1/preferences/{kind}", s.requireUser(s.handlePutPreferences))
	mux.HandleFunc("GET /api/v1/background", s.requireUser(s.handleGetBackground))
	mux.HandleFunc("PUT /api/v1/background", s.requireUser(s.handlePutBackground))
	mux.HandleFunc("DELETE /api/v1/background", s.requireUser(s.handleDeleteBackground))

	mux.HandleFunc("GET /api/v1/system", s.requireUser(s.handleSystem))
	mux.HandleFunc("POST /api/v1/system/power", s.requireAdmin(s.handlePower))

	// Everyone may see the volumes; only administrators may change disks.
	mux.HandleFunc("GET /api/v1/storage", s.requireUser(s.handleStorage))
	mux.HandleFunc("POST /api/v1/storage/rescan", s.requireAdmin(s.handleStorageRescan))
	mux.HandleFunc("POST /api/v1/storage/volumes", s.requireAdmin(s.handleCreateVolume))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireAdmin(s.handleDeleteVolume))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/disks", s.requireAdmin(s.handleAddVolumeDisk))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/disks/{disk}", s.requireAdmin(s.handleRemoveVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/check", s.requireAdmin(s.handleCheckVolume))

	mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PUT /api/v1/users/{name}", s.requireAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{name}", s.requireAdmin(s.handleDeleteUser))
	// Everyone may change their own password.
	mux.HandleFunc("POST /api/v1/users/{name}/password", s.requireUser(s.handleSetPassword))

	mux.HandleFunc("GET /api/v1/shares", s.requireUser(s.handleListShares))
	mux.HandleFunc("POST /api/v1/shares", s.requireAdmin(s.handleCreateShare))
	mux.HandleFunc("PUT /api/v1/shares/{name}", s.requireAdmin(s.handleUpdateShare))
	mux.HandleFunc("DELETE /api/v1/shares/{name}", s.requireAdmin(s.handleDeleteShare))

	// The file manager. Every request names a root the account has access to.
	mux.HandleFunc("GET /api/v1/files/roots", s.requireUser(s.handleFileRoots))
	mux.HandleFunc("GET /api/v1/files/list", s.requireUser(s.handleFileList))
	mux.HandleFunc("GET /api/v1/files/download", s.requireUser(s.handleFileDownload))
	mux.HandleFunc("GET /api/v1/files/zip", s.requireUser(s.handleFileZip))
	mux.HandleFunc("POST /api/v1/files/mkdir", s.requireUser(s.handleFileMkdir))
	mux.HandleFunc("POST /api/v1/files/rename", s.requireUser(s.handleFileRename))
	mux.HandleFunc("POST /api/v1/files/delete", s.requireUser(s.handleFileDelete))
	mux.HandleFunc("POST /api/v1/files/uploads", s.requireUser(s.handleUploadStart))
	mux.HandleFunc("GET /api/v1/files/uploads/{id}", s.requireUser(s.handleUploadStatus))
	mux.HandleFunc("PATCH /api/v1/files/uploads/{id}", s.requireUser(s.handleUploadChunk))
	mux.HandleFunc("POST /api/v1/files/uploads/{id}/finish", s.requireUser(s.handleUploadFinish))
	mux.HandleFunc("DELETE /api/v1/files/uploads/{id}", s.requireUser(s.handleUploadAbort))

	// Docker. Whoever controls containers controls the machine: administrators only.
	mux.HandleFunc("GET /api/v1/docker", s.requireAdmin(s.handleDocker))
	mux.HandleFunc("PUT /api/v1/docker/settings", s.requireAdmin(s.handleDockerSettings))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/{action}", s.requireAdmin(s.handleContainerAction))
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", s.requireAdmin(s.handleContainerRemove))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", s.requireAdmin(s.handleContainerLogs))
	mux.HandleFunc("POST /api/v1/docker/apps", s.requireAdmin(s.handleAppCreate))
	mux.HandleFunc("GET /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppCompose))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppRemove))
	mux.HandleFunc("PUT /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppSetCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/update", s.requireAdmin(s.handleAppUpdate))
	mux.HandleFunc("POST /api/v1/docker/updates/check", s.requireAdmin(s.handleUpdatesCheck))
	mux.HandleFunc("POST /api/v1/docker/updates/apply", s.requireAdmin(s.handleUpdatesApply))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/console", s.requireAdmin(s.handleContainerConsole))

	// The app store: templates to install as Docker apps.
	mux.HandleFunc("GET /api/v1/apps", s.requireAdmin(s.handleApps))
	mux.HandleFunc("POST /api/v1/apps/refresh", s.requireAdmin(s.handleAppsRefresh))
	mux.HandleFunc("POST /api/v1/apps/sources", s.requireAdmin(s.handleSourceAdd))
	mux.HandleFunc("DELETE /api/v1/apps/sources/{id}", s.requireAdmin(s.handleSourceRemove))
	mux.HandleFunc("GET /api/v1/apps/sources/{source}/apps/{app}", s.requireAdmin(s.handleAppTemplate))

	mux.HandleFunc("POST /api/v1/session/confirm", s.requireUser(s.handleConfirm))
	mux.HandleFunc("GET /api/v1/session/confirmed", s.requireUser(s.handleConfirmed))

	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", uiHandler(opts.UI))

	// Browsers must not let another website send requests with our cookie.
	s.handler = securityHeaders(http.NewCrossOriginProtection().Handler(mux))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// apiError is the body of every failed API request. The web UI translates
// Code; Message is for people reading the raw response.
type apiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retryAfter,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

// handleAPINotFound keeps unknown API paths from falling through to the web
// UI, which would answer them with index.html.
func handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
}

// maxJSONBody bounds every JSON request body.
const maxJSONBody = 64 * 1024

// readJSON decodes the request body into v and answers the request itself
// if that fails.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	return readJSONBody(w, r, v)
}

// readJSONBody is readJSON for callers that set their own size limit.
func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "could not read the request body")
		}
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return false
	}
	return true
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type contextKey struct{}

// currentUser returns the account the request is logged in as.
func currentUser(r *http.Request) auth.User {
	user, _ := r.Context().Value(contextKey{}).(auth.User)
	return user
}

// authenticate returns the account behind the session cookie.
func (s *Server) authenticate(r *http.Request) (auth.User, bool) {
	cookie, err := r.Cookie(s.cookieName())
	if err != nil {
		return auth.User{}, false
	}
	session, ok := s.sessions.Get(cookie.Value)
	if !ok {
		return auth.User{}, false
	}
	// The account may have been removed since the login.
	return s.opts.Users.Get(session.User)
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, user)))
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).Role.CanAdminister() {
			writeError(w, http.StatusForbidden, "forbidden", "this needs an owner or administrator")
			return
		}
		next(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
