package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime"

	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/run"
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

// handleLXC lists the system containers with what the create dialog needs.
func (s *Server) handleLXC(w http.ResponseWriter, r *http.Request) {
	memory := s.opts.Monitor.Info().Memory
	writeJSON(w, http.StatusOK, map[string]any{
		"installed": s.opts.LXC.Installed(),
		"dev":       s.opts.LXC.Disabled,
		"host": map[string]any{
			"cpus":        runtime.NumCPU(),
			"memoryBytes": memory.TotalBytes,
		},
		"bridge":     s.opts.Network.BridgeReady(),
		"networks":   s.opts.Network.Connections(),
		"containers": s.opts.LXC.List(s.opts.Storage.Mounted()),
	})
}

// handleLXCImages lists the images of images.linuxcontainers.org, kept in RAM.
func (s *Server) handleLXCImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.opts.LXC.Images(r.Context())
	message := ""
	if err != nil {
		message = err.Error()
	}
	if images == nil {
		images = []lxc.Image{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": images, "error": message})
}

func (s *Server) handleLXCCreate(w http.ResponseWriter, r *http.Request) {
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
	if err := s.opts.LXC.Create(r.Context(), req.Container, s.opts.Storage.Mounted()); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container being created", "container", req.Name, "image", req.Distribution+"/"+req.Release, "privileged", req.Privileged, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleLXCUpdate(w http.ResponseWriter, r *http.Request) {
	var change lxc.Change
	if !readJSON(w, r, &change) {
		return
	}
	name := r.PathValue("name")
	noteTarget(r, name)
	if err := s.opts.LXC.Update(name, change); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container changed", "container", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLXCDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := s.opts.LXC.Delete(r.Context(), name, req.DeleteData, req.Confirm); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container removed", "container", name, "data deleted", req.DeleteData, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLXCAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	noteTarget(r, name)
	// Stopping waits for the container to shut down; a closed page must not break that off.
	if err := s.opts.LXC.Action(context.WithoutCancel(r.Context()), name, action, s.opts.Storage.Mounted()); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container "+action, "container", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleLXCConfig shows the LXC configuration the next start writes.
func (s *Server) handleLXCConfig(w http.ResponseWriter, r *http.Request) {
	text, err := s.opts.LXC.GeneratedConfig(r.PathValue("name"))
	if err != nil {
		writeLXCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config": text})
}

// handleLXCConsole connects a WebSocket to a shell inside a running
// container (mode=shell, the default) or to its text console
// (mode=console), with the same rules as every console.
func (s *Server) handleLXCConsole(w http.ResponseWriter, r *http.Request) {
	name, mode := r.PathValue("name"), r.URL.Query().Get("mode")
	command, err := s.opts.LXC.ConsoleCommand(name, mode)
	if err != nil {
		writeLXCError(w, err)
		return
	}
	s.serveConsole(w, r, "lxc "+name, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		session, err := terminal.Start(command)
		if err != nil {
			return nil, nil, err
		}
		return session, session.Resize, nil
	})
}

func (s *Server) handleLXCSnapshots(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.LXC.Snapshots(r.PathValue("name"))
	if err != nil {
		writeLXCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": list})
}

func (s *Server) handleLXCSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	noteTarget(r, name)
	snapshot, err := s.opts.LXC.CreateSnapshot(context.WithoutCancel(r.Context()), name)
	if err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container snapshot taken", "container", name, "snapshot", snapshot.ID, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, snapshot)
}

func (s *Server) handleLXCSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	name, id := r.PathValue("name"), r.PathValue("snapshot")
	noteTarget(r, name+"@"+id)
	if err := s.opts.LXC.RestoreSnapshot(context.WithoutCancel(r.Context()), name, id); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container snapshot restored", "container", name, "snapshot", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLXCSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	name, id := r.PathValue("name"), r.PathValue("snapshot")
	noteTarget(r, name+"@"+id)
	if err := s.opts.LXC.DeleteSnapshot(context.WithoutCancel(r.Context()), name, id); err != nil {
		writeLXCError(w, err)
		return
	}
	slog.Info("container snapshot deleted", "container", name, "snapshot", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}
