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

func (s *Server) handleUSB(w http.ResponseWriter, r *http.Request) {
	list := []usbDevice{}
	if s.opts.USB != nil {
		devices, err := s.opts.USB.List(r.Context())
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

func (s *Server) handleUSBMount(w http.ResponseWriter, r *http.Request) {
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	if err := s.opts.USB.Mount(r.Context(), r.PathValue("id")); err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("usb drive mounted", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleUSB(w, r)
}

func (s *Server) handleUSBEject(w http.ResponseWriter, r *http.Request) {
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	if err := s.opts.USB.Eject(r.Context(), r.PathValue("id")); err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("usb drive ejected", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleUSB(w, r)
}

func (s *Server) handleUSBDisks(w http.ResponseWriter, r *http.Request) {
	disks, job := []usb.Disk{}, (*usb.FormatJob)(nil)
	if s.opts.USB != nil {
		var err error
		if disks, job, err = s.opts.USB.Disks(r.Context()); err != nil {
			writeUSBError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": disks, "job": job})
}

func (s *Server) handleUSBFormat(w http.ResponseWriter, r *http.Request) {
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	var req usb.FormatRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.USB.Format(r.Context(), r.PathValue("id"), req); err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("drive format started", "fs", req.Filesystem, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleUSBDisks(w, r)
}

func (s *Server) handleUSBPartitions(w http.ResponseWriter, r *http.Request) {
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
		return
	}
	layout, err := s.opts.USB.Partitions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeUSBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, layout)
}

// usbPartitionStarted answers a partition change that was started with the
// drives and the job, like a format.
func (s *Server) usbPartitionStarted(w http.ResponseWriter, r *http.Request, action string, err error) {
	if err != nil {
		writeUSBError(w, err)
		return
	}
	slog.Info("partition change started", "action", action, "number", r.PathValue("number"), "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleUSBDisks(w, r)
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

func (s *Server) handleUSBTable(w http.ResponseWriter, r *http.Request) {
	var req usb.TableRequest
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if readJSON(w, r, &req) {
		s.usbPartitionStarted(w, r, "table", s.opts.USB.NewTable(r.Context(), r.PathValue("id"), req))
	}
}

func (s *Server) handleUSBPartitionCreate(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if readJSON(w, r, &req) {
		s.usbPartitionStarted(w, r, "create", s.opts.USB.CreatePartition(r.Context(), r.PathValue("id"), req))
	}
}

func (s *Server) handleUSBPartitionDelete(w http.ResponseWriter, r *http.Request) {
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok {
		s.usbPartitionStarted(w, r, "delete", s.opts.USB.DeletePartition(r.Context(), r.PathValue("id"), number))
	}
}

func (s *Server) handleUSBPartitionFormat(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok && readJSON(w, r, &req) {
		s.usbPartitionStarted(w, r, "format", s.opts.USB.FormatPartition(r.Context(), r.PathValue("id"), number, req))
	}
}

func (s *Server) handleUSBPartitionLabel(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok && readJSON(w, r, &req) {
		s.usbPartitionStarted(w, r, "label", s.opts.USB.SetLabel(r.Context(), r.PathValue("id"), number, req.Label))
	}
}

func (s *Server) handleUSBPartitionGrow(w http.ResponseWriter, r *http.Request) {
	var req usb.PartitionRequest
	if s.opts.USB == nil {
		writeUSBError(w, usb.ErrNotFound)
	} else if number, ok := usbPartition(w, r); ok && readJSON(w, r, &req) {
		s.usbPartitionStarted(w, r, "grow", s.opts.USB.GrowPartition(r.Context(), r.PathValue("id"), number, req))
	}
}
