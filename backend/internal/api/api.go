// Package api is the HTTP interface of titand: the JSON API under /api/v1
// and the static files of the web UI.
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/startup"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/appaccess"
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
	"github.com/ra5on/TitanOS/backend/internal/hwcheck"
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
	"github.com/ra5on/TitanOS/backend/internal/procs"
	"github.com/ra5on/TitanOS/backend/internal/replication"
	"github.com/ra5on/TitanOS/backend/internal/scripts"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/shutdown"
	"github.com/ra5on/TitanOS/backend/internal/smarthistory"
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
	"github.com/ra5on/TitanOS/backend/internal/viewer"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"github.com/ra5on/TitanOS/backend/internal/webapps"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// Options are the parts the API works with.
type Options struct {
	// UI holds the files of the web UI with index.html at its root.
	UI          fs.FS
	Store       *config.Store
	Users       *auth.Users
	Monitoring  *monitoring.Manager
	Media       *media.Manager
	Maintenance *maintenance.Manager
	Scripts     *scripts.Manager
	// Processes lists and ends processes for the task manager (PLAN 61).
	Processes          *procs.Sampler
	ApplicationBackups *appbackup.Manager
	Monitor            *system.Monitor
	Storage            *storage.Manager
	Shares             *shares.Manager
	// Startup: when apps and VMs start after boot (PLAN 73); nil in tests.
	Startup *startup.Manager
	Files   *files.Manager
	Docker  *docker.Manager
	// SystemData says where TitanOS keeps its own data while no volume is
	// chosen; nil takes the first mounted volume by name.
	SystemData *volumedata.Home
	// SmartHistory keeps the daily SMART values per disk; nil keeps none.
	SmartHistory *smarthistory.Recorder
	// AppProxy gives apps HTTPS names of their own; nil leaves them out.
	AppProxy *appproxy.Manager
	// AppAccess holds which accounts may use which Docker app (PLAN 61);
	// nil gives every app to owners and administrators only.
	AppAccess *appaccess.Manager
	Catalog   *catalog.Manager
	// Terminal hands out the shell on the NAS and knows whether consoles are allowed.
	Terminal *terminal.Manager
	// Viewer runs the remote view of Docker apps (PLAN 63); nil leaves it out.
	Viewer *viewer.Manager
	// Webapps installs and serves the web apps (PLAN 66); nil leaves them out.
	Webapps *webapps.Manager
	VMs     *vm.Manager
	// LXC runs system containers; nil makes one that does nothing.
	LXC          *lxc.Manager
	Hardware     *hardware.Manager
	Nvidia       *hardware.NvidiaControl
	IntelGuc     *hardware.IntelGuc  // GuC/HuC setting of Intel graphics; nil leaves it out
	UASQuirks    *hardware.UASQuirks // usb-storage instead of uas for USB bridges; nil leaves it out
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
	// HomeAssistants are all connections, HomeAssistant the first of them;
	// nil makes a hub of HomeAssistant alone.
	HomeAssistants *homeassistant.Hub
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
	// HWCheck reads the hardware compatibility report; nil switches it off.
	HWCheck *hwcheck.Checker
}

// Server answers all HTTP requests.
type Server struct {
	opts      Options
	sessions  *auth.Sessions
	tokens    *auth.Tokens
	limiter   *auth.Limiter
	prefs     *preferences
	loginLook loginLook
	// loginAccounts limits and checks the account list of the login page.
	loginAccounts loginAccountsState
	// accountPictures: the picture each account chose (account_picture.go).
	accountPictures accountPictures
	appIcon         appIcons
	dav             davState
	mcp             mcpState
	// migration is the move from Docker versions to web apps (PLAN 66).
	migration webappMigration
	haStore   haStorageState
	// haStores is the storage of further Home Assistant connections.
	haStores   map[string]*haStorageState
	haStoresMu sync.Mutex
	haHubOnce  sync.Once
	// containerNames caches Docker names for the process list.
	containerNames containerNameCache
	handler        http.Handler
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

	// The KI-Agent (agent*.go): settings, the Ollama client, running
	// chats and open pages. agentOpen and agentWebFactory replace the data
	// volume and the web in tests.
	agentConf   agentSettingsState
	agentClient atomic.Pointer[agent.Ollama]
	agentRT     agentRuntime
	agentWatch  agentWatchers
	agentMirror agentMirror
	agentSnaps  agentSnapState
	agentOpen   func() (*os.Root, error)
	// viewerOpen replaces viewer.Manager.Open in tests.
	viewerOpen viewerOpener
	// agentSandbox replaces the Docker sandbox of code_run in tests.
	agentSandbox    func(context.Context, docker.SandboxRequest) (docker.SandboxResult, error)
	agentWebFactory func() *agent.Web
	agentInit       agentInitState
	agentReact      agentReactState
	agentStoreOnce  sync.Once
	agentStoreOne   *agent.Store
	agentPull       agentPull
	agentMove       agentMoveState

	loadMu      sync.Mutex
	lastLoadTry time.Time
	ipv64       ipv64State

	// ctx is the root context of the server: it ends with Close (or with
	// the context given to Start), and with it every job the server
	// started on its own. jobs counts those jobs, so Close can wait.
	ctx    context.Context
	cancel context.CancelFunc
	jobs   sync.WaitGroup
	// pending are the stand-ins New made for missing managers; Start
	// loads their settings.
	pending []func(context.Context) error
	// shareSyncs counts the agentSyncShare runs started in the background
	// after a settings change; tests wait for them before they change the
	// storage manager.
	shareSyncs sync.WaitGroup
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
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if s.sessions == nil {
		s.sessions = auth.NewSessions()
	}
	if s.opts.Processes == nil {
		// Not given (tests): the list reads /proc, signals are refused.
		s.opts.Processes = procs.New()
		s.opts.Processes.Disabled = true
	}
	if s.opts.LXC == nil {
		s.opts.LXC = lxc.NewManager(opts.Store, shares.UsersGID)
		s.opts.LXC.Disabled = true
	}
	if s.opts.Firewall == nil {
		s.opts.Firewall = firewall.NewManager(opts.Store)
		s.opts.Firewall.Disabled = true
	}
	// Managers that were not given are made as switched-off stand-ins
	// here; reading their settings is left to Start.
	if s.opts.WireGuard == nil {
		s.opts.WireGuard = wgserver.New(opts.Store)
		s.opts.WireGuard.Disabled = true
		s.pending = append(s.pending, func(context.Context) error { return s.opts.WireGuard.Load() })
	}
	if s.opts.Fans == nil {
		s.opts.Fans = fans.New(opts.Store)
		s.opts.Fans.Disabled = true
		s.pending = append(s.pending, func(context.Context) error { return s.opts.Fans.Load() })
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
		s.pending = append(s.pending, func(context.Context) error { return s.opts.HealthReport.Load() })
	}
	if s.opts.WebPush == nil {
		s.opts.WebPush = webpush.New(opts.Store)
		s.pending = append(s.pending, func(context.Context) error { return s.opts.WebPush.Load() })
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
		s.pending = append(s.pending, s.opts.IPv64.Load)
	}
	// Only now, with every stand-in in place: the areas built here take
	// the managers as they are at this moment.
	if s.opts.Shares != nil {
		s.opts.Shares.OnPurgeResumed(s.sharesArea().sharePurgeDone)
	}
	if c := s.opts.Cluster; c != nil {
		area := s.clusterArea()
		c.Applied, c.Status, c.Notify = area.clusterApplied, area.ClusterStatus, area.clusterNotify
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
	if s.opts.Notify != nil {
		s.opts.Notify.Observer = s.agentObserve
	}
	if s.opts.Cluster != nil {
		s.opts.Cluster.Agent = s.agentClusterHandler()
	}

	mux := &routeMux{ServeMux: http.NewServeMux()}
	// The routes by area (register_*.go), in the order they always had.
	s.registerLogin(mux)
	s.registerPreferences(mux)
	s.registerPower(mux)
	s.registerStorage(mux)
	s.registerUsers(mux)
	s.registerAccount(mux)
	s.registerShares(mux)
	s.registerFiles(mux)
	s.registerUSB(mux)
	s.registerUploads(mux)
	s.registerDocker(mux)
	s.registerAppStore(mux)
	s.registerViewer(mux)
	s.registerWebapps(mux)
	s.registerVMs(mux)
	s.registerHardware(mux)
	s.registerVMDetails(mux)
	s.registerLXC(mux)
	s.registerNetwork(mux)
	s.registerNotifications(mux)
	s.registerWireGuard(mux)
	s.registerFans(mux)
	s.registerHealthReport(mux)
	s.registerPush(mux)
	s.registerHomeAssistant(mux)
	s.registerMaintenance(mux)
	s.registerBackups(mux)
	s.registerUPS(mux)
	s.registerUpdate(mux)
	s.registerRemoteAccess(mux)
	s.registerIPv64(mux)
	s.registerSystemSettings(mux)
	s.registerPlugins(mux)
	s.registerTerminal(mux)
	s.registerScripts(mux)
	s.registerAgent(mux)

	s.routeCluster(mux)

	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", uiHandler(opts.UI, s.appAccessArea().appFrameSources))

	// Browsers must not let another website send requests with our cookie.
	s.patterns = mux.patterns
	s.handler = s.securityHeaders(s.clusterArea().clusterAccess(http.NewCrossOriginProtection().Handler(s.tokensArea().bearerAccess(s.activityLog(mux.ServeMux)))))
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
		// PLAN 61: the desktop shows the pages of TitanOS in windows (frames of
		// its own origin); other sites still may not frame it.
		h.Set("X-Frame-Options", "SAMEORIGIN")
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
	if r.TLS == nil || r.TLS.ServerName == "" || s.core().certificate == nil {
		return false
	}
	info := s.core().certificate.Info()
	if info.LetsEncrypt == nil {
		return false
	}
	served, err := s.core().certificate.GetCertificate(&tls.ClientHelloInfo{ServerName: r.TLS.ServerName})
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
func (au authArea) authenticate(r *http.Request) (auth.User, bool) {
	if name, ok := r.Context().Value(apiTokenIdentity{}).(string); ok {
		return au.activeUser(name)
	}
	// Only in-process requests can carry this unexported context key.
	if name, ok := r.Context().Value(inProcessIdentity{}).(string); ok {
		return au.activeUser(name)
	}
	// Signed by another node of the cluster on behalf of this account.
	if id, ok := cluster.FromContext(r.Context()); ok {
		return au.activeUser(id.User)
	}
	// A locked session (session_lock.go) counts as not logged in here;
	// requireUser tells it apart.
	user, state := au.cookieState(r)
	return user, state == authOK
}

// authenticateState is authenticate that also tells a locked session
// apart; tokens, in-process and cluster identities are never locked.
func (au authArea) authenticateState(r *http.Request) (auth.User, authState) {
	if _, ok := r.Context().Value(apiTokenIdentity{}).(string); ok {
		return stateOf(au.authenticate(r))
	}
	if _, ok := r.Context().Value(inProcessIdentity{}).(string); ok {
		return stateOf(au.authenticate(r))
	}
	if _, ok := cluster.FromContext(r.Context()); ok {
		return stateOf(au.authenticate(r))
	}
	return au.cookieState(r)
}

func stateOf(user auth.User, ok bool) (auth.User, authState) {
	if !ok {
		return auth.User{}, authNone
	}
	return user, authOK
}

// activeUser returns an account that exists and is not locked. Everything
// that acts as an account (sessions, API tokens, MCP, Home
// Assistant, consoles) goes through it, so a lock holds on every path even
// before its sessions are ended.
func (au authArea) activeUser(name string) (auth.User, bool) {
	user, ok := au.users.Get(name)
	if !ok || user.Disabled {
		return auth.User{}, false
	}
	return user, true
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, state := s.authArea().authenticateState(r)
		switch {
		case state == authNone:
			writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
			return
		case state == authLocked && !slices.Contains(lockExempt, r.Pattern):
			writeLocked(w)
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
		gate := s.core().applications
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
	ready := s.core().users.Loaded() && s.core().storage.Settled() && (s.core().update == nil || s.core().update.Healthy())
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ready": ready})
}
