package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

// memoryArea is the memory budget; an area with narrow dependencies
// (PLAN 8a, see fansArea).
type memoryArea struct {
	memory *system.MemoryPolicy
}

func (a memoryArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/memory", admin(a.handleMemoryPolicy))
	mux.HandleFunc("PUT /api/v1/system/memory", admin(a.handleMemoryPolicyChange))
}

func (a memoryArea) handleMemoryPolicy(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		writeError(w, 503, "config_unavailable", "memory settings unavailable")
		return
	}
	writeJSON(w, 200, a.memory.Status())
}
func (a memoryArea) handleMemoryPolicyChange(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		writeError(w, 503, "config_unavailable", "memory settings unavailable")
		return
	}
	if a.memory.Disabled {
		writeError(w, 409, "read_only", "development mode")
		return
	}
	var settings system.MemorySettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.memory.Set(settings); err != nil {
		status := 500
		if errors.Is(err, system.ErrMemoryPercent) {
			status = 400
		}
		writeError(w, status, "memory_policy_failed", err.Error())
		return
	}
	slog.Info("memory budget changed", "by", currentUser(r).Name, "percent", settings.CachePercent)
	a.handleMemoryPolicy(w, r)
}
