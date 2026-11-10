package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hwcheck"
)

// hwcheckArea is the hardware compatibility check; an area with narrow
// dependencies (PLAN 8a, see fansArea). version names TitanOS in the
// files to save.
type hwcheckArea struct {
	check   *hwcheck.Checker
	version string
}

func (a hwcheckArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/hardware/report", admin(a.handleHardwareReport))
	mux.HandleFunc("GET /api/v1/hardware/snapshot", admin(a.handleHardwareSnapshot))
	mux.HandleFunc("GET /api/v1/hardware/disks", admin(a.handleHardwareDisks))
}

// handleHardwareReport is the compatibility check (Diagnose › Hardware
// prüfen, Einstellungen › Hardware › Geräte ohne Treiber). It only reads
// sysfs, procfs and the kernel log. format=text or format=json sends it
// as a file to save.
func (a hwcheckArea) handleHardwareReport(w http.ResponseWriter, r *http.Request) {
	if a.check == nil {
		writeError(w, http.StatusServiceUnavailable, "hwcheck_unavailable", "the hardware check is not available")
		return
	}
	report := a.check.Report(r.Context())
	name := "titanos-hardware-" + time.Now().Format("2006-01-02T15-04-05")
	switch r.URL.Query().Get("format") {
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.txt"`)
		w.Write([]byte(report.Text(a.version)))
	case "json":
		data, _ := json.MarshalIndent(map[string]any{"version": a.version, "report": report}, "", "  ")
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
func (a hwcheckArea) handleHardwareSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.check == nil {
		writeError(w, http.StatusServiceUnavailable, "hwcheck_unavailable", "the hardware check is not available")
		return
	}
	var archive bytes.Buffer
	if err := a.check.Snapshot(r.Context(), &archive, a.version); err != nil {
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
func (a hwcheckArea) handleHardwareDisks(w http.ResponseWriter, r *http.Request) {
	disks := []hwcheck.Disk{}
	if a.check != nil {
		disks = a.check.Disks()
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": disks})
}

// cpuMissing is what a compose file needs that the CPU lacks.
func (a appStoreArea) cpuMissing(compose string) []string {
	if a.hwcheck == nil {
		return []string{}
	}
	return a.hwcheck.MissingCPU(compose)
}
