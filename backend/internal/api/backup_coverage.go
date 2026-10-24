package api

import (
	"net/http"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/backup"
)

type coverageJob struct {
	StateKnown           bool         `json:"stateKnown"`
	ID                   string       `json:"id"`
	Automatic            bool         `json:"automatic"`
	Partial              bool         `json:"partial"`
	Method               string       `json:"method"`
	Destination          string       `json:"destination"`
	DestinationAvailable *bool        `json:"destinationAvailable"`
	State                backup.State `json:"state"`
}
type coverageItem struct {
	Kind      string        `json:"kind"`
	Name      string        `json:"name"`
	Available bool          `json:"available"`
	Status    string        `json:"status"`
	LastOK    *time.Time    `json:"lastOK"`
	Jobs      []coverageJob `json:"jobs"`
}

func coverageFor(kind, name string, available bool, jobs []backup.View, mounted []string, now time.Time) coverageItem {
	item := coverageItem{Kind: kind, Name: name, Available: available, Status: "unconfigured", Jobs: []coverageJob{}}
	hasError := false
	for _, view := range jobs {
		j := view.Job
		matches := kind == "share" && slices.Contains(j.Sources, name)
		method := "files"
		for _, app := range j.Applications {
			if app.Kind == kind && app.Name == name {
				matches = true
				method = app.Method
			}
		}
		if !matches {
			continue
		}
		partial := len(j.Patterns) > 0 || len(j.Skip) > 0 || len(j.Folders[name]) > 0
		var dest *bool
		if j.Destination.Type == "local" {
			value := slices.Contains(mounted, j.Destination.Volume)
			dest = &value
		}
		item.Jobs = append(item.Jobs, coverageJob{StateKnown: view.StateKnown, ID: j.ID, Automatic: j.Enabled, Partial: partial, Method: method, Destination: j.Destination.Type, DestinationAvailable: dest, State: view.State})
		if !view.StateKnown || dest != nil && !*dest || view.State.LastError != "" || view.State.Warning != "" || view.State.CheckError != "" || view.State.Probe != nil && view.State.Probe.Error != "" {
			hasError = true
		}
		if stamp := view.State.LastOK; stamp != nil && (item.LastOK == nil || stamp.After(*item.LastOK)) {
			copy := *stamp
			item.LastOK = &copy
		}
	}
	if len(item.Jobs) > 0 {
		item.Status = "never"
		if item.LastOK != nil {
			item.Status = "covered"
			if now.Sub(*item.LastOK) > 48*time.Hour {
				item.Status = "stale"
			}
		}
		if hasError || !available {
			item.Status = "attention"
		}
	}
	return item
}
func (s *Server) handleBackupCoverage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Backup == nil || s.opts.Storage == nil {
		featureUnavailable(w)
		return
	}
	jobs := s.opts.Backup.List()
	mounted := s.opts.Storage.Mounted()
	out := []coverageItem{}
	for _, share := range s.opts.Shares.List() {
		out = append(out, coverageFor("share", share.Name, slices.Contains(mounted, share.Volume), jobs, mounted, time.Now()))
	}
	sources := []string{}
	if s.opts.Docker != nil {
		status := s.opts.Docker.Status(r.Context())
		if status.Settings.Enabled && !status.Running {
			sources = append(sources, "docker")
		}
		for _, container := range status.Containers {
			out = append(out, coverageFor("docker", container.Name, status.Running, jobs, mounted, time.Now()))
		}
	}
	if s.opts.VMs != nil {
		for _, machine := range s.opts.VMs.List(mounted) {
			out = append(out, coverageFor("vm", machine.Name, machine.Available, jobs, mounted, time.Now()))
		}
	}
	// Retain explicitly configured sources even when an inventory cannot see
	// them, so disappearing shares/apps never erase a coverage problem.
	seen := map[string]bool{}
	for _, item := range out {
		seen[item.Kind+":"+item.Name] = true
	}
	for _, view := range jobs {
		for _, name := range view.Job.Sources {
			key := "share:" + name
			if !seen[key] {
				out = append(out, coverageFor("share", name, false, jobs, mounted, time.Now()))
				seen[key] = true
			}
		}
		for _, app := range view.Job.Applications {
			key := app.Kind + ":" + app.Name
			if !seen[key] {
				out = append(out, coverageFor(app.Kind, app.Name, false, jobs, mounted, time.Now()))
				seen[key] = true
			}
		}
	}
	writeJSON(w, 200, map[string]any{"items": out, "unavailable": sources})
}
