package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

// The cluster (package cluster): requests of other nodes under
// cluster.PeerPrefix, signed requests on behalf of an account, and the
// routes of the settings page.

// clusterArea is the cluster: the requests of other nodes, the guards that
// keep a route on this node or with the owner, and the settings page; an
// area with narrow dependencies (PLAN 8a, see fansArea). cluster is nil
// where there is none. version, monitor and storage describe this node,
// notify, shares and ssh take up what the cluster changes. sessions,
// tokens and prefs are state of the server: what an account that the
// cluster changed or removed loses. requireUser is the check of a login,
// sessionToken names the web session of a request, reconfirmProof asks for
// the password or passkey again, forgetWebDAV clears a WebDAV password.
type clusterArea struct {
	cluster        *cluster.Manager
	version        string
	monitor        *system.Monitor
	storage        *storage.Manager
	notify         *notify.Manager
	shares         *shares.Manager
	ssh            *sshd.Manager
	sessions       sessionsFunc
	tokens         *auth.Tokens
	prefs          *preferences
	requireUser    func(http.HandlerFunc) http.HandlerFunc
	sessionToken   func(r *http.Request) string
	reconfirmProof func(w http.ResponseWriter, user auth.User, p proof) bool
	forgetWebDAV   func(name string)
}

// clusterArea is the area of the server; New wires its callbacks into the
// cluster manager, the handler chain starts with clusterAccess.
func (s *Server) clusterArea() clusterArea {
	return clusterArea{cluster: s.opts.Cluster, version: s.opts.Version, monitor: s.opts.Monitor, storage: s.opts.Storage,
		notify: s.opts.Notify, shares: s.opts.Shares, ssh: s.opts.SSH, sessions: s.sessionStore, tokens: s.tokens, prefs: s.prefs,
		requireUser: s.requireUser, sessionToken: s.authArea().sessionToken, reconfirmProof: s.authArea().reconfirmProof, forgetWebDAV: s.davArea().forgetWebDAV}
}

// routeCluster registers the routes of the cluster.
func (s *Server) routeCluster(mux *routeMux) {
	s.clusterArea().register(mux, s.requireAdmin, s.requireUser)
}

// clusterAccess serves the requests of other nodes and checks those they
// pass on for an account. A signed request carries neither cookie nor
// API token: the account is the one the other node signed for.
func (a clusterArea) clusterAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := a.cluster
		if strings.HasPrefix(r.URL.Path, cluster.PeerPrefix) {
			if c == nil {
				handleAPINotFound(w, r)
				return
			}
			c.PeerHandler().ServeHTTP(w, r)
			return
		}
		if r.Header.Get(cluster.Header) == "" {
			next.ServeHTTP(w, r)
			return
		}
		if c == nil {
			writeError(w, http.StatusUnauthorized, "cluster_unauthorized", "request not signed by the cluster")
			return
		}
		id, err := c.Verify(r)
		if err != nil || id.User == "" {
			writeError(w, http.StatusUnauthorized, "cluster_unauthorized", "request not signed by the cluster")
			return
		}
		// Only the admin node and the nodes it allowed manage this one.
		if !c.MayManage(id.Node) {
			writeError(w, http.StatusForbidden, "cluster_not_permitted", "the node may not manage this one")
			return
		}
		// Only for an account of the cluster: one this node keeps for
		// itself is not the account of the same name on the other node.
		if !c.ClusterAccount(id.User) {
			writeError(w, http.StatusForbidden, "cluster_not_permitted", "the account is not one of the cluster")
			return
		}
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		next.ServeHTTP(w, r.WithContext(cluster.WithIdentity(r.Context(), id)))
	})
}

// viaCluster reports whether a request came from another node.
func viaCluster(r *http.Request) bool {
	_, ok := cluster.FromContext(r.Context())
	return ok
}

// clusterLocal refuses a change from another node: confirming a network
// or firewall change proves that the browser still reaches the NAS, which
// a request through another node does not show.
func (a clusterArea) clusterLocal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if viaCluster(r) {
			writeError(w, http.StatusForbidden, "cluster_local_only", "change this on the web interface of the node itself")
			return
		}
		next(w, r)
	}
}

// requireOwnSession lets only a browser session of this node through: the
// cluster itself is managed from the node, and only a person may switch
// to another node (no API token, MCP or other node).
func (a clusterArea) requireOwnSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if viaCluster(r) || !webSession(r) || a.sessionToken(r) == "" {
			writeError(w, http.StatusForbidden, "web_session_required", "only possible on the web interface")
			return
		}
		next(w, r)
	}
}

func (a clusterArea) requireOwner(next http.HandlerFunc) http.HandlerFunc {
	return a.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != auth.RoleOwner {
			writeError(w, http.StatusForbidden, "forbidden", "this needs an owner")
			return
		}
		next(w, r)
	})
}

func (a clusterArea) register(mux *routeMux, admin, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/cluster", admin(a.requireOwnSession(a.handleCluster)))
	mux.HandleFunc("GET /api/v1/cluster/nodes", user(a.handleClusterNodes))
	mux.HandleFunc("POST /api/v1/cluster", a.requireOwner(a.requireOwnSession(a.handleClusterCreate)))
	mux.HandleFunc("PUT /api/v1/cluster", admin(a.requireOwnSession(a.handleClusterSettings)))
	mux.HandleFunc("POST /api/v1/cluster/preview", a.requireOwner(a.requireOwnSession(a.handleClusterPreview)))
	mux.HandleFunc("POST /api/v1/cluster/join", a.requireOwner(a.requireOwnSession(a.handleClusterJoin)))
	mux.HandleFunc("POST /api/v1/cluster/leave", a.requireOwner(a.requireOwnSession(a.handleClusterLeave)))
	mux.HandleFunc("POST /api/v1/cluster/join-info", admin(a.requireOwnSession(a.handleClusterJoinInfo)))
	mux.HandleFunc("POST /api/v1/cluster/join-key", admin(a.requireOwnSession(a.handleClusterJoinKey)))
	mux.HandleFunc("DELETE /api/v1/cluster/nodes/{id}", a.requireOwner(a.requireOwnSession(a.handleClusterRemove)))
	mux.HandleFunc("PUT /api/v1/cluster/nodes/{id}/manage", a.requireOwner(a.requireOwnSession(a.handleClusterManage)))
	mux.HandleFunc("POST /api/v1/cluster/nodes/{id}/backup-pair", admin(a.requireOwnSession(a.handleClusterBackupPair)))
	mux.HandleFunc("PUT /api/v1/cluster/nodes/{id}/route", admin(a.requireOwnSession(a.handleClusterRoute)))
	mux.HandleFunc("POST /api/v1/cluster/nodes/{id}/admin", a.requireOwner(a.requireOwnSession(a.handleClusterHandOver)))
	mux.HandleFunc("POST /api/v1/cluster/accounts/{name}/share", a.requireOwner(a.requireOwnSession(a.handleClusterShareAccount)))
	mux.HandleFunc("POST /api/v1/cluster/probe", admin(a.requireOwnSession(a.handleClusterProbe)))
	mux.HandleFunc("GET /api/v1/cluster/update", admin(a.requireOwnSession(a.handleClusterUpdateRun)))
	mux.HandleFunc("POST /api/v1/cluster/update", admin(a.requireOwnSession(a.handleClusterUpdateAll)))
	mux.HandleFunc("/api/v1/cluster/nodes/{id}/api/{path...}", user(a.requireOwnSession(a.handleClusterProxy)))
}

func writeClusterError(w http.ResponseWriter, err error) {
	if errors.Is(err, cluster.ErrBackupsOff) {
		writeError(w, http.StatusConflict, "cluster_backups_off", "the node takes no backups of other NAS")
		return
	}
	status, code := cluster.ErrorCode(err)
	if status == http.StatusInternalServerError {
		slog.Error("cluster", "err", err)
	}
	writeError(w, status, code, err.Error())
}

func (a clusterArea) clusterAvailable(w http.ResponseWriter) bool {
	if a.cluster == nil {
		writeError(w, http.StatusConflict, "dev_mode", "the cluster is not available here")
		return false
	}
	return true
}

func (a clusterArea) handleCluster(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	view := a.cluster.View()
	writeJSON(w, http.StatusOK, map[string]any{"cluster": view, "nodes": a.cluster.Overview(r.Context())})
}

// handleClusterNodes lists the nodes for switching between them; every
// account may see them, the other node decides what it may do there. A
// member manages nobody: manages is false and the switcher stays away.
//
// Like the other cluster routes it is for the web interface of this node
// only (docs/rules/CLUSTER.md): a token, MCP or another node gets the
// answer of a NAS outside a cluster, which the switcher takes as "nothing
// to switch to", not an error.
func (a clusterArea) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	if viaCluster(r) || !webSession(r) || a.sessionToken(r) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}, "version": a.version, "manages": false})
		return
	}
	type node struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Self    bool   `json:"self"`
		Online  bool   `json:"online"`
		Version string `json:"version,omitempty"`
		// Compatible: managed fully; else only update, logs and power.
		Compatible bool `json:"compatible"`
	}
	out := []node{}
	manages := false
	if c := a.cluster; c != nil {
		manages = c.Manages()
		for _, v := range c.Overview(r.Context()) {
			n := node{ID: v.ID, Name: v.Name, Self: v.Self, Online: v.Online, Compatible: v.Compatible}
			if v.Status != nil {
				n.Version = v.Status.Version
			}
			out = append(out, n)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out, "version": a.version, "manages": manages})
}

func (a clusterArea) handleClusterCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	if err := a.cluster.Create(req.Name); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster created", "name", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

func (a clusterArea) handleClusterSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     *string       `json:"name"`
		Internet *bool         `json:"internet"`
		Extra    *[]string     `json:"extra"`
		Ways     *cluster.Ways `json:"ways"`
		Tunnel   *bool         `json:"tunnel"`
	}
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	c := a.cluster
	// A member sets its own addresses; name and internet only change from
	// a node that may manage, so unchanged values are left alone.
	view := c.View()
	if req.Name != nil && strings.TrimSpace(*req.Name) == view.Name {
		req.Name = nil
	}
	if req.Internet != nil && *req.Internet == view.Internet {
		req.Internet = nil
	}
	if req.Ways != nil && *req.Ways == view.Ways {
		req.Ways = nil
	}
	if req.Tunnel != nil && *req.Tunnel == view.Tunnel.Enabled {
		req.Tunnel = nil
	}
	if req.Extra != nil {
		if err := c.SetExtra(r.Context(), *req.Extra); err != nil {
			writeClusterError(w, err)
			return
		}
	}
	if req.Name != nil {
		if err := c.Rename(r.Context(), *req.Name); err != nil {
			writeClusterError(w, err)
			return
		}
	}
	if req.Internet != nil {
		if err := c.SetInternet(r.Context(), *req.Internet); err != nil {
			writeClusterError(w, err)
			return
		}
	}
	if req.Ways != nil {
		if err := c.SetWays(r.Context(), *req.Ways); err != nil {
			writeClusterError(w, err)
			return
		}
	}
	if req.Tunnel != nil {
		if err := c.SetTunnel(r.Context(), *req.Tunnel); err != nil {
			writeClusterError(w, err)
			return
		}
	}
	slog.Info("cluster settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

// handleClusterRoute sets the way to one node, an exception to the
// settings of the cluster; all fields empty remove it.
func (a clusterArea) handleClusterRoute(w http.ResponseWriter, r *http.Request) {
	var req cluster.Route
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	if err := a.cluster.SetRoute(r.Context(), r.PathValue("id"), req); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster way to node changed", "node", r.PathValue("id"), "way", req.Manage, "only", req.Only, "backup", req.Backup, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

// handleClusterHandOver gives the admin role to a node, or takes it while
// the admin node does not answer.
func (a clusterArea) handleClusterHandOver(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := a.cluster.HandOver(ctx, r.PathValue("id")); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster admin role handed over", "node", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

// handleClusterShareAccount makes an account that stayed local on the
// admin node an account of the cluster.
func (a clusterArea) handleClusterShareAccount(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := a.cluster.ShareAccount(ctx, r.PathValue("name")); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("local account made a cluster account", "account", r.PathValue("name"), "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

// handleClusterProbe asks every node on each of its ways.
func (a clusterArea) handleClusterProbe(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{"nodes": a.cluster.Probes(ctx), "checked": time.Now().UTC()})
}

func (a clusterArea) handleClusterUpdateRun(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": a.cluster.UpdateRunState()})
}

// handleClusterUpdateAll updates the chosen nodes, one after the other or
// all at once; this node last.
func (a clusterArea) handleClusterUpdateAll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nodes    []string `json:"nodes"`
		Parallel bool     `json:"parallel"`
	}
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r).Name
	if err := a.cluster.UpdateAll(r.Context(), user, req.Nodes, req.Parallel); err != nil {
		if errors.Is(err, cluster.ErrRunning) {
			writeError(w, http.StatusConflict, "cluster_update_running", err.Error())
			return
		}
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster update of nodes started", "nodes", len(req.Nodes), "parallel", req.Parallel, "by", user, "ip", remoteIP(r))
	a.handleClusterUpdateRun(w, r)
}

func (a clusterArea) handleClusterPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Info string `json:"info"`
	}
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p, err := a.cluster.Preview(ctx, req.Info)
	if err != nil {
		writeClusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a clusterArea) handleClusterJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Info string `json:"info"`
	}
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := a.cluster.Join(ctx, req.Info); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster joined", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

func (a clusterArea) handleClusterLeave(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	if err := a.cluster.Leave(r.Context()); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster left", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

// handleClusterJoinInfo hands out the join information after password (and
// code) or a passkey: whoever has it may add a node that sees everything.
func (a clusterArea) handleClusterJoinInfo(w http.ResponseWriter, r *http.Request) {
	var req proof
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !a.reconfirmProof(w, user, req) {
		return
	}
	info := a.cluster.JoinInfo()
	if info == "" && a.cluster.InCluster() {
		writeError(w, http.StatusForbidden, "cluster_not_permitted", "only a node that may manage admits new ones")
		return
	}
	if info == "" {
		writeError(w, http.StatusConflict, "cluster_none", "this node is in no cluster")
		return
	}
	slog.Info("cluster join information shown", "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"joinInfo": info})
}

func (a clusterArea) handleClusterJoinKey(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	if err := a.cluster.NewJoinKey(r.Context()); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster join key replaced", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

func (a clusterArea) handleClusterRemove(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	if err := a.cluster.Remove(r.Context(), r.PathValue("id")); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster node removed", "node", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

// handleClusterManage allows a node to manage the others, or takes it
// back; only on the admin node.
func (a clusterArea) handleClusterManage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Manage bool `json:"manage"`
	}
	if !a.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	if err := a.cluster.SetManage(r.Context(), r.PathValue("id"), req.Manage); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster node may manage", "node", r.PathValue("id"), "manage", req.Manage, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleCluster(w, r)
}

func (a clusterArea) handleClusterBackupPair(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	code, err := a.cluster.PairBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster backup paired", "node", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

func (a clusterArea) handleClusterProxy(w http.ResponseWriter, r *http.Request) {
	if !a.clusterAvailable(w) {
		return
	}
	// A console of the other node is a WebSocket. Browsers send those
	// across origins with the cookie of this site (a page of an app on a
	// neighbouring name is the same site), the protection against
	// cross-site requests lets GET pass, and the proxy removes Origin, so
	// the other node cannot tell which page asks. Check it here, as
	// websocket.Accept does for the consoles of this node.
	if r.Header.Get("Upgrade") != "" && !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross_origin", "this page may not open a connection to another node")
		return
	}
	a.cluster.Proxy(w, r, r.PathValue("id"), currentUser(r).Name, r.PathValue("path"))
}

// sameOrigin reports whether the page that sent the request is this site:
// its Origin names the host the request went to. A request without Origin
// does not count; browsers always send it with a WebSocket.
func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && origin.Host != "" && strings.EqualFold(origin.Host, r.Host)
}

// clusterApplied follows up an account another node changed, as the
// handlers of users.go do for a change made here.
func (a clusterArea) clusterApplied(before auth.User, existed bool, after auth.User, deleted bool) {
	name := after.Name
	if deleted {
		name = before.Name
	}
	switch {
	case deleted:
		a.sessions().DeleteUser(name, "")
		if err := a.tokens.RemoveUser(name); err != nil {
			slog.Error("remove API tokens of the deleted account", "user", name, "err", err)
		}
		a.prefs.forget(name)
		a.notify.ForgetPersonal(name)
		a.notify.ForgetBell(name)
		if err := a.shares.ForgetUser(name); err != nil {
			slog.Error("remove account from shares", "user", name, "err", err)
		}
	case existed && (before.PasswordHash != after.PasswordHash || before.TOTPSecret != after.TOTPSecret ||
		after.Disabled || len(after.Passkeys) < len(before.Passkeys) || !before.Created.Equal(after.Created)):
		// Whoever knew the old password or held a removed factor is out.
		a.sessions().DeleteUser(name, "")
		if before.PasswordHash != after.PasswordHash || before.TOTPSecret != after.TOTPSecret {
			a.forgetWebDAV(name)
		}
	}
	slog.Info("account taken over from the cluster", "user", name, "deleted", deleted)
	a.shares.Kick()
	a.ssh.Kick()
}

// ClusterStatus is what this node tells the others for the overview, from
// memory and the storage status of the last seconds.
func (a clusterArea) ClusterStatus(ctx context.Context) cluster.Status {
	info := a.monitor.Info()
	st := cluster.Status{Name: info.Hostname, Hostname: info.Hostname, Version: a.version, Uptime: info.UptimeSeconds, Volumes: []cluster.VolumeStatus{}}
	cpu := info.CPU.UsagePercent
	st.CPU = &cpu
	st.Memory.Total, st.Memory.Available = info.Memory.TotalBytes, info.Memory.AvailableBytes
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if storage, err := a.storage.Status(ctx); err == nil {
		for _, v := range storage.Volumes {
			st.Volumes = append(st.Volumes, cluster.VolumeStatus{Name: v.Name, State: v.State, Mounted: v.Mounted, SizeBytes: v.SizeBytes, UsedBytes: v.UsedBytes})
		}
	}
	since := time.Now().Add(-24 * time.Hour)
	for _, m := range a.notify.Messages() {
		// Messages meant for some accounts only (a camera's recipients,
		// PLAN 82-3) are theirs, not the node's: the list of another
		// administrator would not show them.
		if m.Time.Before(since) || len(m.To) > 0 {
			continue
		}
		switch m.Severity {
		case notify.Warning:
			st.Warnings++
		case notify.Error:
			st.Errors++
		}
	}
	return st
}

// clusterNotify reports joins, removals and nodes that stopped answering.
func (a clusterArea) clusterNotify(kind, subject string) {
	severity := notify.Info
	switch kind {
	case "cluster_node_removed", "cluster_node_unreachable":
		severity = notify.Warning
	}
	a.notify.Notify(severity, kind, subject)
}
