package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/vm"
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

// servicesArea is the switches of Docker, VMs and system containers; an
// area with narrow dependencies (PLAN 8a, see fansArea). store holds the
// switches, state what was read from it (state of the server), storage
// names the volumes. agentOn says whether the KI-Agent is on for an account.
type servicesArea struct {
	docker  *docker.Manager
	vms     *vm.Manager
	lxc     *lxc.Manager
	store   *config.Store
	storage *storage.Manager
	state   *servicesState
	agentOn func(user auth.User) bool
}

// servicesArea is the area of the server; the tools of MCP and of the
// KI-Agent ask it which services are on.
func (s *Server) servicesArea() servicesArea {
	return servicesArea{docker: s.opts.Docker, vms: s.opts.VMs, lxc: s.opts.LXC, store: s.opts.Store, storage: s.opts.Storage, state: &s.services,
		agentOn: func(user auth.User) bool { return s.agentSettings().Enabled && s.agentAllowed(user) }}
}

func (a servicesArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/services/state", g.user(a.handleServicesState))
	mux.HandleFunc("GET /api/v1/services", g.admin(a.handleServices))
	mux.HandleFunc("PUT /api/v1/services/{name}", g.admin(a.handleServiceSet))
}

// readServices returns the switches, read from the stick once and then
// kept in memory: the navigation and MCP ask often.
func (a servicesArea) readServices() serviceSettings {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if !a.state.loaded {
		var settings serviceSettings
		if err := a.store.ReadJSON(servicesFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("service settings not readable", "err", err)
			return settings
		}
		a.state.settings, a.state.loaded = settings, true
	}
	return a.state.settings
}

// updateServices changes the switches under one lock: two switches set at
// the same time must not each write their own copy of the file and lose
// the other's change. A file that cannot be read is not written over.
func (a servicesArea) updateServices(change func(*serviceSettings)) error {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if !a.state.loaded {
		var settings serviceSettings
		if err := a.store.ReadJSON(servicesFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		a.state.settings, a.state.loaded = settings, true
	}
	settings := a.state.settings
	change(&settings)
	if err := a.store.WriteJSON(servicesFile, settings); err != nil {
		return err
	}
	a.state.settings = settings
	return nil
}

type servicesState struct {
	mu       sync.Mutex
	loaded   bool
	settings serviceSettings
}

// servicesOn are the switches for everyone, for the navigation.
func (a servicesArea) servicesOn() map[string]bool {
	on := map[string]bool{"docker": true, "vms": true, "lxc": true}
	if a.docker != nil {
		on["docker"] = !a.readServices().DockerOff
	}
	if a.vms != nil {
		on["vms"] = !a.vms.Off()
	}
	if a.lxc != nil {
		on["lxc"] = !a.lxc.Off()
	}
	return on
}

func (a servicesArea) handleServicesState(w http.ResponseWriter, r *http.Request) {
	on := a.servicesOn()
	out := make(map[string]bool, len(on)+1)
	for k, v := range on {
		out[k] = v
	}
	// The tab of the KI-Agent: switched on and allowed for this account.
	out["agent"] = a.agentOn(currentUser(r))
	writeJSON(w, http.StatusOK, out)
}

func (a servicesArea) handleServices(w http.ResponseWriter, r *http.Request) {
	mounted := a.storage.Mounted()
	out := map[string]serviceView{}
	if d := a.docker; d != nil {
		status := d.Status(r.Context())
		v := serviceView{Enabled: !a.readServices().DockerOff, Total: len(status.Containers), Available: true}
		for _, c := range status.Containers {
			if c.State == "running" {
				v.Running++
			}
		}
		out["docker"] = v
	}
	if m := a.vms; m != nil {
		out["vms"] = serviceView{Enabled: !m.Off(), Running: len(m.RunningNames()), Total: len(m.List(mounted)), Available: m.HasKVM()}
	}
	if m := a.lxc; m != nil {
		out["lxc"] = serviceView{Enabled: !m.Off(), Running: len(m.RunningNames()), Total: len(m.List(mounted)), Available: m.Installed()}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a servicesArea) handleServiceSet(w http.ResponseWriter, r *http.Request) {
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
		settings := a.docker.Current()
		if req.Enabled == settings.Enabled || !req.Enabled || settings.Volume != "" {
			settings.Enabled = req.Enabled && settings.Volume != ""
			if err := a.docker.SetSettings(settings, a.storage.Names()); err != nil && !errors.Is(err, docker.ErrDisabled) {
				writeDockerError(w, err)
				return
			}
		}
		if err := a.updateServices(func(v *serviceSettings) { v.DockerOff = !req.Enabled }); err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
			return
		}
	case "vms", "lxc":
		if err := a.updateServices(func(v *serviceSettings) {
			if name == "vms" {
				v.VMsOff = !req.Enabled
			} else {
				v.LXCOff = !req.Enabled
			}
		}); err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
			return
		}
		var err error
		if name == "vms" {
			err = a.vms.SetOff(ctx, !req.Enabled)
		} else {
			err = a.lxc.SetOff(ctx, !req.Enabled, a.storage.Mounted())
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
	a.handleServices(w, r)
}
