package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// Encrypted volumes (storage/crypt.go, docs/ENCRYPTION.md). Passwords come
// only from a logged-in browser session: neither API tokens nor the
// assistant (nor Home Assistant, which acts through the same in-process
// path) can unlock a volume or handle its passwords. Passwords are never
// logged; the activity log names only the volume.

// webSession reports a request made with the session cookie of a browser.
func webSession(r *http.Request) bool {
	if _, ok := r.Context().Value(apiTokenIdentity{}).(string); ok {
		return false
	}
	_, agent := r.Context().Value(agentIdentity{}).(string)
	return !agent
}

func (s *Server) requireWebSession(w http.ResponseWriter, r *http.Request) bool {
	// Through another node of the cluster the request came from a browser
	// session there; that node passes on nothing else (requireOwnSession).
	if viaCluster(r) {
		return true
	}
	if !webSession(r) || s.sessionToken(r) == "" {
		writeError(w, http.StatusForbidden, "web_session_required", "only possible on the web interface")
		return false
	}
	return true
}

// writeCryptError adds the waiting time to too many wrong passwords.
func writeCryptError(w http.ResponseWriter, r *http.Request, action string, err error) {
	var attempts storage.AttemptsError
	if errors.As(err, &attempts) {
		seconds := int(attempts.Wait/time.Second) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, http.StatusTooManyRequests, "volume_password_attempts", err.Error())
		return
	}
	writeStorageError(w, r, action, err)
}

func (s *Server) handleVolumeUnlock(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	password := []byte(req.Password)
	req.Password = ""
	defer wipeBytes(password)
	slog.Info("unlock requested", "volume", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.Unlock(context.WithoutCancel(r.Context()), name, password); err != nil {
		writeCryptError(w, r, "unlock volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVolumeLock(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	name := r.PathValue("name")
	if (s.opts.Files != nil && s.opts.Files.Busy()) || (s.opts.Backup != nil && s.opts.Backup.Busy()) || (s.opts.VMs != nil && s.opts.VMs.Busy()) || (s.opts.Media != nil && s.opts.Media.Working()) {
		writeError(w, http.StatusConflict, "volume_work_running", "wait for file, backup and VM operations")
		return
	}
	slog.Info("lock requested", "volume", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.Lock(context.WithoutCancel(r.Context()), name); err != nil {
		writeCryptError(w, r, "lock volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVolumePasswords adds, changes or removes a password (key slot).
func (s *Server) handleVolumePasswords(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	var req struct {
		Action   string `json:"action"` // add, change, remove
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	current, password := []byte(req.Current), []byte(req.Password)
	req.Current, req.Password = "", ""
	defer wipeBytes(current)
	defer wipeBytes(password)
	ctx := context.WithoutCancel(r.Context())
	var err error
	switch req.Action {
	case "add":
		err = s.opts.Storage.AddPassword(ctx, name, current, password)
	case "change":
		err = s.opts.Storage.ChangePassword(ctx, name, current, password)
	case "remove":
		err = s.opts.Storage.RemovePassword(ctx, name, current)
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "action must be add, change or remove")
		return
	}
	noteTarget(r, req.Action)
	slog.Info("volume password action", "volume", name, "action", req.Action, "user", currentUser(r).Name, "ip", remoteIP(r), "ok", err == nil)
	if err != nil {
		writeCryptError(w, r, "volume password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVolumeHeaderBackup downloads the LUKS headers of a volume as tar.
func (s *Server) handleVolumeHeaderBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	name := r.PathValue("name")
	if _, ok := s.volumeExists(name); !ok {
		writeError(w, http.StatusNotFound, "volume_not_found", "no such volume")
		return
	}
	slog.Info("LUKS header backup requested", "volume", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`-luks-header.tar"`)
	w.Header().Set("Cache-Control", "no-store")
	buffered := &headerWriter{w: w}
	if err := s.opts.Storage.HeaderBackup(r.Context(), name, buffered); err != nil {
		if !buffered.started {
			w.Header().Del("Content-Disposition")
			w.Header().Del("Content-Type")
			writeCryptError(w, r, "LUKS header backup", err)
			return
		}
		slog.Error("LUKS header backup broke off", "volume", name, "err", err)
	}
}

// headerWriter remembers whether anything was sent, so that an error before
// the first byte can still become a JSON answer.
type headerWriter struct {
	w       http.ResponseWriter
	started bool
}

func (h *headerWriter) Write(p []byte) (int, error) {
	h.started = true
	return h.w.Write(p)
}

func (s *Server) volumeExists(name string) (string, bool) {
	for _, existing := range s.opts.Storage.Names() {
		if existing == name {
			return existing, true
		}
	}
	return "", false
}

func wipeBytes(secret []byte) {
	for i := range secret {
		secret[i] = 0
	}
}
