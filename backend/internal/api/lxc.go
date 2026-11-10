package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
)

var lxcErrors = []struct {
	err    error
	status int
	code   string
}{
	{lxc.ErrServiceOff, http.StatusConflict, "service_off"},
	{lxc.ErrDisabled, http.StatusConflict, "read_only"},
	{lxc.ErrUnavailable, http.StatusConflict, "lxc_unavailable"},
	{lxc.ErrInvalidName, http.StatusBadRequest, "invalid_lxc_name"},
	{lxc.ErrInvalidDisplayName, http.StatusBadRequest, "invalid_lxc_display_name"},
	{lxc.ErrExists, http.StatusConflict, "lxc_exists"},
	{lxc.ErrNotFound, http.StatusNotFound, "lxc_not_found"},
	{lxc.ErrInvalid, http.StatusBadRequest, "invalid_lxc"},
	{lxc.ErrUnknownVolume, http.StatusConflict, "lxc_volume_unavailable"},
	{lxc.ErrUnknownImage, http.StatusBadRequest, "lxc_unknown_image"},
	{lxc.ErrRunning, http.StatusConflict, "lxc_running"},
	{lxc.ErrNotRunning, http.StatusConflict, "lxc_not_running"},
	{lxc.ErrNotConfirmed, http.StatusBadRequest, "not_confirmed"},
	{lxc.ErrCreating, http.StatusConflict, "lxc_creating"},
	{lxc.ErrBusy, http.StatusConflict, "lxc_busy"},
	{lxc.ErrNoBridge, http.StatusConflict, "lxc_no_bridge"},
	{lxc.ErrBadFolder, http.StatusBadRequest, "invalid_folder"},
	{lxc.ErrMounted, http.StatusConflict, "lxc_mounted"},
	{lxc.ErrNoSnapshots, http.StatusConflict, "lxc_no_snapshots"},
	{lxc.ErrSnapshotNotFound, http.StatusNotFound, "lxc_snapshot_not_found"},
}

func writeLXCError(w http.ResponseWriter, err error) {
	for _, known := range lxcErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	// What LXC or systemd said is the useful part.
	var command *run.CommandError
	if errors.As(err, &command) {
		writeError(w, http.StatusInternalServerError, "lxc_failed", command.Output)
		return
	}
	slog.Error("container action failed", "err", err)
	writeError(w, http.StatusInternalServerError, "lxc_failed", err.Error())
}

// lxcArea is the system containers; an area with narrow dependencies
// (PLAN 8a, see fansArea). storage names the mounted volumes, network the
// bridge and the connections, monitor the memory of the host. serveConsole
// connects a WebSocket to a shell with the rules of every console.
type lxcArea struct {
	lxc          *lxc.Manager
	storage      *storage.Manager
	network      *network.Manager
	monitor      *system.Monitor
	serveConsole consoleFunc
}

func (a lxcArea) register(mux *routeMux, admin, appAdmin, heavyAppAdmin, appConsole func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/lxc", admin(a.handleLXC))
	mux.HandleFunc("GET /api/v1/lxc/images", admin(a.handleLXCImages))
	mux.HandleFunc("POST /api/v1/lxc", heavyAppAdmin(a.handleLXCCreate))
	mux.HandleFunc("PUT /api/v1/lxc/{name}", appAdmin(a.handleLXCUpdate))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}", appAdmin(a.handleLXCDelete))
	mux.HandleFunc("GET /api/v1/lxc/{name}/config", admin(a.handleLXCConfig))
	mux.HandleFunc("GET /api/v1/lxc/{name}/console", appConsole(a.handleLXCConsole))
	mux.HandleFunc("GET /api/v1/lxc/{name}/snapshots", admin(a.handleLXCSnapshots))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots", heavyAppAdmin(a.handleLXCSnapshotCreate))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots/{snapshot}/restore", heavyAppAdmin(a.handleLXCSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}/snapshots/{snapshot}", heavyAppAdmin(a.handleLXCSnapshotDelete))
	mux.HandleFunc("POST /api/v1/lxc/{name}/{action}", appAdmin(a.handleLXCAction))
}

// handleLXC lists the system containers with what the create dialog needs.
func (a lxcArea) handleLXC(w http.ResponseWriter, r *http.Request) {
	memory := a.monitor.Info().Memory
	writeJSON(w, http.StatusOK, map[string]any{
		"installed": a.lxc.Installed(),
		"dev":       a.lxc.Disabled,
		"host": map[string]any{
			"cpus":        runtime.NumCPU(),
			"memoryBytes": memory.TotalBytes,
		},
		"bridge":     a.network.BridgeReady(),
		"networks":   a.network.Connections(),
		"containers": a.lxc.List(a.storage.Mounted()),
	})
}

// handleLXCImages lists the images of images.linuxcontainers.org, kept in RAM.
func (a lxcArea) handleLXCImages(w http.ResponseWriter, r *http.Request) {
	images, err := a.lxc.Images(r.Context())
	message := ""
	if err != nil {
		message = err.Error()
	}
	if images == nil {
		images = []lxc.Image{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": images, "error": message})
}

func (a lxcArea) handleLXCCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		lxc.Container
		// Start starts the container once it was made.
		Start bool `json:"start"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	// What is decided by TitanOS, not by the request.
	req.Btrfs = false
	if err := a.lxc.Create(r.Context(), req.Container, a.storage.Mounted()); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container being created", "container", req.Name, "image", req.Distribution+"/"+req.Release, "privileged", req.Privileged, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (a lxcArea) handleLXCUpdate(w http.ResponseWriter, r *http.Request) {
	var change lxc.Change
	if !readJSON(w, r, &change) {
		return
	}
	name := r.PathValue("name")
	noteTarget(r, name)
	if err := a.lxc.Update(name, change); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container changed", "container", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a lxcArea) handleLXCDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeleteData bool   `json:"deleteData"`
		Confirm    string `json:"confirm"`
	}
	// A body is only needed for deleting the data as well.
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	noteTarget(r, name)
	// With its data: only while its volume is there (see vm.Manager.Delete).
	if req.DeleteData {
		mounted := a.storage.Mounted()
		for _, c := range a.lxc.List(nil) {
			if c.Name == name && !slices.Contains(mounted, c.Volume) {
				writeLXCError(w, lxc.ErrUnknownVolume)
				return
			}
		}
	}
	if err := a.lxc.Delete(r.Context(), name, req.DeleteData, req.Confirm); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container removed", "container", name, "data deleted", req.DeleteData, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a lxcArea) handleLXCAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	noteTarget(r, name)
	// Stopping waits for the container to shut down; a closed page must not break that off.
	if err := a.lxc.Action(context.WithoutCancel(r.Context()), name, action, a.storage.Mounted()); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container "+action, "container", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleLXCConfig shows the LXC configuration the next start writes.
func (a lxcArea) handleLXCConfig(w http.ResponseWriter, r *http.Request) {
	text, err := a.lxc.GeneratedConfig(r.PathValue("name"))
	if err != nil {
		writeLXCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config": text})
}

// handleLXCConsole connects a WebSocket to a shell inside a running
// container (mode=shell, the default) or to its text console
// (mode=console), with the same rules as every console.
func (a lxcArea) handleLXCConsole(w http.ResponseWriter, r *http.Request) {
	name, mode := r.PathValue("name"), r.URL.Query().Get("mode")
	command, err := a.lxc.ConsoleCommand(name, mode)
	if err != nil {
		writeLXCError(w, err)
		return
	}
	a.serveConsole(w, r, "lxc "+name, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		session, err := terminal.Start(command)
		if err != nil {
			return nil, nil, err
		}
		return session, session.Resize, nil
	})
}

func (a lxcArea) handleLXCSnapshots(w http.ResponseWriter, r *http.Request) {
	list, err := a.lxc.Snapshots(r.PathValue("name"))
	if err != nil {
		writeLXCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": list})
}

func (a lxcArea) handleLXCSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	noteTarget(r, name)
	snapshot, err := a.lxc.CreateSnapshot(context.WithoutCancel(r.Context()), name)
	if err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container snapshot taken", "container", name, "snapshot", snapshot.ID, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, snapshot)
}

func (a lxcArea) handleLXCSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	name, id := r.PathValue("name"), r.PathValue("snapshot")
	noteTarget(r, name+"@"+id)
	if err := a.lxc.RestoreSnapshot(context.WithoutCancel(r.Context()), name, id); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container snapshot restored", "container", name, "snapshot", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a lxcArea) handleLXCSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	name, id := r.PathValue("name"), r.PathValue("snapshot")
	noteTarget(r, name+"@"+id)
	if err := a.lxc.DeleteSnapshot(context.WithoutCancel(r.Context()), name, id); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container snapshot deleted", "container", name, "snapshot", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}
