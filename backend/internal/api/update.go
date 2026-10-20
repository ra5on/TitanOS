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

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Update.Status())
}

func (s *Server) handleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"progress": s.opts.Update.CurrentProgress()})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings update.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.Update.SetSettings(settings); err != nil {
		writeUpdateError(w, err)
		return
	}
	slog.Info("update settings changed", "channel", settings.Channel, "auto", settings.AutoInstall, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleUpdate(w, r)
}

// handleUpdateAction looks for a release, installs it into the other slot,
// goes back to the system in the other slot, or takes a pending switch back.
func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	var err error
	action := r.PathValue("action")
	switch action {
	case "check":
		err = s.opts.Update.Check(r.Context())
	case "install":
		err = s.opts.Update.Install(r.Context())
	case "rollback":
		err = s.opts.Update.Rollback()
	case "cancel":
		err = s.opts.Update.Cancel()
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
	s.handleUpdate(w, r)
}
