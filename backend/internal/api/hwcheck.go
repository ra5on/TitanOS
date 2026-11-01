package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hwcheck"
)

// handleHardwareReport is the compatibility check (Diagnose › Hardware
// prüfen, Einstellungen › Hardware › Geräte ohne Treiber). It only reads
// sysfs, procfs and the kernel log. format=text or format=json sends it
// as a file to save.
func (s *Server) handleHardwareReport(w http.ResponseWriter, r *http.Request) {
	if s.opts.HWCheck == nil {
		writeError(w, http.StatusServiceUnavailable, "hwcheck_unavailable", "the hardware check is not available")
		return
	}
	report := s.opts.HWCheck.Report(r.Context())
	name := "titanos-hardware-" + time.Now().Format("2006-01-02T15-04-05")
	switch r.URL.Query().Get("format") {
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.txt"`)
		w.Write([]byte(report.Text(s.opts.Version)))
	case "json":
		data, _ := json.MarshalIndent(map[string]any{"version": s.opts.Version, "report": report}, "", "  ")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.json"`)
		w.Write(append(data, '\n'))
	default:
		writeJSON(w, http.StatusOK, report)
	}
}

// handleHardwareSnapshot sends the hardware snapshot (tar.gz): what the
// hardware page reads from sysfs, the kernel log and the module aliases,
// without serial numbers, MAC or IP addresses. A reported machine becomes a
// test case (backend/internal/hwcheck/testdata/snapshots).
func (s *Server) handleHardwareSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.opts.HWCheck == nil {
		writeError(w, http.StatusServiceUnavailable, "hwcheck_unavailable", "the hardware check is not available")
		return
	}
	var archive bytes.Buffer
	if err := s.opts.HWCheck.Snapshot(r.Context(), &archive, s.opts.Version); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the snapshot could not be made")
		return
	}
	name := "titanos-hardware-snapshot-" + time.Now().Format("2006-01-02T15-04-05")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.tar.gz"`)
	w.Write(archive.Bytes())
}

// handleHardwareDisks lists the disks with SMR, UAS and SAS hints, from
// sysfs only (the volume dialog warns before RAID 5/6 on SMR disks).
func (s *Server) handleHardwareDisks(w http.ResponseWriter, r *http.Request) {
	disks := []hwcheck.Disk{}
	if s.opts.HWCheck != nil {
		disks = s.opts.HWCheck.Disks()
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": disks})
}

// cpuMissing is what a compose file needs that the CPU lacks.
func (s *Server) cpuMissing(compose string) []string {
	if s.opts.HWCheck == nil {
		return []string{}
	}
	return s.opts.HWCheck.MissingCPU(compose)
}
