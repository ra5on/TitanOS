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
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/vm"
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
	// Terminal hands out the shell on the NAS and knows whether consoles are allowed.
	Terminal     *terminal.Manager
	VMs          *vm.Manager
	Hardware     *hardware.Manager
	Accelerators *hardware.AcceleratorMonitor
	Plugins      *plugins.Manager
	Network      *network.Manager
	Notify       *notify.Manager
	// ConfigBackup saves /config to a volume and restores it on a new stick.
	ConfigBackup *configbackup.Manager
	// Diagnose reads the logs and packs bundles for bug reports.
	Diagnose *diagnose.Manager
	// SSH switches the SSH access; Certificate holds the certificate of
	// the web UI and is nil in development mode, where there is no TLS.
	SSH         *sshd.Manager
	Certificate *tlscert.Manager
	// Update installs new versions into the other slot of the stick.
	Update *update.Manager
	// Backup copies shares to another volume, server or the cloud.
	Backup *backup.Manager
	// UPS watches the uninterruptible power supply.
	UPS      *ups.Manager
	Settings *system.Settings
	Power    system.Power
	Version  string
	// ShareDir holds the texts of the licenses.
	ShareDir string
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
	// A new stick may take the configuration from a backup instead.
	mux.HandleFunc("GET /api/v1/setup/backups", s.handleSetupBackups)
	mux.HandleFunc("POST /api/v1/setup/restore", s.handleSetupRestore)
	mux.HandleFunc("POST /api/v1/setup/restore/upload", s.handleSetupRestoreUpload)
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
	mux.HandleFunc("GET /api/v1/storage/disk-sleep", s.requireAdmin(s.handleDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/disk-sleep", s.requireAdmin(s.handleSetDiskSleep))
	mux.HandleFunc("POST /api/v1/storage/rescan", s.requireAdmin(s.handleStorageRescan))
	mux.HandleFunc("POST /api/v1/storage/volumes", s.requireAdmin(s.handleCreateVolume))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireAdmin(s.handleDeleteVolume))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/disks", s.requireAdmin(s.handleAddVolumeDisk))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/disks/{disk}", s.requireAdmin(s.handleRemoveVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/check", s.requireAdmin(s.handleCheckVolume))
	mux.HandleFunc("GET /api/v1/storage/tests", s.requireAdmin(s.handleDiskTests))
	mux.HandleFunc("POST /api/v1/storage/tests", s.requireAdmin(s.handleDiskTestStart))
	mux.HandleFunc("DELETE /api/v1/storage/tests/{disk}", s.requireAdmin(s.handleDiskTestStop))

	mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PUT /api/v1/users/{name}", s.requireAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{name}", s.requireAdmin(s.handleDeleteUser))
	// Everyone may change their own password.
	mux.HandleFunc("POST /api/v1/users/{name}/password", s.requireUser(s.handleSetPassword))

	// One's own account.
	mux.HandleFunc("GET /api/v1/account/sessions", s.requireUser(s.handleAccountSessions))
	mux.HandleFunc("DELETE /api/v1/account/sessions/{id}", s.requireUser(s.handleAccountSessionEnd))
	mux.HandleFunc("POST /api/v1/account/2fa/begin", s.requireUser(s.handleTwoFactorBegin))
	mux.HandleFunc("POST /api/v1/account/2fa/enable", s.requireUser(s.handleTwoFactorEnable))
	mux.HandleFunc("POST /api/v1/account/2fa/disable", s.requireUser(s.handleTwoFactorDisable))
	mux.HandleFunc("GET /api/v1/account/ssh-keys", s.requireUser(s.handleAccountKeys))
	mux.HandleFunc("POST /api/v1/account/ssh-keys", s.requireUser(s.handleAccountKeyAdd))
	mux.HandleFunc("DELETE /api/v1/account/ssh-keys/{id}", s.requireUser(s.handleAccountKeyRemove))

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

	// Virtual machines. Like containers: administrators only.
	mux.HandleFunc("GET /api/v1/vms", s.requireAdmin(s.handleVMs))
	mux.HandleFunc("POST /api/v1/vms", s.requireAdmin(s.handleVMCreate))
	mux.HandleFunc("GET /api/v1/vms/media", s.requireAdmin(s.handleVMMedia))
	mux.HandleFunc("GET /api/v1/vms/host", s.requireAdmin(s.handleVMHost))
	mux.HandleFunc("GET /api/v1/hardware", s.requireAdmin(s.handleHardware))
	mux.HandleFunc("POST /api/v1/vms/{name}/grow", s.requireAdmin(s.handleVMGrow))
	mux.HandleFunc("GET /api/v1/vms/{name}/serial", s.requireAdmin(s.handleVMSerial))
	mux.HandleFunc("PUT /api/v1/vms/{name}", s.requireAdmin(s.handleVMUpdate))
	mux.HandleFunc("DELETE /api/v1/vms/{name}", s.requireAdmin(s.handleVMDelete))
	mux.HandleFunc("GET /api/v1/vms/{name}/xml", s.requireAdmin(s.handleVMXML))
	mux.HandleFunc("GET /api/v1/vms/{name}/screen", s.requireAdmin(s.handleVMScreen))
	mux.HandleFunc("POST /api/v1/vms/{name}/{action}", s.requireAdmin(s.handleVMAction))

	// How the NAS is connected. Changes are on trial until confirmed.
	mux.HandleFunc("GET /api/v1/network", s.requireAdmin(s.handleNetwork))
	mux.HandleFunc("PUT /api/v1/network", s.requireAdmin(s.handleNetworkChange))
	mux.HandleFunc("POST /api/v1/network/confirm", s.requireAdmin(s.handleNetworkConfirm))
	mux.HandleFunc("POST /api/v1/network/revert", s.requireAdmin(s.handleNetworkRevert))
	mux.HandleFunc("GET /api/v1/notifications", s.requireAdmin(s.handleNotifications))
	mux.HandleFunc("GET /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettings))
	mux.HandleFunc("PUT /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettingsChange))
	mux.HandleFunc("POST /api/v1/notifications/test", s.requireAdmin(s.handleNotifyTest))
	mux.HandleFunc("GET /api/v1/config-backup", s.requireAdmin(s.handleConfigBackup))
	mux.HandleFunc("PUT /api/v1/config-backup", s.requireAdmin(s.handleConfigBackupSettings))
	mux.HandleFunc("POST /api/v1/config-backup/run", s.requireAdmin(s.handleConfigBackupRun))
	mux.HandleFunc("GET /api/v1/config-backup/download", s.requireAdmin(s.handleConfigBackupDownload))
	mux.HandleFunc("GET /api/v1/logs", s.requireAdmin(s.handleLogs))
	mux.HandleFunc("GET /api/v1/diagnostics/bundle", s.requireAdmin(s.handleDiagnosticBundle))
	mux.HandleFunc("GET /api/v1/about", s.requireUser(s.handleAbout))

	// Backups of shares. They hold everyone's files: administrators only.
	mux.HandleFunc("GET /api/v1/backups", s.requireAdmin(s.handleBackups))
	mux.HandleFunc("POST /api/v1/backups", s.requireAdmin(s.handleBackupCreate))
	mux.HandleFunc("POST /api/v1/backups/ssh-key", s.requireAdmin(s.handleBackupKey))
	mux.HandleFunc("PUT /api/v1/backups/{id}", s.requireAdmin(s.handleBackupUpdate))
	mux.HandleFunc("DELETE /api/v1/backups/{id}", s.requireAdmin(s.handleBackupDelete))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots", s.requireAdmin(s.handleBackupSnapshots))
	mux.HandleFunc("GET /api/v1/backups/{id}/password", s.requireAdmin(s.handleBackupPassword))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", s.requireAdmin(s.handleBackupRestore))
	mux.HandleFunc("POST /api/v1/backups/{id}/{action}", s.requireAdmin(s.handleBackupAction))
	mux.HandleFunc("GET /api/v1/ups", s.requireAdmin(s.handleUPS))
	mux.HandleFunc("PUT /api/v1/ups", s.requireAdmin(s.handleUPSChange))
	mux.HandleFunc("POST /api/v1/ups/scan", s.requireAdmin(s.handleUPSScan))
	mux.HandleFunc("GET /api/v1/update", s.requireAdmin(s.handleUpdate))
	mux.HandleFunc("PUT /api/v1/update/settings", s.requireAdmin(s.handleUpdateSettings))
	mux.HandleFunc("POST /api/v1/update/{action}", s.requireAdmin(s.handleUpdateAction))
	mux.HandleFunc("GET /api/v1/system/ssh", s.requireAdmin(s.handleSSH))
	mux.HandleFunc("PUT /api/v1/system/ssh", s.requireAdmin(s.handleSSHChange))
	mux.HandleFunc("GET /api/v1/system/certificate", s.requireAdmin(s.handleCertificate))
	mux.HandleFunc("PUT /api/v1/system/certificate", s.requireAdmin(s.handleCertificateSet))
	mux.HandleFunc("DELETE /api/v1/system/certificate", s.requireAdmin(s.handleCertificateRemove))
	mux.HandleFunc("PUT /api/v1/system/timezone", s.requireAdmin(s.handleTimezone))
	mux.HandleFunc("PUT /api/v1/system/hostname", s.requireAdmin(s.handleHostname))

	// Plugins become part of the system: administrators only.
	mux.HandleFunc("GET /api/v1/plugins", s.requireAdmin(s.handlePlugins))
	mux.HandleFunc("POST /api/v1/plugins", s.requireAdmin(s.handlePluginInstall))
	mux.HandleFunc("POST /api/v1/plugins/{name}/{action}", s.requireAdmin(s.handlePluginAction))
	mux.HandleFunc("DELETE /api/v1/plugins/{name}", s.requireAdmin(s.handlePluginRemove))

	// Shells on the NAS and in containers.
	mux.HandleFunc("GET /api/v1/terminal", s.requireAdmin(s.handleTerminal))
	mux.HandleFunc("PUT /api/v1/terminal", s.requireAdmin(s.handleTerminalSettings))
	mux.HandleFunc("GET /api/v1/terminal/shell", s.requireAdmin(s.handleShell))
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

// handleHealth says that titand answers and, as "ready", that it has its
// configuration and volumes. After an update the boot check waits for
// that before the new system is marked good.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ready := s.opts.Users.Loaded() && s.opts.Storage.Settled() && (s.opts.Update == nil || s.opts.Update.Healthy())
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ready": ready})
}
