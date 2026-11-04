package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/scripts"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// registerScripts registers Tools › Scripts. Scripts run as root: every
// route, reading ones too (output may hold secrets), needs the web session
// of an owner or administrator. They are not in publicRoutes and not in
// the MCP tool catalog: what runs as root comes from the person only.
func (s *Server) registerScripts(mux *routeMux) {
	route := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.requireAdmin(s.browserOnly(s.scriptsAvailable(h))))
	}
	route("GET /api/v1/scripts", s.handleScripts)
	route("PUT /api/v1/scripts/volume", s.handleScriptsVolume)
	route("POST /api/v1/scripts", s.handleScriptCreate)
	route("GET /api/v1/scripts/{id}", s.handleScript)
	route("PUT /api/v1/scripts/{id}", s.handleScriptUpdate)
	route("DELETE /api/v1/scripts/{id}", s.handleScriptDelete)
	route("POST /api/v1/scripts/{id}/run", s.handleScriptRun)
	route("POST /api/v1/scripts/{id}/cancel", s.handleScriptCancel)
	route("GET /api/v1/scripts/{id}/output", s.handleScriptOutput)
	route("GET /api/v1/scripts/{id}/runs", s.handleScriptRuns)
}

var scriptID = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (s *Server) scriptsAvailable(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Scripts == nil {
			featureUnavailable(w)
			return
		}
		if id := r.PathValue("id"); id != "" && !scriptID.MatchString(id) {
			writeError(w, http.StatusNotFound, "script_not_found", "no such script")
			return
		}
		next(w, r)
	}
}

func scriptError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, scripts.ErrNotFound):
		writeError(w, http.StatusNotFound, "script_not_found", err.Error())
	case errors.Is(err, scripts.ErrRunning):
		writeError(w, http.StatusConflict, "script_running", err.Error())
	case errors.Is(err, scripts.ErrNotRunning):
		writeError(w, http.StatusConflict, "script_not_running", err.Error())
	case errors.Is(err, scripts.ErrUnavailable), errors.Is(err, volumedata.ErrUnavailable):
		writeError(w, http.StatusConflict, "script_volume_missing", err.Error())
	case errors.Is(err, scripts.ErrDisabled):
		writeError(w, http.StatusConflict, "dev_mode", err.Error())
	case errors.Is(err, scripts.ErrInvalid):
		writeError(w, http.StatusBadRequest, "script_invalid", err.Error())
	case errors.Is(err, scripts.ErrVolume):
		writeError(w, http.StatusBadRequest, "script_volume_invalid", err.Error())
	case errors.Is(err, scripts.ErrTooMany):
		writeError(w, http.StatusConflict, "script_too_many", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "script_failed", err.Error())
	}
}

func (s *Server) handleScripts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Scripts.Status())
}

func (s *Server) handleScriptsVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Volume string `json:"volume"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Volume)
	if err := s.opts.Scripts.SetVolume(req.Volume); err != nil {
		scriptError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Scripts.Status())
}

// readScript reads a draft; the content may be up to scripts.MaxContent,
// more than the usual JSON limit.
func readScript(w http.ResponseWriter, r *http.Request) (scripts.Draft, bool) {
	var d scripts.Draft
	r.Body = http.MaxBytesReader(w, r.Body, 2*scripts.MaxContent+maxJSONBody)
	return d, readJSONBody(w, r, &d)
}

type scriptAnswer struct {
	scripts.Script
	Content string `json:"content"`
}

func (s *Server) handleScriptCreate(w http.ResponseWriter, r *http.Request) {
	d, ok := readScript(w, r)
	if !ok {
		return
	}
	noteTarget(r, d.Name)
	created, err := s.opts.Scripts.Create(d)
	if err != nil {
		scriptError(w, err)
		return
	}
	content := ""
	if d.Content != nil {
		content = *d.Content
	}
	writeJSON(w, http.StatusCreated, scriptAnswer{created, content})
}

func (s *Server) handleScript(w http.ResponseWriter, r *http.Request) {
	script, content, err := s.opts.Scripts.Get(r.PathValue("id"))
	if err != nil {
		scriptError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scriptAnswer{script, content})
}

func (s *Server) handleScriptUpdate(w http.ResponseWriter, r *http.Request) {
	d, ok := readScript(w, r)
	if !ok {
		return
	}
	noteTarget(r, d.Name)
	changed, err := s.opts.Scripts.Update(r.PathValue("id"), d)
	if err != nil {
		scriptError(w, err)
		return
	}
	_, content, err := s.opts.Scripts.Get(changed.ID)
	if err != nil {
		scriptError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scriptAnswer{changed, content})
}

// scriptName names a script for the activity log.
func (s *Server) scriptName(r *http.Request) {
	for _, v := range s.opts.Scripts.Status().Scripts {
		if v.ID == r.PathValue("id") {
			noteTarget(r, v.Name)
		}
	}
}

func (s *Server) handleScriptDelete(w http.ResponseWriter, r *http.Request) {
	s.scriptName(r)
	if err := s.opts.Scripts.Delete(r.PathValue("id")); err != nil {
		scriptError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleScriptRun(w http.ResponseWriter, r *http.Request) {
	s.scriptName(r)
	if err := s.opts.Scripts.Run(r.PathValue("id")); err != nil {
		scriptError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleScriptCancel(w http.ResponseWriter, r *http.Request) {
	s.scriptName(r)
	if err := s.opts.Scripts.Cancel(r.PathValue("id")); err != nil {
		scriptError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleScriptOutput(w http.ResponseWriter, r *http.Request) {
	from, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	if from < 0 {
		from = 0
	}
	out, err := s.opts.Scripts.Output(r.PathValue("id"), from)
	if err != nil {
		scriptError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleScriptRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.opts.Scripts.Runs(r.PathValue("id"))
	if err != nil {
		scriptError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}
