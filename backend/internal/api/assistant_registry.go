package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
)

// Which tools the model gets is decided for every question from the role
// of the account: a tool the role may not use is not even offered. The
// tools check the account themselves as well. See docs/ASSISTANT.md.

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
		if err := assistant.DecodeArgs(args, &none); err != nil {
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

func reading(name, description string, parameters map[string]any, untrusted bool, call func(context.Context, json.RawMessage) (any, error)) assistant.Tool {
	if parameters == nil {
		parameters = schema(nil)
	}
	return assistant.Tool{Name: name, Description: description, Parameters: parameters, Untrusted: untrusted,
		Read: func(ctx context.Context, args json.RawMessage) (assistant.Result, error) {
			v, err := call(ctx, args)
			if err != nil {
				return assistant.Result{}, err
			}
			return assistant.JSON(v)
		}}
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enum(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

// assistantAllowed reports whether an account may chat at all.
func (s *Server) assistantAllowed(user auth.User) bool {
	settings := s.opts.Assistant.Settings()
	return settings.Enabled && (user.Role.CanAdminister() || settings.Users)
}

// assistantWeb reports whether an account may search the web.
func (s *Server) assistantWeb(user auth.User) bool {
	settings := s.opts.Assistant.Settings()
	return settings.Web && (user.Role.CanAdminister() || settings.WebUsers)
}

// toolAreas says which part of the NAS a tool belongs to, for the settings
// of what the assistant may do there. Tools not listed are always there.
var toolAreas = map[string]string{
	"files_list": assistant.AreaFiles, "file_read": assistant.AreaFiles, "file_jobs": assistant.AreaFiles, "files_search": assistant.AreaFiles,
	"folder_create": assistant.AreaFiles, "file_rename": assistant.AreaFiles, "file_delete": assistant.AreaFiles,
	"file_write": assistant.AreaFiles, "files_move": assistant.AreaFiles, "file_copy": assistant.AreaFiles,
	"network_status": assistant.AreaSystem, "hardware_status": assistant.AreaSystem, "logs_search": assistant.AreaSystem, "ups_status": assistant.AreaSystem,
	"update_status": assistant.AreaUpdates, "update_action": assistant.AreaUpdates, "power_action": assistant.AreaUpdates,
	"storage_overview": assistant.AreaStorage, "disk_tests": assistant.AreaStorage, "volume_action": assistant.AreaStorage,
	"volume_create": assistant.AreaStorage, "disk_test": assistant.AreaStorage,
	"shares_list": assistant.AreaShares, "share_create": assistant.AreaShares, "share_delete": assistant.AreaShares,
	"docker_list": assistant.AreaDocker, "container_logs": assistant.AreaDocker, "container_action": assistant.AreaDocker,
	"app_action": assistant.AreaDocker, "apps_update_all": assistant.AreaDocker, "docker_check_updates": assistant.AreaDocker,
	"vm_list": assistant.AreaVMs, "vm_action": assistant.AreaVMs,
	"backup_status": assistant.AreaBackups, "backup_action": assistant.AreaBackups, "stick_backup": assistant.AreaBackups,
	"accounts_list": assistant.AreaAccounts, "account_action": assistant.AreaAccounts,
}

// assistantTools are the tools of an account: those of its role, in the
// areas the settings allow, and only reading ones where they allow reading.
// r is the request of the chat, whose login api_read uses.
func (s *Server) assistantTools(user auth.User, r *http.Request) []assistant.Tool {
	settings := s.opts.Assistant.Settings()
	var kept []assistant.Tool
	writes := false
	for _, tool := range s.allAssistantTools(user, r) {
		if area := toolAreas[tool.Name]; area != "" {
			tool.Area = area
			switch settings.Access(area) {
			case assistant.AccessOff:
				continue
			case assistant.AccessRead:
				if tool.Plan != nil {
					continue
				}
			}
		}
		writes = writes || tool.Plan != nil
		kept = append(kept, tool)
	}
	if writes {
		kept = append(kept, s.taskTool())
	}
	return kept
}

func (s *Server) allAssistantTools(user auth.User, r *http.Request) []assistant.Tool {
	tools := []assistant.Tool{
		reading("docs_search", "Search the TitanOS manual (README and documentation) for how something works or is set up. Returns the best matching sections.",
			schema(map[string]any{"query": str("Words to look for, in English or German.")}, "query"), false, s.toolDocsSearch),
		reading("docs_read", "Read one document of the TitanOS manual, or only its sections whose heading contains the given text.",
			schema(map[string]any{"file": str("File name as docs_search shows it, e.g. docs/NETWORKING.md."), "heading": str("Part of a heading, optional.")}, "file"), false, s.toolDocsRead),
		reading("files_list", "List a folder in the file manager: the shares this account may open (root share/<name>) and, for administrators, volumes (volume/<name>) and USB drives. Without root, lists the roots.",
			schema(map[string]any{"root": str("Root ID such as share/Media, from the list of roots."), "path": str("Folder inside the root, empty for its top.")}), true, s.assistantFilesList(user)),
		reading("file_read", "Read a text file (txt, md, json, yaml, conf, log, csv …) or a Word (.docx) or Excel (.xlsx) document from the file manager, with the rights of this account.",
			schema(map[string]any{"root": str("Root ID such as share/Media."), "path": str("Path of the file inside the root.")}, "root", "path"), true, s.assistantFileRead(user)),
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
				return s.assistantMyShares(user), nil
			})),
		)
	}
	if s.assistantWeb(user) {
		tools = append(tools, s.webTools()...)
	}
	tools = append(tools, s.fileTools(user)...)
	if user.Role.CanAdminister() {
		tools = append(tools, s.apiTools(user, r)...)
	}
	return append(tools, s.assistantActions(user)...)
}

func (s *Server) assistantMyShares(user auth.User) map[string]any {
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

// hiddenPath reports whether the assistant must not see a path: what
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

// assistantRoot finds a root of the file manager for the assistant: the
// same as the account has, without the stick.
func (s *Server) assistantRoot(user auth.User, id string) (files.Root, error) {
	for _, root := range s.fileRoots(user) {
		if root.ID == id && root.Kind != "stick" {
			return root, nil
		}
	}
	return files.Root{}, assistant.Errorf("no root %q; call files_list without root to see them", id)
}

func (s *Server) assistantFilesList(user auth.User) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var req struct{ Root, Path string }
		if err := assistant.DecodeArgs(args, &req); err != nil {
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
		root, err := s.assistantRoot(user, req.Root)
		if err != nil {
			return nil, err
		}
		if hiddenPath(req.Path) {
			return nil, assistant.Errorf("this folder is not shown to the assistant")
		}
		entries, err := s.opts.Files.List(root, req.Path)
		if err != nil {
			return nil, assistant.Errorf("%v", err)
		}
		entries = slices.DeleteFunc(entries, func(e files.Entry) bool { return hiddenPath(e.Name) })
		cut := len(entries) > 300
		return map[string]any{"root": root.ID, "path": req.Path, "entries": entries[:min(len(entries), 300)], "cut": cut}, nil
	}
}

func (s *Server) assistantFileRead(user auth.User) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var req struct{ Root, Path string }
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		root, err := s.assistantRoot(user, req.Root)
		if err != nil {
			return nil, err
		}
		if hiddenPath(req.Path) {
			return nil, assistant.Errorf("this file is not shown to the assistant")
		}
		if ext := strings.ToLower(path.Ext(req.Path)); ext == ".docx" || ext == ".xlsx" {
			text, err := s.readDocument(root, req.Path)
			if err != nil {
				return nil, assistant.Errorf("%v", err)
			}
			return map[string]any{"root": root.ID, "path": req.Path, "content": text}, nil
		}
		if strings.EqualFold(path.Ext(req.Path), ".pdf") {
			return nil, assistant.Errorf("PDF files cannot be read; Word, Excel and text files can")
		}
		text, err := s.opts.Files.ReadText(root, req.Path)
		if err != nil {
			return nil, assistant.Errorf("%v", err)
		}
		return map[string]any{"root": root.ID, "path": req.Path, "content": text.Content}, nil
	}
}

func (s *Server) toolDocsSearch(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct{ Query string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	manual := s.opts.Assistant.Manual
	if len(manual.Files()) == 0 {
		return nil, assistant.Errorf("the manual is not part of this build of titand")
	}
	sections := manual.Search(req.Query, 6)
	if len(sections) == 0 {
		return map[string]any{"sections": []any{}, "files": manual.Files()}, nil
	}
	return map[string]any{"sections": sections}, nil
}

func (s *Server) toolDocsRead(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct{ File, Heading string }
	if err := assistant.DecodeArgs(args, &req); err != nil {
		return nil, err
	}
	text, ok := s.opts.Assistant.Manual.Read(req.File, req.Heading)
	if !ok {
		return nil, assistant.Errorf("no document %q with that heading; documents: %s", req.File, strings.Join(s.opts.Assistant.Manual.Files(), ", "))
	}
	return map[string]any{"file": req.File, "text": text}, nil
}

func (s *Server) webTools() []assistant.Tool {
	web := s.opts.Assistant.Web
	return []assistant.Tool{
		{
			Name:        "web_search",
			Description: "Search the web with DuckDuckGo. Returns titles, addresses and short excerpts; read a page with web_read for details.",
			Parameters:  schema(map[string]any{"query": str("What to search for.")}, "query"),
			Untrusted:   true,
			Read: func(ctx context.Context, args json.RawMessage) (assistant.Result, error) {
				var req struct{ Query string }
				if err := assistant.DecodeArgs(args, &req); err != nil {
					return assistant.Result{}, err
				}
				results, err := web.Search(ctx, s.opts.Version, req.Query)
				if err != nil {
					return assistant.Result{}, assistant.Errorf("%v", err)
				}
				res, err := assistant.JSON(map[string]any{"query": req.Query, "results": results})
				for _, r := range results[:min(len(results), 5)] {
					res.Sources = append(res.Sources, assistant.Source{Title: r.Title, URL: r.URL})
				}
				return res, err
			},
		},
		{
			Name:        "web_read",
			Description: "Read the text of one web page (http or https). Pages on the home network cannot be read.",
			Parameters:  schema(map[string]any{"url": str("Address of the page.")}, "url"),
			Untrusted:   true,
			Read: func(ctx context.Context, args json.RawMessage) (assistant.Result, error) {
				var req struct{ URL string }
				if err := assistant.DecodeArgs(args, &req); err != nil {
					return assistant.Result{}, err
				}
				page, err := web.Read(ctx, s.opts.Version, req.URL)
				if err != nil {
					return assistant.Result{}, err
				}
				res, err := assistant.JSON(page)
				title := page.Title
				if title == "" {
					title = page.URL
				}
				res.Sources = []assistant.Source{{Title: title, URL: page.URL}}
				return res, err
			},
		},
	}
}
