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

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/hwcheck"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

var vmErrors = []struct {
	err    error
	status int
	code   string
}{
	{vm.ErrServiceOff, http.StatusConflict, "service_off"},
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
	{vm.ErrNoCopy, http.StatusNotFound, "vm_copy_not_found"},
	{hardware.ErrUnknown, http.StatusBadRequest, "device_unavailable"},
	{hardware.ErrNotPassable, http.StatusBadRequest, "device_unavailable"},
	{hardware.ErrIncompleteGroup, http.StatusBadRequest, "hardware_incomplete_group"},
	{hardware.ErrInUse, http.StatusConflict, "hardware_in_use"},
	{docker.ErrHardwareBusy, http.StatusConflict, "vm_hardware_busy"},
}

func writeVMError(w http.ResponseWriter, err error) {
	status, code, message := vmErrorOf(err)
	writeError(w, status, code, message)
}

// vmErrorOf is the status, code and message of a failed VM action.
func vmErrorOf(err error) (int, string, string) {
	for _, known := range vmErrors {
		if errors.Is(err, known.err) {
			return known.status, known.code, err.Error()
		}
	}
	// What libvirt or QEMU said is the useful part.
	var command *run.CommandError
	if errors.As(err, &command) {
		return http.StatusInternalServerError, "vm_failed", command.Output
	}
	slog.Error("VM action failed", "err", err)
	return http.StatusInternalServerError, "vm_failed", err.Error()
}

// vmsArea is the virtual machines; an area with narrow dependencies
// (PLAN 8a, see fansArea). storage names the volumes, hardware and hwcheck
// what the host offers, monitor its memory, network its bridges.
// serveConsole and serveStream connect the serial console and the screen.
type vmsArea struct {
	vms          *vm.Manager
	storage      *storage.Manager
	hardware     *hardware.Manager
	hwcheck      *hwcheck.Checker
	monitor      *system.Monitor
	network      *network.Manager
	serveConsole consoleFunc
	serveStream  streamFunc
}

// vmsArea is the area of the server.
func (s *Server) vmsArea() vmsArea {
	return vmsArea{vms: s.opts.VMs, storage: s.opts.Storage, hardware: s.opts.Hardware, hwcheck: s.opts.HWCheck, monitor: s.opts.Monitor,
		network: s.opts.Network, serveConsole: s.serveConsole, serveStream: s.serveStream}
}

func (a vmsArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/vms", g.admin(a.handleVMs))
	mux.HandleFunc("POST /api/v1/vms", g.heavyAppAdmin(a.handleVMCreate))
	mux.HandleFunc("GET /api/v1/vms/media", g.admin(a.handleVMMedia))
	mux.HandleFunc("GET /api/v1/vms/host", g.admin(a.handleVMHost))
	// Taking a VM over from the copy of its settings on a volume.
	mux.HandleFunc("GET /api/v1/vms/adopt", g.admin(a.handleVMAdoptable))
	mux.HandleFunc("POST /api/v1/vms/adopt", g.appAdmin(a.handleVMAdopt))
}

// registerDetails registers the routes of snapshots, consoles and actions
// of single virtual machines.
func (a vmsArea) registerDetails(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/vms/{name}/snapshots", g.admin(a.handleVMSnapshots))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots", g.heavyAppAdmin(a.handleVMSnapshotCreate))
	mux.HandleFunc("PUT /api/v1/vms/{name}/snapshot-schedule", g.appAdmin(a.handleVMSnapshotSchedule))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshot-recovery", g.heavyAppAdmin(a.handleVMSnapshotRecover))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots/{snapshot}/restore", g.heavyAppAdmin(a.handleVMSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/vms/{name}/snapshots/{snapshot}", g.heavyAppAdmin(a.handleVMSnapshotDelete))
	mux.HandleFunc("POST /api/v1/vms/{name}/grow", g.heavyAppAdmin(a.handleVMGrow))
	mux.HandleFunc("GET /api/v1/vms/{name}/serial", g.appConsole(a.handleVMSerial))
	mux.HandleFunc("PUT /api/v1/vms/{name}", g.appAdmin(a.handleVMUpdate))
	mux.HandleFunc("DELETE /api/v1/vms/{name}", g.appAdmin(a.handleVMDelete))
	mux.HandleFunc("GET /api/v1/vms/{name}/xml", g.admin(a.handleVMXML))
	mux.HandleFunc("GET /api/v1/vms/{name}/addresses", g.admin(a.handleVMAddresses))
	mux.HandleFunc("PUT /api/v1/vms/{name}/medium", g.appAdmin(a.handleVMMedium))
	mux.HandleFunc("GET /api/v1/vms/{name}/screen", g.appConsole(a.handleVMScreen))
	mux.HandleFunc("POST /api/v1/vms/{name}/{action}", g.appAdmin(a.handleVMAction))
}

func (a vmsArea) handleVMs(w http.ResponseWriter, r *http.Request) {
	memory := a.monitor.Info().Memory
	writeJSON(w, http.StatusOK, map[string]any{
		"kvm": a.vms.HasKVM(),
		"host": map[string]any{
			"cpus":                 runtime.NumCPU(),
			"memoryBytes":          memory.TotalBytes,
			"usedMemoryBytes":      memory.UsedBytes,
			"availableMemoryBytes": memory.AvailableBytes,
			"cpuModel":             a.monitor.Info().CPU.Model,
		},
		// With the bridge, VMs can have an address of their own in the network.
		"bridge":   a.network.BridgeReady(),
		"networks": a.network.Connections(),
		"vms":      a.vms.List(a.storage.Mounted()),
	})
}

func (a vmsArea) handleVMCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		vm.VM
		// Start boots the VM right after it was made.
		Start bool `json:"start"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	mounted := a.storage.Mounted()
	// Validate only the hardware the owner explicitly selected.
	pci, err := a.hardware.Check(req.PCI, req.USB)
	if err != nil {
		writeVMError(w, err)
		return
	}
	req.PCI = pci
	if err := a.vms.Create(r.Context(), req.VM, mounted); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM created", "vm", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	started := false
	if req.Start {
		if err := a.vms.Start(r.Context(), req.Name, mounted); err != nil {
			// The VM exists; only its first start failed. The answer says
			// both: an error answer would read as "not created", and
			// creating it again would fail with "exists".
			_, code, message := vmErrorOf(err)
			writeJSON(w, http.StatusCreated, map[string]any{"started": false, "startError": apiError{Code: code, Message: message}})
			return
		}
		started = true
	}
	writeJSON(w, http.StatusCreated, map[string]any{"started": started})
}

// handleVMAdoptable lists the copies of VM settings on the mounted volumes
// whose VMs are not registered. The volumes are only read here, on request.
func (a vmsArea) handleVMAdoptable(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"candidates": a.vms.AdoptCandidates(a.storage.Mounted())})
}

// handleVMAdopt registers the VM of such a copy again.
func (a vmsArea) handleVMAdopt(w http.ResponseWriter, r *http.Request) {
	var req vm.AdoptRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Volume+"/vms/"+req.Directory)
	result, err := a.vms.Adopt(r.Context(), req, a.storage.Mounted())
	if err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM taken over", "vm", result.Name, "volume", req.Volume, "folder", req.Directory, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, result)
}

func (a vmsArea) handleVMUpdate(w http.ResponseWriter, r *http.Request) {
	var change vm.Change
	if !readJSON(w, r, &change) {
		return
	}
	name := r.PathValue("name")
	pci, err := a.hardware.Check(change.PCI, change.USB)
	if err != nil {
		writeVMError(w, err)
		return
	}
	change.PCI = pci
	if err := a.vms.Update(r.Context(), name, change, a.storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM changed", "vm", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a vmsArea) handleVMDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeleteDisks bool   `json:"deleteDisks"`
		Confirm     string `json:"confirm"`
	}
	// A body is only needed for deleting the disks as well.
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.vms.Delete(name, req.DeleteDisks, req.Confirm); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM removed", "vm", name, "disks deleted", req.DeleteDisks, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a vmsArea) handleVMAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	var err error
	if action == "start" {
		err = a.vms.Start(r.Context(), name, a.storage.Mounted())
	} else {
		err = a.vms.Action(r.Context(), name, action)
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
func (a vmsArea) handleVMMedium(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.vms.SetMedium(r.Context(), name, req.Path); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM medium changed", "vm", name, "medium", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a vmsArea) handleVMAddresses(w http.ResponseWriter, r *http.Request) {
	addresses, err := a.vms.Addresses(r.Context(), r.PathValue("name"))
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, addresses)
}

func (a vmsArea) handleVMXML(w http.ResponseWriter, r *http.Request) {
	description, err := a.vms.XML(r.PathValue("name"))
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"xml": description})
}

func (a vmsArea) handleVMMedia(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"media": a.vms.Media(a.storage.Mounted())})
}

// handleVMHost describes what the NAS offers a new VM: its CPUs, disk
// images that are there already and disks that are in no volume.
func (a vmsArea) handleVMHost(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"cpus":   a.vms.CPUs("/sys"),
		"images": a.vms.Images(r.Context(), a.storage.Mounted()),
		"disks":  a.vms.WholeDisks(),
		// VT-x/AMD-V and the IOMMU, with what to switch on where.
		"virt": a.vmVirtualization(r),
	})
}

func (a vmsArea) handleVMGrow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk      int   `json:"disk"`
		SizeBytes int64 `json:"sizeBytes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.vms.Grow(r.Context(), name, req.Disk, req.SizeBytes); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("disk of a VM grown", "vm", name, "disk", req.Disk, "bytes", req.SizeBytes, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a vmsArea) vmVirtualization(r *http.Request) any {
	if a.hwcheck == nil {
		return nil
	}
	return a.hwcheck.Virtualization(r.Context())
}

// handleHardware lists the devices that can be handed to VMs, with the
// VMs that have them.
func (a vmsArea) handleHardware(w http.ResponseWriter, r *http.Request) {
	users := map[string][]string{}
	for _, machine := range a.vms.List(nil) {
		for _, device := range append(append([]string{}, machine.PCI...), machine.USB...) {
			users[device] = append(users[device], machine.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hardware": a.hardware.Status(), "usedBy": users})
}

// handleVMSerial connects a WebSocket to the serial port of a running VM,
// with the same checks as every console.
func (a vmsArea) handleVMSerial(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.serveConsole(w, r, "vm "+name, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		running := false
		for _, known := range a.vms.List(nil) {
			running = running || (known.Name == name && known.State == "running")
		}
		if !running {
			return nil, nil, vm.ErrNotRunning
		}
		port, err := net.DialTimeout("unix", a.vms.SerialSocket(name), 3*time.Second)
		if err != nil {
			return nil, nil, err
		}
		// A serial line has no size to tell.
		return port, func(int, int) {}, nil
	})
}

// vmRunning reports whether a VM of that name runs.
func (a vmsArea) vmRunning(name string) bool {
	for _, known := range a.vms.List(nil) {
		if known.Name == name && known.State == "running" {
			return true
		}
	}
	return false
}

// vmScreenConsole is how the screen of a VM is served: a console with
// binary RFB as it is, and only keyboard and mouse count as
// input. The viewer sends small messages, but a pasted clipboard can be
// larger.
func vmScreenConsole(name string) consoleOptions {
	return consoleOptions{target: "vm screen " + name, raw: true, readLimit: 4 << 20, input: vncInput}
}

// handleVMScreen connects a WebSocket to the screen of a running VM. The
// VNC server of QEMU listens on a socket only titand can open; this is the
// only way to it, behind the login and with the rules of every VM console:
// terminals switched on, the password entered again (unless switched off),
// the end after 15 minutes without keyboard or mouse input, and the end
// with the login. The stream is binary RFB in both directions.
func (a vmsArea) handleVMScreen(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Whether a VM runs is no secret to an administrator (the list shows
	// it); telling it first gives a clear error instead of a closed socket.
	if !a.vmRunning(name) {
		writeError(w, http.StatusConflict, "vm_not_running", "the VM is not running")
		return
	}
	a.serveStream(w, r, vmScreenConsole(name), func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		if !a.vmRunning(name) {
			return nil, nil, vm.ErrNotRunning
		}
		screen, err := net.DialTimeout("unix", a.vms.VNCSocket(name), 3*time.Second)
		if err != nil {
			return nil, nil, err
		}
		return screen, nil, nil
	})
}
