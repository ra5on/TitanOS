package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
)

// The other functions of the web UI for the assistant: a fixed list of
// routes with their area and level. api_read calls a GET route with the
// login of the chat; api_change proposes a change as a card like every
// action. Routes that give away secrets (backup passwords, downloads of
// the stick backup, consoles, private keys) or that only a person may use
// (changing or confirming the network, confirming a firewall change) are
// not in the list and cannot be called.

type apiRoute struct {
	Method string
	Path   string // with {parameters}
	Area   string
	Level  string // for changes: normal, danger or destroy
	Text   string
}

var apiRoutes = []apiRoute{
	// system
	{"GET", "/system", assistant.AreaSystem, "", "Name, version, CPU, memory, temperatures, networks of the dashboard."},
	{"GET", "/network", assistant.AreaSystem, "", "Network configuration and state, bond modes and time zone."},
	{"POST", "/network/revert", assistant.AreaSystem, assistant.LevelNormal, "Undo a network change that is still on trial."},
	{"PUT", "/system/hostname", assistant.AreaSystem, assistant.LevelDanger, `Rename the NAS (body {"hostname": "…"}).`},
	{"PUT", "/system/timezone", assistant.AreaSystem, assistant.LevelNormal, `Set the time zone (body {"timezone": "Europe/Berlin"}).`},
	{"GET", "/notifications/settings", assistant.AreaSystem, "", "Ways notifications are sent (e-mail, ntfy, Telegram, Discord); secrets only as set or not."},
	{"PUT", "/notifications/settings", assistant.AreaSystem, assistant.LevelNormal, "Change how notifications are sent (body as GET; empty secret fields keep the stored value)."},
	{"POST", "/notifications/test", assistant.AreaSystem, assistant.LevelNormal, "Send a test notification with the saved settings."},
	{"POST", "/notifications/dismiss", assistant.AreaSystem, assistant.LevelNormal, `Delete notifications from the bell (body {"ids": [1, 2]}).`},
	{"GET", "/system/ssh", assistant.AreaSystem, "", "Whether SSH is on and who may log in."},
	{"PUT", "/system/ssh", assistant.AreaSystem, assistant.LevelDanger, `Switch SSH on or off (body {"enabled": true}).`},
	{"GET", "/ups", assistant.AreaSystem, "", "UPS settings and state."},
	{"PUT", "/ups", assistant.AreaSystem, assistant.LevelNormal, "Change the UPS settings (body as GET › settings)."},
	{"GET", "/system/energy", assistant.AreaSystem, "", "Energy settings (CPU governor and more)."},
	{"PUT", "/system/energy", assistant.AreaSystem, assistant.LevelNormal, "Change the energy settings (body: GET /system/energy settings only: governor, preference, platform)."},
	{"GET", "/system/memory", assistant.AreaSystem, "", "Memory policy."},
	{"PUT", "/system/memory", assistant.AreaSystem, assistant.LevelNormal, "Change the memory policy (body: GET /system/memory settings only: cachePercent)."},
	{"GET", "/system/power-button", assistant.AreaSystem, "", "What the power button does."},
	{"PUT", "/system/power-button", assistant.AreaSystem, assistant.LevelNormal, "Change what the power button does (body: GET /system/power-button settings only: action)."},
	{"GET", "/terminal", assistant.AreaSystem, "", "Settings of terminals and consoles."},
	{"PUT", "/terminal", assistant.AreaSystem, assistant.LevelDanger, "Change terminals and consoles (body: enabled and confirm booleans from GET; omit read-only nasShell)."},
	{"GET", "/hardware", assistant.AreaSystem, "", "PCI and USB devices and which VM uses them."},
	{"GET", "/usb", assistant.AreaSystem, "", "USB drives that can be mounted."},
	{"POST", "/usb/{id}/mount", assistant.AreaSystem, assistant.LevelNormal, "Mount a USB drive by its ID from GET /usb."},
	{"POST", "/usb/{id}/eject", assistant.AreaSystem, assistant.LevelNormal, "Eject a USB drive."},
	{"GET", "/usb/disks", assistant.AreaSystem, "", "Whole unmanaged drives, also empty ones, and the last format. Formatting is only offered in the web UI."},
	{"GET", "/plugins/tailscale", assistant.AreaSystem, "", "Tailscale settings and state."},
	{"PUT", "/plugins/tailscale", assistant.AreaSystem, assistant.LevelDanger, "Change the Tailscale settings (body as GET › settings)."},
	{"GET", "/plugins/netbird", assistant.AreaSystem, "", "NetBird settings and state; the setup key only as set or not."},
	{"PUT", "/plugins/netbird", assistant.AreaSystem, assistant.LevelDanger, "Change the NetBird settings (body as GET › settings; the setup key is not part of it). Runs 2 minutes on trial; the person confirms it in the web UI or it goes back."},
	{"GET", "/about", assistant.AreaSystem, "", "Version, kernel and licenses."},
	// plugins: the dedicated tools plugins_list, plugin_install … check
	// the target first; installing goes by name from the signed catalog only.
	{"GET", "/plugins", assistant.AreaPlugins, "", "Installed and available plugins."},
	{"POST", "/plugins/catalog", assistant.AreaPlugins, assistant.LevelNormal, "Load the list of official plugins for this version."},
	{"POST", "/plugins/{name}/{action}", assistant.AreaPlugins, assistant.LevelDanger, "Switch a plugin on or off (action enable or disable)."},
	{"DELETE", "/plugins/{name}", assistant.AreaPlugins, assistant.LevelDestroy, "Remove a plugin."},
	// firewall: rules only. Switching the firewall on or off, changing the
	// network and confirming a change on trial stay in the web UI.
	{"GET", "/firewall", assistant.AreaNetwork, "", "Firewall: on/off, zone matrix (home, vpn, internet) per service and Docker app, own rules, automatic blocking, blocked addresses, recently dropped packets."},
	{"PUT", "/firewall", assistant.AreaNetwork, assistant.LevelDanger, "Change the rules of the firewall (body: the config object from GET /firewall with the same enabled value; switching it on or off is only done in the web UI). A change of a switched-on firewall runs 2 minutes on trial; the person confirms it in the web UI or it goes back."},
	{"POST", "/firewall/revert", assistant.AreaNetwork, assistant.LevelNormal, "Undo a firewall change that is still on trial."},
	{"PUT", "/firewall/autoblock", assistant.AreaNetwork, assistant.LevelDanger, "Change the automatic blocking after failed logins (body: config.autoBlock of GET /firewall)."},
	{"POST", "/firewall/unblock", assistant.AreaNetwork, assistant.LevelDanger, "Release an automatically blocked address (body: {address})."},
	// updates
	{"GET", "/update", assistant.AreaUpdates, "", "Update state, channel, slots and an update that was found."},
	{"PUT", "/update/settings", assistant.AreaUpdates, assistant.LevelNormal, "Change the update settings (body as GET › settings)."},
	{"GET", "/config-backup", assistant.AreaBackups, "", "Settings and copies of the backup of the stick."},
	{"PUT", "/config-backup", assistant.AreaBackups, assistant.LevelNormal, "Change the settings of the stick backup (body as GET › settings)."},
	// storage
	{"GET", "/storage", assistant.AreaStorage, "", "Disks and volumes in full detail."},
	{"GET", "/storage/disk-sleep", assistant.AreaStorage, "", "Sleep time of hard disks: default, overrides per volume and disk, effective value and state of each disk."},
	{"PUT", "/storage/disk-sleep", assistant.AreaStorage, assistant.LevelNormal, `Set the sleep time of hard disks (body {"idleMinutes": 30}; 0 is off). Optional "volumes" {"<volume>": minutes} and "disks" {"<disk id from GET>": minutes} replace all overrides; a missing entry inherits, 0 never sleeps; leave them out to keep the stored ones.`},
	{"POST", "/storage/volumes/{name}/disks", assistant.AreaStorage, assistant.LevelDestroy, `Add a disk to a volume or replace a missing one (body {"disk": "sdc", "erase": true}); everything on the disk is erased.`},
	{"DELETE", "/storage/volumes/{name}/disks/{disk}", assistant.AreaStorage, assistant.LevelDestroy, "Take a disk out of a volume."},
	{"GET", "/storage/volumes/{name}/snapshots", assistant.AreaStorage, "", "Snapshots of a Btrfs volume."},
	{"POST", "/storage/volumes/{name}/snapshots", assistant.AreaStorage, assistant.LevelNormal, `Create a snapshot of a Btrfs volume (body {"action": "create"}).`},
	{"DELETE", "/storage/tests/{disk}", assistant.AreaStorage, assistant.LevelNormal, "Stop a disk test or remove its result."},
	// shares
	{"GET", "/shares", assistant.AreaShares, "", "Shares with rights per account, recycle bin, NFS and Time Machine."},
	{"PUT", "/shares/{name}", assistant.AreaShares, assistant.LevelDanger, "Change a share: rights, guest access, recycle bin, NFS, Time Machine (body: the whole share as in GET /shares)."},
	// docker
	{"GET", "/docker", assistant.AreaDocker, "", "Docker settings, containers, apps and app setups."},
	{"PUT", "/docker/settings", assistant.AreaDocker, assistant.LevelDanger, "Change the Docker settings (body as GET › settings)."},
	{"GET", "/docker/apps/{name}", assistant.AreaDocker, "", "The compose file of an app."},
	{"GET", "/docker/auto-update", assistant.AreaDocker, "", "Apps that update by themselves, the window and the last results."},
	{"PUT", "/docker/apps/{name}", assistant.AreaDocker, assistant.LevelDanger, `Replace the compose file of an app and recreate it (body {"compose": "…"}); data in its paths stays.`},
	{"POST", "/docker/apps", assistant.AreaDocker, assistant.LevelNormal, `Create an app from a compose file (body {"name": "…", "compose": "…"}).`},
	{"DELETE", "/docker/apps/{name}/job", assistant.AreaDocker, assistant.LevelNormal, "Hide the message of a failed app setup."},
	{"GET", "/docker/containers/{id}/logs", assistant.AreaDocker, "", "Log of a container (?tail=200)."},
	{"DELETE", "/docker/containers/{id}", assistant.AreaDocker, assistant.LevelDanger, "Remove a container that belongs to no app."},
	{"GET", "/apps", assistant.AreaDocker, "", "The app store: sources and templates."},
	{"GET", "/apps/sources/{source}/apps/{app}", assistant.AreaDocker, "", "The compose template of an app in the store."},
	{"POST", "/apps/refresh", assistant.AreaDocker, assistant.LevelNormal, "Load the app sources again."},
	{"POST", "/apps/sources", assistant.AreaDocker, assistant.LevelNormal, `Add an app source (body {"url": "…"}).`},
	{"DELETE", "/apps/sources/{id}", assistant.AreaDocker, assistant.LevelNormal, "Remove an app source."},
	// vms
	{"GET", "/vms", assistant.AreaVMs, "", "Virtual machines with their configuration."},
	{"GET", "/vms/host", assistant.AreaVMs, "", "CPUs, memory and volumes available for VMs."},
	{"GET", "/vms/media", assistant.AreaVMs, "", "Installation media and disk images found on the NAS."},
	{"POST", "/vms", assistant.AreaVMs, assistant.LevelNormal, "Create a VM (body as the configuration in GET /vms; see /vms/host)."},
	{"PUT", "/vms/{name}", assistant.AreaVMs, assistant.LevelDanger, "Change a VM that is off (body: its whole configuration from GET /vms)."},
	{"PUT", "/vms/{name}/medium", assistant.AreaVMs, assistant.LevelNormal, `Insert or eject the CD of a VM (body {"path": "/mnt/…iso"} or {"path": ""}).`},
	{"POST", "/vms/{name}/grow", assistant.AreaVMs, assistant.LevelDanger, "Make a VM disk larger (body: disk is the zero-based disk index; sizeBytes is the new total size in bytes)."},
	{"GET", "/vms/{name}/snapshots", assistant.AreaVMs, "", "Snapshots of a VM."},
	{"POST", "/vms/{name}/snapshots", assistant.AreaVMs, assistant.LevelNormal, `Take a snapshot of a VM (body {"name": "…"}).`},
	{"POST", "/vms/{name}/snapshots/{snapshot}/restore", assistant.AreaVMs, assistant.LevelDestroy, "Go back to a snapshot; what changed since is lost. Optional body memory=true restores saved RAM. TitanOS supplies the VM confirmation."},
	{"DELETE", "/vms/{name}/snapshots/{snapshot}", assistant.AreaVMs, assistant.LevelDestroy, "Delete a snapshot. Use its ID in the path and its exact name from GET snapshots in body.confirm."},
	// backups
	{"GET", "/backups", assistant.AreaBackups, "", "Backup jobs with schedule, sources, destination and state."},
	{"GET", "/backups/{id}/snapshots", assistant.AreaBackups, "", "The saved states of a backup job."},
	{"PUT", "/backups/{id}", assistant.AreaBackups, assistant.LevelNormal, "Change a backup job (body: the job from GET /backups)."},
	{"POST", "/backups/{id}/restore", assistant.AreaBackups, assistant.LevelNormal, `Restore a saved state, or with "paths" only those files and folders, into a new folder (body {"snapshot": "…", "volume": "…", "paths": ["/mnt/…"]}); nothing is overwritten.`},
	{"GET", "/backups/{id}/snapshots/{snapshot}/files", assistant.AreaBackups, "", "One folder of a saved state (?path=/mnt/volume/share/folder&offset=N), folders first, pages of 500."},
	{"POST", "/backups/{id}/check", assistant.AreaBackups, assistant.LevelNormal, `Check the repository of a backup job in the background (body {"readData": 0-100}, percent of the data to read back); the result is lastCheck/checkError in GET /backups.`},
	{"DELETE", "/backups/{id}", assistant.AreaBackups, assistant.LevelDestroy, "Delete a backup job (the saved data stays at the destination)."},
	// accounts
	{"GET", "/users", assistant.AreaAccounts, "", "Accounts with role and two-factor login."},
}

var routeParam = regexp.MustCompile(`\{[a-z]+\}`)

func (r apiRoute) match(method, p string) (map[string]string, bool) {
	if r.Method != method {
		return nil, false
	}
	return matchPattern(r.Path, p)
}

// allowedRoutes are the routes the settings let the assistant use.
func (s *Server) allowedRoutes() []apiRoute {
	settings := s.opts.Assistant.Settings()
	var list []apiRoute
	for _, r := range apiRoutes {
		switch settings.Access(r.Area) {
		case assistant.AccessWrite:
			list = append(list, r)
		case assistant.AccessRead:
			if r.Method == http.MethodGet {
				list = append(list, r)
			}
		}
	}
	return list
}

func (s *Server) findRoute(method, raw string) (apiRoute, map[string]string, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "" || u.Scheme != "" {
		return apiRoute{}, nil, "", assistant.Errorf("give a path such as /docker, without host")
	}
	p := "/" + strings.Trim(strings.TrimPrefix(u.EscapedPath(), "/api/v1"), "/")
	for _, r := range s.allowedRoutes() {
		if params, ok := r.match(method, p); ok {
			full := p
			if u.RawQuery != "" {
				full += "?" + u.RawQuery
			}
			return r, params, full, nil
		}
	}
	return apiRoute{}, nil, "", assistant.Errorf("%s %s is not in api_routes or not allowed by the settings", method, p)
}

func (s *Server) apiTools(user auth.User, chat *http.Request) []assistant.Tool {
	return []assistant.Tool{
		reading("api_routes", "List the other functions of the web interface the assistant may use: method, path, area and what it does. Use api_read for GET, api_change for the rest.",
			schema(map[string]any{"area": enum("Only this area.", assistant.Areas...)}), false,
			func(ctx context.Context, args json.RawMessage) (any, error) {
				var req struct{ Area string }
				if err := assistant.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				type route struct {
					Method string `json:"method"`
					Path   string `json:"path"`
					Area   string `json:"area"`
					Text   string `json:"what"`
				}
				list := []route{}
				for _, r := range s.allowedRoutes() {
					if req.Area == "" || r.Area == req.Area {
						list = append(list, route{r.Method, r.Path, r.Area, r.Text})
					}
				}
				return map[string]any{"routes": list}, nil
			}),
		{
			Name:        "api_read",
			Description: "Read from a GET route of api_routes, e.g. /docker/apps/immich or /notifications/settings. Returns the JSON answer.",
			Parameters:  schema(map[string]any{"path": str("Path of the route with its values filled in, optionally with ?query.")}, "path"),
			Read: func(ctx context.Context, args json.RawMessage) (assistant.Result, error) {
				var req struct{ Path string }
				if err := assistant.DecodeArgs(args, &req); err != nil {
					return assistant.Result{}, err
				}
				_, _, full, err := s.findRoute(http.MethodGet, req.Path)
				if err != nil {
					return assistant.Result{}, err
				}
				data, err := s.internalGet(ctx, chat, full)
				if err != nil {
					return assistant.Result{}, err
				}
				return assistant.Result{Text: diagnose.Secrets(string(data))}, nil
			},
		},
		actionTool("api_change", "Propose a change through a route of api_routes that is not GET. Read the current value first and send the whole object with only what was asked changed.",
			map[string]any{"method": enum("Method of the route.", "POST", "PUT", "DELETE"), "path": str("Path with its values filled in."),
				"body": map[string]any{"type": "object", "description": "JSON body, if the route takes one."}, "summary": str("What changes, in one line for the person.")},
			[]string{"method", "path", "summary"},
			func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
				var req struct {
					Method, Path, Summary string
					Body                  json.RawMessage
				}
				if err := assistant.DecodeArgs(args, &req); err != nil {
					return nil, err
				}
				route, params, full, err := s.findRoute(strings.ToUpper(req.Method), req.Path)
				if err != nil {
					return nil, err
				}
				if len(req.Body) > 512<<10 {
					return nil, assistant.Errorf("the body is too large")
				}
				var body any
				if len(req.Body) > 0 && string(req.Body) != "null" {
					if err := json.Unmarshal(req.Body, &body); err != nil {
						return nil, assistant.Errorf("body is no JSON: %v", err)
					}
				}
				summary := strings.TrimSpace(req.Summary)
				if summary == "" {
					summary = route.Text
				}
				a := card("api.change", map[string]string{"method": route.Method, "path": full, "summary": summary, "what": route.Text}, route.Method, full, body)
				a.Level = route.Level
				if route.Level == assistant.LevelDestroy {
					// The value of the last parameter names what goes.
					names := routeParam.FindAllString(route.Path, -1)
					a.Confirm = params[strings.Trim(names[len(names)-1], "{}")]
					if a.Confirm == "" {
						a.Level = assistant.LevelDanger
					}
				}
				if route.Path == "/vms/{name}/snapshots/{snapshot}/restore" {
					a.Confirm, a.ConfirmField = params["name"], "body:confirm"
				}
				if route.Path == "/vms/{name}/snapshots/{snapshot}" && route.Method == "DELETE" {
					fields, _ := body.(map[string]any)
					name, _ := fields["confirm"].(string)
					if name == "" {
						return nil, assistant.Errorf("read GET /vms/%s/snapshots first: use the snapshot ID in the path and its exact name in body.confirm", params["name"])
					}
					a.Confirm, a.ConfirmField = name, "body:confirm"
				}
				if a.Params["preview"] = previewBody(body); a.Params["preview"] == "" {
					delete(a.Params, "preview")
				}
				return a, nil
			}),
	}
}

func previewBody(body any) string {
	if body == nil {
		return ""
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	if r := []rune(string(data)); len(r) > 1200 {
		return string(r[:1200]) + " …"
	}
	return string(data)
}

// internalGet calls a GET route of this server with the login of the chat,
// so that the rights are exactly those of the account.
func (s *Server) internalGet(ctx context.Context, chat *http.Request, p string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1"+p, nil)
	if err != nil {
		return nil, err
	}
	for _, c := range chat.Cookies() {
		req.AddCookie(c)
	}
	req.RemoteAddr = chat.RemoteAddr
	req.Host = chat.Host
	req.TLS = chat.TLS
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, assistant.CodedErrorf("result_too_large", "API response exceeds 1 MiB; use a narrower dedicated tool")
	}
	if res.StatusCode != http.StatusOK {
		return nil, assistant.Errorf("the route answered %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	if !json.Valid(data) {
		return nil, assistant.Errorf("the route sent no JSON")
	}
	return data, nil
}

// apiRouteList is for tests: every route of the list must exist.
func apiRouteList() []string {
	var list []string
	for _, r := range apiRoutes {
		list = append(list, fmt.Sprintf("%s /api/v1%s", r.Method, r.Path))
	}
	slices.Sort(list)
	return list
}
