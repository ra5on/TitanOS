// Package api is the HTTP interface of titand: the JSON API under /api/v1
// and the static files of the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
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
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/usb"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
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
	Version       string
	// ShareDir holds the texts of the licenses.
	ShareDir string
	// SecureCookies is on whenever the API is served over HTTPS.
	SecureCookies bool
	// Sessions are the logins; nil starts with none, kept in memory only.
	Sessions *auth.Sessions
	// Assistant is the chat with a model from Ollama; nil makes one that is off.
	Assistant *assistant.Manager
	// Activity records who changed what; nil records nothing.
	Activity *activity.Manager
	// Thumbs makes previews for the file manager; nil offers none.
	Thumbs *thumbs.Manager
	// FileIndex is the optional full-text index; nil offers no content search.
	FileIndex *fileindex.Manager
}

// Server answers all HTTP requests.
type Server struct {
	opts        Options
	sessions    *auth.Sessions
	tokens      *auth.Tokens
	limiter     *auth.Limiter
	prefs       *preferences
	loginLook   loginLook
	handler     http.Handler
	knowledge   *assistant.Knowledge
	automations *assistant.Automations
	chats       *assistant.Chats
	agentMu     sync.Mutex
	// power is a restart or shutdown that waits for a rebuild; see system.go.
	powerMu sync.Mutex
	power   pendingPower
	// rebuilding replaces the rebuild check in tests.
	rebuilding func() bool
	scheduleMu sync.Mutex
	agentJobs  map[string]*agentJob
	agentSlots chan struct{}
	// push holds the one-time tokens of buttons in notifications.
	push           pushTokens
	assistSessions assistSessions
	haUndo         haUndoStore
	passkeys       passkeyChallenges

	loadMu      sync.Mutex
	lastLoadTry time.Time
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
	if s.opts.Assistant == nil {
		s.opts.Assistant = assistant.NewManager(opts.Store)
	}
	if s.opts.LXC == nil {
		s.opts.LXC = lxc.NewManager(opts.Store, shares.UsersGID)
		s.opts.LXC.Disabled = true
	}
	if s.opts.Firewall == nil {
		s.opts.Firewall = firewall.NewManager(opts.Store)
		s.opts.Firewall.Disabled = true
	}
	if s.opts.WebPush == nil {
		s.opts.WebPush = webpush.New(opts.Store)
		s.opts.WebPush.Load()
	}
	if s.opts.UI != nil {
		// The manual is built into the web UI (see web/vite.config.ts).
		if manual, err := fs.Sub(s.opts.UI, "manual"); err == nil {
			s.opts.Assistant.Manual = assistant.NewManual(manual)
		}
	}
	s.chats = assistant.NewChats(s.chatsDir)
	s.automations = assistant.NewAutomations(s.chatsDir)
	s.knowledge = assistant.NewKnowledge(s.chatsDir)
	if s.opts.Notify != nil {
		s.opts.Notify.SetPersonalDataDir(s.chatsDir)
	}
	s.agentJobs = map[string]*agentJob{}
	s.agentSlots = make(chan struct{}, 1)
	if s.opts.HomeAssistant != nil {
		s.opts.HomeAssistant.OnEvent("mobile_app_notification_action", s.handleHANotificationAction)
		s.opts.HomeAssistant.OnEvent("titanos_ask", s.handleAssist)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/session", s.handleSession)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	// A new stick may take the configuration from a backup instead.
	mux.HandleFunc("GET /api/v1/setup/backups", s.handleSetupBackups)
	mux.HandleFunc("POST /api/v1/setup/restore", s.handleSetupRestore)
	mux.HandleFunc("POST /api/v1/setup/restore/upload", s.handleSetupRestoreUpload)
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/login/passkey/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/v1/login/passkey", s.handlePasskeyLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)
	// The look of the login page is public; changing it is not.
	mux.HandleFunc("GET /api/v1/login-appearance", s.handleLoginAppearance)
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

	// Everyone may see the volumes; only administrators may change disks.
	s.operationsRoutes(mux)
	mux.HandleFunc("GET /api/v1/storage", s.requireUser(s.handleStorage))
	mux.HandleFunc("GET /api/v1/storage/disk-sleep", s.requireAdmin(s.handleDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/disk-sleep", s.requireAdmin(s.handleSetDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/quotas", s.requireAdmin(s.handleSetShareQuotas))
	mux.HandleFunc("POST /api/v1/storage/rescan", s.requireAdmin(s.handleStorageRescan))
	mux.HandleFunc("GET /api/v1/storage/smart", s.requireAdmin(s.handleSmart))
	mux.HandleFunc("POST /api/v1/storage/smart/refresh", s.requireAdmin(s.handleSmartRefresh))
	mux.HandleFunc("GET /api/v1/storage/smart/{disk}", s.requireAdmin(s.handleSmartDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes", s.requireHeavyAdmin(s.handleCreateVolume))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}", s.requireHeavyAdmin(s.handleDeleteVolume))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/disks", s.requireHeavyAdmin(s.handleAddVolumeDisk))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/disks/{disk}", s.requireHeavyAdmin(s.handleRemoveVolumeDisk))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/profile", s.requireHeavyAdmin(s.handleBtrfsProfile))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/cache", s.requireHeavyAdmin(s.handleVolumeCache))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/cache", s.requireHeavyAdmin(s.handleVolumeCacheRemove))
	// Encrypted volumes: browser sessions only, never in the assistant
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
	mux.HandleFunc("POST /api/v1/docker/networks", s.requireHeavyAdmin(s.handleLANCreate))
	mux.HandleFunc("DELETE /api/v1/docker/networks/{name}", s.requireAdmin(s.handleLANRemove))
	mux.HandleFunc("PUT /api/v1/docker/settings", s.requireAdmin(s.handleDockerSettings))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/{action}", s.requireAdmin(s.handleContainerAction))
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", s.requireAdmin(s.handleContainerRemove))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", s.requireAdmin(s.handleContainerLogs))
	mux.HandleFunc("POST /api/v1/docker/apps", s.requireHeavyAdmin(s.handleAppCreate))
	mux.HandleFunc("GET /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/network", s.requireHeavyAdmin(s.handleLANAssign))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppRemove))
	mux.HandleFunc("PUT /api/v1/docker/apps/{name}", s.requireAdmin(s.handleAppSetCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/update", s.requireHeavyAdmin(s.handleAppUpdate))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}/job", s.requireAdmin(s.handleAppJobDismiss))
	mux.HandleFunc("POST /api/v1/docker/updates/check", s.requireAdmin(s.handleUpdatesCheck))
	mux.HandleFunc("POST /api/v1/docker/updates/apply", s.requireHeavyAdmin(s.handleUpdatesApply))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/console", s.requireAdmin(s.handleContainerConsole))
	mux.HandleFunc("GET /api/v1/docker/proxy", s.requireAdmin(s.handleAppProxy))
	mux.HandleFunc("PUT /api/v1/docker/proxy", s.requireAdmin(s.handleAppProxyDomain))
	mux.HandleFunc("PUT /api/v1/docker/proxy/apps/{app}", s.requireAdmin(s.handleAppProxyRoute))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/apps/{app}", s.requireAdmin(s.handleAppProxyRemove))
	mux.HandleFunc("PUT /api/v1/docker/proxy/targets/{id}", s.requireAdmin(s.handleAppProxyTarget))
	mux.HandleFunc("DELETE /api/v1/docker/proxy/targets/{id}", s.requireAdmin(s.handleAppProxyTargetRemove))

	// The app store: templates to install as Docker apps.
	mux.HandleFunc("GET /api/v1/apps", s.requireAdmin(s.handleApps))
	mux.HandleFunc("POST /api/v1/apps/refresh", s.requireAdmin(s.handleAppsRefresh))
	mux.HandleFunc("POST /api/v1/apps/sources", s.requireAdmin(s.handleSourceAdd))
	mux.HandleFunc("DELETE /api/v1/apps/sources/{id}", s.requireAdmin(s.handleSourceRemove))
	mux.HandleFunc("GET /api/v1/apps/sources/{source}/apps/{app}", s.requireAdmin(s.handleAppTemplate))

	// Virtual machines. Like containers: administrators only.
	mux.HandleFunc("GET /api/v1/vms", s.requireAdmin(s.handleVMs))
	mux.HandleFunc("GET /api/v1/vms/imports", s.requireAdmin(s.handleMediaImports))
	mux.HandleFunc("POST /api/v1/vms/imports", s.requireHeavyAdmin(s.handleMediaImport))
	mux.HandleFunc("POST /api/v1/vms/virtio", s.requireHeavyAdmin(s.handleVirtIO))
	mux.HandleFunc("PUT /api/v1/vms/imports/{id}/data", s.requireHeavyAdmin(s.handleMediaChunk))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/verify", s.requireHeavyAdmin(s.handleMediaUploadVerify))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/{action}", s.requireAdmin(s.handleMediaControl))
	mux.HandleFunc("POST /api/v1/vms", s.requireHeavyAdmin(s.handleVMCreate))
	mux.HandleFunc("GET /api/v1/vms/media", s.requireAdmin(s.handleVMMedia))
	mux.HandleFunc("GET /api/v1/vms/host", s.requireAdmin(s.handleVMHost))
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
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots", s.requireHeavyAdmin(s.handleVMSnapshotCreate))
	mux.HandleFunc("PUT /api/v1/vms/{name}/snapshot-schedule", s.requireAdmin(s.handleVMSnapshotSchedule))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshot-recovery", s.requireHeavyAdmin(s.handleVMSnapshotRecover))
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots/{snapshot}/restore", s.requireHeavyAdmin(s.handleVMSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/vms/{name}/snapshots/{snapshot}", s.requireHeavyAdmin(s.handleVMSnapshotDelete))
	mux.HandleFunc("POST /api/v1/vms/{name}/grow", s.requireHeavyAdmin(s.handleVMGrow))
	mux.HandleFunc("GET /api/v1/vms/{name}/serial", s.requireAdmin(s.handleVMSerial))
	mux.HandleFunc("PUT /api/v1/vms/{name}", s.requireAdmin(s.handleVMUpdate))
	mux.HandleFunc("DELETE /api/v1/vms/{name}", s.requireAdmin(s.handleVMDelete))
	mux.HandleFunc("GET /api/v1/vms/{name}/xml", s.requireAdmin(s.handleVMXML))
	mux.HandleFunc("GET /api/v1/vms/{name}/addresses", s.requireAdmin(s.handleVMAddresses))
	mux.HandleFunc("PUT /api/v1/vms/{name}/medium", s.requireAdmin(s.handleVMMedium))
	mux.HandleFunc("GET /api/v1/vms/{name}/screen", s.requireAdmin(s.handleVMScreen))
	mux.HandleFunc("POST /api/v1/vms/{name}/{action}", s.requireAdmin(s.handleVMAction))

	// System containers (LXC). Like VMs: administrators only.
	mux.HandleFunc("GET /api/v1/lxc", s.requireAdmin(s.handleLXC))
	mux.HandleFunc("GET /api/v1/lxc/images", s.requireAdmin(s.handleLXCImages))
	mux.HandleFunc("POST /api/v1/lxc", s.requireHeavyAdmin(s.handleLXCCreate))
	mux.HandleFunc("PUT /api/v1/lxc/{name}", s.requireAdmin(s.handleLXCUpdate))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}", s.requireAdmin(s.handleLXCDelete))
	mux.HandleFunc("GET /api/v1/lxc/{name}/config", s.requireAdmin(s.handleLXCConfig))
	mux.HandleFunc("GET /api/v1/lxc/{name}/console", s.requireAdmin(s.handleLXCConsole))
	mux.HandleFunc("GET /api/v1/lxc/{name}/snapshots", s.requireAdmin(s.handleLXCSnapshots))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots", s.requireHeavyAdmin(s.handleLXCSnapshotCreate))
	mux.HandleFunc("POST /api/v1/lxc/{name}/snapshots/{snapshot}/restore", s.requireHeavyAdmin(s.handleLXCSnapshotRestore))
	mux.HandleFunc("DELETE /api/v1/lxc/{name}/snapshots/{snapshot}", s.requireHeavyAdmin(s.handleLXCSnapshotDelete))
	mux.HandleFunc("POST /api/v1/lxc/{name}/{action}", s.requireAdmin(s.handleLXCAction))

	// How the NAS is connected. Changes are on trial until confirmed.
	mux.HandleFunc("GET /api/v1/network", s.requireAdmin(s.handleNetwork))
	mux.HandleFunc("POST /api/v1/network/diagnostics", s.requireAdmin(s.handleNetworkDiagnostics))
	mux.HandleFunc("PUT /api/v1/network", s.requireAdmin(s.handleNetworkChange))
	mux.HandleFunc("PUT /api/v1/network/options", s.requireAdmin(s.handleNetworkOptions))
	mux.HandleFunc("POST /api/v1/network/bridge", s.requireAdmin(s.handleNetworkBridge))
	mux.HandleFunc("POST /api/v1/network/confirm", s.requireAdmin(s.handleNetworkConfirm))
	mux.HandleFunc("POST /api/v1/network/revert", s.requireAdmin(s.handleNetworkRevert))
	// The packet filter; changes are on trial like network changes.
	mux.HandleFunc("GET /api/v1/firewall", s.requireAdmin(s.handleFirewall))
	mux.HandleFunc("PUT /api/v1/firewall", s.requireAdmin(s.handleFirewallChange))
	mux.HandleFunc("POST /api/v1/firewall/confirm", s.requireAdmin(s.handleFirewallConfirm))
	mux.HandleFunc("POST /api/v1/firewall/revert", s.requireAdmin(s.handleFirewallRevert))
	mux.HandleFunc("PUT /api/v1/firewall/autoblock", s.requireAdmin(s.handleFirewallAutoBlock))
	mux.HandleFunc("POST /api/v1/firewall/unblock", s.requireAdmin(s.handleFirewallUnblock))
	mux.HandleFunc("DELETE /api/v1/firewall/recent", s.requireAdmin(s.handleFirewallClearRecent))
	mux.HandleFunc("GET /api/v1/notifications", s.requireAdmin(s.handleNotifications))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", s.requireAdmin(s.handleDismissNotifications))
	mux.HandleFunc("GET /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettings))
	mux.HandleFunc("PUT /api/v1/notifications/settings", s.requireAdmin(s.handleNotifySettingsChange))
	mux.HandleFunc("POST /api/v1/notifications/test", s.requireAdmin(s.handleNotifyTest))
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
	mux.HandleFunc("GET /api/v1/config-backup", s.requireAdmin(s.handleConfigBackup))
	mux.HandleFunc("PUT /api/v1/config-backup", s.requireAdmin(s.handleConfigBackupSettings))
	mux.HandleFunc("POST /api/v1/config-backup/run", s.requireAdmin(s.handleConfigBackupRun))
	mux.HandleFunc("GET /api/v1/config-backup/download", s.requireAdmin(s.handleConfigBackupDownload))
	mux.HandleFunc("GET /api/v1/logs", s.requireAdmin(s.handleLogs))
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
	mux.HandleFunc("PUT /api/v1/system/timezone", s.requireAdmin(s.handleTimezone))
	mux.HandleFunc("PUT /api/v1/system/hostname", s.requireAdmin(s.handleHostname))
	// The chat in the web UI. Its settings are for administrators; who may
	// chat says requireAssistant.
	mux.HandleFunc("GET /api/v1/assistant/automations", s.requireAssistant(s.handleAutomations))
	mux.HandleFunc("GET /api/v1/assistant/knowledge", s.requireAssistant(s.handleKnowledge))
	mux.HandleFunc("POST /api/v1/assistant/knowledge", s.requireAssistant(s.handleKnowledgeSave))
	mux.HandleFunc("POST /api/v1/assistant/knowledge/search", s.requireAssistant(s.handleKnowledgeSearch))
	mux.HandleFunc("POST /api/v1/assistant/knowledge/delete", s.requireAssistant(s.handleKnowledgeDelete))
	mux.HandleFunc("POST /api/v1/assistant/automations", s.requireAssistant(s.handleAutomationSave))
	mux.HandleFunc("POST /api/v1/assistant/automations/preview", s.requireAssistant(s.handleAutomationPreview))
	mux.HandleFunc("DELETE /api/v1/assistant/automations/{automation}", s.requireAssistant(s.handleAutomationDelete))
	mux.HandleFunc("POST /api/v1/assistant/automations/{automation}/run", s.requireAssistant(s.handleAutomationRun))
	mux.HandleFunc("GET /api/v1/assistant/status", s.requireUser(s.handleAssistantStatus))
	mux.HandleFunc("GET /api/v1/assistant", s.requireAdmin(s.handleAssistant))
	mux.HandleFunc("PUT /api/v1/assistant", s.requireAdmin(s.handleAssistantChange))
	mux.HandleFunc("GET /api/v1/assistant/prompt", s.requireAdmin(s.handleAgentPrompt))
	mux.HandleFunc("POST /api/v1/assistant/probe", s.requireAdmin(s.handleAgentProbe))
	mux.HandleFunc("POST /api/v1/assistant/chats/{id}/control", s.requireAssistant(s.handleAgentControl))
	mux.HandleFunc("PUT /api/v1/assistant/chats/{id}/schedule", s.requireAssistant(s.handleAgentSchedule))
	mux.HandleFunc("GET /api/v1/assistant/chats/{id}/diagnostics", s.requireAssistant(s.handleAgentDiagnostics))
	mux.HandleFunc("POST /api/v1/assistant/models", s.requireAdmin(s.handleAssistantModels))
	mux.HandleFunc("POST /api/v1/assistant/models/pull", s.requireAdmin(s.handleAssistantPull))
	mux.HandleFunc("POST /api/v1/assistant/models/delete", s.requireAdmin(s.handleAssistantModelDelete))
	mux.HandleFunc("GET /api/v1/assistant/ollama-apps", s.requireAdmin(s.handleAssistantOllamaApps))
	mux.HandleFunc("GET /api/v1/assistant/models", s.requireAssistant(s.handleAssistantChatModels))
	mux.HandleFunc("GET /api/v1/assistant/chats", s.requireAssistant(s.handleAssistantChats))
	mux.HandleFunc("POST /api/v1/assistant/chats", s.requireAssistant(s.handleAssistantChatCreate))
	// The buttons of a notification: the one-time token is the permission.
	mux.HandleFunc("POST /api/v1/assistant/push/{token}/{decision}", s.handlePushDecision)
	mux.HandleFunc("DELETE /api/v1/assistant/chats", s.requireAssistant(s.handleAssistantChatsDelete))
	mux.HandleFunc("GET /api/v1/assistant/chats/{id}", s.requireAssistant(s.handleAssistantChatGet))
	mux.HandleFunc("PUT /api/v1/assistant/chats/{id}", s.requireAssistant(s.handleAssistantChatRename))
	mux.HandleFunc("DELETE /api/v1/assistant/chats/{id}", s.requireAssistant(s.handleAssistantChatDelete))
	mux.HandleFunc("POST /api/v1/assistant/chats/{id}/messages", s.requireAssistant(s.handleAssistantMessage))
	mux.HandleFunc("POST /api/v1/assistant/chats/{id}/regenerate", s.requireAssistant(s.handleAssistantRegenerate))
	mux.HandleFunc("POST /api/v1/assistant/chats/{id}/actions/{action}", s.requireAssistant(s.handleAssistantAction))
	// What the assistant writes and moves in the file manager, once confirmed.
	mux.HandleFunc("POST /api/v1/assistant/files/edit", s.requireAssistant(s.handleDocumentEdit))
	mux.HandleFunc("POST /api/v1/assistant/files/write", s.requireAssistant(s.handleAssistantWrite))
	mux.HandleFunc("POST /api/v1/assistant/files/move", s.requireAssistant(s.handleAssistantMove))

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
	mux.HandleFunc("POST /api/v1/plugins", s.requireHeavyAdmin(s.handlePluginInstall))
	mux.HandleFunc("POST /api/v1/plugins/{name}/update", s.requireHeavyAdmin(s.handlePluginUpdate))
	mux.HandleFunc("POST /api/v1/plugins/{name}/{action}", s.requireAdmin(s.handlePluginAction))
	mux.HandleFunc("DELETE /api/v1/plugins/{name}", s.requireAdmin(s.handlePluginRemove))

	// Shells on the NAS and in containers.
	mux.HandleFunc("GET /api/v1/terminal", s.requireAdmin(s.handleTerminal))
	mux.HandleFunc("PUT /api/v1/terminal", s.requireAdmin(s.handleTerminalSettings))
	mux.HandleFunc("GET /api/v1/terminal/shell", s.requireAdmin(s.handleShell))
	mux.HandleFunc("POST /api/v1/session/confirm", s.requireUser(s.handleConfirm))
	mux.HandleFunc("GET /api/v1/session/confirmed", s.requireUser(s.handleConfirmed))

	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", uiHandler(opts.UI))

	// Browsers must not let another website send requests with our cookie.
	s.handler = securityHeaders(http.NewCrossOriginProtection().Handler(s.bearerAccess(s.activityLog(mux))))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
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

// readJSONBody is readJSON for callers that set their own size limit.
func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
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
	if name, ok := r.Context().Value(agentIdentity{}).(string); ok {
		return s.activeUser(name)
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
// that acts as an account (sessions, API tokens, the assistant, Home
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
		if gate := s.opts.ApplicationBackups; gate != nil && (strings.HasPrefix(r.URL.Path, "/api/v1/docker/") || strings.HasPrefix(r.URL.Path, "/api/v1/vms") || strings.HasPrefix(r.URL.Path, "/api/v1/vm-clones/") || r.URL.Path == "/api/v1/lxc" || strings.HasPrefix(r.URL.Path, "/api/v1/lxc/")) && (r.Method != "GET" || strings.HasSuffix(r.URL.Path, "/console") || strings.HasSuffix(r.URL.Path, "/serial")) {
			if !gate.Gate.TryRLock() {
				writeError(w, http.StatusLocked, "application_backup_running", "application backup owns the application lifecycle")
				return
			}
			defer gate.Gate.RUnlock()
			if gate.Busy() {
				writeError(w, http.StatusLocked, "application_backup_running", "review the pending application backup before changing applications")
				return
			}
		}
		next(w, r)
	})
}

// handleHealth says that titand answers and, as "ready", that it has its
// configuration and volumes. After an update the boot check waits for
// that before the new system is marked good.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ready := s.opts.Users.Loaded() && s.opts.Storage.Settled() && (s.opts.Update == nil || s.opts.Update.Healthy())
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ready": ready})
}
