package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

var vmErrors = []struct {
	err    error
	status int
	code   string
}{
	{vm.ErrSnapshotBusy, http.StatusConflict, "vm_snapshot_busy"},
	{vm.ErrSnapshotUnsupported, http.StatusConflict, "vm_snapshot_unsupported"},
	{vm.ErrSnapshotRecovery, http.StatusConflict, "vm_snapshot_recovery"},
	{vm.ErrSnapshotRuntime, http.StatusConflict, "vm_snapshot_runtime"},
	{vm.ErrSnapshotExists, http.StatusConflict, "vm_snapshot_exists"},
	{vm.ErrDisabled, http.StatusConflict, "read_only"},
	{vm.ErrNoKVM, http.StatusConflict, "no_kvm"},
	{vm.ErrInvalidName, http.StatusBadRequest, "invalid_vm_name"},
	{vm.ErrInvalidDisplayName, http.StatusBadRequest, "invalid_vm_display_name"},
	{vm.ErrExists, http.StatusConflict, "vm_exists"},
	{vm.ErrNotFound, http.StatusNotFound, "vm_not_found"},
	{vm.ErrInvalid, http.StatusBadRequest, "invalid_vm"},
	{vm.ErrUnknownVolume, http.StatusConflict, "vm_volume_unavailable"},
	{vm.ErrNoSpace, http.StatusConflict, "no_space"},
	{vm.ErrBadISO, http.StatusBadRequest, "invalid_iso"},
	{vm.ErrRunning, http.StatusConflict, "vm_running"},
	{vm.ErrNotRunning, http.StatusConflict, "vm_not_running"},
	{vm.ErrNotConfirmed, http.StatusBadRequest, "not_confirmed"},
	{vm.ErrPreparing, http.StatusConflict, "vm_preparing"},
	{vm.ErrNoBridge, http.StatusConflict, "no_bridge"},
	{vm.ErrBadFolder, http.StatusBadRequest, "invalid_folder"},
	{vm.ErrBadImage, http.StatusBadRequest, "invalid_image"},
	{hardware.ErrUnknown, http.StatusBadRequest, "device_unavailable"},
	{hardware.ErrNotPassable, http.StatusBadRequest, "device_unavailable"},
	{hardware.ErrIncompleteGroup, http.StatusBadRequest, "hardware_incomplete_group"},
}

func writeVMError(w http.ResponseWriter, err error) {
	for _, known := range vmErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	// What libvirt or QEMU said is the useful part.
	var command *run.CommandError
	if errors.As(err, &command) {
		writeError(w, http.StatusInternalServerError, "vm_failed", command.Output)
		return
	}
	slog.Error("VM action failed", "err", err)
	writeError(w, http.StatusInternalServerError, "vm_failed", err.Error())
}

func (s *Server) handleVMs(w http.ResponseWriter, r *http.Request) {
	memory := s.opts.Monitor.Info().Memory
	writeJSON(w, http.StatusOK, map[string]any{
		"kvm": s.opts.VMs.HasKVM(),
		"host": map[string]any{
			"cpus":                 runtime.NumCPU(),
			"memoryBytes":          memory.TotalBytes,
			"usedMemoryBytes":      memory.UsedBytes,
			"availableMemoryBytes": memory.AvailableBytes,
			"cpuModel":             s.opts.Monitor.Info().CPU.Model,
		},
		// With the bridge, VMs can have an address of their own in the network.
		"bridge":   s.opts.Network.BridgeReady(),
		"networks": s.opts.Network.Connections(),
		"vms":      s.opts.VMs.List(s.opts.Storage.Mounted()),
	})
}

func (s *Server) handleVMCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		vm.VM
		// Start boots the VM right after it was made.
		Start bool `json:"start"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	mounted := s.opts.Storage.Mounted()
	// Validate only the hardware the owner explicitly selected.
	pci, err := s.opts.Hardware.Check(req.PCI, req.USB)
	if err != nil {
		writeVMError(w, err)
		return
	}
	req.PCI = pci
	if err := s.opts.VMs.Create(r.Context(), req.VM, mounted); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM created", "vm", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	if req.Start {
		if err := s.opts.VMs.Start(r.Context(), req.Name, mounted); err != nil {
			// The VM exists; only its first start failed.
			writeVMError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleVMUpdate(w http.ResponseWriter, r *http.Request) {
	var change vm.Change
	if !readJSON(w, r, &change) {
		return
	}
	name := r.PathValue("name")
	pci, err := s.opts.Hardware.Check(change.PCI, change.USB)
	if err != nil {
		writeVMError(w, err)
		return
	}
	change.PCI = pci
	if err := s.opts.VMs.Update(r.Context(), name, change, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM changed", "vm", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVMDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeleteDisks bool   `json:"deleteDisks"`
		Confirm     string `json:"confirm"`
	}
	// A body is only needed for deleting the disks as well.
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.VMs.Delete(name, req.DeleteDisks, req.Confirm); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM removed", "vm", name, "disks deleted", req.DeleteDisks, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVMAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	var err error
	if action == "start" {
		err = s.opts.VMs.Start(r.Context(), name, s.opts.Storage.Mounted())
	} else {
		err = s.opts.VMs.Action(r.Context(), name, action)
	}
	if err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM "+action, "vm", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleVMMedium puts an installation medium into the CD drive of a VM or,
// with an empty path, takes it out; while the VM runs, too.
func (s *Server) handleVMMedium(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.VMs.SetMedium(r.Context(), name, req.Path); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM medium changed", "vm", name, "medium", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVMAddresses(w http.ResponseWriter, r *http.Request) {
	addresses, err := s.opts.VMs.Addresses(r.Context(), r.PathValue("name"))
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, addresses)
}

func (s *Server) handleVMXML(w http.ResponseWriter, r *http.Request) {
	description, err := s.opts.VMs.XML(r.PathValue("name"))
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"xml": description})
}

func (s *Server) handleVMMedia(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"media": s.opts.VMs.Media(s.opts.Storage.Mounted())})
}

// handleVMHost describes what the NAS offers a new VM: its CPUs, disk
// images that are there already and disks that are in no volume.
func (s *Server) handleVMHost(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"cpus":   s.opts.VMs.CPUs("/sys"),
		"images": s.opts.VMs.Images(r.Context(), s.opts.Storage.Mounted()),
		"disks":  s.opts.VMs.WholeDisks(),
	})
}

func (s *Server) handleVMGrow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk      int   `json:"disk"`
		SizeBytes int64 `json:"sizeBytes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.VMs.Grow(r.Context(), name, req.Disk, req.SizeBytes); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("disk of a VM grown", "vm", name, "disk", req.Disk, "bytes", req.SizeBytes, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleHardware lists the devices that can be handed to VMs, with the
// VMs that have them.
func (s *Server) handleHardware(w http.ResponseWriter, r *http.Request) {
	users := map[string][]string{}
	for _, machine := range s.opts.VMs.List(nil) {
		for _, device := range append(append([]string{}, machine.PCI...), machine.USB...) {
			users[device] = append(users[device], machine.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hardware": s.opts.Hardware.Status(), "usedBy": users})
}

// handleVMSerial connects a WebSocket to the serial port of a running VM,
// with the same checks as every console.
func (s *Server) handleVMSerial(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.serveConsole(w, r, "vm "+name, true, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		running := false
		for _, known := range s.opts.VMs.List(nil) {
			running = running || (known.Name == name && known.State == "running")
		}
		if !running {
			return nil, nil, vm.ErrNotRunning
		}
		port, err := net.DialTimeout("unix", s.opts.VMs.SerialSocket(name), 3*time.Second)
		if err != nil {
			return nil, nil, err
		}
		// A serial line has no size to tell.
		return port, func(int, int) {}, nil
	})
}

// handleVMScreen connects a WebSocket to the screen of a running VM. The
// VNC server of QEMU listens on a socket only titand can open; this is the
// only way to it, and it is behind the login.
func (s *Server) handleVMScreen(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	found := false
	for _, known := range s.opts.VMs.List(nil) {
		found = found || (known.Name == name && known.State == "running")
	}
	if !found {
		writeError(w, http.StatusConflict, "vm_not_running", "the VM is not running")
		return
	}
	screen, err := net.DialTimeout("unix", s.opts.VMs.VNCSocket(name), 3*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "vm_failed", "the screen of the VM is not reachable")
		return
	}
	defer screen.Close()
	// Accept checks that the page asking is this site, not another one.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// The viewer sends small messages, but a pasted clipboard can be larger.
	conn.SetReadLimit(4 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	slog.Info("VM screen opened", "vm", name, "user", currentUser(r).Name, "ip", remoteIP(r))

	go func() {
		defer cancel()
		buf := make([]byte, 64*1024)
		for {
			n, err := screen.Read(buf)
			if n > 0 && conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Debug("VM screen", "vm", name, "err", err)
				}
				// The VM was switched off.
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if _, err := screen.Write(data); err != nil {
			return
		}
	}
}
