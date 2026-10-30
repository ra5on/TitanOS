package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// The cluster (package cluster): requests of other nodes under
// cluster.PeerPrefix, signed requests on behalf of an account, and the
// routes of the settings page.

// clusterAccess serves the requests of other nodes and checks those they
// pass on for an account. A signed request carries neither cookie nor
// API token: the account is the one the other node signed for.
func (s *Server) clusterAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := s.opts.Cluster
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
func (s *Server) clusterLocal(next http.HandlerFunc) http.HandlerFunc {
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
// to another node (no API token, assistant or other node).
func (s *Server) requireOwnSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if viaCluster(r) || !webSession(r) || s.sessionToken(r) == "" {
			writeError(w, http.StatusForbidden, "web_session_required", "only possible on the web interface")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireOwner(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != auth.RoleOwner {
			writeError(w, http.StatusForbidden, "forbidden", "this needs an owner")
			return
		}
		next(w, r)
	})
}

func (s *Server) routeCluster(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/cluster", s.requireAdmin(s.handleCluster))
	mux.HandleFunc("GET /api/v1/cluster/nodes", s.requireUser(s.handleClusterNodes))
	mux.HandleFunc("POST /api/v1/cluster", s.requireOwner(s.requireOwnSession(s.handleClusterCreate)))
	mux.HandleFunc("PUT /api/v1/cluster", s.requireAdmin(s.requireOwnSession(s.handleClusterSettings)))
	mux.HandleFunc("POST /api/v1/cluster/preview", s.requireOwner(s.requireOwnSession(s.handleClusterPreview)))
	mux.HandleFunc("POST /api/v1/cluster/join", s.requireOwner(s.requireOwnSession(s.handleClusterJoin)))
	mux.HandleFunc("POST /api/v1/cluster/leave", s.requireOwner(s.requireOwnSession(s.handleClusterLeave)))
	mux.HandleFunc("POST /api/v1/cluster/join-info", s.requireAdmin(s.requireOwnSession(s.handleClusterJoinInfo)))
	mux.HandleFunc("POST /api/v1/cluster/join-key", s.requireAdmin(s.requireOwnSession(s.handleClusterJoinKey)))
	mux.HandleFunc("DELETE /api/v1/cluster/nodes/{id}", s.requireOwner(s.requireOwnSession(s.handleClusterRemove)))
	mux.HandleFunc("POST /api/v1/cluster/nodes/{id}/backup-pair", s.requireAdmin(s.requireOwnSession(s.handleClusterBackupPair)))
	mux.HandleFunc("/api/v1/cluster/nodes/{id}/api/{path...}", s.requireUser(s.requireOwnSession(s.handleClusterProxy)))
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

func (s *Server) clusterAvailable(w http.ResponseWriter) bool {
	if s.opts.Cluster == nil {
		writeError(w, http.StatusConflict, "dev_mode", "the cluster is not available here")
		return false
	}
	return true
}

func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	if !s.clusterAvailable(w) {
		return
	}
	view := s.opts.Cluster.View()
	writeJSON(w, http.StatusOK, map[string]any{"cluster": view, "nodes": s.opts.Cluster.Overview(r.Context())})
}

// handleClusterNodes lists the nodes for switching between them; every
// account may see them, the other node decides what it may do there.
func (s *Server) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	type node struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Self    bool   `json:"self"`
		Online  bool   `json:"online"`
		Version string `json:"version,omitempty"`
	}
	out := []node{}
	if s.opts.Cluster != nil {
		for _, v := range s.opts.Cluster.Overview(r.Context()) {
			n := node{ID: v.ID, Name: v.Name, Self: v.Self, Online: v.Online}
			if v.Status != nil {
				n.Version = v.Status.Version
			}
			out = append(out, n)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out, "version": s.opts.Version})
}

func (s *Server) handleClusterCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !s.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Cluster.Create(req.Name); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster created", "name", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleCluster(w, r)
}

func (s *Server) handleClusterSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     *string   `json:"name"`
		Internet *bool     `json:"internet"`
		Extra    *[]string `json:"extra"`
	}
	if !s.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	c := s.opts.Cluster
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
	slog.Info("cluster settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleCluster(w, r)
}

func (s *Server) handleClusterPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Info string `json:"info"`
	}
	if !s.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p, err := s.opts.Cluster.Preview(ctx, req.Info)
	if err != nil {
		writeClusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleClusterJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Info string `json:"info"`
	}
	if !s.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.opts.Cluster.Join(ctx, req.Info); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster joined", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleCluster(w, r)
}

func (s *Server) handleClusterLeave(w http.ResponseWriter, r *http.Request) {
	if !s.clusterAvailable(w) {
		return
	}
	if err := s.opts.Cluster.Leave(r.Context()); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster left", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleCluster(w, r)
}

// handleClusterJoinInfo hands out the join information after password (and
// code) or a passkey: whoever has it may add a node that sees everything.
func (s *Server) handleClusterJoinInfo(w http.ResponseWriter, r *http.Request) {
	var req proof
	if !s.clusterAvailable(w) || !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !s.reconfirmProof(w, user, req) {
		return
	}
	info := s.opts.Cluster.JoinInfo()
	if info == "" {
		writeError(w, http.StatusConflict, "cluster_none", "this node is in no cluster")
		return
	}
	slog.Info("cluster join information shown", "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"joinInfo": info})
}

func (s *Server) handleClusterJoinKey(w http.ResponseWriter, r *http.Request) {
	if !s.clusterAvailable(w) {
		return
	}
	if err := s.opts.Cluster.NewJoinKey(r.Context()); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster join key replaced", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleCluster(w, r)
}

func (s *Server) handleClusterRemove(w http.ResponseWriter, r *http.Request) {
	if !s.clusterAvailable(w) {
		return
	}
	if err := s.opts.Cluster.Remove(r.Context(), r.PathValue("id")); err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster node removed", "node", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleCluster(w, r)
}

func (s *Server) handleClusterBackupPair(w http.ResponseWriter, r *http.Request) {
	if !s.clusterAvailable(w) {
		return
	}
	code, err := s.opts.Cluster.PairBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		writeClusterError(w, err)
		return
	}
	slog.Info("cluster backup paired", "node", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

func (s *Server) handleClusterProxy(w http.ResponseWriter, r *http.Request) {
	if !s.clusterAvailable(w) {
		return
	}
	s.opts.Cluster.Proxy(w, r, r.PathValue("id"), currentUser(r).Name, r.PathValue("path"))
}

// clusterApplied follows up an account another node changed, as the
// handlers of users.go do for a change made here.
func (s *Server) clusterApplied(before auth.User, existed bool, after auth.User, deleted bool) {
	name := after.Name
	if deleted {
		name = before.Name
	}
	switch {
	case deleted:
		s.sessions.DeleteUser(name, "")
		s.prefs.forget(name)
		s.opts.Notify.ForgetPersonal(name)
		if err := s.chats.ForgetUser(name); err != nil {
			slog.Error("remove chats of the account", "user", name, "err", err)
		}
		if err := s.opts.Shares.ForgetUser(name); err != nil {
			slog.Error("remove account from shares", "user", name, "err", err)
		}
	case existed && (before.PasswordHash != after.PasswordHash || before.TOTPSecret != after.TOTPSecret ||
		after.Disabled || len(after.Passkeys) < len(before.Passkeys) || !before.Created.Equal(after.Created)):
		// Whoever knew the old password or held a removed factor is out.
		s.sessions.DeleteUser(name, "")
	}
	slog.Info("account taken over from the cluster", "user", name, "deleted", deleted)
	s.opts.Shares.Kick()
	s.opts.SSH.Kick()
}

// ClusterStatus is what this node tells the others for the overview, from
// memory and the storage status of the last seconds.
func (s *Server) ClusterStatus(ctx context.Context) cluster.Status {
	info := s.opts.Monitor.Info()
	st := cluster.Status{Name: info.Hostname, Hostname: info.Hostname, Version: s.opts.Version, Uptime: info.UptimeSeconds, Volumes: []cluster.VolumeStatus{}}
	cpu := info.CPU.UsagePercent
	st.CPU = &cpu
	st.Memory.Total, st.Memory.Available = info.Memory.TotalBytes, info.Memory.AvailableBytes
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if storage, err := s.opts.Storage.Status(ctx); err == nil {
		for _, v := range storage.Volumes {
			st.Volumes = append(st.Volumes, cluster.VolumeStatus{Name: v.Name, State: v.State, Mounted: v.Mounted, SizeBytes: v.SizeBytes, UsedBytes: v.UsedBytes})
		}
	}
	since := time.Now().Add(-24 * time.Hour)
	for _, m := range s.opts.Notify.Messages() {
		if m.Time.Before(since) {
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

// clusterNotify reports joins and removals.
func (s *Server) clusterNotify(kind, subject string) {
	severity := notify.Info
	if kind == "cluster_node_removed" {
		severity = notify.Warning
	}
	s.opts.Notify.Notify(severity, kind, subject)
}
