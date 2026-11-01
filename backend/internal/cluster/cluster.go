// Package cluster joins several TitanOS NAS into a cluster, as in Proxmox:
// from the web interface of the admin node all nodes can be managed, its
// accounts come to all of them and backups go from one node to another
// without a pairing code. The admin node is the one that created the
// cluster; it can allow other nodes to manage as well (Node.Manage). The
// other nodes are members: they are managed but manage nobody, and their
// own accounts stay with them (roles.go).
//
// There is no high availability, no quorum and no shared storage: every
// node keeps working on its own when the others are away.
//
// Nodes talk over the HTTPS port of titand, on any way between them (home
// network, Tailscale, NetBird, WireGuard, an address of one's own). The
// way does not carry the security: every connection checks the cluster
// certificate of the other node by its fingerprint (ServerName), and every
// request is signed with the secret of the cluster (sign.go).
//
// Files on the stick, written only on a change:
//
//	cluster/node.json     this node: ID, certificate and key, own addresses
//	cluster/cluster.json  the cluster: name, nodes, versions of the accounts
//	cluster/secret.json   secret of the cluster and the join key
package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http/httputil"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	nodeFile    = "cluster/node.json"
	clusterFile = "cluster/cluster.json"
	secretFile  = "cluster/secret.json"

	// MaxNodes bounds the cluster; every node talks to every other one.
	MaxNodes = 16
	// maxAddresses bounds the addresses of one node, own ones included.
	maxAddresses = 16
)

var (
	ErrNotLoaded    = errors.New("cluster: settings not loaded from /config")
	ErrNoCluster    = errors.New("cluster: this node is in no cluster")
	ErrInCluster    = errors.New("cluster: this node is in a cluster already")
	ErrInvalid      = errors.New("cluster: invalid request")
	ErrJoinInfo     = errors.New("cluster: the join information is not valid")
	ErrJoinKey      = errors.New("cluster: the join key is wrong")
	ErrUnreachable  = errors.New("cluster: the node is not reachable")
	ErrUnknownNode  = errors.New("cluster: no such node")
	ErrTooMany      = errors.New("cluster: too many nodes")
	ErrSelf         = errors.New("cluster: this is the node itself")
	ErrVersion      = errors.New("cluster: the node runs another version")
	ErrDisabled     = errors.New("cluster: not available in development mode")
	ErrUnauthorized = errors.New("cluster: request not signed by the cluster")
	ErrNotPermitted = errors.New("cluster: this node may not manage the cluster")
)

// Node is a member of the cluster.
type Node struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Addresses are host:port pairs in the order to try; Extra are those
	// the administrator added (a DDNS name, an address of a VPN of one's
	// own), Addresses also those the node found itself.
	Addresses []string `json:"addresses"`
	Extra     []string `json:"extra,omitempty"`
	// Cert is the SHA-256 of the cluster certificate of the node, in hex.
	Cert string `json:"cert"`
	// Ways names the way of each address (ways.go); older nodes send none.
	Ways map[string]string `json:"ways,omitempty"`
	// TunnelKey is the public WireGuard key of the node for the tunnel of
	// the cluster (tunnel.go).
	TunnelKey string `json:"tunnelKey,omitempty"`
	// Manage allows a node other than the admin node to manage the others;
	// only the admin node sets it.
	Manage  bool      `json:"manage,omitempty"`
	Joined  time.Time `json:"joined"`
	Updated time.Time `json:"updated"`
}

// AccountVersion says which change of an account is the newest: the later
// time wins, on the same time the higher node ID.
type AccountVersion struct {
	Hash    string `json:"hash"`
	Time    int64  `json:"time"`
	Node    string `json:"node"`
	Deleted bool   `json:"deleted,omitempty"`
}

func (v AccountVersion) newer(than AccountVersion) bool {
	return v.Time > than.Time || v.Time == than.Time && v.Node > than.Node
}

// State is the cluster as this node knows it.
type State struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Generation counts the changes of the secret; a node with an older
	// one was removed or missed its removal.
	Generation int `json:"generation"`
	// Internet allows addresses on the internet for the nodes.
	Internet bool `json:"internet,omitempty"`
	// Admin is the ID of the admin node; see adminLocked for a cluster from
	// before, or when it is gone.
	Admin string `json:"admin,omitempty"`
	// AdminGen counts the handovers of the admin role; the higher one wins.
	AdminGen int    `json:"adminGen,omitempty"`
	Nodes    []Node `json:"nodes"`
	// Ways are the settings for the connections, Routes the exceptions per
	// node (ways.go); Tunnel is the WireGuard tunnel of TitanOS (tunnel.go).
	Ways     Ways                      `json:"ways"`
	Routes   map[string]Route          `json:"routes,omitempty"`
	Tunnel   Tunnel                    `json:"tunnel"`
	Accounts map[string]AccountVersion `json:"accounts"`
	Created  time.Time                 `json:"created"`
}

type secrets struct {
	// Secret signs the requests between the nodes (32 bytes, hex).
	Secret string `json:"secret"`
	// JoinKey lets a new node join (32 bytes, hex). Every node shows it.
	JoinKey string `json:"joinKey"`
}

type self struct {
	ID   string `json:"id"`
	Cert string `json:"cert"` // PEM of certificate and key
	// Extra are the addresses the administrator added for this node.
	Extra []string `json:"extra,omitempty"`
	// TunnelKey is the private WireGuard key for the tunnel of the cluster.
	TunnelKey string `json:"tunnelKey,omitempty"`
}

// Account is what the cluster needs of the accounts of this node.
type Accounts interface {
	List() []auth.User
	Get(name string) (auth.User, bool)
	Import(user auth.User) (auth.User, bool, error)
	Forget(name string) (auth.User, bool, error)
}

// Status is what a node tells the others about itself for the overview.
type Status struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Protocol is the level of the cluster protocol (Protocol); older
	// nodes send none and speak level 1.
	Protocol int `json:"protocol,omitempty"`
	// Time is the clock of the node (Unix seconds), for the comparison.
	Time     int64    `json:"time,omitempty"`
	Hostname string   `json:"hostname"`
	Uptime   int64    `json:"uptimeSeconds"`
	CPU      *float64 `json:"cpuPercent"`
	Memory   struct {
		Total     uint64 `json:"totalBytes"`
		Available uint64 `json:"availableBytes"`
	} `json:"memory"`
	Volumes []VolumeStatus `json:"volumes"`
	// Warnings and Errors count the notifications of the last day.
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

// VolumeStatus is one volume in Status.
type VolumeStatus struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Mounted   bool   `json:"mounted"`
	SizeBytes uint64 `json:"sizeBytes"`
	UsedBytes uint64 `json:"usedBytes"`
}

// Manager is the cluster membership of this node.
type Manager struct {
	store *config.Store

	// Users are the accounts of this node.
	Users Accounts
	// Applied is told about every account taken over from another node,
	// so that sessions end and Samba and sshd follow. deleted is true when
	// the account was removed.
	Applied func(before auth.User, existed bool, after auth.User, deleted bool)
	// Name returns the name of this node (its host name).
	Name func() string
	// Version is the version of titand.
	Version string
	// Port is the HTTPS port of titand, for the addresses of this node.
	Port int
	// Status returns the overview of this node, from memory only.
	Status func(ctx context.Context) Status
	// Local, if set, replaces the addresses of the network interfaces
	// (tests, development mode).
	Local func() []string
	// Insecure talks plain HTTP between nodes: development mode only,
	// where titand serves no HTTPS.
	Insecure bool
	// Loopback lets nodes on the same machine talk (tests; development
	// mode sets it with Insecure).
	Loopback bool
	// Notify reports joins and removals as notifications.
	Notify func(kind, subject string)
	// Backups is the receiving end of backups from other nodes.
	Backups BackupTarget
	// TailscaleName returns the MagicDNS name of this node, "" without
	// Tailscale; it is passed on as an address besides the IPs.
	TailscaleName func() string
	// Update checks for a release and installs it on this node (Update all
	// nodes); it returns the running and the new version, "" for none.
	Update func(ctx context.Context) (from, to string, err error)
	// Runner runs ip and wg for the tunnel; nil leaves the network alone.
	Runner Runner

	mu      sync.Mutex
	loaded  bool
	self    self
	state   *State // nil outside a cluster
	secrets secrets
	// cert is the parsed certificate of this node.
	cert certificate
	// reach is the address each node was last reached at.
	reach map[string]string
	// status caches the overview of the other nodes.
	status map[string]cachedStatus
	nonces *nonces
	// limiter slows down guessing the join key.
	limiter *auth.Limiter
	// pushing serializes the account changes sent to the others,
	// announcing the members: a message is built and sent under it, so
	// that no older one overtakes a newer one.
	pushing    sync.Mutex
	announcing sync.Mutex
	proxy   *httputil.ReverseProxy
	// returnTried is when the preferred way was last tried per node.
	returnTried map[string]time.Time
	// down is since when a node did not answer, alerted whether that was
	// reported.
	down    map[string]time.Time
	alerted map[string]bool
	// run is the last "update all nodes".
	run *UpdateRun
	// tunnelUp is the configuration last applied to the tunnel, tunnelErr
	// why the last try failed.
	tunnelUp  string
	tunnelErr string
}

// NewManager returns the cluster of the node with the settings in store.
func NewManager(store *config.Store) *Manager {
	m := &Manager{
		store: store, reach: map[string]string{}, status: map[string]cachedStatus{},
		returnTried: map[string]time.Time{}, down: map[string]time.Time{}, alerted: map[string]bool{},
		nonces: newNonces(), limiter: auth.NewLimiter(),
	}
	m.proxy = m.newProxy()
	return m
}

// Load reads the settings and creates the identity of the node on its
// first start.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var me self
	switch err := m.store.ReadJSON(nodeFile, &me); {
	case errors.Is(err, fs.ErrNotExist):
		id, err := randomHex(16)
		if err != nil {
			return err
		}
		pem, err := newCertificate()
		if err != nil {
			return err
		}
		me = self{ID: id, Cert: pem}
		if err := m.store.WriteJSON(nodeFile, me); err != nil {
			return err
		}
	case err != nil:
		return err
	}
	cert, err := parseCertificate(me.Cert)
	if err != nil {
		return err
	}
	var state State
	switch err := m.store.ReadJSON(clusterFile, &state); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		var sec secrets
		if err := m.store.ReadJSON(secretFile, &sec); err != nil {
			return err
		}
		if state.Accounts == nil {
			state.Accounts = map[string]AccountVersion{}
		}
		m.state, m.secrets = &state, sec
	}
	m.self, m.cert, m.loaded = me, cert, true
	return nil
}

// ID returns the ID of this node.
func (m *Manager) ID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.self.ID
}

// InCluster reports whether this node is in a cluster.
func (m *Manager) InCluster() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != nil
}

// View is the cluster as the settings page shows it.
type View struct {
	Self     string `json:"self"`
	Name     string `json:"name,omitempty"`
	Internet bool   `json:"internet"`
	Nodes    []Node `json:"nodes"`
	// Admin is the ID of the admin node; Manages whether this node may
	// manage the others.
	Admin   string `json:"admin,omitempty"`
	Manages bool   `json:"manages"`
	// Extra are the addresses added for this node.
	Extra []string `json:"extra"`
	// Found are the addresses this node found itself, FoundWays their ways.
	Found     []string          `json:"found"`
	FoundWays map[string]string `json:"foundWays"`
	// Ways and Routes are the way settings (ways.go), Tunnel the tunnel of
	// TitanOS with the state on this node (tunnel.go).
	Ways   Ways             `json:"ways"`
	Routes map[string]Route `json:"routes"`
	Tunnel TunnelView       `json:"tunnel"`
	// Version and Protocol are those of this node.
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

// View returns the cluster for the settings page.
func (m *Manager) View() View {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, kinds := m.foundLocked()
	view := View{Self: m.self.ID, Nodes: []Node{}, Extra: slices.Clone(m.self.Extra), Found: found, FoundWays: kinds,
		Routes: map[string]Route{}, Version: m.Version, Protocol: Protocol}
	if view.Extra == nil {
		view.Extra = []string{}
	}
	if m.state != nil {
		view.Name, view.Internet = m.state.Name, m.state.Internet
		view.Nodes = slices.Clone(m.state.Nodes)
		view.Admin, view.Manages = m.adminLocked(), m.mayManageLocked(m.self.ID)
		view.Ways = m.state.Ways
		for k, v := range m.state.Routes {
			view.Routes[k] = v
		}
		view.Tunnel = m.tunnelViewLocked()
	}
	return view
}

// JoinInfo lets another node join; it is empty outside a cluster. The
// settings page shows it only after password or passkey, never with View.
func (m *Manager) JoinInfo() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.joinInfoLocked()
}

// node returns a member other than this node.
func (m *Manager) node(id string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return Node{}, ErrNoCluster
	}
	if id == m.self.ID {
		return Node{}, ErrSelf
	}
	for _, n := range m.state.Nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return Node{}, ErrUnknownNode
}

// others returns the members other than this node.
func (m *Manager) others() []Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(m.state.Nodes), func(n Node) bool { return n.ID == m.self.ID })
}

// saveLocked writes state and secrets; m.mu is held.
func (m *Manager) saveLocked(state *State, sec secrets) error {
	if state == nil {
		if err := m.store.Remove(clusterFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := m.store.Remove(secretFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		m.state, m.secrets = nil, secrets{}
		return nil
	}
	// The secret first: a cluster file without its secret could not be read.
	if sec != m.secrets {
		if err := m.store.WriteJSON(secretFile, sec); err != nil {
			return err
		}
	}
	if err := m.store.WriteJSON(clusterFile, state); err != nil {
		return err
	}
	m.state, m.secrets = state, sec
	return nil
}

// cloneState copies the state for a change.
func cloneState(s *State) *State {
	next := *s
	next.Nodes = slices.Clone(s.Nodes)
	next.Routes = make(map[string]Route, len(s.Routes))
	for k, v := range s.Routes {
		next.Routes[k] = v
	}
	next.Tunnel.Addresses = make(map[string]string, len(s.Tunnel.Addresses))
	for k, v := range s.Tunnel.Addresses {
		next.Tunnel.Addresses[k] = v
	}
	next.Accounts = make(map[string]AccountVersion, len(s.Accounts))
	for k, v := range s.Accounts {
		next.Accounts[k] = v
	}
	return &next
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// validName is a name of a node or cluster: printable, one line, not too long.
func validName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 63 {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// Run keeps the addresses of this node current and the accounts in step
// with the other nodes.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	m.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.reconcile(ctx)
		}
	}
}

func (m *Manager) reconcile(ctx context.Context) {
	if !m.InCluster() {
		return
	}
	if err := m.refreshAddresses(ctx); err != nil {
		slog.Warn("cluster: addresses of this node not passed on", "err", err)
	}
	m.syncAccounts(ctx)
	m.applyTunnel(ctx)
	m.returnToPreferred(ctx)
	m.watchNodes(ctx)
}
