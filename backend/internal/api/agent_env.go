package api

import (
	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/smarthistory"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// agentEnv is what the KI-Agent (agent*.go) reads directly: the managers
// and the few answers of areas it asks for (PLAN 8a), like toolEnv for the
// tools of the catalog. The agent stays with the server, which owns its
// settings, running chats and watchers. Agent code takes the environment
// when it is called (env := s.agentEnv()), so it always sees the managers
// of the server as they are then.
type agentEnv struct {
	store        *config.Store
	version      string
	users        *auth.Users
	monitor      *system.Monitor
	monitoring   *monitoring.Manager
	accelerators *hardware.AcceleratorMonitor
	smartHistory *smarthistory.Recorder
	settings     *system.Settings
	storage      *storage.Manager
	shares       *shares.Manager
	files        *files.Manager
	docker       *docker.Manager
	catalog      *catalog.Manager
	backup       *backup.Manager
	backupTarget *backuptarget.Manager
	cluster      *cluster.Manager
	notify       *notify.Manager
	webPush      *webpush.Manager
	activity     *activity.Manager
	tailscale    *tailscale.Manager
	netbird      *netbird.Manager
	wireGuard    *wgserver.Manager
	letsEncrypt  *letsencrypt.Manager

	// fileRoots says where an account may work (file manager), userNames
	// names the accounts a share can name (shares), servicesOn are the
	// switches of Docker, VMs and system containers.
	fileRoots  func(user auth.User) []files.Root
	userNames  func() []string
	servicesOn func() map[string]bool
}

// agentEnv is the environment of the KI-Agent of this server.
func (s *Server) agentEnv() agentEnv {
	return agentEnv{store: s.opts.Store, version: s.opts.Version, users: s.opts.Users, monitor: s.opts.Monitor, monitoring: s.opts.Monitoring,
		accelerators: s.opts.Accelerators, smartHistory: s.opts.SmartHistory, settings: s.opts.Settings, storage: s.opts.Storage,
		shares: s.opts.Shares, files: s.opts.Files, docker: s.opts.Docker, catalog: s.opts.Catalog, backup: s.opts.Backup,
		backupTarget: s.opts.BackupTarget, cluster: s.opts.Cluster, notify: s.opts.Notify, webPush: s.opts.WebPush, activity: s.opts.Activity,
		tailscale: s.opts.Tailscale, netbird: s.opts.Netbird, wireGuard: s.opts.WireGuard, letsEncrypt: s.opts.LetsEncrypt,
		fileRoots: s.filesArea().fileRoots, userNames: s.sharesArea().userNames, servicesOn: s.servicesArea().servicesOn}
}
