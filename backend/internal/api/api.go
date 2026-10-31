// Package api is the HTTP interface of titand: the JSON API under /api/v1
// and the static files of the web UI.
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/fans"
	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/healthreport"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
	"github.com/ra5on/TitanOS/backend/internal/logkeep"
	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/media"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/replication"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/shutdown"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/usb"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// Options are the parts the API works with.
type Options struct {
	// UI holds the files of the web UI with index.html at its root.
	UI                 fs.FS
	Store              *config.Store
	Users              *auth.Users
	Monitoring         *monitoring.Manager
	Media              *media.Manager
	Maintenance        *maintenance.Manager
	ApplicationBackups *appbackup.Manager
	Monitor            *system.Monitor
	Storage            *storage.Manager
	Shares             *shares.Manager
	Files              *files.Manager
	Docker             *docker.Manager
	// AppProxy gives apps HTTPS names of their own; nil leaves them out.
	AppProxy *appproxy.Manager
	Catalog  *catalog.Manager
	// Terminal hands out the shell on the NAS and knows whether consoles are allowed.
	Terminal *terminal.Manager
	VMs      *vm.Manager
	// LXC runs system containers; nil makes one that does nothing.
	LXC          *lxc.Manager
	Hardware     *hardware.Manager
	Nvidia       *hardware.NvidiaControl
	IntelGuc     *hardware.IntelGuc // GuC/HuC setting of Intel graphics; nil leaves it out
	Accelerators *hardware.AcceleratorMonitor
	Plugins      *plugins.Manager
	Tailscale    *tailscale.Manager
	Netbird      *netbird.Manager
	Network      *network.Manager
	// Firewall is the packet filter; nil makes one that is off.
	Firewall *firewall.Manager
	Notify   *notify.Manager
	// WebPush reaches subscribed browsers; nil makes one in RAM only.
	WebPush *webpush.Manager
	// WireGuard is the VPN server of the NAS; nil makes one that is off.
	WireGuard *wgserver.Manager
	// HealthReport sends the regular summary; nil makes one that is off.
	HealthReport *healthreport.Manager
	// Fans drives the fans; nil makes one that only reads.
	Fans *fans.Manager
	// IPv64 is the account area of IPv64.net; nil makes one that is off.
	IPv64 *ipv64.Manager
	// IPv64Reserved are ports of titand that are never published.
	IPv64Reserved []int
	// HomeAssistant is optional; nil leaves the connection out.
	HomeAssistant *homeassistant.Manager
	// ConfigBackup saves /config to a volume and restores it on a new stick.
	ConfigBackup *configbackup.Manager
	// Diagnose reads the logs and packs bundles for bug reports.
	Diagnose *diagnose.Manager
	// SSH switches the SSH access; Certificate holds the certificate of
	// the web UI and is nil in development mode, where there is no TLS.
	SSH         *sshd.Manager
	Certificate *tlscert.Manager
	// LetsEncrypt fetches certificates over ACME; DDNS keeps host names
	// at the addresses of the NAS. Both nil leave the pages without data.
	LetsEncrypt *letsencrypt.Manager
	DDNS        *ddns.Manager
	// Update installs new versions into the other slot of the stick.
	Update *update.Manager
	// Backup copies shares to another volume, server or the cloud.
	Backup *backup.Manager
	// BackupTarget takes in the backups of other TitanOS over Tailscale.
	BackupTarget *backuptarget.Manager
	// Replication sends Btrfs volumes to other TitanOS; ReplicaReceiver
	// holds what others sent here.
	Replication     *replication.Manager
	ReplicaReceiver *replication.Receiver
	// USB lists and mounts unmanaged physical data drives; nil if unavailable.
	USB *usb.Manager
	// UPS watches the uninterruptible power supply.
	UPS      *ups.Manager
	Settings *system.Settings
	Energy   *system.EnergyManager
	// PowerSchedule is optional; nil leaves the schedule out.
	PowerSchedule *system.PowerScheduler
	PowerButton   *system.PowerButtonManager
	MemoryPolicy  *system.MemoryPolicy
	Power         system.Power
	// Shutdown ends the guests before a planned restart or shutdown.
	Shutdown *shutdown.Manager
	Version  string
	// ShareDir holds the texts of the licenses.
	ShareDir string
	// SecureCookies is on whenever the API is served over HTTPS.
	SecureCookies bool
	// Sessions are the logins; nil starts with none, kept in memory only.
	Sessions *auth.Sessions
	// Activity records who changed what; nil records nothing.
	Activity *activity.Manager
	// Cluster is the membership in a cluster of NAS; nil in development
	// tests that do not need it.
	Cluster *cluster.Manager
	// Thumbs makes previews for the file manager; nil offers none.
	Thumbs *thumbs.Manager
	// FileIndex is the optional full-text index; nil offers no content search.
	FileIndex *fileindex.Manager
	// LogKeep keeps the journal on a volume; nil offers only this start.
	LogKeep *logkeep.Manager
}

// Server answers all HTTP requests.
type Server struct {
	opts      Options
	sessions  *auth.Sessions
	tokens    *auth.Tokens
	limiter   *auth.Limiter
	prefs     *preferences
	loginLook loginLook
	appIcon   appIcons
	dav       davState
	mcp       mcpState
	haStore   haStorageState
	handler   http.Handler
	// patterns are all routes of the mux, for tests that compare them
	// with the route list of the public API.
	patterns []string
	// manual is the documentation for docs_search and docs_read over MCP.
	manual *toolcatalog.Manual
	// power is a restart or shutdown that waits for a rebuild; see system.go.
	powerMu sync.Mutex
	power   pendingPower
	// rebuilding replaces the rebuild check in tests.
	rebuilding func() bool
	haUndo     haUndoStore
	services   servicesState
	passkeys   passkeyChallenges

	loadMu      sync.Mutex
	lastLoadTry time.Time
	ipv64       ipv64Area
}

// New returns the handler for all requests.
func New(opts Options) *Server {
	s := &Server{
		opts:     opts,
		sessions: opts.Sessions,
		limiter:  auth.NewLimiter(),
		prefs:    newPreferences(opts.Store),
		tokens:   auth.NewTokens(opts.Store),
	}
	if s.sessions == nil {
		s.sessions = auth.NewSessions()
	}
	if s.opts.Shares != nil {
		s.opts.Shares.OnPurgeResumed(s.sharePurgeDone)
	}
	if c := s.opts.Cluster; c != nil {
		c.Applied, c.Status, c.Notify = s.clusterApplied, s.ClusterStatus, s.clusterNotify
	}
	if s.opts.LXC == nil {
		s.opts.LXC = lxc.NewManager(opts.Store, shares.UsersGID)
		s.opts.LXC.Disabled = true
	}
	if s.opts.Firewall == nil {
		s.opts.Firewall = firewall.NewManager(opts.Store)
		s.opts.Firewall.Disabled = true
	}
	if s.opts.WireGuard == nil {
		s.opts.WireGuard = wgserver.New(opts.Store)
		s.opts.WireGuard.Disabled = true
		s.opts.WireGuard.Load()
	}
	if s.opts.Fans == nil {
		s.opts.Fans = fans.New(opts.Store)
		s.opts.Fans.Disabled = true
		s.opts.Fans.Load()
	}
	if s.opts.HealthReport == nil {
		s.opts.HealthReport = healthreport.New(opts.Store)
		s.opts.HealthReport.Collect = func() healthreport.Data { return healthreport.Data{MemoryPercent: -1} }
		s.opts.HealthReport.Send = func(title, text, status string) error {
			if s.opts.Notify == nil {
				return notify.ErrNoChannel
			}
			return s.opts.Notify.SendReport(title, text)
		}
		s.opts.HealthReport.Load()
	}
	if s.opts.WebPush == nil {
		s.opts.WebPush = webpush.New(opts.Store)
		s.opts.WebPush.Load()
	}
	if s.opts.IPv64 == nil {
		s.opts.IPv64 = ipv64.NewManager(opts.Store)
		s.opts.IPv64.Disabled = true
		s.opts.IPv64.Options = ipv64Options
		s.opts.IPv64.Fallback = func() (string, string) {
			var ddns, le string
			if s.opts.DDNS != nil {
				ddns = s.opts.DDNS.IPv64Key()
			}
			if s.opts.LetsEncrypt != nil {
				le = s.opts.LetsEncrypt.IPv64Key()
			}
			return ddns, le
		}
		s.opts.IPv64.Load(context.Background())
	}
	s.manual = toolcatalog.NewManual(nil)
	if s.opts.UI != nil {
		// The manual is built into the web UI (see web/vite.config.ts).
		if manual, err := fs.Sub(s.opts.UI, "manual"); err == nil {
			s.manual = toolcatalog.NewManual(manual)
		}
	}
	if s.opts.Notify != nil {
		s.opts.Notify.SetPersonalDataDir(s.notifyDir)
		s.opts.Notify.Audience = s.notifyAudience
	}
	s.haStore.check = make(chan struct{}, 1)

	mux := &routeMux{ServeMux: http.NewServeMux()}
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/session", s.handleSession)
	// Whoever sets up first owns the NAS: only from the home network.
	mux.HandleFunc("POST /api/v1/setup", setupLocal(s.handleSetup))
	// A new stick may take the configuration from a backup instead.
	mux.HandleFunc("GET /api/v1/setup/backups", setupLocal(s.handleSetupBackups))
	mux.HandleFunc("POST /api/v1/setup/restore", setupLocal(s.handleSetupRestore))
	mux.HandleFunc("POST /api/v1/setup/restore/upload", setupLocal(s.handleSetupRestoreUpload))
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/login/passkey/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/v1/login/passkey", s.handlePasskeyLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)
	// The look of the login page is public; changing it is not.
	mux.HandleFunc("GET /api/v1/login-appearance", s.handleLoginAppearance)
	mux.HandleFunc("GET /icons/{file}", s.handleAppIconFile)
	mux.HandleFunc("/dav/", s.handleWebDAV)
	mux.HandleFunc("/dav", s.handleWebDAV)
	// The MCP server checks its own token (mcp.go).
	mux.HandleFunc(mcpPath, s.handleMCP)
	mux.HandleFunc("GET /api/v1/system/mcp", s.requireUser(s.handleMCPSettings))
	mux.HandleFunc("PUT /api/v1/system/mcp", s.requireAdmin(s.handleMCPSettingsSet))
	mux.HandleFunc("GET /api/v1/system/webdav", s.requireAdmin(s.handleWebDAVSettings))
	mux.HandleFunc("PUT /api/v1/system/webdav", s.requireAdmin(s.handleWebDAVSettingsSet))
	mux.HandleFunc("GET /api/v1/account/webdav", s.requireUser(s.handleAccountWebDAV))
	mux.HandleFunc("POST /api/v1/account/webdav", s.requireUser(s.handleAccountWebDAVNew))
	mux.HandleFunc("DELETE /api/v1/account/webdav", s.requireUser(s.handleAccountWebDAVDelete))
	mux.HandleFunc("GET /api/v1/app-icon", s.requireAdmin(s.handleAppIcon))
	mux.HandleFunc("PUT /api/v1/app-icon", s.requireAdmin(s.handlePutAppIcon))
	mux.HandleFunc("GET /api/v1/login-appearance/background/{id}", s.handleLoginBackground)
	mux.HandleFunc("PUT /api/v1/login-appearance", s.requireAdmin(s.handlePutLoginAppearance))
	mux.HandleFunc("DELETE /api/v1/login-appearance", s.requireAdmin(s.handleDeleteLoginAppearance))

	mux.HandleFunc("GET /api/v1/preferences", s.requireUser(s.handleGetPreferences))
	mux.HandleFunc("PUT /api/v1/preferences/{kind}", s.requireUser(s.handlePutPreferences))
	mux.HandleFunc("GET /api/v1/background/{id}", s.requireUser(s.handleGetDashboardImage))
	mux.HandleFunc("GET /api/v1/background", s.requireUser(s.handleGetBackground))
	mux.HandleFunc("PUT /api/v1/background", s.requireUser(s.handlePutBackground))
	mux.HandleFunc("DELETE /api/v1/background", s.requireUser(s.handleDeleteBackground))

	mux.HandleFunc("GET /api/v1/system", s.requireUser(s.handleSystem))
	mux.HandleFunc("POST /api/v1/system/power", s.requireAdmin(s.handlePower))
	mux.HandleFunc("GET /api/v1/system/power", s.requireAdmin(s.handlePowerStatus))
	mux.HandleFunc("DELETE /api/v1/system/power", s.requireAdmin(s.handlePowerCancel))
	mux.HandleFunc("POST /api/v1/system/power/now", s.requireAdmin(s.handlePowerNow))

	// Everyone may see the volumes; only administrators may change disks.
	s.operationsRoutes(mux)
	mux.HandleFunc("GET /api/v1/storage", s.requireUser(s.handleStorage))
	mux.HandleFunc("GET /api/v1/storage/disk-sleep", s.requireAdmin(s.handleDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/disk-sleep", s.requireAdmin(s.handleSetDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/quotas", s.requireAdmin(s.handleSetShareQuotas))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/sync-speed", s.requireAdmin(s.handleSetSyncSpeed))
	mux.HandleFunc("POST /api/v1/storage/rescan", s.requireAdmin(s.handleStorageRescan))
	mux.HandleFunc("GET /api/v1/storage/smart", s.requireAdmin(s.handleSmart))
	mux.HandleFunc("POST /api/v1/storage/smart/refresh", s.requireAdmin(s.handleSmartRefresh))
	mux.HandleFunc("GET /api/v1/storage/smart/{disk}", s.requireAdmin(s.handleSmartDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes", s.requireHeavyAdmin(s.handleCreateVolume))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireHeavyAdmin(s.handleDeleteVolume))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/disks", s.requireHeavyAdmin(s.handleAddVolumeDisk))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/disks/{disk}", s.requireHeavyAdmin(s.handleRemoveVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/replace", s.requireHeavyAdmin(s.handleReplaceVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/disks/{disk}/identify", s.requireAdmin(s.handleIdentifyDisk))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/profile", s.requireHeavyAdmin(s.handleBtrfsProfile))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/cache", s.requireHeavyAdmin(s.handleVolumeCache))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/cache", s.requireHeavyAdmin(s.handleVolumeCacheRemove))
	// Encrypted volumes: browser sessions only, never over MCP
	// catalog or the public API (storage_crypt.go).
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/unlock", s.requireAdmin(s.handleVolumeUnlock))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/lock", s.requireHeavyAdmin(s.handleVolumeLock))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/passwords", s.requireAdmin(s.handleVolumePasswords))
	mux.HandleFunc("GET /api/v1/storage/volumes/{name}/luks-header", s.requireAdmin(s.handleVolumeHeaderBackup))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/check", s.requireHeavyAdmin(s.handleCheckVolume))
	mux.HandleFunc("GET /api/v1/storage/volumes/{name}/snapshots", s.requireAdmin(s.handleSnapshots))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/snapshots", s.requireHeavyAdmin(s.handleSnapshotAction))
	mux.HandleFunc("GET /api/v1/storage/tests", s.requireAdmin(s.handleDiskTests))
	mux.HandleFunc("POST /api/v1/storage/tests", s.requireHeavyAdmin(s.handleDiskTestStart))
	mux.HandleFunc("DELETE /api/v1/storage/tests/{disk}", s.requireAdmin(s.handleDiskTestStop))
	mux.HandleFunc("POST /api/v1/storage/tests/{disk}/resume", s.requireHeavyAdmin(s.handleDiskTestResume))
	mux.HandleFunc("DELETE /api/v1/storage/test-notices", s.requireAdmin(s.handleDiskTestNotices))

	mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PUT /api/v1/users/{name}", s.requireAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{name}", s.requireAdmin(s.handleDeleteUser))
	mux.HandleFunc("POST /api/v1/users/{name}/lock", s.requireAdmin(s.handleLockUser(true)))
	mux.HandleFunc("POST /api/v1/users/{name}/unlock", s.requireAdmin(s.handleLockUser(false)))
	// Everyone may change their own password.
	mux.HandleFunc("POST /api/v1/users/{name}/password", s.requireUser(s.handleSetPassword))

	// One's own account.
	mux.HandleFunc("GET /api/v1/account/sessions", s.requireUser(s.handleAccountSessions))
	mux.HandleFunc("GET /api/v1/account/tokens", s.requireUser(s.handleTokens))
	mux.HandleFunc("POST /api/v1/account/tokens", s.requireUser(s.handleTokenCreate))
	mux.HandleFunc("DELETE /api/v1/account/tokens/{id}", s.requireUser(s.handleTokenRevoke))
	mux.HandleFunc("GET /api/v1/openapi.json", s.requireUser(s.handleOpenAPI))
	mux.HandleFunc("DELETE /api/v1/account/sessions/{id}", s.requireUser(s.handleAccountSessionEnd))
	mux.HandleFunc("DELETE /api/v1/account/sessions", s.requireUser(s.handleAccountSessionsEndOthers))
	mux.HandleFunc("POST /api/v1/account/2fa/begin", s.requireUser(s.handleTwoFactorBegin))
	mux.HandleFunc("GET /api/v1/account/passkeys", s.requireUser(s.handlePasskeys))
	mux.HandleFunc("POST /api/v1/account/passkeys/begin", s.requireUser(s.handlePasskeyBegin))
	mux.HandleFunc("POST /api/v1/account/passkeys", s.requireUser(s.handlePasskeyFinish))
	mux.HandleFunc("DELETE /api/v1/account/passkeys/{id}", s.requireUser(s.handlePasskeyDelete))
	mux.HandleFunc("POST /api/v1/account/passkeys/confirm", s.requireUser(s.handlePasskeyConfirmBegin))
	mux.HandleFunc("POST /api/v1/account/2fa/enable", s.requireUser(s.handleTwoFactorEnable))
	mux.HandleFunc("POST /api/v1/account/2fa/disable", s.requireUser(s.handleTwoFactorDisable))
	mux.HandleFunc("POST /api/v1/account/2fa/recovery", s.requireUser(s.handleTwoFactorRecovery))
	mux.HandleFunc("GET /api/v1/account/ssh-keys", s.requireUser(s.handleAccountKeys))
	mux.HandleFunc("POST /api/v1/account/ssh-keys", s.requireUser(s.handleAccountKeyAdd))
	mux.HandleFunc("DELETE /api/v1/account/ssh-keys/{id}", s.requireUser(s.handleAccountKeyRemove))
	mux.HandleFunc("GET /api/v1/account/notifications", s.requireUser(s.handleAccountNotify))
	mux.HandleFunc("PUT /api/v1/account/notifications", s.requireUser(s.handleAccountNotifyChange))
	mux.HandleFunc("POST /api/v1/account/notifications/test", s.requireUser(s.handleAccountNotifyTest))

	mux.HandleFunc("GET /api/v1/shares", s.requireUser(s.handleListShares))
	mux.HandleFunc("POST /api/v1/shares", s.requireAdmin(s.handleCreateShare))
	mux.HandleFunc("PUT /api/v1/shares/{name}", s.requireAdmin(s.handleUpdateShare))
	mux.HandleFunc("DELETE /api/v1/shares/{name}", s.requireAdmin(s.handleDeleteShare))
	mux.HandleFunc("POST /api/v1/shares/{name}/unlock", s.requireAdmin(s.handleShareUnlock))
	mux.HandleFunc("GET /api/v1/shares/{name}/delete-data", s.requireAdmin(s.handleShareDataPreview))
	mux.HandleFunc("POST /api/v1/shares/{name}/delete-data", s.requireHeavyAdmin(s.handleShareDataDelete))

	// The file manager. Every request names a root the account has access to.
	mux.HandleFunc("GET /api/v1/files/roots", s.requireUser(s.handleFileRoots))
	mux.HandleFunc("GET /api/v1/files/undo", s.requireUser(s.handleFileUndoList))
	mux.HandleFunc("POST /api/v1/files/undo/{id}", s.requireUser(s.handleFileUndo))
	mux.HandleFunc("POST /api/v1/files/rename-preview", s.requireUser(s.handleFileRenamePreview))
	mux.HandleFunc("POST /api/v1/files/rename-batch", s.requireUser(s.handleFileRenameBatch))
	mux.HandleFunc("POST /api/v1/files/archive", s.requireUser(s.handleFileArchiveList))
	mux.HandleFunc("POST /api/v1/files/extract", s.requireUser(s.handleFileExtract))
	mux.HandleFunc("GET /api/v1/files/text/previous", s.requireUser(s.handleFilePrevious))
	mux.HandleFunc("POST /api/v1/files/duplicates", s.requireUser(s.handleFileDuplicates))
	mux.HandleFunc("POST /api/v1/files/search", s.requireUser(s.handleFileSearch))
	mux.HandleFunc("GET /api/v1/files/thumb", s.requireUser(s.handleFileThumb))
	mux.HandleFunc("GET /api/v1/files/photos", s.requireUser(s.handleFilePhotos))
	mux.HandleFunc("GET /api/v1/files/index", s.requireAdmin(s.handleFileIndex))
	mux.HandleFunc("PUT /api/v1/files/index", s.requireAdmin(s.handleSetFileIndex))
	mux.HandleFunc("POST /api/v1/files/index/run", s.requireAdmin(s.handleFileIndexRun))
	mux.HandleFunc("DELETE /api/v1/files/index/{volume}", s.requireAdmin(s.handleFileIndexRemove))
	mux.HandleFunc("DELETE /api/v1/files/thumbs/{volume}", s.requireAdmin(s.handleThumbCacheClear))
	mux.HandleFunc("POST /api/v1/files/compare", s.requireUser(s.handleFileCompare))
	mux.HandleFunc("GET /api/v1/files/properties", s.requireUser(s.handleFileProperties))
	mux.HandleFunc("GET /api/v1/files/list", s.requireUser(s.handleFileList))
	mux.HandleFunc("GET /api/v1/files/download", s.requireUser(s.handleFileDownload))
	mux.HandleFunc("GET /api/v1/files/zip", s.requireUser(s.handleFileZip))
	mux.HandleFunc("POST /api/v1/files/create", s.requireUser(s.handleFileCreate))
	mux.HandleFunc("POST /api/v1/files/mkdir", s.requireUser(s.handleFileMkdir))
	mux.HandleFunc("POST /api/v1/files/rename", s.requireUser(s.handleFileRename))
	mux.HandleFunc("POST /api/v1/files/delete", s.requireUser(s.handleFileDelete))
	mux.HandleFunc("GET /api/v1/files/trash", s.requireUser(s.handleTrashList))
	mux.HandleFunc("POST /api/v1/files/trash/{action}", s.requireUser(s.handleTrash))
	mux.HandleFunc("GET /api/v1/files/text", s.requireUser(s.handleReadFileText))
	mux.HandleFunc("PUT /api/v1/files/text", s.requireUser(s.handleFileText))
	mux.HandleFunc("POST /api/v1/files/inspect", s.requireUser(s.handleFileInspect))
	mux.HandleFunc("POST /api/v1/files/copy", s.requireUser(s.handleFileCopy))
	mux.HandleFunc("POST /api/v1/files/write-document", s.requireUser(s.handleWriteDocument))
	mux.HandleFunc("POST /api/v1/files/move-many", s.requireUser(s.handleMoveMany))
	mux.HandleFunc("POST /api/v1/files/url-download/probe", s.requireUser(s.handleFileDownloadProbe))
	mux.HandleFunc("POST /api/v1/files/url-download", s.requireUser(s.handleFileURLDownload))
	mux.HandleFunc("POST /api/v1/files/jobs/{id}/control", s.requireUser(s.handleFileJobControl))
	mux.HandleFunc("POST /api/v1/files/usage", s.requireUser(s.handleFileUsage))
	mux.HandleFunc("GET /api/v1/files/jobs", s.requireUser(s.handleFileJobs))
	mux.HandleFunc("POST /api/v1/files/jobs/{id}/retry", s.requireUser(s.handleFileJobRetry))
	mux.HandleFunc("POST /api/v1/files/jobs/{id}/cancel", s.requireUser(s.handleFileJobCancel))
	mux.HandleFunc("GET /api/v1/usb", s.requireAdmin(s.handleUSB))
	mux.HandleFunc("POST /api/v1/usb/{id}/mount", s.requireAdmin(s.handleUSBMount))
	mux.HandleFunc("POST /api/v1/usb/{id}/eject", s.requireAdmin(s.handleUSBEject))
	mux.HandleFunc("GET /api/v1/usb/disks", s.requireAdmin(s.handleUSBDisks))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/format", s.requireAdmin(s.handleUSBFormat))
	mux.HandleFunc("GET /api/v1/usb/disks/{id}/partitions", s.requireAdmin(s.handleUSBPartitions))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/table", s.requireAdmin(s.handleUSBTable))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions", s.requireAdmin(s.handleUSBPartitionCreate))
	mux.HandleFunc("DELETE /api/v1/usb/disks/{id}/partitions/{number}", s.requireAdmin(s.handleUSBPartitionDelete))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions/{number}/format", s.requireAdmin(s.handleUSBPartitionFormat))
	mux.HandleFunc("PUT /api/v1/usb/disks/{id}/partitions/{number}/label", s.requireAdmin(s.handleUSBPartitionLabel))
	mux.HandleFunc("POST /api/v1/usb/disks/{id}/partitions/{number}/grow", s.requireAdmin(s.handleUSBPartitionGrow))
	mux.HandleFunc("GET /api/v1/files/uploads", s.requireUser(s.handleUploads))
	mux.HandleFunc("POST /api/v1/files/uploads/{id}/verify", s.requireUser(s.handleUploadVerify))
	mux.HandleFunc("POST /api/v1/files/uploads", s.requireUser(s.handleUploadStart))
	mux.HandleFunc("GET /api/v1/files/uploads/{id}", s.requireUser(s.handleUploadStatus))
	mux.HandleFunc("PATCH /api/v1/files/uploads/{id}", s.requireUser(s.handleUploadChunk))
	mux.HandleFunc("POST /api/v1/files/uploads/{id}/finish", s.requireUser(s.handleUploadFinish))
	mux.HandleFunc("DELETE /api/v1/files/uploads/{id}", s.requireUser(s.handleUploadAbort))

	// Docker. Whoever controls containers controls the machine: administrators only.
	mux.HandleFunc("GET /api/v1/docker", s.requireAdmin(s.handleDocker))
	mux.HandleFunc("GET /api/v1/docker/networks", s.requireAdmin(s.handleLANNetworks))
	mux.HandleFunc("POST /api/v1/docker/networks", s.requireHeavyAppAdmin(s.handleLANCreate))
	mux.HandleFunc("DELETE /api/v1/docker/networks/{name}", s.requireAppAdmin(s.handleLANRemove))
	mux.HandleFunc("PUT /api/v1/docker/settings", s.requireAppAdmin(s.handleDockerSettings))
	mux.HandleFunc("GET /api/v1/services/state", s.requireUser(s.handleServicesState))
	mux.HandleFunc("GET /api/v1/services", s.requireAdmin(s.handleServices))
	mux.HandleFunc("PUT /api/v1/services/{name}", s.requireAdmin(s.handleServiceSet))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/{action}", s.requireAppAdmin(s.handleContainerAction))
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", s.requireAppAdmin(s.handleContainerRemove))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", s.requireAdmin(s.handleContainerLogs))
	mux.HandleFunc("POST /api/v1/docker/apps", s.requireHeavyAppAdmin(s.handleAppCreate))
	mux.HandleFunc("GET /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/network", s.requireHeavyAppAdmin(s.handleLANAssign))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}", s.requireAppAdmin(s.handleAppRemove))
	mux.HandleFunc("PUT /api/v1/docker/apps/{name}", s.requireAppAdmin(s.handleAppSetCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/update", s.requireHeavyAppAdmin(s.handleAppUpdate))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}/job", s.requireAppAdmin(s.handleAppJobDismiss))
	mux.HandleFunc("POST /api/v1/docker/updates/check", s.requireAppAdmin(s.handleUpdatesCheck))
	mux.HandleFunc("GET /api/v1/docker/auto-update", s.requireAdmin(s.handleAutoUpdates))
	mux.HandleFunc("PUT /api/v1/docker/auto-update", s.requireAppAdmin(s.handleAutoUpdatesSet))
	mux.HandleFunc("POST /api/v1/docker/updates/apply", s.requireHeavyAppAdmin(s.handleUpdatesApply))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/console", s.requireAppConsole(s.handleContainerConsole))
	mux.HandleFunc("GET /api/v1/docker/proxy", s.requireAdmin(s.handleAppProxy))
	mux.HandleFunc("PUT /api/v1/docker/proxy", s.requireAppAdmin(s.handleAppProxyDomain))
	mux.HandleFunc("PUT /api/v1/docker/proxy/apps/{app}", s.requireAppAdmin(s.handleAppProxyRoute))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/apps/{app}", s.requireAppAdmin(s.handleAppProxyRemove))
	mux.HandleFunc("POST /api/v1/docker/proxy/apps/{app}/tailnet", s.requireAppAdmin(s.handleAppTailnet))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/apps/{app}/tailnet", s.requireAppAdmin(s.handleAppTailnetRemove))
	mux.HandleFunc("POST /api/v1/docker/proxy/apps/{app}/tailnet/check", s.requireAppAdmin(s.handleAppTailnetCheck))
	mux.HandleFunc("PUT /api/v1/docker/proxy/targets/{id}", s.requireAppAdmin(s.handleAppProxyTarget))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/targets/{id}", s.requireAppAdmin(s.handleAppProxyTargetRemove))

	// The app store: templates to install as Docker apps.
	mux.HandleFunc("GET /api/v1/apps", s.requireAdmin(s.handleApps))
	mux.HandleFunc("POST /api/v1/apps/refresh", s.requireAdmin(s.handleAppsRefresh))
	mux.HandleFunc("POST /api/v1/apps/sources", s.requireAdmin(s.handleSourceAdd))
	mux.HandleFunc("DELETE /api/v1/apps/sources/{id}", s.requireAdmin(s.handleSourceRemove))
	mux.HandleFunc("GET /api/v1/apps/sources/{source}/apps/{app}", s.requireAdmin(s.handleAppTemplate))

	// Virtual machines. Like containers: administrators only.
	mux.HandleFunc("GET /api/v1/vms", s.requireAdmin(s.handleVMs))
	mux.HandleFunc("GET /api/v1/vms/imports", s.requireAdmin(s.handleMediaImports))
	mux.HandleFunc("POST /api/v1/vms/imports", s.requireHeavyAppAdmin(s.handleMediaImport))
	mux.HandleFunc("POST /api/v1/vms/virtio", s.requireHeavyAppAdmin(s.handleVirtIO))
	mux.HandleFunc("PUT /api/v1/vms/imports/{id}/data", s.requireHeavyAppAdmin(s.handleMediaChunk))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/verify", s.requireHeavyAppAdmin(s.handleMediaUploadVerify))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/{action}", s.requireAppAdmin(s.handleMediaControl))
	mux.HandleFunc("POST /api/v1/vms", s.requireHeavyAppAdmin(s.handleVMCreate))
	mux.HandleFunc("GET /api/v1/vms/media", s.requireAdmin(s.handleVMMedia))
	mux.HandleFunc("GET /api/v1/vms/host", s.requireAdmin(s.handleVMHost))
	// Taking a VM over from the copy of its settings on a volume.
	mux.HandleFunc("GET /api/v1/vms/adopt", s.requireAdmin(s.handleVMAdoptable))
	mux.HandleFunc("POST /api/v1/vms/adopt", s.requireAppAdmin(s.handleVMAdopt))
	mux.HandleFunc("GET /api/v1/hardware", s.requireAdmin(s.handleHardware))
	mux.HandleFunc("GET /api/v1/system/memory", s.requireAdmin(s.handleMemoryPolicy))
	mux.HandleFunc("PUT /api/v1/system/memory", s.requireAdmin(s.handleMemoryPolicyChange))
	mux.HandleFunc("GET /api/v1/system/power-button", s.requireAdmin(s.handlePowerButton))
	mux.HandleFunc("PUT /api/v1/system/power-button", s.requireAdmin(s.handlePowerButtonChange))
	mux.HandleFunc("GET /api/v1/system/energy", s.requireAdmin(s.handleEnergy))
	mux.HandleFunc("PUT /api/v1/system/energy", s.requireAdmin(s.handleEnergyChange))
	mux.HandleFunc("GET /api/v1/system/power-schedule", s.requireAdmin(s.handlePowerSchedule))
	mux.HandleFunc("PUT /api/v1/system/power-schedule", s.requireAdmin(s.handlePowerScheduleChange))
	mux.HandleFunc("GET /api/v1/hardware/container-devices", s.requireAdmin(s.handleContainerDevices))
	mux.HandleFunc("GET /api/v1/hardware/nvidia", s.requireAdmin(s.handleNvidia))
	mux.HandleFunc("PUT /api/v1/hardware/nvidia/{address}", s.requireAdmin(s.handleNvidiaSet))
	mux.HandleFunc("GET /api/v1/hardware/intel-gpu", s.requireAdmin(s.handleIntelGuc))
	mux.HandleFunc("PUT /api/v1/hardware/intel-gpu", s.requireAdmin(s.handleIntelGucSet))
	mux.HandleFunc("GET /api/v1/vms/{name}/snapshots", s.requireAdmin(s.handleVMSnapshots))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots", s.requireHeavyAppAdmin(s.handleVMSnapshotCreate))
	mux.HandleFunc("PUT /api/v1/vms/{name}/snapshot-schedule", s.requireAppAdmin(s.handleVMSnapshotSchedule))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshot-recovery", s.requireHeavyAppAdmin(s.handleVMSnapshotRecover))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots/{snapshot}/restore", s.requireHeavyAppAdmin(s.handleVMSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/vms/{name}/snapshots/{snapshot}", s.requireHeavyAppAdmin(s.handleVMSnapshotDelete))
	mux.HandleFunc("POST /api/v1/vms/{name}/grow", s.requireHeavyAppAdmin(s.handleVMGrow))
	mux.HandleFunc("GET /api/v1/vms/{name}/serial", s.requireAppConsole(s.handleVMSerial))
	mux.HandleFunc("PUT /api/v1/vms/{name}", s.requireAppAdmin(s.handleVMUpdate))
	mux.HandleFunc("DELETE /api/v1/vms/{name}", s.requireAppAdmin(s.handleVMDelete))
	mux.HandleFunc("GET /api/v1/vms/{name}/xml", s.requireAdmin(s.handleVMXML))
	mux.HandleFunc("GET /api/v1/vms/{name}/addresses", s.requireAdmin(s.handleVMAddresses))
	mux.HandleFunc("PUT /api/v1/vms/{name}/medium", s.requireAppAdmin(s.handleVMMedium))
	mux.HandleFunc("GET /api/v1/vms/{name}/screen", s.requireAdmin(s.handleVMScreen))
	mux.HandleFunc("POST /api/v1/vms/{name}/{action}", s.requireAppAdmin(s.handleVMAction))

	// System containers (LXC). Like VMs: administrators only.
	mux.HandleFunc("GET /api/v1/lxc", s.requireAdmin(s.handleLXC))
	mux.HandleFunc("GET /api/v1/lxc/images", s.requireAdmin(s.handleLXCImages))
	mux.HandleFunc("POST /api/v1/lxc", s.requireHeavyAppAdmin(s.handleLXCCreate))
	mux.HandleFunc("PUT /api/v1/lxc/{name}", s.requireAppAdmin(s.handleLXCUpdate))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}", s.requireAppAdmin(s.handleLXCDelete))
	mux.HandleFunc("GET /api/v1/lxc/{name}/config", s.requireAdmin(s.handleLXCConfig))
	mux.HandleFunc("GET /api/v1/lxc/{name}/console", s.requireAppConsole(s.handleLXCConsole))
	mux.HandleFunc("GET /api/v1/lxc/{name}/snapshots", s.requireAdmin(s.handleLXCSnapshots))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots", s.requireHeavyAppAdmin(s.handleLXCSnapshotCreate))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots/{snapshot}/restore", s.requireHeavyAppAdmin(s.handleLXCSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}/snapshots/{snapshot}", s.requireHeavyAppAdmin(s.handleLXCSnapshotDelete))
	mux.HandleFunc("POST /api/v1/lxc/{name}/{action}", s.requireAppAdmin(s.handleLXCAction))

	// How the NAS is connected. Changes are on trial until confirmed.
	mux.HandleFunc("GET /api/v1/network", s.requireAdmin(s.handleNetwork))
	mux.HandleFunc("POST /api/v1/network/diagnostics", s.requireAdmin(s.handleNetworkDiagnostics))
	mux.HandleFunc("PUT /api/v1/network", s.requireAdmin(s.clusterLocal(s.handleNetworkChange)))
	mux.HandleFunc("PUT /api/v1/network/options", s.requireAdmin(s.clusterLocal(s.handleNetworkOptions)))
	mux.HandleFunc("POST /api/v1/network/bridge", s.requireAdmin(s.clusterLocal(s.handleNetworkBridge)))
	mux.HandleFunc("POST /api/v1/network/confirm", s.requireAdmin(s.clusterLocal(s.handleNetworkConfirm)))
	mux.HandleFunc("POST /api/v1/network/revert", s.requireAdmin(s.handleNetworkRevert))
	// The packet filter; changes are on trial like network changes.
	mux.HandleFunc("GET /api/v1/firewall", s.requireAdmin(s.handleFirewall))
	mux.HandleFunc("PUT /api/v1/firewall", s.requireAdmin(s.clusterLocal(s.handleFirewallChange)))
	mux.HandleFunc("POST /api/v1/firewall/confirm", s.requireAdmin(s.clusterLocal(s.handleFirewallConfirm)))
	mux.HandleFunc("POST /api/v1/firewall/revert", s.requireAdmin(s.handleFirewallRevert))
	mux.HandleFunc("PUT /api/v1/firewall/autoblock", s.requireAdmin(s.handleFirewallAutoBlock))
	mux.HandleFunc("POST /api/v1/firewall/unblock", s.requireAdmin(s.handleFirewallUnblock))
	mux.HandleFunc("DELETE /api/v1/firewall/recent", s.requireAdmin(s.handleFirewallClearRecent))
	mux.HandleFunc("GET /api/v1/notifications", s.requireAdmin(s.handleNotifications))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", s.requireAdmin(s.handleDismissNotifications))
	mux.HandleFunc("GET /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettings))
	mux.HandleFunc("PUT /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettingsChange))
	mux.HandleFunc("POST /api/v1/notifications/test", s.requireAdmin(s.handleNotifyTest))
	mux.HandleFunc("GET /api/v1/network/wireguard", s.requireAdmin(s.handleWireGuard))
	mux.HandleFunc("PUT /api/v1/network/wireguard", s.requireAdmin(s.handleWireGuardSet))
	mux.HandleFunc("POST /api/v1/network/wireguard/peers", s.requireAdmin(s.handleWireGuardAdd))
	mux.HandleFunc("POST /api/v1/network/wireguard/peers/remove", s.requireAdmin(s.handleWireGuardRemove))
	mux.HandleFunc("GET /api/v1/system/fans", s.requireAdmin(s.handleFans))
	mux.HandleFunc("PUT /api/v1/system/fans", s.requireAdmin(s.handleFansSet))
	mux.HandleFunc("GET /api/v1/health-report", s.requireAdmin(s.handleHealthReport))
	mux.HandleFunc("PUT /api/v1/health-report", s.requireAdmin(s.handleHealthReportSet))
	mux.HandleFunc("POST /api/v1/health-report/send", s.requireAdmin(s.handleHealthReportSend))
	mux.HandleFunc("GET /api/v1/notifications/push", s.requireAdmin(s.handlePushDevices))
	mux.HandleFunc("POST /api/v1/notifications/push", s.requireAdmin(s.handlePushSubscribe))
	mux.HandleFunc("DELETE /api/v1/notifications/push/{id}", s.requireAdmin(s.handlePushRemove))
	mux.HandleFunc("POST /api/v1/notifications/push/{id}/test", s.requireAdmin(s.handlePushTest))
	mux.HandleFunc("GET /api/v1/homeassistant/settings", s.requireAdmin(s.handleHASettings))
	mux.HandleFunc("PUT /api/v1/homeassistant/settings", s.requireAdmin(s.handleHASettingsChange))
	mux.HandleFunc("POST /api/v1/homeassistant/check", s.requireAdmin(s.handleHACheck))
	mux.HandleFunc("GET /api/v1/homeassistant/entities", s.requireAdmin(s.handleHAEntities))
	mux.HandleFunc("POST /api/v1/homeassistant/undo", s.requireAdmin(s.handleHAUndo))
	mux.HandleFunc("POST /api/v1/homeassistant/call", s.requireAdmin(s.handleHACall))
	mux.HandleFunc("GET /api/v1/homeassistant/notify-services", s.requireUser(s.handleHANotifyServices))
	// Storage for Home Assistant: a share, an account and the entry there.
	mux.HandleFunc("GET /api/v1/homeassistant/storage", s.requireAdmin(s.handleHAStorage))
	mux.HandleFunc("POST /api/v1/homeassistant/storage", s.requireAdmin(s.handleHAStorageAdd))
	mux.HandleFunc("POST /api/v1/homeassistant/storage/register", s.requireAdmin(s.handleHAStorageRegister))
	mux.HandleFunc("POST /api/v1/homeassistant/storage/check", s.requireAdmin(s.handleHAStorageCheck))
	mux.HandleFunc("PUT /api/v1/homeassistant/storage/{mount}", s.requireAdmin(s.handleHAStorageChange))
	mux.HandleFunc("DELETE /api/v1/homeassistant/storage/{mount}", s.requireAdmin(s.handleHAStorageRemove))
	mux.HandleFunc("GET /api/v1/config-backup", s.requireAdmin(s.handleConfigBackup))
	mux.HandleFunc("PUT /api/v1/config-backup", s.requireAdmin(s.handleConfigBackupSettings))
	mux.HandleFunc("POST /api/v1/config-backup/run", s.requireAdmin(s.handleConfigBackupRun))
	mux.HandleFunc("GET /api/v1/config-backup/download", s.requireAdmin(s.handleConfigBackupDownload))
	mux.HandleFunc("GET /api/v1/logs", s.requireAdmin(s.handleLogs))
	mux.HandleFunc("GET /api/v1/logs/export", s.requireAdmin(s.handleLogsExport))
	mux.HandleFunc("GET /api/v1/activity", s.requireAdmin(s.handleActivity))
	mux.HandleFunc("PUT /api/v1/activity", s.requireAdmin(s.handleActivitySettings))
	mux.HandleFunc("GET /api/v1/diagnostics/bundle", s.requireAdmin(s.handleDiagnosticBundle))
	mux.HandleFunc("GET /api/v1/about", s.requireUser(s.handleAbout))

	// Backups of shares. They hold everyone's files: administrators only.
	mux.HandleFunc("POST /api/v1/backups/smb/discover", s.requireAdmin(s.handleSMBDiscover))
	mux.HandleFunc("PUT /api/v1/backups/smb", s.requireAdmin(s.backupJobOr("smb", s.handleSMBSave)))
	mux.HandleFunc("DELETE /api/v1/backups/smb/{target}", s.requireAdmin(s.handleSMBDelete))
	mux.HandleFunc("GET /api/v1/backups/progress", s.requireAdmin(s.handleUSBBackupProgress))
	mux.HandleFunc("GET /api/v1/backups", s.requireAdmin(s.handleBackups))
	mux.HandleFunc("POST /api/v1/backups", s.requireAdmin(s.handleBackupCreate))
	mux.HandleFunc("GET /api/v1/backups/target", s.requireAdmin(s.handleBackupTarget))
	mux.HandleFunc("PUT /api/v1/backups/target", s.requireAdmin(s.backupJobOr("target", s.handleBackupTargetSet)))
	mux.HandleFunc("POST /api/v1/backups/target/sources", s.requireAdmin(s.handleBackupTargetPair))
	mux.HandleFunc("DELETE /api/v1/backups/target/sources/{name}", s.requireAdmin(s.handleBackupTargetRemove))
	mux.HandleFunc("GET /api/v1/replication", s.requireAdmin(s.handleReplication))
	mux.HandleFunc("POST /api/v1/replication", s.requireAdmin(s.handleReplicationSave))
	mux.HandleFunc("PUT /api/v1/replication/{id}", s.requireAdmin(s.handleReplicationSave))
	mux.HandleFunc("DELETE /api/v1/replication/{id}", s.requireAdmin(s.handleReplicationRemove))
	mux.HandleFunc("POST /api/v1/replication/{id}/run", s.requireHeavyAdmin(s.handleReplicationRun))
	mux.HandleFunc("POST /api/v1/replication/received/restore", s.requireHeavyAdmin(s.handleReplicaRestore))
	mux.HandleFunc("GET /api/v1/backups/folders", s.requireAdmin(s.handleBackupFolders))
	mux.HandleFunc("POST /api/v1/backups/ssh-key", s.requireAdmin(s.handleBackupKey))
	mux.HandleFunc("PUT /api/v1/backups/{id}", s.requireAdmin(s.handleBackupUpdate))
	mux.HandleFunc("DELETE /api/v1/backups/{id}", s.requireAdmin(s.handleBackupDelete))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots", s.requireAdmin(s.handleBackupSnapshots))
	mux.HandleFunc("GET /api/v1/backups/{id}/password", s.requireAdmin(s.handleBackupPassword))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", s.requireHeavyAdmin(s.handleBackupRestore))
	mux.HandleFunc("POST /api/v1/backups/{id}/check", s.requireHeavyAdmin(s.handleBackupCheck))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots/{snapshot}/files", s.requireAdmin(s.handleBackupFiles))
	mux.HandleFunc("POST /api/v1/backups/{id}/{action}", s.requireAdmin(s.handleBackupAction))
	mux.HandleFunc("GET /api/v1/ups", s.requireAdmin(s.handleUPS))
	mux.HandleFunc("PUT /api/v1/ups", s.requireAdmin(s.handleUPSChange))
	mux.HandleFunc("POST /api/v1/ups/scan", s.requireAdmin(s.handleUPSScan))
	mux.HandleFunc("GET /api/v1/update", s.requireAdmin(s.handleUpdate))
	mux.HandleFunc("GET /api/v1/update/progress", s.requireAdmin(s.handleUpdateProgress))
	mux.HandleFunc("PUT /api/v1/update/settings", s.requireAdmin(s.handleUpdateSettings))
	mux.HandleFunc("POST /api/v1/update/{action}", s.requireAdmin(s.handleUpdateAction))
	mux.HandleFunc("GET /api/v1/system/ssh", s.requireAdmin(s.handleSSH))
	mux.HandleFunc("PUT /api/v1/system/ssh", s.requireAdmin(s.handleSSHChange))
	mux.HandleFunc("PUT /api/v1/system/ssh/networks", s.requireAdmin(s.handleSSHNetworks))
	mux.HandleFunc("GET /api/v1/system/certificate", s.requireAdmin(s.handleCertificate))
	mux.HandleFunc("PUT /api/v1/system/certificate", s.requireAdmin(s.handleCertificateSet))
	mux.HandleFunc("DELETE /api/v1/system/certificate", s.requireAdmin(s.handleCertificateRemove))
	mux.HandleFunc("GET /api/v1/system/letsencrypt", s.requireAdmin(s.handleLetsEncrypt))
	mux.HandleFunc("PUT /api/v1/system/letsencrypt", s.requireAdmin(s.handleLetsEncryptChange))
	mux.HandleFunc("POST /api/v1/system/letsencrypt/renew", s.requireAdmin(s.handleLetsEncryptRenew))
	mux.HandleFunc("GET /api/v1/network/ddns", s.requireAdmin(s.handleDDNS))
	mux.HandleFunc("PUT /api/v1/network/ddns", s.requireAdmin(s.handleDDNSChange))
	mux.HandleFunc("POST /api/v1/network/ddns/update", s.requireAdmin(s.handleDDNSUpdate))
	// IPv64.net: POST everywhere, so that a typed-in API key stays out of addresses.
	mux.HandleFunc("POST /api/v1/network/ipv64/account", s.requireAdmin(s.handleIPv64Account))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains", s.requireAdmin(s.handleIPv64Domains))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/create", s.requireAdmin(s.handleIPv64DomainCreate))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/change", s.requireAdmin(s.handleIPv64DomainChange))
	mux.HandleFunc("POST /api/v1/network/ipv64/records", s.requireAdmin(s.handleIPv64Records))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/add", s.requireAdmin(s.handleIPv64RecordAdd))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/delete", s.requireAdmin(s.handleIPv64RecordDelete))
	mux.HandleFunc("POST /api/v1/network/ipv64/healthchecks", s.requireAdmin(s.handleIPv64Healthchecks))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/on", s.requireAdmin(s.handleIPv64HeartbeatOn))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/off", s.requireAdmin(s.handleIPv64HeartbeatOff))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/status", s.requireAdmin(s.handleIPv64HeartbeatStatus))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/test", s.requireAdmin(s.handleIPv64HeartbeatTest))
	s.ipv64Routes(mux)
	mux.HandleFunc("PUT /api/v1/system/timezone", s.requireAdmin(s.handleTimezone))
	mux.HandleFunc("PUT /api/v1/system/hostname", s.requireAdmin(s.handleHostname))

	// Plugins become part of the system: administrators only.
	mux.HandleFunc("GET /api/v1/plugins/tailscale", s.requireAdmin(s.handleTailscale))
	mux.HandleFunc("GET /api/v1/plugins/tailscale/devices", s.requireAdmin(s.handleTailscaleDevices))
	mux.HandleFunc("PUT /api/v1/plugins/tailscale", s.requireAdmin(s.handleTailscaleSettings))
	mux.HandleFunc("POST /api/v1/plugins/tailscale/control/{action}", s.requireAdmin(s.handleTailscaleAction))
	mux.HandleFunc("GET /api/v1/plugins/netbird", s.requireAdmin(s.handleNetbird))
	mux.HandleFunc("GET /api/v1/plugins/netbird/peers", s.requireAdmin(s.handleNetbirdPeers))
	mux.HandleFunc("PUT /api/v1/plugins/netbird", s.requireAdmin(s.handleNetbirdSettings))
	mux.HandleFunc("PUT /api/v1/plugins/netbird/setup-key", s.requireAdmin(s.handleNetbirdSetupKey))
	mux.HandleFunc("POST /api/v1/plugins/netbird/control/{action}", s.requireAdmin(s.handleNetbirdAction))
	mux.HandleFunc("GET /api/v1/plugins", s.requireAdmin(s.handlePlugins))
	mux.HandleFunc("GET /api/v1/plugins/progress", s.requireAdmin(s.handlePluginProgress))
	mux.HandleFunc("DELETE /api/v1/plugins/progress/{id}", s.requireAdmin(s.handlePluginProgressForget))
	mux.HandleFunc("GET /api/v1/plugins/releases", s.requireAdmin(s.handlePluginOldReleases))
	mux.HandleFunc("POST /api/v1/plugins/cleanup", s.requireAdmin(s.handlePluginCleanup))
	mux.HandleFunc("POST /api/v1/plugins/nvidia/updates", s.requireAdmin(s.handleDriverUpdate))
	mux.HandleFunc("POST /api/v1/plugins/catalog", s.requireAdmin(s.handlePluginCatalog))
	mux.HandleFunc("POST /api/v1/plugins/recommendations/{name}/dismiss", s.requireAdmin(s.handlePluginDismiss))
	mux.HandleFunc("DELETE /api/v1/plugins/recommendations/dismissed", s.requireAdmin(s.handlePluginRestoreDismissed))
	mux.HandleFunc("GET /api/v1/plugins/sources", s.requireAdmin(s.handlePluginSources))
	mux.HandleFunc("POST /api/v1/plugins/sources", s.requireAdmin(s.handlePluginSourceAdd))
	mux.HandleFunc("DELETE /api/v1/plugins/sources/{id}", s.requireAdmin(s.handlePluginSourceRemove))
	mux.HandleFunc("POST /api/v1/plugins/external/inspect", s.requireAdmin(s.handlePluginInspect))
	mux.HandleFunc("POST /api/v1/plugins/external", s.requireHeavyAdmin(s.handlePluginExternal))
	mux.HandleFunc("POST /api/v1/plugins", s.requireHeavyAdmin(s.handlePluginInstall))
	mux.HandleFunc("POST /api/v1/plugins/{name}/update", s.requireHeavyAdmin(s.handlePluginUpdate))
	mux.HandleFunc("POST /api/v1/plugins/{name}/{action}", s.requireAdmin(s.handlePluginAction))
	mux.HandleFunc("DELETE /api/v1/plugins/{name}", s.requireAdmin(s.handlePluginRemove))

	// Shells on the NAS and in containers.
	mux.HandleFunc("GET /api/v1/terminal", s.requireAdmin(s.handleTerminal))
	mux.HandleFunc("PUT /api/v1/terminal", s.requireAdmin(s.handleTerminalSettings))
	mux.HandleFunc("GET /api/v1/system/login", s.requireAdmin(s.handleLoginSettings))
	mux.HandleFunc("PUT /api/v1/system/login", s.requireAdmin(s.handleSetLoginSettings))
	mux.HandleFunc("GET /api/v1/terminal/shell", s.requireAdmin(s.handleShell))
	mux.HandleFunc("POST /api/v1/session/confirm", s.requireUser(s.handleConfirm))
	mux.HandleFunc("GET /api/v1/session/confirmed", s.requireUser(s.handleConfirmed))

	s.routeCluster(mux)

	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", uiHandler(opts.UI))

	// Browsers must not let another website send requests with our cookie.
	s.patterns = mux.patterns
	s.handler = s.securityHeaders(s.clusterAccess(http.NewCrossOriginProtection().Handler(s.bearerAccess(s.activityLog(mux.ServeMux)))))
	return s
}

// routeMux is a ServeMux that remembers its patterns.
type routeMux struct {
	*http.ServeMux
	patterns []string
}

func (m *routeMux) Handle(pattern string, h http.Handler) {
	m.patterns = append(m.patterns, pattern)
	m.ServeMux.Handle(pattern, h)
}

func (m *routeMux) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	m.patterns = append(m.patterns, pattern)
	m.ServeMux.HandleFunc(pattern, h)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		if s.trustedTLS(r) {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}

// hstsValue keeps browsers on HTTPS for half a year, for the name of the
// request only: other names of the NAS (apps, .local, the IP address) keep
// their own certificates.
const hstsValue = "max-age=15552000"

// trustedTLS reports whether the request came over a certificate from
// Let's Encrypt. Only then is HSTS safe: with the self-signed or an own
// certificate the browser could no longer be told to accept it.
func (s *Server) trustedTLS(r *http.Request) bool {
	if r.TLS == nil || r.TLS.ServerName == "" || s.opts.Certificate == nil {
		return false
	}
	info := s.opts.Certificate.Info()
	if info.LetsEncrypt == nil {
		return false
	}
	served, err := s.opts.Certificate.GetCertificate(&tls.ClientHelloInfo{ServerName: r.TLS.ServerName})
	return err == nil && tlscert.Describe(served).Fingerprint == info.LetsEncrypt.Fingerprint
}

// apiError is the body of every failed API request. The web UI translates
// Code; Message is for people reading the raw response.
type apiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retryAfter,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

// handleAPINotFound keeps unknown API paths from falling through to the web
// UI, which would answer them with index.html.
func handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
}

// maxJSONBody bounds every JSON request body.
const maxJSONBody = 64 * 1024

// readJSON decodes the request body into v and answers the request itself
// if that fails.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	return readJSONBody(w, r, v)
}

// jsonReadTimeout is how long a client has to send a JSON body. The
// server has no ReadTimeout (uploads and WebSockets take long), so the
// deadline is set here, for JSON bodies only.
var jsonReadTimeout = 30 * time.Second

// readJSONBody is readJSON for callers that set their own size limit.
func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	// Not every writer supports it (tests, in-process requests); without,
	// the body is read as before.
	control := http.NewResponseController(w)
	timed := control.SetReadDeadline(time.Now().Add(jsonReadTimeout)) == nil
	body, err := io.ReadAll(r.Body)
	if timed && err == nil {
		// The handler may run long after the body; an expired deadline
		// would end its request context. After a failed read it stays, so
		// that the server does not wait for the rest of the body.
		control.SetReadDeadline(time.Time{})
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		var netErr net.Error
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
		} else if errors.As(err, &netErr) && netErr.Timeout() {
			writeError(w, http.StatusRequestTimeout, "request_timeout", "the request body did not arrive in time")
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "could not read the request body")
		}
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return false
	}
	return true
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type contextKey struct{}

// currentUser returns the account the request is logged in as.
func currentUser(r *http.Request) auth.User {
	user, _ := r.Context().Value(contextKey{}).(auth.User)
	return user
}

// authenticate returns the account behind the session cookie.
func (s *Server) authenticate(r *http.Request) (auth.User, bool) {
	if name, ok := r.Context().Value(apiTokenIdentity{}).(string); ok {
		return s.activeUser(name)
	}
	// Only in-process requests can carry this unexported context key.
	if name, ok := r.Context().Value(inProcessIdentity{}).(string); ok {
		return s.activeUser(name)
	}
	// Signed by another node of the cluster on behalf of this account.
	if id, ok := cluster.FromContext(r.Context()); ok {
		return s.activeUser(id.User)
	}
	cookie, err := r.Cookie(s.cookieName())
	if err != nil {
		return auth.User{}, false
	}
	session, ok := s.sessions.Get(cookie.Value)
	if !ok {
		return auth.User{}, false
	}
	// The account may have been removed or locked since the login.
	return s.activeUser(session.User)
}

// activeUser returns an account that exists and is not locked. Everything
// that acts as an account (sessions, API tokens, MCP, Home
// Assistant, consoles) goes through it, so a lock holds on every path even
// before its sessions are ended.
func (s *Server) activeUser(name string) (auth.User, bool) {
	user, ok := s.opts.Users.Get(name)
	if !ok || user.Disabled {
		return auth.User{}, false
	}
	return user, true
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
			return
		}
		noteActivity(r, user)
		next(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, user)))
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).Role.CanAdminister() {
			writeError(w, http.StatusForbidden, "forbidden", "this needs an owner or administrator")
			return
		}
		next(w, r)
	})
}

// requireAppAdmin is requireAdmin for routes that change Docker apps, VMs
// or system containers. An application backup owns their lifecycle: the
// change waits for none to run and holds the gate until it is done.
func (s *Server) requireAppAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(s.appLifecycle(true, next))
}

// requireHeavyAppAdmin is requireAppAdmin for changes that also need
// admission as a heavy operation.
func (s *Server) requireHeavyAppAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(s.appLifecycle(true, s.admitted(next)))
}

// requireAppConsole is requireAdmin for consoles of apps, VMs and system
// containers. They are not opened while an application backup runs, but
// an open console must not block a backup for as long as it stays open,
// so the gate is checked only when it opens.
func (s *Server) requireAppConsole(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(s.appLifecycle(false, next))
}

// appLifecycle refuses the request while an application backup runs or
// waits for review. With hold it keeps the gate for the whole request.
func (s *Server) appLifecycle(hold bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gate := s.opts.ApplicationBackups
		if gate == nil {
			next(w, r)
			return
		}
		if !gate.Gate.TryRLock() {
			writeError(w, http.StatusLocked, "application_backup_running", "application backup owns the application lifecycle")
			return
		}
		if hold {
			defer gate.Gate.RUnlock()
		}
		if gate.Busy() {
			if !hold {
				gate.Gate.RUnlock()
			}
			writeError(w, http.StatusLocked, "application_backup_running", "review the pending application backup before changing applications")
			return
		}
		if !hold {
			gate.Gate.RUnlock()
		}
		next(w, r)
	}
}

// handleHealth says that titand answers and, as "ready", that it has its
// configuration and volumes. After an update the boot check waits for
// that before the new system is marked good.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ready := s.opts.Users.Loaded() && s.opts.Storage.Settled() && (s.opts.Update == nil || s.opts.Update.Healthy())
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ready": ready})
}
