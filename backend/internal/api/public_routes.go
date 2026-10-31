package api

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// publicRoute is one route of the public API (docs/PUBLIC-API.md). The
// list below is the whole contract: the token check (publicScope), the
// OpenAPI file and the changing tools of the MCP server (mcp_tools.go) are
// all made from it. A token never opens account, shell, credential, update,
// network/firewall confirmation or assistant endpoints.
type publicRoute struct {
	Method string
	Path   string // below /api/v1, with {parameters}
	Scope  string
	// Tool names the MCP tool made from this route. Empty: MCP does not
	// offer the route itself (it has a tool of the assistant catalog for
	// the same data, or the answer is not JSON).
	Tool string
	// Level of a change, as for the assistant: normal, danger or destroy.
	// Danger and destroy need confirm=true over MCP.
	Level  string
	En, De string
	// Query are the query parameters of a GET, Body the fields of the JSON
	// body; Required names the ones that must be there.
	Query, Body map[string]any
	Required    []string
}

func rootPath() map[string]any {
	return map[string]any{"root": str("Root ID such as share/Documents, from GET /files/roots."), "path": str("Path inside the root, empty for its top.")}
}

// routeParam finds the {parameters} of a route path.
var routeParam = regexp.MustCompile(`\{[a-z]+\}`)

var publicRoutes = []publicRoute{
	{Method: "GET", Path: "/system", Scope: "system:read", En: "Name, version, CPU, memory, temperatures.", De: "Name, Version, CPU, Arbeitsspeicher, Temperaturen."},
	{Method: "GET", Path: "/storage", Scope: "system:read", En: "Disks and volumes.", De: "Platten und Volumes."},
	{Method: "GET", Path: "/network", Scope: "system:read", En: "Network state.", De: "Netzwerkzustand."},
	{Method: "GET", Path: "/firewall", Scope: "system:read", En: "Firewall state.", De: "Zustand der Firewall."},
	{Method: "GET", Path: "/plugins", Scope: "system:read", En: "Installed and available plugins.", De: "Installierte und verfügbare Plugins."},
	{Method: "GET", Path: "/openapi.json", Scope: "system:read", En: "This OpenAPI file.", De: "Diese OpenAPI-Datei."},
	{Method: "GET", Path: "/shares", Scope: "files:read", En: "Shares the account may open.", De: "Freigaben, die das Konto öffnen darf."},
	{Method: "GET", Path: "/files/roots", Scope: "files:read", En: "Roots of the file manager.", De: "Wurzeln des Dateimanagers."},
	{Method: "GET", Path: "/files/list", Scope: "files:read", En: "List a folder.", De: "Ordner auflisten.", Query: rootPath(), Required: []string{"root"}},
	{Method: "GET", Path: "/files/download", Scope: "files:read", En: "Streams the authorized file. Supports HTTP Range. The path is relative to the selected root.", De: "Liefert die Datei, unterstützt HTTP-Range.", Query: rootPath(), Required: []string{"root", "path"}},
	{Method: "GET", Path: "/files/properties", Scope: "files:read", En: "Properties of a file or folder.", De: "Eigenschaften einer Datei oder eines Ordners.", Query: rootPath(), Required: []string{"root"}},
	{Method: "GET", Path: "/files/text", Scope: "files:read", Tool: "files_read_text",
		En: "Read a UTF-8 text file (up to 512 KiB) with its version; files_write_text needs that version.",
		De: "Textdatei (UTF-8, bis 512 KiB) samt Version lesen; files_write_text braucht diese Version.", Query: rootPath(), Required: []string{"root", "path"}},
	{Method: "GET", Path: "/files/jobs", Scope: "files:read", En: "Copy and move jobs of the account.", De: "Kopier- und Verschiebeaufträge des Kontos."},
	{Method: "GET", Path: "/docker", Scope: "docker:read", En: "Docker state, containers and apps.", De: "Docker-Zustand, Container und Apps."},
	{Method: "GET", Path: "/docker/containers/{id}/logs", Scope: "docker:read", En: "Log of a container.", De: "Protokoll eines Containers.", Query: map[string]any{"tail": map[string]any{"type": "integer", "minimum": 1, "description": "Newest lines, 300 if left out."}}},
	{Method: "GET", Path: "/vms", Scope: "vms:read", En: "Virtual machines.", De: "Virtuelle Maschinen."},

	{Method: "POST", Path: "/files/mkdir", Scope: "files:write", Tool: "files_mkdir", Level: toolcatalog.LevelNormal,
		En: "Create a folder.", De: "Ordner anlegen.", Body: rootPath(), Required: []string{"root", "path"}},
	{Method: "POST", Path: "/files/create", Scope: "files:write", Tool: "files_create", Level: toolcatalog.LevelNormal,
		En: "Create an empty text file; an existing file is never replaced.", De: "Leere Textdatei anlegen; eine vorhandene wird nie ersetzt.", Body: rootPath(), Required: []string{"root", "path"}},
	{Method: "POST", Path: "/files/rename", Scope: "files:write", Tool: "files_rename", Level: toolcatalog.LevelNormal,
		En: "Rename a file or folder inside its root.", De: "Datei oder Ordner innerhalb der Wurzel umbenennen.",
		Body: map[string]any{"root": str("Root ID."), "from": str("Current path."), "to": str("New path.")}, Required: []string{"root", "from", "to"}},
	{Method: "POST", Path: "/files/delete", Scope: "files:write", Tool: "files_delete", Level: toolcatalog.LevelDestroy,
		En: "Delete files or folders: into the recycle bin of the share when it has one, otherwise (or with permanent) for good.",
		De: "Dateien oder Ordner löschen: in den Papierkorb der Freigabe, wenn sie einen hat, sonst (oder mit permanent) endgültig.",
		Body: map[string]any{"root": str("Root ID."), "paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "description": "Paths inside the root."},
			"permanent": map[string]any{"type": "boolean", "default": false, "description": "Delete for good instead of the recycle bin."},
			"versions":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Optional versions per path; a changed entry is not deleted."}},
		Required: []string{"root", "paths"}},
	{Method: "POST", Path: "/files/copy", Scope: "files:write", Tool: "files_copy", Level: toolcatalog.LevelDanger,
		En: "Copy or move files and folders as a background job (202); poll GET /files/jobs. With move the originals go after the copy, with conflict=replace existing files are overwritten.",
		De: "Dateien und Ordner im Hintergrund kopieren oder verschieben (202); Stand über GET /files/jobs. Mit move verschwinden die Originale nach der Kopie, mit conflict=replace werden vorhandene Dateien überschrieben.",
		Body: map[string]any{"root": str("Source root ID."), "paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "description": "Source paths."},
			"toRoot": str("Target root ID."), "toDir": str("Target folder inside the target root."),
			"move":     map[string]any{"type": "boolean", "default": false, "description": "Move instead of copy."},
			"conflict": map[string]any{"type": "string", "enum": []string{"rename", "skip", "replace"}, "default": "rename", "description": "What happens to an existing name."}},
		Required: []string{"root", "paths", "toRoot", "toDir"}},
	{Method: "PUT", Path: "/files/text", Scope: "files:write", Tool: "files_write_text", Level: toolcatalog.LevelDanger,
		En:       "Replace the content of a text file; the old content stays as <name>.prev. Needs the version from files_read_text; a changed file is rejected.",
		De:       "Inhalt einer Textdatei ersetzen; der alte bleibt als <name>.prev. Braucht die Version aus files_read_text; eine inzwischen geänderte Datei wird abgelehnt.",
		Body:     map[string]any{"root": str("Root ID."), "path": str("Path of the file."), "content": str("New content, UTF-8."), "version": str("Version returned by GET /files/text; a changed source is rejected.")},
		Required: []string{"root", "path", "content", "version"}},
	{Method: "POST", Path: "/docker/containers/{id}/start", Scope: "docker:write", Tool: "docker_container_start", Level: toolcatalog.LevelNormal, En: "Start a container.", De: "Container starten."},
	{Method: "POST", Path: "/docker/containers/{id}/stop", Scope: "docker:write", Tool: "docker_container_stop", Level: toolcatalog.LevelDanger, En: "Stop a container; its service is unavailable until it starts again.", De: "Container stoppen; sein Dienst ist bis zum nächsten Start nicht erreichbar."},
	{Method: "POST", Path: "/docker/containers/{id}/restart", Scope: "docker:write", Tool: "docker_container_restart", Level: toolcatalog.LevelDanger, En: "Restart a container; its service is briefly unavailable.", De: "Container neu starten; sein Dienst ist kurz nicht erreichbar."},
	{Method: "POST", Path: "/docker/apps/{name}/network", Scope: "docker:write", Tool: "docker_app_network", Level: toolcatalog.LevelDanger,
		En: "Give one service of a managed app its own address in a LAN network; the app is recreated.", De: "Einem Dienst einer verwalteten App eine eigene Adresse in einem LAN-Netz geben; die App wird neu erstellt.",
		Body: map[string]any{"service": str("Compose service."), "network": str("Docker LAN network."), "address": str("Fixed address, optional.")}, Required: []string{"service", "network"}},
	{Method: "POST", Path: "/vms/{name}/start", Scope: "vms:write", Tool: "vm_start", Level: toolcatalog.LevelNormal, En: "Start a virtual machine.", De: "Virtuelle Maschine starten."},
	{Method: "POST", Path: "/vms/{name}/shutdown", Scope: "vms:write", Tool: "vm_shutdown", Level: toolcatalog.LevelDanger, En: "Shut a virtual machine down cleanly (ACPI).", De: "Virtuelle Maschine sauber herunterfahren (ACPI)."},
	{Method: "POST", Path: "/vms/{name}/reboot", Scope: "vms:write", Tool: "vm_reboot", Level: toolcatalog.LevelDanger, En: "Restart a virtual machine.", De: "Virtuelle Maschine neu starten."},
}

// matchPattern matches a path against a route path with {parameters}. A
// parameter is one non-empty segment without slashes and never "..".
func matchPattern(pattern, p string) (map[string]string, bool) {
	want := strings.Split(pattern, "/")
	got := strings.Split(p, "/")
	if len(want) != len(got) {
		return nil, false
	}
	params := map[string]string{}
	for i := range want {
		if routeParam.MatchString(want[i]) {
			v, err := url.PathUnescape(got[i])
			if err != nil || v == "" || strings.ContainsAny(v, "/\\") || v == ".." {
				return nil, false
			}
			params[strings.Trim(want[i], "{}")] = v
		} else if want[i] != got[i] {
			return nil, false
		}
	}
	return params, true
}

// publicScope is the scope a token needs for a request, empty when the
// route is not part of the public API.
func publicScope(method, path string) string {
	p, ok := strings.CutPrefix(path, "/api/v1")
	if !ok {
		return ""
	}
	for _, route := range publicRoutes {
		if route.Method != method {
			continue
		}
		if _, ok := matchPattern(route.Path, p); ok {
			return route.Scope
		}
	}
	return ""
}
