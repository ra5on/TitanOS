package api

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/fans"
)

// Settings › Temperatures › Fans (docs/FANS.md).

func (s *Server) handleFans(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"fans": s.opts.Fans.Fans(), "readOnly": s.opts.Fans.Disabled})
}

func (s *Server) handleFansSet(w http.ResponseWriter, r *http.Request) {
	var req fans.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Fans.Set(req); err != nil {
		if errors.Is(err, fans.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_fans", err.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	s.handleFans(w, r)
}
