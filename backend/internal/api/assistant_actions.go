package api

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// The actions of the assistant. A tool here never changes anything: it
// checks the target and describes the API route that the browser calls
// once the person confirms the card, with the session of the account.
// The level of confirmation is decided here, never by the model.
//
// Not offered at all, also with confirmation: terminal and consoles,
// secrets (backup passwords, .env, recovery and pairing codes), backups of
// the stick, plugins, certificate, SSH, network, one's own password and
// second factor. For those the assistant links the page.

type plan func(ctx context.Context, args json.RawMessage) (*assistant.Action, error)

// actionTool wraps a plan: every action has a reason, which the card shows.
func actionTool(name, description string, properties map[string]any, required []string, p plan) assistant.Tool {
	props := map[string]any{"reason": str("Why, in one short sentence for the person.")}
	for k, v := range properties {
		props[k] = v
	}
	return assistant.Tool{
		Name: name, Description: description + " Executed by TitanOS under the configured execution policy; never claim success before the result.",
		Parameters: schema(props, append(slices.Clone(required), "reason")...),
		Plan: func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			if err := assistant.ValidateArguments(schema(props, append(slices.Clone(required), "reason")...), args); err != nil {
				return nil, err
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(args, &fields); err != nil {
				return nil, assistant.Errorf("invalid arguments: %v", err)
			}
			var reason string
			_ = json.Unmarshal(fields["reason"], &reason)
			delete(fields, "reason")
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return nil, assistant.Errorf("reason is required")
			}
			rest, _ := json.Marshal(fields)
			a, err := p(ctx, rest)
			if err != nil {
				return nil, err
			}
			if r := []rune(reason); len(r) > 300 {
				reason = string(r[:300]) + " …"
			}
			a.Reason = reason
			if a.Level == "" {
				a.Level = assistant.LevelNormal
			}
			return a, nil
		},
	}
}

func card(key string, params map[string]string, method, route string, body any) *assistant.Action {
	return &assistant.Action{Key: key, Params: params, Method: method, Path: "/api/v1" + route, Body: body}
}

func withLevel(a *assistant.Action, level, confirm, field string) *assistant.Action {
	a.Level, a.Confirm, a.ConfirmField = level, confirm, field
	return a
}

func (s *Server) assistantActions(user auth.User) []assistant.Tool {
	tools := []assistant.Tool{
		actionTool("folder_create", "Propose to create a folder in the file manager.",
			map[string]any{"root": str("Root ID such as share/Media."), "path": str("Path of the new folder inside the root.")}, []string{"root", "path"},
			s.planFolder(user)),
		actionTool("file_rename", "Propose to rename or move a file or folder within one root of the file manager.",
			map[string]any{"root": str("Root ID."), "from": str("Current path inside the root."), "to": str("New path inside the root.")}, []string{"root", "from", "to"},
			s.planRename(user)),
		actionTool("file_delete", "Propose to delete one file or folder: into the recycle bin of the share if it has one, otherwise for good.",
			map[string]any{"root": str("Root ID."), "path": str("Path inside the root."), "permanent": map[string]any{"type": "boolean", "description": "Delete for good, not into the recycle bin."}}, []string{"root", "path"},
			s.planDelete(user)),
	}
	if !user.Role.CanAdminister() {
		return tools
	}
	return append(tools,
		actionTool("container_action", "Propose to start, stop or restart a Docker container.",
			map[string]any{"container": str("Name as docker_list shows it."), "action": enum("What to do.", "start", "stop", "restart")}, []string{"container", "action"},
			s.planContainer),
		actionTool("app_action", "Propose to update an app (new image, container made anew), or to remove it, keeping or deleting its data.",
			map[string]any{"app": str("Name of the app (Compose project)."), "action": enum("What to do.", "update", "remove", "remove_with_data")}, []string{"app", "action"},
			s.planApp),
		actionTool("apps_update_all", "Propose to update every app with a newer image.", nil, nil, s.planAppsUpdateAll),
		actionTool("docker_check_updates", "Propose to ask the registries for new images now.", nil, nil, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			return card("docker.check", nil, "POST", "/docker/updates/check", nil), assistant.DecodeArgs(args, &struct{}{})
		}),
		actionTool("vm_action", "Propose to start, shut down, reboot or force off a virtual machine, or to remove it, keeping or deleting its disks.",
			map[string]any{"vm": str("Name as vm_list shows it."), "action": enum("What to do.", "start", "shutdown", "reboot", "poweroff", "remove", "remove_with_disks")}, []string{"vm", "action"},
			s.planVM),
		actionTool("backup_action", "Propose to run a backup job now, or to stop a running one.",
			map[string]any{"job": str("ID as backup_status shows it."), "action": enum("What to do.", "run", "stop")}, []string{"job", "action"},
			s.planBackup),
		actionTool("volume_action", "Propose to start or stop the check (scrub) of a volume, or to delete a volume with all its data.",
			map[string]any{"volume": str("Name of the volume."), "action": enum("What to do.", "check", "stop_check", "delete")}, []string{"volume", "action"},
			s.planVolume),
		actionTool("volume_create", "Propose to create a new volume from free disks. Everything on these disks is erased.",
			map[string]any{
				"name":       str("Name: ASCII letters (case preserved), digits, -, at most 12 characters, starting with a letter."),
				"level":      enum("RAID level. A Btrfs volume is always raid1 with exactly two disks.", "raid0", "raid1", "raid5", "raid6", "raid10"),
				"filesystem": enum("File system.", "ext4", "xfs", "btrfs"),
				"disks":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Disk names such as sdb, as storage_overview shows them."},
			}, []string{"name", "level", "filesystem", "disks"},
			s.planVolumeCreate),
		actionTool("disk_test", "Propose to test a disk that is in no volume: read only (data stays) or overwrite and read back (erases everything).",
			map[string]any{"disk": str("Disk name such as sdb."), "mode": enum("read keeps the data, write erases it.", "read", "write")}, []string{"disk", "mode"},
			s.planDiskTest),
		actionTool("share_create", "Propose to create a new SMB share on a volume; the account creating it may read and write.",
			map[string]any{"name": str("Name of the share."), "volume": str("Volume it lives on.")}, []string{"name", "volume"},
			s.planShareCreate),
		actionTool("share_delete", "Propose to remove a share. Its files stay on the volume.",
			map[string]any{"share": str("Name of the share.")}, []string{"share"}, s.planShareDelete(user)),
		actionTool("account_action", "Propose to change the role of an account or to delete it.",
			map[string]any{"account": str("Name of the account."), "action": enum("What to do.", "make_owner", "make_admin", "make_user", "delete")}, []string{"account", "action"},
			s.planAccount(user)),
		actionTool("update_action", "Propose to look for a TitanOS update, install the one found, go back to the previous version, undo a pending switch, or go back to the previous app version.",
			map[string]any{"action": enum("What to do.", "check", "install", "rollback", "cancel", "app_rollback")}, []string{"action"},
			s.planUpdate),
		actionTool("power_action", "Propose to restart or shut down the whole NAS. Containers and VMs are stopped cleanly.",
			map[string]any{"action": enum("What to do.", "reboot", "poweroff")}, []string{"action"}, s.planPower),
		actionTool("stick_backup", "Propose to make a backup of the settings on the stick now.", nil, nil, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			return card("stick.backup", nil, "POST", "/config-backup/run", nil), assistant.DecodeArgs(args, &struct{}{})
		}),
	)
}

func esc(s string) string { return url.PathEscape(s) }

func (s *Server) planContainer(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Container, Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"start", "stop", "restart"}, req.Action) {
		return nil, assistant.Errorf("action must be start, stop or restart")
	}
	name := strings.TrimPrefix(strings.TrimSpace(req.Container), "/")
	for _, c := range s.opts.Docker.Status(ctx).Containers {
		if c.Name == name {
			return card("container."+req.Action, map[string]string{"name": c.Name}, "POST", "/docker/containers/"+esc(c.ID)+"/"+req.Action, nil), nil
		}
	}
	return nil, assistant.Errorf("no container %q; docker_list shows the names", req.Container)
}

func (s *Server) planApp(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ App, Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	status := s.opts.Docker.Status(ctx)
	_, failed := status.Jobs[req.App]
	if !slices.Contains(status.Apps, req.App) && !failed {
		return nil, assistant.Errorf("no app %q; docker_list shows the app of each container", req.App)
	}
	params := map[string]string{"name": req.App}
	switch req.Action {
	case "update":
		return card("app.update", params, "POST", "/docker/apps/"+esc(req.App)+"/update", nil), nil
	case "remove":
		return withLevel(card("app.remove", params, "DELETE", "/docker/apps/"+esc(req.App), nil), assistant.LevelDanger, "", ""), nil
	case "remove_with_data":
		return withLevel(card("app.purge", params, "DELETE", "/docker/apps/"+esc(req.App), nil), assistant.LevelDestroy, req.App, "query:deleteData"), nil
	}
	return nil, assistant.Errorf("action must be update, remove or remove_with_data")
}

func (s *Server) planAppsUpdateAll(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	if err := assistant.DecodeArgs(args, &struct{}{}); err != nil {
		return nil, err
	}
	apps := s.opts.Docker.AppsWithUpdates(ctx)
	if len(apps) == 0 {
		return nil, assistant.Errorf("no app has a newer image; docker_check_updates asks the registries again")
	}
	return card("apps.updateAll", map[string]string{"apps": strings.Join(apps, ", ")}, "POST", "/docker/updates/apply", nil), nil
}

func (s *Server) planVM(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ VM, Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	found := false
	for _, vm := range s.opts.VMs.List(nil) {
		found = found || vm.Name == req.VM
	}
	if !found {
		return nil, assistant.Errorf("no VM %q; vm_list shows the names", req.VM)
	}
	params := map[string]string{"name": req.VM}
	route := "/vms/" + esc(req.VM)
	switch req.Action {
	case "start", "shutdown", "reboot":
		return card("vm."+req.Action, params, "POST", route+"/"+req.Action, nil), nil
	case "poweroff":
		return withLevel(card("vm.poweroff", params, "POST", route+"/poweroff", nil), assistant.LevelDanger, "", ""), nil
	case "remove":
		return withLevel(card("vm.remove", params, "DELETE", route, nil), assistant.LevelDanger, "", ""), nil
	case "remove_with_disks":
		return withLevel(card("vm.purge", params, "DELETE", route, map[string]any{"deleteDisks": true}), assistant.LevelDestroy, req.VM, "body:confirm"), nil
	}
	return nil, assistant.Errorf("unknown action %q", req.Action)
}

func (s *Server) planBackup(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Job, Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if req.Action != "run" && req.Action != "stop" {
		return nil, assistant.Errorf("action must be run or stop")
	}
	for _, view := range s.opts.Backup.List() {
		if view.Job.ID == req.Job {
			return card("backup."+req.Action, map[string]string{"name": req.Job}, "POST", "/backups/"+esc(req.Job)+"/"+req.Action, nil), nil
		}
	}
	return nil, assistant.Errorf("no backup job %q; backup_status shows them", req.Job)
}

func (s *Server) planVolume(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Volume, Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if !slices.Contains(s.opts.Storage.Names(), req.Volume) {
		return nil, assistant.Errorf("no volume %q; storage_overview shows them", req.Volume)
	}
	params := map[string]string{"name": req.Volume}
	route := "/storage/volumes/" + esc(req.Volume)
	switch req.Action {
	case "check":
		return card("volume.check", params, "POST", route+"/check", map[string]string{"action": "start"}), nil
	case "stop_check":
		return card("volume.stopCheck", params, "POST", route+"/check", map[string]string{"action": "stop"}), nil
	case "delete":
		return withLevel(card("volume.delete", params, "DELETE", route, map[string]any{}), assistant.LevelDestroy, req.Volume, "body:confirm"), nil
	}
	return nil, assistant.Errorf("action must be check, stop_check or delete")
}

func (s *Server) planVolumeCreate(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req storage.CreateRequest
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if len(req.Disks) == 0 {
		return nil, assistant.Errorf("disks are required")
	}
	status, err := s.opts.Storage.Status(ctx)
	if err != nil {
		return nil, err
	}
	var models []string
	for _, name := range req.Disks {
		i := slices.IndexFunc(status.Disks, func(d storage.Disk) bool { return d.Name == name })
		if i < 0 {
			return nil, assistant.Errorf("no disk %q; storage_overview shows the disks", name)
		}
		d := status.Disks[i]
		if d.Volume != "" || d.Testing || d.Mounted {
			return nil, assistant.Errorf("disk %s is in use", name)
		}
		models = append(models, name+" ("+strings.TrimSpace(d.Model)+")")
	}
	body := map[string]any{"name": req.Name, "level": req.Level, "filesystem": req.Filesystem, "disks": req.Disks, "erase": req.Disks}
	return withLevel(card("volume.create", map[string]string{"name": req.Name, "level": req.Level, "filesystem": req.Filesystem, "disks": strings.Join(models, ", ")},
		"POST", "/storage/volumes", body), assistant.LevelDestroy, req.Name, ""), nil
}

func (s *Server) planDiskTest(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Disk, Mode string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	status, err := s.opts.Storage.Status(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(status.Disks, func(d storage.Disk) bool { return d.Name == req.Disk })
	if i < 0 {
		return nil, assistant.Errorf("no disk %q; storage_overview shows the disks", req.Disk)
	}
	params := map[string]string{"disk": req.Disk, "model": strings.TrimSpace(status.Disks[i].Model)}
	switch req.Mode {
	case storage.TestRead:
		return card("disk.testRead", params, "POST", "/storage/tests", map[string]string{"disk": req.Disk, "mode": storage.TestRead}), nil
	case storage.TestWrite:
		return withLevel(card("disk.testWrite", params, "POST", "/storage/tests", map[string]string{"disk": req.Disk, "mode": storage.TestWrite}),
			assistant.LevelDestroy, req.Disk, "body:confirm"), nil
	}
	return nil, assistant.Errorf("mode must be read or write")
}

func (s *Server) planShareCreate(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Name, Volume string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if !slices.Contains(s.opts.Storage.Names(), req.Volume) {
		return nil, assistant.Errorf("no volume %q", req.Volume)
	}
	body := map[string]any{"name": req.Name, "volume": req.Volume, "smb": true, "browseable": true}
	return card("share.create", map[string]string{"name": req.Name, "volume": req.Volume}, "POST", "/shares", body), nil
}

func (s *Server) planShareDelete(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req struct{ Share string }
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		for _, share := range s.opts.Shares.List() {
			if share.Name == req.Share {
				return withLevel(card("share.delete", map[string]string{"name": share.Name}, "DELETE", "/shares/"+esc(share.Name), nil), assistant.LevelDanger, "", ""), nil
			}
		}
		return nil, assistant.Errorf("no share %q; shares_list shows them", req.Share)
	}
}

func (s *Server) planAccount(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req struct{ Account, Action string }
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		target, ok := s.opts.Users.Get(req.Account)
		if !ok {
			return nil, assistant.Errorf("no account %q", req.Account)
		}
		params := map[string]string{"name": target.Name}
		route := "/users/" + esc(target.Name)
		if req.Action == "delete" {
			if target.Name == user.Name {
				return nil, assistant.Errorf("the account you are logged in with cannot be deleted")
			}
			return withLevel(card("account.delete", params, "DELETE", route, nil), assistant.LevelDestroy, target.Name, ""), nil
		}
		role, known := map[string]auth.Role{"make_owner": auth.RoleOwner, "make_admin": auth.RoleAdmin, "make_user": auth.RoleUser}[req.Action]
		if !known {
			return nil, assistant.Errorf("unknown action %q", req.Action)
		}
		params["role"] = string(role)
		return withLevel(card("account.role", params, "PUT", route, map[string]any{"role": role}), assistant.LevelDestroy, target.Name, ""), nil
	}
}

func (s *Server) planUpdate(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if s.opts.Update == nil {
		return nil, assistant.Errorf("updates are not available on this system")
	}
	switch req.Action {
	case "check", "cancel":
		return card("update."+req.Action, nil, "POST", "/update/"+req.Action, nil), nil
	case "install":
		params := map[string]string{}
		if a := s.opts.Update.Status().Available; a != nil {
			params["version"] = a.Version
		} else {
			return nil, assistant.Errorf("no update was found; check first")
		}
		return withLevel(card("update.install", params, "POST", "/update/install", nil), assistant.LevelDanger, "", ""), nil
	case "rollback":
		return withLevel(card("update.rollback", nil, "POST", "/update/rollback", nil), assistant.LevelDanger, "", ""), nil
	case "app_rollback":
		return withLevel(card("update.appRollback", nil, "POST", "/update/app-rollback", nil), assistant.LevelDanger, "", ""), nil
	}
	return nil, assistant.Errorf("unknown action %q", req.Action)
}

func (s *Server) planPower(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
	var req struct{ Action string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	if req.Action != "reboot" && req.Action != "poweroff" {
		return nil, assistant.Errorf("action must be reboot or poweroff")
	}
	return withLevel(card("power."+req.Action, nil, "POST", "/system/power", map[string]string{"action": req.Action}), assistant.LevelDanger, "", ""), nil
}

// fileTarget checks a path in a root of the file manager for an action.
func (s *Server) fileTarget(user auth.User, rootID, p string, write bool) (string, error) {
	root, err := s.assistantRoot(user, rootID)
	if err != nil {
		return "", err
	}
	if write && !root.Writable {
		return "", assistant.Errorf("this account may not write in %s", root.Name)
	}
	if strings.HasPrefix(p, "/") || strings.ContainsRune(p, '\\') || slices.Contains(strings.Split(p, "/"), "..") {
		return "", assistant.CodedErrorf("invalid_path", "use a relative path inside the selected root without ..")
	}
	clean := strings.TrimPrefix(path.Clean("/"+p), "/")
	if clean == "" || hiddenPath(clean) {
		return "", assistant.Errorf("invalid path %q", p)
	}
	return clean, nil
}

func (s *Server) planFolder(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req struct{ Root, Path string }
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		p, err := s.fileTarget(user, req.Root, req.Path, true)
		if err != nil {
			return nil, err
		}
		return card("folder.create", map[string]string{"root": req.Root, "path": p}, "POST", "/files/mkdir", map[string]string{"root": req.Root, "path": p}), nil
	}
}

func (s *Server) planRename(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req struct{ Root, From, To string }
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		from, err := s.fileTarget(user, req.Root, req.From, true)
		if err != nil {
			return nil, err
		}
		to, err := s.fileTarget(user, req.Root, req.To, true)
		if err != nil {
			return nil, err
		}
		return card("file.rename", map[string]string{"root": req.Root, "from": from, "to": to}, "POST", "/files/rename",
			map[string]string{"root": req.Root, "from": from, "to": to}), nil
	}
}

func (s *Server) planDelete(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req struct {
			Root, Path string
			Permanent  bool
		}
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		p, err := s.fileTarget(user, req.Root, req.Path, true)
		if err != nil {
			return nil, err
		}
		root, _ := s.assistantRoot(user, req.Root)
		params := map[string]string{"root": req.Root, "path": p}
		body := map[string]any{"root": req.Root, "paths": []string{p}, "permanent": req.Permanent}
		if req.Permanent || !root.Recycle {
			return withLevel(card("file.delete", params, "POST", "/files/delete", body), assistant.LevelDestroy, path.Base(p), ""), nil
		}
		return withLevel(card("file.trash", params, "POST", "/files/delete", body), assistant.LevelDanger, "", ""), nil
	}
}
