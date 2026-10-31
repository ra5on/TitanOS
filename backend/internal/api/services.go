package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// Settings › Services (decided 31.10.2026): Docker, VMs and system
// containers can be switched off by whoever does not use them; the service
// then does not run. Docker keeps its own switch (docker/settings.json);
// VMs and LXC are in system/services.json, read once at the start. Data is
// never touched; older versions ignore the file.

const servicesFile = "system/services.json"

type serviceSettings struct {
	// DockerOff is set only when Docker was switched off here: a new NAS
	// has Docker not yet set up, and its tab is where that happens.
	DockerOff bool `json:"dockerOff,omitempty"`
	VMsOff    bool `json:"vmsOff,omitempty"`
	LXCOff    bool `json:"lxcOff,omitempty"`
}

type serviceView struct {
	Enabled bool `json:"enabled"`
	Running int  `json:"running"`
	Total   int  `json:"total"`
	// Available is false when the service cannot run here at all.
	Available bool `json:"available"`
}

// readServices returns the switches, read from the stick once and then
// kept in memory: the navigation and MCP ask often.
func (s *Server) readServices() serviceSettings {
	s.services.mu.Lock()
	defer s.services.mu.Unlock()
	if !s.services.loaded {
		var settings serviceSettings
		if err := s.opts.Store.ReadJSON(servicesFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("service settings not readable", "err", err)
			return settings
		}
		s.services.settings, s.services.loaded = settings, true
	}
	return s.services.settings
}

func (s *Server) writeServices(settings serviceSettings) error {
	s.services.mu.Lock()
	defer s.services.mu.Unlock()
	if err := s.opts.Store.WriteJSON(servicesFile, settings); err != nil {
		return err
	}
	s.services.settings, s.services.loaded = settings, true
	return nil
}

type servicesState struct {
	mu       sync.Mutex
	loaded   bool
	settings serviceSettings
}

// servicesOn are the switches for everyone, for the navigation.
func (s *Server) servicesOn() map[string]bool {
	on := map[string]bool{"docker": true, "vms": true, "lxc": true}
	if s.opts.Docker != nil {
		on["docker"] = !s.readServices().DockerOff
	}
	if s.opts.VMs != nil {
		on["vms"] = !s.opts.VMs.Off()
	}
	if s.opts.LXC != nil {
		on["lxc"] = !s.opts.LXC.Off()
	}
	return on
}

func (s *Server) handleServicesState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.servicesOn())
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	mounted := s.opts.Storage.Mounted()
	out := map[string]serviceView{}
	if d := s.opts.Docker; d != nil {
		status := d.Status(r.Context())
		v := serviceView{Enabled: !s.readServices().DockerOff, Total: len(status.Containers), Available: true}
		for _, c := range status.Containers {
			if c.State == "running" {
				v.Running++
			}
		}
		out["docker"] = v
	}
	if m := s.opts.VMs; m != nil {
		out["vms"] = serviceView{Enabled: !m.Off(), Running: len(m.RunningNames()), Total: len(m.List(mounted)), Available: m.HasKVM()}
	}
	if m := s.opts.LXC; m != nil {
		out["lxc"] = serviceView{Enabled: !m.Off(), Running: len(m.RunningNames()), Total: len(m.List(mounted)), Available: m.Installed()}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleServiceSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	noteTarget(r, name)
	ctx := context.WithoutCancel(r.Context())
	switch name {
	case "docker":
		// On again: the engine runs where it is set up; without a volume
		// the Docker page asks for one.
		settings := s.opts.Docker.Current()
		if req.Enabled == settings.Enabled || !req.Enabled || settings.Volume != "" {
			settings.Enabled = req.Enabled && settings.Volume != ""
			if err := s.opts.Docker.SetSettings(settings, s.opts.Storage.Names()); err != nil && !errors.Is(err, docker.ErrDisabled) {
				writeDockerError(w, err)
				return
			}
		}
		services := s.readServices()
		services.DockerOff = !req.Enabled
		if err := s.writeServices(services); err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
			return
		}
	case "vms", "lxc":
		settings := s.readServices()
		if name == "vms" {
			settings.VMsOff = !req.Enabled
		} else {
			settings.LXCOff = !req.Enabled
		}
		if err := s.writeServices(settings); err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
			return
		}
		var err error
		if name == "vms" {
			err = s.opts.VMs.SetOff(ctx, !req.Enabled)
		} else {
			err = s.opts.LXC.SetOff(ctx, !req.Enabled, s.opts.Storage.Mounted())
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "service_switch_failed", err.Error())
			return
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such service")
		return
	}
	slog.Info("service switched", "service", name, "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleServices(w, r)
}
