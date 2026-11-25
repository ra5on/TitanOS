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
		if kind == "personal" && j.Personal != nil && j.Personal.Account == name {
			matches = true
			method = "age"
		}
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
func (a backupsArea) handleBackupCoverage(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil || a.storage == nil {
		featureUnavailable(w)
		return
	}
	jobs := a.backup.List()
	mounted := a.storage.Mounted()
	out := []coverageItem{}
	for _, share := range a.shares.List() {
		out = append(out, coverageFor("share", share.Name, slices.Contains(mounted, share.Volume), jobs, mounted, time.Now()))
	}
	sources := []string{}
	if a.personalSources != nil {
		personal, err := a.personalSources()
		if err != nil {
			sources = append(sources, "personal")
		} else {
			for _, source := range personal {
				// A revoked recovery key must not make a newly enabled account
				// appear protected by a job that still addresses its old key.
				current := slices.DeleteFunc(slices.Clone(jobs), func(v backup.View) bool {
					return v.Job.Personal != nil && v.Job.Personal.Account == source.Account && v.Job.Personal.ID != source.ID
				})
				out = append(out, coverageFor("personal", source.Account, true, current, mounted, time.Now()))
			}
		}
	}
	if a.docker != nil {
		status := a.docker.Status(r.Context())
		if status.Settings.Enabled && !status.Running {
			sources = append(sources, "docker")
		}
		for _, container := range status.Containers {
			out = append(out, coverageFor("docker", container.Name, status.Running, jobs, mounted, time.Now()))
		}
	}
	if a.vms != nil {
		for _, machine := range a.vms.List(mounted) {
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
		if p := view.Job.Personal; p != nil && !seen["personal:"+p.Account] {
			out = append(out, coverageFor("personal", p.Account, false, jobs, mounted, time.Now()))
			seen["personal:"+p.Account] = true
		}
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
