package api

// PLAN 73: GET/PUT /api/v1/system/startup – when Docker apps and VMs start
// after boot (internal/startup). Owners and administrators only.

import (
	"encoding/json"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/startup"
)

func (s *Server) handleStartup(w http.ResponseWriter, r *http.Request) {
	if s.opts.Startup == nil {
		writeJSON(w, http.StatusOK, map[string]any{"settings": startup.Settings{Order: []startup.Entry{}}, "readySince": nil})
		return
	}
	var since any
	if t := s.opts.Startup.ReadySince(); !t.IsZero() {
		since = t
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.opts.Startup.Settings(), "readySince": since})
}

func (s *Server) handlePutStartup(w http.ResponseWriter, r *http.Request) {
	if s.opts.Startup == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "not available")
		return
	}
	var in startup.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid settings")
		return
	}
	if err := s.opts.Startup.Save(in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	noteTarget(r, "startup")
	writeJSON(w, http.StatusOK, s.opts.Startup.Settings())
}
