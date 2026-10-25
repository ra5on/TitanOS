package api

import (
	"errors"
	"log/slog"
	"net/http"

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
}

func writeUSBError(w http.ResponseWriter, err error) {
	if errors.Is(err, usb.ErrMountFailed) {
		slog.Warn("drive mount failed", "err", err)
	}
	for _, known := range usbErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.err.Error())
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
