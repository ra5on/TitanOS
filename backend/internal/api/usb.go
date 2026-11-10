package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/usb"
)

var usbErrors = []struct {
	err    error
	status int
	code   string
}{
	{usb.ErrDisabled, http.StatusConflict, "read_only"},
	{usb.ErrNotFound, http.StatusNotFound, "usb_not_found"},
	{usb.ErrUnsupported, http.StatusUnprocessableEntity, "usb_unsupported"},
	{usb.ErrMountFailed, http.StatusUnprocessableEntity, "drive_mount_failed"},
	{usb.ErrBusy, http.StatusConflict, "usb_busy"},
	{usb.ErrMounted, http.StatusConflict, "usb_mounted"},
	{usb.ErrNotMounted, http.StatusConflict, "usb_not_mounted"},
	{usb.ErrReadOnly, http.StatusConflict, "usb_read_only"},
	{usb.ErrInUse, http.StatusConflict, "usb_in_use"},
	{usb.ErrNotFormattable, http.StatusConflict, "usb_not_formattable"},
	{usb.ErrFormatInvalid, http.StatusBadRequest, "usb_format_invalid"},
	{usb.ErrPartitionUnsupported, http.StatusUnprocessableEntity, "usb_partition_unsupported"},
}

func writeUSBError(w http.ResponseWriter, err error) {
	if errors.Is(err, usb.ErrMountFailed) {
		slog.Warn("drive mount failed", "err", err)
	}
	for _, known := range usbErrors {
		if errors.Is(err, known.err) {
			message := known.err.Error()
			if known.err == usb.ErrNotFormattable || known.err == usb.ErrFormatInvalid || known.err == usb.ErrPartitionUnsupported {
				// Says which rule was broken; it holds no paths.
				message = err.Error()
			}
			writeError(w, known.status, known.code, message)
			return
		}
	}
	slog.Warn("usb", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "the drive could not be handled")
}

type usbDevice struct {
	usb.Device
	FreeBytes uint64 `json:"freeBytes"`
	// Root is the file manager root of a mounted drive.
	Root string `json:"root"`
}

// usbArea is the unmanaged data drives; an area with narrow dependencies
// (PLAN 8a, see fansArea).
type usbArea struct {
	usb *usb.Manager
}

func (a usbArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/usb", admin(a.handleUSB))
	mux.HandleFunc("POST /api/v1/usb/{id}/mount", admin(a.handleUSBMount))
	mux.HandleFunc("POST /api/v1/usb/{id}/eject", admin(a.handleUSBEject))
	mux.HandleFunc("GET /api/v1/usb/disks", admin(a.handleUSBDisks))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/format", admin(a.handleUSBFormat))
	mux.HandleFunc("GET /api/v1/usb/disks/{id}/partitions", admin(a.handleUSBPartitions))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/table", admin(a.handleUSBTable))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions", admin(a.handleUSBPartitionCreate))
	mux.HandleFunc("DELETE /api/v1/usb/disks/{id}/partitions/{number}", admin(a.handleUSBPartitionDelete))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions/{number}/format", admin(a.handleUSBPartitionFormat))
	mux.HandleFunc("PUT /api/v1/usb/disks/{id}/partitions/{number}/label", admin(a.handleUSBPartitionLabel))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions/{number}/grow", admin(a.handleUSBPartitionGrow))
}

func (a usbArea) handleUSB(w http.ResponseWriter, r *http.Request) {
	list := []usbDevice{}
	if a.usb != nil {
		devices, err := a.usb.List(r.Context())
		if err != nil {
			writeUSBError(w, err)
			return
		}
		for _, d := range devices {
			item := usbDevice{Device: d}
			if d.Mounted {
				item.Root = "usb/" + d.Mount
				_, item.FreeBytes = files.Usage(files.Root{Dir: d.Dir})
			}
			list = append(list, item)
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a usbArea) handleUSBMount(w http.ResponseWriter, r *http.Request) {
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	if err := a.usb.Mount(r.Context(), r.PathValue("id")); err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("usb drive mounted", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUSB(w, r)
}

func (a usbArea) handleUSBEject(w http.ResponseWriter, r *http.Request) {
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	if err := a.usb.Eject(r.Context(), r.PathValue("id")); err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("usb drive ejected", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUSB(w, r)
}

func (a usbArea) handleUSBDisks(w http.ResponseWriter, r *http.Request) {
	disks, job := []usb.Disk{}, (*usb.FormatJob)(nil)
	if a.usb != nil {
		var err error
		if disks, job, err = a.usb.Disks(r.Context()); err != nil {
			writeUSBError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": disks, "job": job})
}

func (a usbArea) handleUSBFormat(w http.ResponseWriter, r *http.Request) {
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	var req usb.FormatRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.usb.Format(r.Context(), r.PathValue("id"), req); err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("drive format started", "fs", req.Filesystem, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUSBDisks(w, r)
}

func (a usbArea) handleUSBPartitions(w http.ResponseWriter, r *http.Request) {
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	layout, err := a.usb.Partitions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeUSBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, layout)
}

// usbPartitionStarted answers a partition change that was started with the
// drives and the job, like a format.
func (a usbArea) usbPartitionStarted(w http.ResponseWriter, r *http.Request, action string, err error) {
	if err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("partition change started", "action", action, "number", r.PathValue("number"), "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUSBDisks(w, r)
}

// usbPartition reads the partition number of a route; a bad one is a
// partition that is not there.
func usbPartition(w http.ResponseWriter, r *http.Request) (int, bool) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number < 1 {
		writeUSBError(w, usb.ErrNotFound)
		return 0, false
	}
	return number, true
}

func (a usbArea) handleUSBTable(w http.ResponseWriter, r *http.Request) {
	var req usb.TableRequest
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if readJSON(w, r, &req) {
		a.usbPartitionStarted(w, r, "table", a.usb.NewTable(r.Context(), r.PathValue("id"), req))
	}
}

func (a usbArea) handleUSBPartitionCreate(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if readJSON(w, r, &req) {
		a.usbPartitionStarted(w, r, "create", a.usb.CreatePartition(r.Context(), r.PathValue("id"), req))
	}
}

func (a usbArea) handleUSBPartitionDelete(w http.ResponseWriter, r *http.Request) {
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok {
		a.usbPartitionStarted(w, r, "delete", a.usb.DeletePartition(r.Context(), r.PathValue("id"), number))
	}
}

func (a usbArea) handleUSBPartitionFormat(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok && readJSON(w, r, &req) {
		a.usbPartitionStarted(w, r, "format", a.usb.FormatPartition(r.Context(), r.PathValue("id"), number, req))
	}
}

func (a usbArea) handleUSBPartitionLabel(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok && readJSON(w, r, &req) {
		a.usbPartitionStarted(w, r, "label", a.usb.SetLabel(r.Context(), r.PathValue("id"), number, req.Label))
	}
}

func (a usbArea) handleUSBPartitionGrow(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if a.usb == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok && readJSON(w, r, &req) {
		a.usbPartitionStarted(w, r, "grow", a.usb.GrowPartition(r.Context(), r.PathValue("id"), number, req))
	}
}
