package api

import (
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// toolEnv is what the tools of the catalog and of the MCP server
// (catalog*.go, mcp*.go) read directly: the managers and the few answers of
// areas they ask for (PLAN 8a). The tools are not a route area: they stay
// with the server, which owns the catalog, the MCP settings and the handler
// chain an action runs through. A tool takes the environment when it is
// called (env := s.toolEnv()), so it always sees the managers of the
// server as they are then.
type toolEnv struct {
	store    *config.Store
	version  string
	users    *auth.Users
	monitor  *system.Monitor
	hardware *hardware.Manager
	storage  *storage.Manager
	shares   *shares.Manager
	files    *files.Manager
	docker   *docker.Manager
	vms      *vm.Manager
	lxc      *lxc.Manager
	plugins  *plugins.Manager
	network  *network.Manager
	firewall *firewall.Manager
	backup   *backup.Manager
	update   *update.Manager
	ups      *ups.Manager
	notify   *notify.Manager
	diagnose *diagnose.Manager
	activity *activity.Manager

	// fileRoots says where an account may work, fileRoot resolves one root
	// with the rights of the account of a request (it answers the request
	// itself when it refuses); both are the file manager's.
	fileRoots func(user auth.User) []files.Root
	fileRoot  func(w http.ResponseWriter, r *http.Request, id string) (files.Root, bool)
	// servicesOn are the switches of Docker, VMs and system containers.
	servicesOn func() map[string]bool
}

// toolEnv is the environment of the tools of this server.
func (s *Server) toolEnv() toolEnv {
	return toolEnv{store: s.opts.Store, version: s.opts.Version, users: s.opts.Users, monitor: s.opts.Monitor, hardware: s.opts.Hardware,
		storage: s.opts.Storage, shares: s.opts.Shares, files: s.opts.Files, docker: s.opts.Docker, vms: s.opts.VMs, lxc: s.opts.LXC,
		plugins: s.opts.Plugins, network: s.opts.Network, firewall: s.opts.Firewall, backup: s.opts.Backup, update: s.opts.Update,
		ups: s.opts.UPS, notify: s.opts.Notify, diagnose: s.opts.Diagnose, activity: s.opts.Activity,
		fileRoots: s.filesArea().fileRoots, fileRoot: s.filesArea().fileRoot, servicesOn: s.servicesArea().servicesOn}
}
