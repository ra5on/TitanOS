package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

// shareDataUsers lists what else on the NAS uses path: disks, media and
// folders of VMs, mounts and data folders of Docker apps, and backup jobs
// that save the share or write into it.
func (s *Server) shareDataUsers(ctx context.Context, share shares.Share, path string) []shares.Usage {
	var out []shares.Usage
	add := func(kind, name, p string) {
		if p != "" && shares.Overlaps(p, path) {
			out = append(out, shares.Usage{Kind: kind, Name: name, Path: p})
		}
	}
	mounted := s.opts.Storage.Mounted()
	if s.opts.VMs != nil {
		for _, machine := range s.opts.VMs.List(mounted) {
			name := machine.DisplayName
			if name == "" {
				name = machine.Name
			}
			folder := machine.Directory
			if folder == "" {
				folder = machine.Name
			}
			add(shares.UseVM, name, filepath.Join(s.opts.Storage.MountPoint(machine.Volume), "vms", folder))
			for _, disk := range machine.Disks {
				p := disk.Path
				if p == "" {
					volume := disk.Volume
					if volume == "" {
						volume = machine.Volume
					}
					p = filepath.Join(s.opts.Storage.MountPoint(volume), "vms", folder, disk.File)
				}
				add(shares.UseVM, name, p)
			}
			add(shares.UseVM, name, machine.ISO)
			add(shares.UseVM, name, machine.VirtIOISO)
			for _, f := range machine.Folders {
				add(shares.UseVM, name, f.Path)
			}
		}
	}
	if s.opts.Docker != nil {
		inventory, cancel := context.WithTimeout(ctx, 15*time.Second)
		paths, err := s.opts.Docker.HostPaths(inventory)
		cancel()
		if err != nil {
			out = append(out, shares.Usage{Kind: shares.UseDockerUnknown})
		}
		for _, p := range paths {
			add(shares.UseDocker, p.Name, p.Path)
		}
	}
	if s.opts.Backup != nil {
		for _, view := range s.opts.Backup.CachedViews() {
			job := view.Job
			if slices.Contains(job.Sources, share.Name) || (slices.Contains(job.Extras, share.Name) && (share.Name == "appdata" || share.Name == "vms")) {
				out = append(out, shares.Usage{Kind: shares.UseBackupSource, Name: job.ID})
			}
			if job.Destination.Type == "local" && job.Destination.Volume != "" {
				add(shares.UseBackupTarget, job.ID, filepath.Join(s.opts.Storage.MountPoint(job.Destination.Volume), job.Destination.Folder))
			}
		}
	}
	return out
}

// handleShareDataPreview shows what deleting a share's data would delete
// and what stands in the way.
func (s *Server) handleShareDataPreview(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	share, found := s.findShare(name)
	if !found {
		writeShareError(w, shares.ErrNotFound)
		return
	}
	preview, err := s.opts.Shares.PreviewPurge(r.Context(), name, s.opts.Storage.Mounted(), func(ctx context.Context, path string) []shares.Usage {
		return s.shareDataUsers(ctx, share, path)
	})
	if err != nil {
		writeShareError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) findShare(name string) (shares.Share, bool) {
	for _, share := range s.opts.Shares.List() {
		if share.Name == name {
			return share, true
		}
	}
	return shares.Share{}, false
}

// handleShareDataDelete removes a share and deletes its folder as a job.
func (s *Server) handleShareDataDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	noteTarget(r, name)
	share, found := s.findShare(name)
	if !found {
		writeShareError(w, shares.ErrNotFound)
		return
	}
	user := currentUser(r).Name
	job, err := s.opts.Shares.StartPurge(r.Context(), shares.PurgeRequest{
		Name: name, Path: req.Path, By: user, Mounted: s.opts.Storage.Mounted,
		Check: func(ctx context.Context, path string) []shares.Usage { return s.shareDataUsers(ctx, share, path) },
		Done:  s.sharePurgeDone,
	})
	var blocked *shares.BlockedError
	switch {
	case errors.As(err, &blocked):
		writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{Code: "share_in_use", Message: err.Error()}, "blockers": blocked.Usages})
		return
	case err != nil:
		writeShareError(w, err)
		return
	}
	slog.Info("share removal with its data started", "share", name, "path", job.Path, "by", user, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, job)
}

// sharePurgeDone reports the end of a deletion: a notification and an
// entry in the activity log in the name of whoever started it.
func (s *Server) sharePurgeDone(job shares.PurgeJob) {
	action, severity := "share_data_deleted", notify.Info
	if job.Error != "" {
		action, severity = "share_data_delete_failed", notify.Error
	}
	if s.opts.Notify != nil {
		s.opts.Notify.Notify(severity, action, job.Share)
	}
	if s.opts.Activity != nil {
		s.opts.Activity.Record(activity.Entry{User: job.By, Via: activity.ViaSystem, Action: action, Target: job.Path, OK: job.Error == ""})
	}
}
