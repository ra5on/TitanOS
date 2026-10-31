package api

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// volumeChecks are the regular checks offered when a volume is created
// (decided 31.10.2026): all chosen by default, the person decides. They
// become maintenance jobs; maintenance is switched on with them.
type volumeChecks struct {
	// Parity is the monthly RAID check, or the scrub of a Btrfs volume.
	Parity     bool `json:"parity"`
	SmartShort bool `json:"smartShort"`
	SmartLong  bool `json:"smartLong"`
	// Snapshots are daily, 14 kept; Btrfs only.
	Snapshots bool `json:"snapshots"`
}

func (c volumeChecks) any() bool { return c.Parity || c.SmartShort || c.SmartLong || c.Snapshots }

// addVolumeChecks adds the chosen checks of a new volume as maintenance
// jobs, through the same route as the maintenance page so that the same
// rules apply. A failure is logged; the volume is there either way.
func (s *Server) addVolumeChecks(ctx context.Context, r *http.Request, req storage.CreateRequest, checks volumeChecks) {
	if !checks.any() || s.opts.Maintenance == nil {
		return
	}
	settings := s.opts.Maintenance.Status().Settings
	if !settings.Enabled && settings.Volume == "" {
		settings.Volume = req.Name
	}
	settings.Enabled = true
	var disks []storage.Disk
	if checks.SmartShort || checks.SmartLong {
		if status, err := s.opts.Storage.Status(ctx); err == nil {
			disks = status.Disks
		}
	}
	settings.Jobs = append(settings.Jobs, volumeCheckJobs(req, checks, disks)...)
	code, answer := s.internalAs(ctx, currentUser(r), http.MethodPut, "/api/v1/maintenance", settings)
	if code != http.StatusOK {
		slog.Warn("checks of the new volume not scheduled", "volume", req.Name, "status", code, "answer", answer)
	}
}

// volumeCheckJobs are the maintenance jobs for the checks of a new volume.
func volumeCheckJobs(req storage.CreateRequest, checks volumeChecks, disks []storage.Disk) []maintenance.Job {
	jobs := []maintenance.Job{}
	job := func(name, kind, target, every string) maintenance.Job {
		j := maintenance.Job{Name: name, Kind: kind, Target: target, Enabled: true, Every: every, Time: "03:30", Missed: "window", Weekdays: []int{}}
		switch every {
		case "weekly":
			j.Weekdays = []int{0}
		case "monthly":
			j.MonthDay = 1
		}
		return j
	}
	if checks.Parity {
		name := req.Name + ": RAID-Prüfung"
		if req.Filesystem == "btrfs" {
			name = req.Name + ": Scrub"
		}
		jobs = append(jobs, job(name, "raid-check", req.Name, "monthly"))
	}
	if checks.Snapshots && req.Filesystem == "btrfs" {
		j := job(req.Name+": Snapshots", "snapshot", req.Name, "daily")
		j.Keep, j.AllowPrune = 14, true
		jobs = append(jobs, j)
	}
	if checks.SmartShort || checks.SmartLong {
		{
			for _, d := range disks {
				if d.Serial == "" || !slices.Contains(req.Disks, d.Name) {
					continue
				}
				if checks.SmartShort {
					j := job("SMART kurz: "+d.Serial, "smart-short", d.Serial, "weekly")
					j.Weekdays = []int{3}
					jobs = append(jobs, j)
				}
				if checks.SmartLong {
					j := job("SMART lang: "+d.Serial, "smart-long", d.Serial, "monthly")
					j.MonthDay = 15
					jobs = append(jobs, j)
				}
			}
		}
	}
	return jobs
}
