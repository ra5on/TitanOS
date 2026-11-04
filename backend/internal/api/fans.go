package api

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/fans"
)

// Settings › Temperatures › Fans (docs/FANS.md).
//
// fansArea is an area with narrow dependencies (PLAN 8a): it holds only what
// its handlers use instead of the whole Options. New areas follow this
// pattern; the others move over step by step.
type fansArea struct {
	fans *fans.Manager
}

func (a fansArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/fans", admin(a.handleFans))
	mux.HandleFunc("PUT /api/v1/system/fans", admin(a.handleFansSet))
}

func (a fansArea) handleFans(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"fans": a.fans.Fans(), "readOnly": a.fans.Disabled})
}

func (a fansArea) handleFansSet(w http.ResponseWriter, r *http.Request) {
	var req fans.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.fans.Set(req); err != nil {
		if errors.Is(err, fans.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_fans", err.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.handleFans(w, r)
}
