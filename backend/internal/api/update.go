package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/update"
)

var updateErrors = []struct {
	err    error
	status int
	code   string
}{
	{update.ErrDisabled, http.StatusConflict, "read_only"},
	{update.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{update.ErrInvalid, http.StatusBadRequest, "invalid_update_settings"},
	{update.ErrNoKey, http.StatusConflict, "update_no_key"},
	{update.ErrSignature, http.StatusBadGateway, "update_bad_signature"},
	{update.ErrManifest, http.StatusBadGateway, "update_bad_manifest"},
	{update.ErrBusy, http.StatusConflict, "update_busy"},
	{update.ErrNothing, http.StatusConflict, "update_nothing"},
	{update.ErrTooOld, http.StatusConflict, "update_too_old"},
	{update.ErrNoSpace, http.StatusConflict, "update_no_space"},
	{update.ErrUnconfirmed, http.StatusConflict, "update_unconfirmed"},
	{update.ErrWorking, http.StatusConflict, "update_working"},
}

func writeUpdateError(w http.ResponseWriter, err error) {
	for _, known := range updateErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	// The project page could not be reached, or the stick refused a write.
	writeError(w, http.StatusBadGateway, "update_failed", err.Error())
}

// updateArea is the updates of TitanOS; an area with narrow dependencies
// (PLAN 8a, see fansArea).
type updateArea struct {
	update *update.Manager
}

func (a updateArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/update", admin(a.handleUpdate))
	mux.HandleFunc("GET /api/v1/update/progress", admin(a.handleUpdateProgress))
	mux.HandleFunc("PUT /api/v1/update/settings", admin(a.handleUpdateSettings))
	mux.HandleFunc("GET /api/v1/update/versions", admin(a.handleUpdateVersions))
	mux.HandleFunc("POST /api/v1/update/install-version", admin(a.handleUpdateInstallVersion))
	mux.HandleFunc("POST /api/v1/update/{action}", admin(a.handleUpdateAction))
}

func (a updateArea) handleUpdate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.update.Status())
}

func (a updateArea) handleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"progress": a.update.CurrentProgress()})
}

func (a updateArea) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings update.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.update.SetSettings(settings); err != nil {
		writeUpdateError(w, err)
		return
	}
	slog.Info("update settings changed", "channel", settings.Channel, "auto", settings.AutoInstall, "checkEvery", settings.CheckEvery, "kernel", settings.Kernel, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUpdate(w, r)
}

// handleUpdateAction looks for a release, installs it into the other slot,
// goes back to the system in the other slot, or takes a pending switch back.
func (a updateArea) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	var err error
	action := r.PathValue("action")
	switch action {
	case "check":
		err = a.update.Check(r.Context())
	case "install":
		err = a.update.Install(r.Context())
	case "rollback":
		err = a.update.Rollback()
	case "cancel":
		err = a.update.Cancel()
	case "app-rollback":
		err = a.update.AppRollback()
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such action")
		return
	}
	if err != nil {
		writeUpdateError(w, err)
		return
	}
	if action != "check" {
		slog.Info("update action", "action", action, "by", currentUser(r).Name, "ip", remoteIP(r))
	}
	a.handleUpdate(w, r)
}

// handleUpdateVersions lists the releases that can be chosen.
func (a updateArea) handleUpdateVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := a.update.Versions(r.Context())
	if err != nil {
		writeUpdateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions, "floor": update.ChoiceFloor})
}

// handleUpdateInstallVersion installs a chosen release, newer or older.
func (a updateArea) handleUpdateInstallVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.update.InstallVersion(r.Context(), req.Version); err != nil {
		writeUpdateError(w, err)
		return
	}
	slog.Info("update action", "action", "install-version", "version", req.Version, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUpdate(w, r)
}
