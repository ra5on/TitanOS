package api

import (
	"context"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// haArea is Home Assistant: the connections, what the NAS tells it over
// MQTT and what it may switch, the storage and backups of Home Assistant on
// the NAS, and undoing a call (homeassistant*.go, ha_*.go); an area with
// narrow dependencies (PLAN 8a, see fansArea). The receiver is hass: ha is
// a connection in these files.
//
// home is the first connection (nil: none); hub reads every connection,
// which the server builds on first use. The other managers are what the
// telemetry reports and the storage of Home Assistant uses. maintenance
// and applications read the planned maintenance and the coordinator of
// application backups when they are asked (tests replace both on a running
// server). undo, first, stores and storesMu are state of the server.
// storageBusy says whether an array is rebuilt or checked, internalAs
// calls a route of the web API in the name of an account, activeUser
// finds an account that is not locked, webSession lets only a person at a
// web session through, agentButtons and agentButton are the tasks of the
// KI-Agent Home Assistant can start, findShare and userNames are the
// shares'.
type haArea struct {
	home         *homeassistant.Manager
	hub          func() *homeassistant.Hub
	store        *config.Store
	version      string
	users        *auth.Users
	monitor      *system.Monitor
	monitoring   *monitoring.Manager
	accelerators *hardware.AcceleratorMonitor
	storage      *storage.Manager
	shares       *shares.Manager
	files        *files.Manager
	docker       *docker.Manager
	vms          *vm.Manager
	backup       *backup.Manager
	update       *update.Manager
	ups          *ups.Manager
	notify       *notify.Manager
	ssh          *sshd.Manager
	maintenance  func() *maintenance.Manager
	applications func() *appbackup.Manager

	undo     *haUndoStore
	first    *haStorageState
	stores   *map[string]*haStorageState
	storesMu *sync.Mutex

	storageBusy  func(ctx context.Context) bool
	internalAs   func(ctx context.Context, user auth.User, method, route string, body any) (int, string)
	activeUser   func(name string) (auth.User, bool)
	webSession   webSessionFunc
	agentButtons func() []homeassistant.AgentTaskValues
	agentButton  func(name, action string) error
	findShare    func(name string) (shares.Share, bool)
	userNames    func() []string
}

// haArea is the area of the server; the KI-Agent and the tool catalog ask
// it for connections and tools.
func (s *Server) haArea() haArea {
	return haArea{home: s.opts.HomeAssistant, hub: s.haHub, store: s.opts.Store, version: s.opts.Version, users: s.opts.Users,
		monitor: s.opts.Monitor, monitoring: s.opts.Monitoring, accelerators: s.opts.Accelerators, storage: s.opts.Storage,
		shares: s.opts.Shares, files: s.opts.Files, docker: s.opts.Docker, vms: s.opts.VMs, backup: s.opts.Backup, update: s.opts.Update,
		ups: s.opts.UPS, notify: s.opts.Notify, ssh: s.opts.SSH,
		maintenance:  func() *maintenance.Manager { return s.opts.Maintenance },
		applications: func() *appbackup.Manager { return s.opts.ApplicationBackups },
		undo:         &s.haUndo, first: &s.haStore, stores: &s.haStores, storesMu: &s.haStoresMu,
		storageBusy: s.storageBusy, internalAs: s.internalAs, activeUser: s.authArea().activeUser, webSession: s.requireWebSession,
		agentButtons: s.agentButtons, agentButton: s.agentButton, findShare: s.sharesArea().findShare, userNames: s.sharesArea().userNames}
}

// HASnapshot is what the NAS tells Home Assistant over MQTT; titand's
// wiring calls it.
func (s *Server) HASnapshot(ctx context.Context) homeassistant.Snapshot {
	return s.haArea().HASnapshot(ctx)
}

// HACommand carries out what Home Assistant asked for over MQTT; titand's
// wiring calls it.
func (s *Server) HACommand(ctx context.Context, kind, name, action string) error {
	return s.haArea().HACommand(ctx, kind, name, action)
}

// RunHAStorage watches whether Home Assistant still puts its backups on
// the shares; titand's wiring starts it.
func (s *Server) RunHAStorage(ctx context.Context) {
	s.haArea().RunHAStorage(ctx)
}
