package api

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
	"io"
	"time"
)

// Which tools the model gets is decided for every question from the role
// of the account: a tool the role may not use is not even offered. The
// tools check the account themselves as well. See docs/MCP.md.

func schema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	s := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func noArgs(call func(ctx context.Context) (any, error)) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var none struct{}
		if err := toolcatalog.DecodeArgs(args, &none); err != nil {
			return nil, err
		}
		return call(ctx)
	}
}

// clampLines keeps a number of lines between 1 and 200, 50 if unset.
func clampLines(lines int) int {
	if lines <= 0 {
		return 50
	}
	return min(lines, 200)
}

func reading(name, description string, parameters map[string]any, untrusted bool, call func(context.Context, json.RawMessage) (any, error)) toolcatalog.Tool {
	if parameters == nil {
		parameters = schema(nil)
	}
	return toolcatalog.Tool{Name: name, Description: description, Parameters: parameters, Untrusted: untrusted,
		Read: func(ctx context.Context, args json.RawMessage) (toolcatalog.Result, error) {
			v, err := call(ctx, args)
			if err != nil {
				return toolcatalog.Result{}, err
			}
			return toolcatalog.JSON(v)
		}}
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enum(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

// toolAreas says which part of the NAS a tool belongs to, for grouping the
// tools on the MCP page.
var toolAreas = map[string]string{
	"docs_search": toolcatalog.AreaSystem, "docs_read": toolcatalog.AreaSystem, "files_list": toolcatalog.AreaFiles, "document_read": toolcatalog.AreaFiles, "file_read": toolcatalog.AreaFiles, "file_jobs": toolcatalog.AreaFiles, "files_search": toolcatalog.AreaFiles,
	"folder_create": toolcatalog.AreaFiles, "file_rename": toolcatalog.AreaFiles, "file_delete": toolcatalog.AreaFiles,
	"file_write": toolcatalog.AreaFiles, "files_move": toolcatalog.AreaFiles, "file_copy": toolcatalog.AreaFiles,
	"system_status": toolcatalog.AreaSystem, "notifications": toolcatalog.AreaSystem, "nas_info": toolcatalog.AreaSystem, "my_shares": toolcatalog.AreaFiles,
	"network_status": toolcatalog.AreaSystem, "hardware_status": toolcatalog.AreaSystem, "logs_search": toolcatalog.AreaSystem, "ups_status": toolcatalog.AreaSystem,
	"update_status": toolcatalog.AreaUpdates, "update_action": toolcatalog.AreaUpdates, "power_action": toolcatalog.AreaUpdates,
	"storage_overview": toolcatalog.AreaStorage, "disk_tests": toolcatalog.AreaStorage, "volume_action": toolcatalog.AreaStorage,
	"volume_create": toolcatalog.AreaStorage, "disk_test": toolcatalog.AreaStorage,
	"shares_list": toolcatalog.AreaShares, "share_create": toolcatalog.AreaShares, "share_delete": toolcatalog.AreaShares,
	"docker_list": toolcatalog.AreaDocker, "container_logs": toolcatalog.AreaDocker, "container_action": toolcatalog.AreaDocker,
	"app_action": toolcatalog.AreaDocker, "apps_update_all": toolcatalog.AreaDocker, "docker_check_updates": toolcatalog.AreaDocker,
	"vm_list": toolcatalog.AreaVMs, "vm_action": toolcatalog.AreaVMs,
	"backup_status": toolcatalog.AreaBackups, "backup_action": toolcatalog.AreaBackups, "stick_backup": toolcatalog.AreaBackups,
	"accounts_list": toolcatalog.AreaAccounts, "account_action": toolcatalog.AreaAccounts,
	"plugins_list": toolcatalog.AreaPlugins, "plugin_install": toolcatalog.AreaPlugins, "plugin_switch": toolcatalog.AreaPlugins, "plugin_update": toolcatalog.AreaPlugins, "plugin_remove": toolcatalog.AreaPlugins,
	"network_diagnostics": toolcatalog.AreaNetwork, "firewall_status": toolcatalog.AreaNetwork, "firewall_rule_add": toolcatalog.AreaNetwork, "firewall_rule_remove": toolcatalog.AreaNetwork, "firewall_unblock": toolcatalog.AreaNetwork,
	"backup_snapshots": toolcatalog.AreaBackups, "backup_files": toolcatalog.AreaBackups, "backup_check": toolcatalog.AreaBackups, "backup_restore_files": toolcatalog.AreaBackups, "backup_schedule": toolcatalog.AreaBackups,
	"docker_auto_updates": toolcatalog.AreaDocker, "docker_auto_update": toolcatalog.AreaDocker,
	"ha_diagnostics": toolcatalog.AreaHome, "ha_capabilities": toolcatalog.AreaHome, "ha_undo": toolcatalog.AreaHome, "ha_history": toolcatalog.AreaHome, "ha_entities": toolcatalog.AreaHome, "ha_state": toolcatalog.AreaHome, "ha_call": toolcatalog.AreaHome,
}

// catalogTools are all tools of the fixed catalog for an account: those of
// its role, each with its area. The tools check the account themselves as
// well, every time they run.
func (s *Server) catalogTools(user auth.User) []toolcatalog.Tool {
	tools := []toolcatalog.Tool{
		reading("docs_search", "Search the TitanOS manual (README and documentation) for how something works or is set up. Returns the best matching sections.",
			schema(map[string]any{"query": str("Words to look for, in English or German.")}, "query"), false, s.toolDocsSearch),
		reading("docs_read", "Read one document of the TitanOS manual, or only its sections whose heading contains the given text.",
			schema(map[string]any{"file": str("File name as docs_search shows it, e.g. docs/NETWORKING.md."), "heading": str("Part of a heading, optional.")}, "file"), false, s.toolDocsRead),
	}
	tools = append(tools, s.statusTools(user)...)
	tools = append(tools, s.fileTools(user)...)
	if user.Role.CanAdminister() {
		tools = append(tools, s.areaTools()...)
		tools = append(tools, s.haTools(user)...)
	}
	tools = append(tools, s.catalogActions(user)...)
	for i := range tools {
		tools[i].Area = toolAreas[tools[i].Name]
	}
	return tools
}

// statusTools are the reading tools of the fixed catalog about the state of
// the NAS and the files of the account.
func (s *Server) statusTools(user auth.User) []toolcatalog.Tool {
	tools := []toolcatalog.Tool{
		reading("files_list", "List a folder in the file manager: the shares this account may open (root share/<name>) and, for administrators, volumes (volume/<name>) and USB drives. Without root, lists the roots.",
			schema(map[string]any{"root": str("Root ID such as share/Media, from the list of roots."), "path": str("Folder inside the root, empty for its top."), "offset": map[string]any{"type": "integer", "minimum": 0, "description": "Next page from nextOffset when cut is true."}}), true, s.catalogFilesList(user)),
		reading("file_read", "Read a text file (txt, md, json, yaml, conf, log, csv …) or a Word (.docx) Excel (.xlsx) or PDF document (first 20 pages; document_read for further pages) from the file manager, with the rights of this account.",
			schema(map[string]any{"root": str("Root ID such as share/Media."), "path": str("Path of the file inside the root.")}, "root", "path"), true, s.catalogFileRead(user)),
		reading("file_jobs", "Copy and move jobs of this account in the file manager, with progress.",
			nil, false, noArgs(func(ctx context.Context) (any, error) {
				return map[string]any{"jobs": s.opts.Files.Jobs(user.Name)}, nil
			})),
	}
	if user.Role.CanAdminister() {
		linesArg := map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "How many of the newest lines, 50 if left out."}
		tools = append(tools,
			reading("system_status", "Name, version, uptime, CPU load, memory and temperatures of the NAS.", nil, false, noArgs(s.toolSystemStatus)),
			reading("notifications", "The latest notifications of the NAS (disk and volume problems, backups, updates …), newest first, kept since the last start.",
				schema(map[string]any{
					"limit":       map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "How many, 20 if left out."},
					"minSeverity": enum("Leave out less severe ones.", "info", "warning", "error"),
				}), true, s.toolNotifications),
			reading("update_status", "The running TitanOS version, the system in the other slot to fall back to, and a newer release if one was found.", nil, false, noArgs(s.toolUpdateStatus)),
			reading("ups_status", "State of the uninterruptible power supply: on battery, charge, runtime left.", nil, false, noArgs(s.toolUPSStatus)),
			reading("storage_overview", "Volumes (RAID level, state, size, use, rebuild or check in progress) and disks (model, size, role, SMART health, temperature, errors, wear).", nil, false, noArgs(s.toolStorage)),
			reading("disk_tests", "Running and finished disk tests (read test, overwrite test, SMART self-test) with progress and result.", nil, false,
				noArgs(func(ctx context.Context) (any, error) { return map[string]any{"tests": s.opts.Storage.Tests()}, nil })),
			reading("shares_list", "The SMB/NFS shares with their volume and whether they are available.", nil, false, noArgs(s.toolShares)),
			reading("docker_list", "Docker containers with state, image, app, CPU, memory, ports and whether an image update is available.", nil, true, noArgs(s.toolDocker)),
			reading("container_logs", "The newest log lines of one container. Addresses, account names and secrets are replaced by placeholders.",
				schema(map[string]any{"container": str("Name of the container, as docker_list shows it."), "lines": linesArg}, "container"), true, s.toolContainerLogs),
			reading("vm_list", "Virtual machines with state, vCPUs, memory, load and the addresses of running ones.", nil, false, noArgs(s.toolVMs)),
			reading("backup_status", "Backup jobs with where they go, the last run and its result, the last backup that worked, the next run, and whether a job is overdue.", nil, false, noArgs(s.toolBackups)),
			reading("network_status", "Network ports, addresses, bond, VLAN, bridge, gateway and name servers of the NAS, and a change waiting for confirmation.", nil, false,
				noArgs(func(ctx context.Context) (any, error) { return s.opts.Network.Status(ctx), nil })),
			reading("hardware_status", "PCI and USB devices (graphics cards, NPUs, controllers), IOMMU, drivers and which VM uses a device.", nil, false,
				noArgs(func(ctx context.Context) (any, error) {
					used := map[string][]string{}
					for _, machine := range s.opts.VMs.List(nil) {
						for _, device := range append(append([]string{}, machine.PCI...), machine.USB...) {
							used[device] = append(used[device], machine.Name)
						}
					}
					return map[string]any{"hardware": s.opts.Hardware.Status(), "usedBy": used}, nil
				})),
			reading("accounts_list", "The accounts of the NAS with their role and whether they use two-factor login.", nil, false, noArgs(func(ctx context.Context) (any, error) {
				type account struct {
					Name      string `json:"name"`
					Role      string `json:"role"`
					TwoFactor bool   `json:"twoFactor"`
				}
				list := []account{}
				for _, u := range s.opts.Users.List() {
					list = append(list, account{u.Name, string(u.Role), u.TOTPSecret != ""})
				}
				return map[string]any{"accounts": list}, nil
			})),
			reading("logs_search", "Lines of the system log since the last start, oldest first. Addresses, account names and secrets are replaced by placeholders.",
				schema(map[string]any{
					"source":   enum("Part of the system, all if left out.", "all", "titand", "kernel", "storage", "shares", "docker", "vms", "network"),
					"minLevel": enum("Least severe level to show, info if left out.", "error", "warning", "info"),
					"search":   map[string]any{"type": "string", "maxLength": 200, "description": "Only lines containing this text (case does not matter)."},
					"lines":    linesArg,
				}), true, s.toolLogs),
		)
	} else {
		tools = append(tools,
			reading("nas_info", "Name and TitanOS version of the NAS.", nil, false, noArgs(func(ctx context.Context) (any, error) {
				return map[string]any{"hostname": s.opts.Monitor.Info().Hostname, "version": s.opts.Version}, nil
			})),
			reading("my_shares", "The shares this account may open, whether it may write, and the free space of each.", nil, false, noArgs(func(ctx context.Context) (any, error) {
				return s.catalogMyShares(user), nil
			})),
		)
	}
	return tools
}

func (s *Server) catalogMyShares(user auth.User) map[string]any {
	type share struct {
		Root      string `json:"root"`
		Name      string `json:"name"`
		Writable  bool   `json:"writable"`
		FreeBytes uint64 `json:"freeBytes"`
		Recycle   bool   `json:"recycleBin"`
	}
	list := []share{}
	for _, root := range s.fileRoots(user) {
		if root.Kind != "share" {
			continue
		}
		item := share{Root: root.ID, Name: root.Name, Writable: root.Writable, Recycle: root.Recycle}
		var st syscall.Statfs_t
		if syscall.Statfs(root.Dir, &st) == nil {
			item.FreeBytes = st.Bavail * uint64(st.Bsize)
		}
		list = append(list, item)
	}
	return map[string]any{"shares": list}
}

// hiddenPath reports whether MCP must not see a path: what
// TitanOS keeps on a volume (chats, backups of the stick) and .env files.
func hiddenPath(p string) bool {
	for _, part := range strings.Split(path.Clean("/"+p), "/") {
		lower := strings.ToLower(part)
		if lower == ".titanos" || lower == ".env" || strings.HasSuffix(lower, ".env") || strings.HasSuffix(lower, ".titanos-part") {
			return true
		}
	}
	return false
}

// Preserve error categories without putting paths or file contents in the
// default diagnostic export. The model still receives actionable details.
func catalogFileError(err error) error {
	code, next := "file_failed", "Inspect the target and its availability before retrying."
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code, next = "tool_timeout", "Search a narrower subfolder; this is not evidence that no files exist."
	case errors.Is(err, files.ErrNotFound):
		code, next = "file_not_found", "Use files_list to discover an existing relative path."
	case errors.Is(err, files.ErrInvalid):
		code, next = "invalid_path", "Use a relative path inside the exact root ID from files_list."
	case errors.Is(err, files.ErrReadOnly):
		code, next = "file_read_only", "Choose an accessible writable root; do not bypass permissions."
	case errors.Is(err, files.ErrProtected):
		code, next = "file_protected", "This path is managed by a VM and cannot be accessed here."
	case errors.Is(err, files.ErrNoSpace):
		code = "file_no_space"
	}
	return toolcatalog.CodedErrorf(code, "%v. %s", err, next)
}

// catalogRoot finds a root of the file manager for MCP: the
// same as the account has, without the stick.
func (s *Server) catalogRoot(user auth.User, id string) (files.Root, error) {
	for _, root := range s.fileRoots(user) {
		if root.ID == id && root.Kind != "stick" {
			return root, nil
		}
	}
	ids := []string{}
	for _, root := range s.fileRoots(user) {
		if root.Kind != "stick" {
			ids = append(ids, root.ID)
		}
	}
	return files.Root{}, toolcatalog.CodedErrorf("root_not_found", "no root %q; use an exact root ID from %v. Call files_list with {} first. A root such as volume/nvme is one value; do not split it into root=volume and path=nvme", id, ids)
}

func (s *Server) catalogFilesList(user auth.User) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var req struct {
			Root, Path string
			Offset     int
		}
		if err := toolcatalog.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		if req.Root == "" {
			type rootInfo struct {
				ID       string `json:"root"`
				Name     string `json:"name"`
				Kind     string `json:"kind"`
				Writable bool   `json:"writable"`
				Recycle  bool   `json:"recycleBin"`
			}
			roots := []rootInfo{}
			for _, root := range s.fileRoots(user) {
				if root.Kind != "stick" {
					roots = append(roots, rootInfo{root.ID, root.Name, root.Kind, root.Writable, root.Recycle})
				}
			}
			return map[string]any{"roots": roots}, nil
		}
		root, err := s.catalogRoot(user, req.Root)
		if err != nil {
			return nil, err
		}
		if hiddenPath(req.Path) {
			return nil, toolcatalog.Errorf("this folder is not shown over MCP")
		}
		entries, err := s.opts.Files.List(root, req.Path)
		if err != nil {
			return nil, catalogFileError(err)
		}
		entries = slices.DeleteFunc(entries, func(e files.Entry) bool { return hiddenPath(e.Name) })
		offset := max(0, min(req.Offset, len(entries)))
		end := min(offset+100, len(entries))
		return map[string]any{"root": root.ID, "path": req.Path, "entries": entries[offset:end], "total": len(entries), "nextOffset": end, "cut": end < len(entries)}, nil
	}
}

func (s *Server) catalogFileRead(user auth.User) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var req struct{ Root, Path string }
		if err := toolcatalog.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		root, err := s.catalogRoot(user, req.Root)
		if err != nil {
			return nil, err
		}
		if hiddenPath(req.Path) {
			return nil, toolcatalog.Errorf("this file is not shown over MCP")
		}
		if ext := strings.ToLower(path.Ext(req.Path)); ext == ".docx" || ext == ".xlsx" {
			text, err := s.readDocument(root, req.Path)
			if err != nil {
				return nil, catalogFileError(err)
			}
			return map[string]any{"root": root.ID, "path": req.Path, "content": text}, nil
		}
		if strings.EqualFold(path.Ext(req.Path), ".pdf") {
			return s.readPDF(ctx, root, req.Path, 1, 20)
		}
		text, err := s.opts.Files.ReadText(root, req.Path)
		if err != nil {
			return nil, catalogFileError(err)
		}
		return map[string]any{"root": root.ID, "path": req.Path, "content": text.Content}, nil
	}
}

func (s *Server) toolDocsSearch(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct{ Query string }
	if err := toolcatalog.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	manual := s.manual
	if len(manual.Files()) == 0 {
		return nil, toolcatalog.Errorf("the manual is not part of this build of titand")
	}
	sections := manual.Search(req.Query, 6)
	if len(sections) == 0 {
		return map[string]any{"sections": []any{}, "files": manual.Files()}, nil
	}
	return map[string]any{"sections": sections}, nil
}

func (s *Server) toolDocsRead(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct{ File, Heading string }
	if err := toolcatalog.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	text, ok := s.manual.Read(req.File, req.Heading)
	if !ok {
		return nil, toolcatalog.Errorf("no document %q with that heading; documents: %s", req.File, strings.Join(s.manual.Files(), ", "))
	}
	return map[string]any{"file": req.File, "text": text}, nil
}

func (s *Server) readPDF(ctx context.Context, root files.Root, path string, first, last int, ocr ...bool) (any, error) {
	f, info, err := s.opts.Files.Open(root, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info.Size() > 50<<20 {
		return nil, toolcatalog.Errorf("PDF exceeds 50 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (50<<20)+1))
	if err != nil {
		return nil, err
	}
	var result office.PDFReading
	if len(ocr) > 0 && ocr[0] {
		result, err = office.OCRPDF(ctx, data, first, last)
	} else {
		result, err = office.ReadPDF(ctx, data, first, last)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"root": root.ID, "path": path, "document": result, "observedAt": time.Now()}, nil
}
