package cluster

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// PeerPrefix is where the other nodes talk to this one.
const PeerPrefix = "/api/v1/cluster/peer/"

var errRateLimited = errors.New("cluster: too many attempts")

func remoteHost(remote string) string {
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// writePeerError answers a node with the code the API uses for err.
func writePeerError(w http.ResponseWriter, err error) {
	status, code := ErrorCode(err)
	writeError(w, status, code, err.Error())
}

// ErrorCode returns the HTTP status and error code for an error of this
// package, for the API.
func ErrorCode(err error) (int, string) {
	var pe *PeerError
	switch {
	case errors.As(err, &pe) && pe.Code != "":
		return http.StatusBadGateway, pe.Code
	case errors.Is(err, ErrJoinKey):
		return http.StatusForbidden, "cluster_join_key"
	case errors.Is(err, errRateLimited):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, ErrTooMany):
		return http.StatusConflict, "cluster_full"
	case errors.Is(err, ErrInCluster):
		return http.StatusConflict, "cluster_member"
	case errors.Is(err, ErrNoCluster):
		return http.StatusConflict, "cluster_none"
	case errors.Is(err, ErrJoinInfo):
		return http.StatusBadRequest, "cluster_join_info"
	case errors.Is(err, ErrVersion):
		return http.StatusConflict, "cluster_version"
	case errors.Is(err, ErrUnknownNode):
		return http.StatusNotFound, "cluster_node_unknown"
	case errors.Is(err, ErrSelf):
		return http.StatusConflict, "cluster_node_self"
	case errors.Is(err, ErrUnreachable):
		return http.StatusBadGateway, "cluster_unreachable"
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized, "cluster_unauthorized"
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, ErrNotLoaded):
		return http.StatusServiceUnavailable, "config_unavailable"
	case errors.Is(err, ErrDisabled):
		return http.StatusConflict, "dev_mode"
	}
	return http.StatusInternalServerError, "internal"
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return false
	}
	return true
}

// PeerHandler serves the requests of other nodes under PeerPrefix. Only
// joining goes without the signature of the cluster; it needs the join key.
func (m *Manager) PeerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, PeerPrefix)
		switch {
		case r.Method == "POST" && path == "preview":
			m.peerPreview(w, r)
			return
		case r.Method == "POST" && path == "join":
			m.peerJoin(w, r)
			return
		}
		id, err := m.Verify(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "cluster_unauthorized", "request not signed by the cluster")
			return
		}
		r = r.WithContext(WithIdentity(r.Context(), id))
		switch {
		case r.Method == "GET" && path == "status":
			if m.Status == nil {
				writeJSON(w, http.StatusOK, Status{Version: m.Version})
				return
			}
			writeJSON(w, http.StatusOK, m.Status(r.Context()))
		case r.Method == "GET" && path == "accounts":
			writeJSON(w, http.StatusOK, m.versions())
		case r.Method == "POST" && path == "accounts":
			var records []accountRecord
			if !readBody(w, r, &records) {
				return
			}
			if err := m.takeAccounts(records); err != nil {
				writePeerError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && path == "node":
			var n Node
			if !readBody(w, r, &n) {
				return
			}
			if err := m.updateNode(id.Node, n); err != nil {
				writePeerError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && path == "members":
			var msg membership
			if !readBody(w, r, &msg) {
				return
			}
			if err := m.takeMembership(msg); err != nil {
				writePeerError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && path == "leave":
			// Answer first: the removal tells the leaving node as well.
			w.WriteHeader(http.StatusNoContent)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if err := m.Remove(r.Context(), id.Node); err != nil {
				slog.Warn("cluster: leaving node not removed", "err", err)
			}
		case r.Method == "POST" && path == "backup/pair":
			m.peerPair(w, r, id)
		case strings.HasPrefix(path, "backup/"):
			m.peerBackup(w, r, strings.TrimPrefix(path, "backup"))
		default:
			writeError(w, http.StatusNotFound, "not_found", "no such route")
		}
	})
}

// joinAllowed refuses joining from the internet unless the cluster allows it.
func (m *Manager) joinAllowed(w http.ResponseWriter, r *http.Request) bool {
	m.mu.Lock()
	internet := m.state != nil && m.state.Internet
	m.mu.Unlock()
	if !m.allowedPeer(r.RemoteAddr, internet) {
		writeError(w, http.StatusForbidden, "cluster_join_network", "joining is not possible from this network")
		return false
	}
	return true
}

func (m *Manager) peerPreview(w http.ResponseWriter, r *http.Request) {
	if !m.joinAllowed(w, r) {
		return
	}
	var req previewRequest
	if !readBody(w, r, &req) {
		return
	}
	if err := m.checkJoinKey(r.RemoteAddr, req.Key); err != nil {
		writePeerError(w, err)
		return
	}
	m.mu.Lock()
	res := previewResponse{Name: m.state.Name, Version: m.Version, Nodes: []string{}, Accounts: []string{}}
	for _, n := range m.state.Nodes {
		res.Nodes = append(res.Nodes, n.Name)
	}
	m.mu.Unlock()
	for _, u := range m.Users.List() {
		res.Accounts = append(res.Accounts, u.Name)
	}
	writeJSON(w, http.StatusOK, res)
}

func (m *Manager) peerJoin(w http.ResponseWriter, r *http.Request) {
	if !m.joinAllowed(w, r) {
		return
	}
	var req joinRequest
	if !readBody(w, r, &req) {
		return
	}
	res, err := m.admit(r.Context(), r.RemoteAddr, req)
	if err != nil {
		writePeerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
