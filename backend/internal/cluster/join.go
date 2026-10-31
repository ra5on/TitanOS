package cluster

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// JoinPrefix starts the join information a node shows for others to join.
const JoinPrefix = "titanos-cluster:"

// joinInfo is the content of the join information: where the cluster is
// reached, the certificates to trust and the key to join with. It holds no
// account and no password.
type joinInfo struct {
	V     int        `json:"v"`
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Nodes []joinNode `json:"nodes"`
	Key   string     `json:"key"`
}

type joinNode struct {
	Addresses []string `json:"a"`
	Cert      string   `json:"c"`
}

// joinInfoLocked returns the join information of the cluster. m.mu is held.
func (m *Manager) joinInfoLocked() string {
	if m.state == nil || m.secrets.JoinKey == "" {
		return ""
	}
	// Only nodes that may manage admit new ones; a member shows nothing.
	if !m.mayManageLocked(m.self.ID) {
		return ""
	}
	info := joinInfo{V: 1, ID: m.state.ID, Name: m.state.Name, Key: m.secrets.JoinKey}
	// This node first: it is the one whose page the information came from.
	me := m.selfNodeLocked()
	info.Nodes = append(info.Nodes, joinNode{Addresses: me.Addresses, Cert: me.Cert})
	for _, n := range m.state.Nodes {
		if n.ID != me.ID && m.mayManageLocked(n.ID) {
			info.Nodes = append(info.Nodes, joinNode{Addresses: n.Addresses, Cert: n.Cert})
		}
	}
	data, _ := json.Marshal(info)
	return JoinPrefix + base64.RawURLEncoding.EncodeToString(data)
}

func parseJoinInfo(text string) (joinInfo, error) {
	body, ok := strings.CutPrefix(strings.Join(strings.Fields(text), ""), JoinPrefix)
	if !ok {
		return joinInfo{}, ErrJoinInfo
	}
	data, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return joinInfo{}, ErrJoinInfo
	}
	var info joinInfo
	if json.Unmarshal(data, &info) != nil || info.V != 1 || len(info.Key) != 64 || len(info.Nodes) == 0 || len(info.Nodes) > MaxNodes {
		return joinInfo{}, ErrJoinInfo
	}
	for _, n := range info.Nodes {
		if len(n.Cert) != 64 || len(n.Addresses) == 0 || len(n.Addresses) > maxAddresses {
			return joinInfo{}, ErrJoinInfo
		}
	}
	return info, nil
}

// accountVersion describes the account as this node has it now.
func accountVersion(user auth.User, node string, at time.Time) AccountVersion {
	return AccountVersion{Hash: accountHash(user), Time: at.UnixNano(), Node: node}
}

// Create starts a cluster with this node as its only member.
func (m *Manager) Create(name string) error {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	id, err := randomHex(16)
	if err != nil {
		return err
	}
	secret, err := randomHex(32)
	if err != nil {
		return err
	}
	key, err := randomHex(32)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if m.state != nil {
		return ErrInCluster
	}
	now := time.Now().UTC().Truncate(time.Second)
	me := m.selfNodeLocked()
	me.Joined, me.Updated = now, now
	state := &State{ID: id, Name: name, Nodes: []Node{me}, Accounts: map[string]AccountVersion{}, Created: now, Admin: me.ID}
	for _, user := range m.Users.List() {
		state.Accounts[user.Name] = accountVersion(user, me.ID, now)
	}
	return m.saveLocked(state, secrets{Secret: secret, JoinKey: key})
}

// Preview tells what joining with info would do, before anything changes.
type Preview struct {
	Cluster string   `json:"cluster"`
	Nodes   []string `json:"nodes"`
	// Replaced are accounts of this node the cluster has under the same
	// name: they become those of the cluster (password, two-factor login).
	Replaced []string `json:"replaced"`
	// Local are accounts only this node has: they stay here and do not
	// go to the cluster.
	Local []string `json:"local"`
	// Taken are accounts only the cluster has: they come to this node.
	Taken []string `json:"taken"`
}

type previewRequest struct {
	Key string `json:"key"`
}

type previewResponse struct {
	Name     string   `json:"name"`
	Nodes    []string `json:"nodes"`
	Accounts []string `json:"accounts"`
	Version  string   `json:"version"`
}

// candidateNodes turns the nodes of join information into nodes to call.
func (info joinInfo) candidateNodes() []Node {
	var out []Node
	for i, n := range info.Nodes {
		out = append(out, Node{ID: fmt.Sprintf("join-%d", i), Name: "?", Addresses: n.Addresses, Cert: n.Cert})
	}
	return out
}

// askCluster sends an unsigned request of the join to the first node of
// info that answers.
func (m *Manager) askCluster(ctx context.Context, info joinInfo, path string, in, out any) error {
	lastErr := ErrUnreachable
	for _, n := range info.candidateNodes() {
		err := m.try(ctx, n, func(ctx context.Context, address string) (*http.Response, error) {
			return m.postJSON(ctx, n, address, path, in)
		}, out)
		if err == nil {
			return nil
		}
		if IsPeerError(err, "cluster_join_key") || IsPeerError(err, "rate_limited") || IsPeerError(err, "cluster_full") {
			return err
		}
		lastErr = err
	}
	return lastErr
}

// Preview asks the cluster in info what joining would change here.
func (m *Manager) Preview(ctx context.Context, text string) (Preview, error) {
	info, err := parseJoinInfo(text)
	if err != nil {
		return Preview{}, err
	}
	if m.InCluster() {
		return Preview{}, ErrInCluster
	}
	var res previewResponse
	if err := m.askCluster(ctx, info, "/api/v1/cluster/peer/preview", previewRequest{Key: info.Key}, &res); err != nil {
		return Preview{}, err
	}
	if res.Version != m.Version {
		return Preview{}, fmt.Errorf("%w: %s", ErrVersion, res.Version)
	}
	p := Preview{Cluster: res.Name, Nodes: res.Nodes, Replaced: []string{}, Local: []string{}, Taken: []string{}}
	local := map[string]bool{}
	for _, u := range m.Users.List() {
		local[u.Name] = true
		if slices.Contains(res.Accounts, u.Name) {
			p.Replaced = append(p.Replaced, u.Name)
		} else {
			p.Local = append(p.Local, u.Name)
		}
	}
	for _, name := range res.Accounts {
		if !local[name] {
			p.Taken = append(p.Taken, name)
		}
	}
	return p, nil
}

type joinRequest struct {
	Key     string `json:"key"`
	Node    Node   `json:"node"`
	Version string `json:"version"`
}

type joinResponse struct {
	State    State       `json:"state"`
	Secret   string      `json:"secret"`
	JoinKey  string      `json:"joinKey"`
	Accounts []auth.User `json:"accounts"`
}

// Join makes this node a member of the cluster in info. The accounts of
// the admin node replace those of the same name here; accounts only this
// node has stay here and do not go to the cluster.
func (m *Manager) Join(ctx context.Context, text string) error {
	info, err := parseJoinInfo(text)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return ErrNotLoaded
	}
	if m.state != nil {
		m.mu.Unlock()
		return ErrInCluster
	}
	me := m.selfNodeLocked()
	m.mu.Unlock()
	var res joinResponse
	if err := m.askCluster(ctx, info, "/api/v1/cluster/peer/join", joinRequest{Key: info.Key, Node: me, Version: m.Version}, &res); err != nil {
		return err
	}
	if res.State.ID != info.ID || len(res.Secret) != 64 || !slices.ContainsFunc(res.State.Nodes, func(n Node) bool { return n.ID == me.ID }) {
		return fmt.Errorf("%w: answer of the cluster", ErrInvalid)
	}
	// The accounts of the cluster first, then this node is a member.
	for _, user := range res.Accounts {
		m.importAccount(user)
	}
	state := res.State
	if state.Accounts == nil {
		state.Accounts = map[string]AccountVersion{}
	}
	m.mu.Lock()
	err = m.saveLocked(&state, secrets{Secret: res.Secret, JoinKey: res.JoinKey})
	m.mu.Unlock()
	if err != nil {
		return err
	}
	slog.Info("cluster joined", "cluster", state.Name, "nodes", len(state.Nodes))
	return nil
}

// importAccount takes over an account of the cluster and lets Applied
// follow up.
func (m *Manager) importAccount(user auth.User) {
	if current, ok := m.Users.Get(user.Name); ok && accountHash(current) == accountHash(user) {
		return
	}
	before, existed, err := m.Users.Import(user)
	if err != nil {
		slog.Error("cluster: account not taken over", "user", user.Name, "err", err)
		return
	}
	if m.Applied != nil {
		m.Applied(before, existed, user, false)
	}
}

// handlePreview answers a node that wants to join, if its key is right.
func (m *Manager) checkJoinKey(remote, key string) error {
	limit := "cluster-join:" + remoteHost(remote)
	if m.limiter.RetryAfter(limit) > 0 {
		return errRateLimited
	}
	m.mu.Lock()
	want := m.secrets.JoinKey
	inCluster := m.state != nil
	m.mu.Unlock()
	if !inCluster || want == "" || subtle.ConstantTimeCompare([]byte(key), []byte(want)) != 1 {
		m.limiter.Fail(limit)
		return ErrJoinKey
	}
	return nil
}

// admit adds a node that joins with the right key and returns what it
// needs: the cluster, its secrets and the accounts.
func (m *Manager) admit(ctx context.Context, remote string, req joinRequest) (joinResponse, error) {
	if err := m.checkJoinKey(remote, req.Key); err != nil {
		return joinResponse{}, err
	}
	if err := m.managing(); err != nil {
		return joinResponse{}, err
	}
	n := req.Node
	// A new node is a member; only the admin node allows it more.
	n.Manage = false
	if len(n.ID) != 32 || !validName(n.Name) || len(n.Cert) != 64 || len(n.Addresses) > maxAddresses {
		return joinResponse{}, fmt.Errorf("%w: node", ErrInvalid)
	}
	if req.Version != m.Version {
		return joinResponse{}, fmt.Errorf("%w: %s", ErrVersion, req.Version)
	}
	m.mu.Lock()
	state := m.state
	if state == nil {
		m.mu.Unlock()
		return joinResponse{}, ErrNoCluster
	}
	if slices.ContainsFunc(state.Nodes, func(x Node) bool { return x.ID == n.ID }) {
		m.mu.Unlock()
		return joinResponse{}, fmt.Errorf("%w: node is a member already", ErrInvalid)
	}
	if len(state.Nodes) >= MaxNodes {
		m.mu.Unlock()
		return joinResponse{}, ErrTooMany
	}
	now := time.Now().UTC().Truncate(time.Second)
	n.Joined, n.Updated = now, now
	next := cloneState(state)
	next.Admin = adminOf(state)
	next.Nodes = append(next.Nodes, n)
	sec := m.secrets
	if err := m.saveLocked(next, sec); err != nil {
		m.mu.Unlock()
		return joinResponse{}, err
	}
	m.mu.Unlock()
	slog.Info("cluster: node joined", "node", n.Name, "ip", remoteHost(remote))
	if m.Notify != nil {
		m.Notify("cluster_node_joined", n.Name)
	}
	go m.announce(context.WithoutCancel(ctx), "")
	return joinResponse{State: *next, Secret: sec.Secret, JoinKey: sec.JoinKey, Accounts: m.clusterAccounts(next)}, nil
}

// membership is what all nodes get when nodes come or go.
type membership struct {
	Nodes      []Node `json:"nodes"`
	JoinKey    string `json:"joinKey"`
	Generation int    `json:"generation"`
	// Secret is the new secret, set when Generation went up.
	Secret string `json:"secret,omitempty"`
	Name   string `json:"name"`
	// Internet is whether the cluster allows nodes on the internet.
	Internet bool `json:"internet"`
	// Admin is the admin node; with Node.Manage only it may change it.
	Admin string `json:"admin,omitempty"`
}

// announce sends the members, the join key and, after a removal, the new
// secret to every other node. skip is a node not to tell.
func (m *Manager) announce(ctx context.Context, skip string) {
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return
	}
	msg := membership{Nodes: slices.Clone(m.state.Nodes), JoinKey: m.secrets.JoinKey, Generation: m.state.Generation, Name: m.state.Name, Internet: m.state.Internet, Admin: m.adminLocked()}
	m.mu.Unlock()
	for _, n := range m.others() {
		if n.ID == skip {
			continue
		}
		if err := m.call(ctx, n, "POST", "/api/v1/cluster/peer/members", "", msg, nil); err != nil {
			slog.Warn("cluster: node not told of the members", "node", n.Name, "err", err)
		}
	}
}

// takeMembership takes over the members node from sent. Only a node that
// may manage changes the members, and only the admin node who else may.
func (m *Manager) takeMembership(from string, msg membership) error {
	if len(msg.Nodes) == 0 || len(msg.Nodes) > MaxNodes || len(msg.JoinKey) != 64 || !validName(msg.Name) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return ErrNoCluster
	}
	if !m.mayManageLocked(from) {
		return ErrNotPermitted
	}
	admin := m.adminLocked()
	if from != admin {
		// Who may manage stays as the admin node set it.
		manage := map[string]bool{}
		for _, n := range m.state.Nodes {
			manage[n.ID] = n.Manage
		}
		msg.Nodes = slices.Clone(msg.Nodes)
		for i := range msg.Nodes {
			msg.Nodes[i].Manage = manage[msg.Nodes[i].ID]
		}
		msg.Admin = m.state.Admin
	}
	sec := m.secrets
	next := cloneState(m.state)
	if !slices.ContainsFunc(msg.Nodes, func(n Node) bool { return n.ID == m.self.ID }) {
		// This node was removed: it leaves, keeping its accounts.
		slog.Warn("cluster: this node was removed from the cluster")
		return m.saveLocked(nil, secrets{})
	}
	if msg.Generation > next.Generation {
		if len(msg.Secret) != 64 {
			return ErrInvalid
		}
		next.Generation, sec.Secret = msg.Generation, msg.Secret
	}
	// The own entry stays as this node knows it.
	me := slices.IndexFunc(next.Nodes, func(n Node) bool { return n.ID == m.self.ID })
	nodes := slices.Clone(msg.Nodes)
	if me >= 0 {
		if i := slices.IndexFunc(nodes, func(n Node) bool { return n.ID == m.self.ID }); i >= 0 {
			// Except whether it may manage: that is the admin node's call.
			manage := nodes[i].Manage
			nodes[i] = next.Nodes[me]
			nodes[i].Manage = manage
		}
	}
	next.Nodes, next.Name, next.Internet, next.Admin = nodes, msg.Name, msg.Internet, msg.Admin
	sec.JoinKey = msg.JoinKey
	return m.saveLocked(next, sec)
}

// NewJoinKey replaces the join key: the old join information stops
// working, the members stay.
func (m *Manager) NewJoinKey(ctx context.Context) error {
	if err := m.managing(); err != nil {
		return err
	}
	key, err := randomHex(32)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	sec := m.secrets
	sec.JoinKey = key
	if err := m.saveLocked(m.state, sec); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.announce(ctx, "")
	return nil
}

// SetInternet allows or forbids addresses on the internet for the nodes.
func (m *Manager) SetInternet(ctx context.Context, on bool) error {
	if err := m.managing(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	next := cloneState(m.state)
	next.Internet = on
	if err := m.saveLocked(next, m.secrets); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.announce(ctx, "")
	return m.refreshAddresses(ctx)
}

// Rename gives the cluster a new name.
func (m *Manager) Rename(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	if err := m.managing(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	next := cloneState(m.state)
	next.Name = name
	if err := m.saveLocked(next, m.secrets); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.announce(ctx, "")
	return nil
}

// Remove takes a node out of the cluster. The secret and the join key
// change, so that the removed node can neither talk to the others nor join
// again with what it knew. A node that is away during this has to join
// again.
func (m *Manager) Remove(ctx context.Context, id string) error {
	if m.isAdmin(id) {
		// The admin node leaves itself; nobody else takes it out.
		return ErrNotPermitted
	}
	return m.remove(ctx, id)
}

// remove takes node id out of the cluster; see Remove. A node that leaves
// asks for it itself, the admin node as well.
func (m *Manager) remove(ctx context.Context, id string) error {
	if err := m.managing(); err != nil {
		return err
	}
	removed, err := m.node(id)
	if errors.Is(err, ErrSelf) {
		removed, err = m.selfNode(), nil
	}
	if err != nil {
		return err
	}
	secret, err := randomHex(32)
	if err != nil {
		return err
	}
	key, err := randomHex(32)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	next := cloneState(m.state)
	next.Nodes = slices.DeleteFunc(next.Nodes, func(n Node) bool { return n.ID == id })
	next.Generation++
	// When the admin node left, the node that joined first after it.
	next.Admin = adminOf(next)
	msg := membership{Nodes: next.Nodes, JoinKey: key, Generation: next.Generation, Secret: secret, Name: next.Name, Internet: next.Internet, Admin: next.Admin}
	m.mu.Unlock()
	// The others first, signed with the secret they still have; then this
	// node switches. The removed node hears it with the old secret too.
	for _, n := range m.others() {
		if err := m.call(ctx, n, "POST", "/api/v1/cluster/peer/members", "", msg, nil); err != nil && n.ID != id {
			slog.Warn("cluster: node not told of the removal; it has to join again", "node", n.Name, "err", err)
		}
	}
	m.mu.Lock()
	if id == m.self.ID {
		// A node that may manage left on its own: it keeps its accounts.
		err = m.saveLocked(nil, secrets{})
	} else {
		err = m.saveLocked(next, secrets{Secret: secret, JoinKey: key})
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	slog.Info("cluster: node removed", "node", removed.Name)
	if m.Notify != nil {
		m.Notify("cluster_node_removed", removed.Name)
	}
	return nil
}

// Leave takes this node out of the cluster. Another member does the
// removal, so that the secret changes for the rest; if none answers, this
// node leaves on its own. The accounts stay here as they are.
func (m *Manager) Leave(ctx context.Context) error {
	if !m.InCluster() {
		return ErrNoCluster
	}
	// A node that may manage tells the others itself, so that the admin
	// node leaves even when no other node may manage.
	if m.Manages() {
		return m.remove(ctx, m.ID())
	}
	for _, n := range m.managersFirst(m.others()) {
		if err := m.call(ctx, n, "POST", "/api/v1/cluster/peer/leave", "", struct{}{}, nil); err == nil {
			break
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return nil // the removal arrived already
	}
	return m.saveLocked(nil, secrets{})
}
