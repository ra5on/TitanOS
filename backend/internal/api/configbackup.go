package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

func writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, configbackup.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_backup_settings", err.Error())
	case errors.Is(err, configbackup.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	case errors.Is(err, configbackup.ErrDisabled):
		writeError(w, http.StatusConflict, "read_only", err.Error())
	case errors.Is(err, configbackup.ErrNoVolume):
		writeError(w, http.StatusConflict, "backup_no_volume", err.Error())
	case errors.Is(err, configbackup.ErrNotFound):
		writeError(w, http.StatusNotFound, "backup_not_found", err.Error())
	case errors.Is(err, configbackup.ErrNotABackup):
		writeError(w, http.StatusBadRequest, "not_a_backup", err.Error())
	case errors.Is(err, configbackup.ErrPasswordRequired):
		writeError(w, http.StatusUnauthorized, "backup_password_required", err.Error())
	case errors.Is(err, configbackup.ErrWrongPassword):
		writeError(w, http.StatusUnauthorized, "backup_password_wrong", err.Error())
	case errors.Is(err, configbackup.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_backup_settings", err.Error())
	case errors.Is(err, configbackup.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	default:
		slog.Error("backup of the configuration failed", "err", err)
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
	}
}

// configBackupArea is the backup of the stick; an area with narrow
// dependencies (PLAN 8a, see fansArea). storage names the volumes a backup
// can go to. confirmed says whether the password was entered again for this
// request.
type configBackupArea struct {
	backup    *configbackup.Manager
	storage   *storage.Manager
	confirmed confirmedFunc
}

func (a configBackupArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/config-backup", admin(a.handleConfigBackup))
	mux.HandleFunc("PUT /api/v1/config-backup", admin(a.handleConfigBackupSettings))
	mux.HandleFunc("POST /api/v1/config-backup/run", admin(a.handleConfigBackupRun))
	mux.HandleFunc("GET /api/v1/config-backup/download", admin(a.handleConfigBackupDownload))
}

func (a configBackupArea) handleConfigBackup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"backup":      a.backup.State(a.storage.Mounted()),
		"volumes":     a.storage.Names(),
		"passwordSet": a.backup.PasswordSet(),
	})
}

func (a configBackupArea) handleConfigBackupSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		configbackup.Settings
		// Password for encrypted backups; empty keeps the one there is.
		Password string `json:"password,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	settings := req.Settings
	// Checked before the password is stored: a refused request must not
	// leave a new password behind that the person believes was rejected.
	if err := configbackup.CheckSettings(settings, a.storage.Names()); err != nil {
		writeBackupError(w, err)
		return
	}
	if req.Password != "" {
		if err := a.backup.SetPassword(req.Password); err != nil {
			writeBackupError(w, err)
			return
		}
	}
	if settings.Encrypt && !a.backup.PasswordSet() {
		writeError(w, http.StatusBadRequest, "backup_password_required", "a password is needed to encrypt backups")
		return
	}
	if err := a.backup.SetSettings(settings, a.storage.Names()); err != nil {
		writeBackupError(w, err)
		return
	}
	slog.Info("backup settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleConfigBackup(w, r)
}

func (a configBackupArea) handleConfigBackupRun(w http.ResponseWriter, r *http.Request) {
	if _, err := a.backup.Create(a.storage.Mounted()); err != nil {
		writeBackupError(w, err)
		return
	}
	a.handleConfigBackup(w, r)
}

// handleConfigBackupDownload hands out a backup, to keep a copy away from
// the NAS. It holds password hashes and every secret of the NAS, so the
// password has to be entered again first, as for a console.
func (a configBackupArea) handleConfigBackupDownload(w http.ResponseWriter, r *http.Request) {
	if !a.confirmed(r) {
		writeError(w, http.StatusForbidden, "confirm_required", "enter your password again to download a backup")
		return
	}
	name := r.URL.Query().Get("name")
	file, err := a.backup.Open(a.storage.Mounted(), r.URL.Query().Get("volume"), name)
	if err != nil {
		writeBackupError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeBackupError(w, err)
		return
	}
	slog.Info("backup of the configuration downloaded", "backup", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="titanos-`+name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, file)
}

// newStick reports whether this is a stick without accounts. Only then
// may a backup be put back, and it needs no login: there is nobody yet who
// could log in. A restore gives nobody access they did not have, it brings
// back the accounts of the backup.
func (au authArea) newStick(w http.ResponseWriter) bool {
	setupMu.Lock()
	defer setupMu.Unlock()
	return au.newStickLocked(w)
}

// setupMu keeps the first owner account and a restore of a backup apart:
// both are allowed only while there is no account, and only one may win.
var setupMu sync.Mutex

func (au authArea) newStickLocked(w http.ResponseWriter) bool {
	if !au.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return false
	}
	if au.users.Count() > 0 {
		writeError(w, http.StatusConflict, "setup_done", "this NAS is set up already")
		return false
	}
	return true
}

// setupLocal lets the first setup and the restore of a backup through only
// from the network the NAS stands in: loopback, private (also IPv6 ULA)
// and link-local addresses, and the networks of its own interfaces (a home
// network often has a public IPv6 prefix only). A NAS that is reachable
// from the internet before it is set up must not be taken over by the
// first stranger. A request a reverse proxy or tunnel passed on comes from
// the address of the proxy, which says nothing about who asks: it is
// refused as well.
func setupLocal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr, err := netip.ParseAddr(remoteIP(r))
		addr = addr.Unmap()
		if err != nil || viaProxy(r) || !(addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || onLink(addr)) {
			slog.Warn("setup from outside the home network refused", "ip", remoteIP(r))
			writeError(w, http.StatusForbidden, "setup_not_local", "set up the NAS from a device in the same network")
			return
		}
		next(w, r)
	}
}

// onLink reports whether an address is in a network of an interface of
// this machine.
func onLink(addr netip.Addr) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		prefix, err := netip.ParsePrefix(a.String())
		if err == nil && prefix.Bits() > 0 && prefix.Masked().Contains(addr) {
			return true
		}
	}
	return false
}

// handleSetupBackups lists the backups found on the disks of this machine.
func (au authArea) handleSetupBackups(w http.ResponseWriter, r *http.Request) {
	if !au.newStick(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": au.configBackup.Find(r.Context())})
}

// restored finishes a restore: the accounts are there now, and the NAS
// starts again to take everything else from the stick.
func (au authArea) restored(w http.ResponseWriter, r *http.Request, source string) {
	// From here on the setup is closed, even before the restart.
	if err := au.users.Load(); err != nil {
		slog.Warn("accounts not readable after the restore", "err", err)
	}
	slog.Info("configuration restored, restarting", "from", source, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
	au.goJob(func(context.Context) {
		time.Sleep(time.Second)
		if err := au.power.Reboot(); err != nil {
			slog.Error("restart after the restore failed", "err", err)
		}
	})
}

func (au authArea) handleSetupRestore(w http.ResponseWriter, r *http.Request) {
	setupMu.Lock()
	defer setupMu.Unlock()
	var req struct{ Device, Name, Password string }
	if !readJSON(w, r, &req) || !au.newStickLocked(w) {
		return
	}
	if _, err := au.configBackup.RestoreFound(r.Context(), req.Device, req.Name, req.Password); err != nil {
		writeBackupError(w, err)
		return
	}
	au.restored(w, r, req.Device+"/"+req.Name)
}

// setupUploadTimeout is how long a backup of the settings (a few MiB) may
// take to arrive during the first setup.
const setupUploadTimeout = 2 * time.Minute

// handleSetupRestoreUpload takes a backup file as the body of the request.
func (au authArea) handleSetupRestoreUpload(w http.ResponseWriter, r *http.Request) {
	// The body has a time limit: the server sets none (uploads of files
	// take long), and a device that starts the upload and then stalls
	// would hold the lock of the first setup for everyone else.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(setupUploadTimeout)) // not every writer supports it (tests)
	setupMu.Lock()
	defer setupMu.Unlock()
	if !au.newStickLocked(w) {
		return
	}
	if _, err := au.configBackup.RestoreUpload(r.Body, r.Header.Get("X-Backup-Password")); err != nil {
		writeBackupError(w, err)
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{}) // see above
	au.restored(w, r, "upload")
}
