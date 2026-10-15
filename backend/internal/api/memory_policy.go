package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

func (s *Server) handleMemoryPolicy(w http.ResponseWriter, r *http.Request) {
	if s.opts.MemoryPolicy == nil {
		writeError(w, 503, "config_unavailable", "memory settings unavailable")
		return
	}
	writeJSON(w, 200, s.opts.MemoryPolicy.Status())
}
func (s *Server) handleMemoryPolicyChange(w http.ResponseWriter, r *http.Request) {
	if s.opts.MemoryPolicy == nil {
		writeError(w, 503, "config_unavailable", "memory settings unavailable")
		return
	}
	if s.opts.MemoryPolicy.Disabled {
		writeError(w, 409, "read_only", "development mode")
		return
	}
	var settings system.MemorySettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.MemoryPolicy.Set(settings); err != nil {
		status := 500
		if errors.Is(err, system.ErrMemoryPercent) {
			status = 400
		}
		writeError(w, status, "memory_policy_failed", err.Error())
		return
	}
	slog.Info("memory budget changed", "by", currentUser(r).Name, "percent", settings.CachePercent)
	s.handleMemoryPolicy(w, r)
}
