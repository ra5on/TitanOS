package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// The KI-Agent across a cluster (PLAN 47, decision 5.11.2026): every node
// chooses its own agent or the one of the admin node. With the admin
// node's agent only the model comes from there: chats, memory and rights
// stay on the node. The admin node releases each node (a request with a
// button, revocable at any time).

const (
	agentSourceOwn     = ""
	agentSourceCluster = "cluster"
)

// agentShare is a node that asked to use the model of this (admin) node.
type agentShare struct {
	Node    string    `json:"node"`
	Name    string    `json:"name"`
	Allowed bool      `json:"allowed"`
	Asked   time.Time `json:"asked"`
}

// The states of a node using the admin node's model.
const (
	shareAllowed     = "allowed"
	sharePending     = "pending"
	shareOff         = "off"         // the admin node's agent is switched off
	shareUnreachable = "unreachable" // no answer from the admin node
	shareOld         = "old"         // the admin node does not know it yet
	shareNoCluster   = "nocluster"   // not (or no longer) a member
)

// ollamaPaths are what a node may ask the admin node's Ollama: models,
// their details, what is loaded, and a chat. No pull, no delete.
var ollamaPaths = []string{"/api/version", "/api/tags", "/api/show", "/api/ps", "/api/chat"}

// agentClusterHandler is the agent end of the admin node for the other
// nodes (cluster.Manager.Agent). The cluster checked the signature and
// the certificate of the node; this decides by the release.
func (s *Server) agentClusterHandler() http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			target := pr.In.Context().Value(agentRelayKey{}).(*url.URL)
			pr.SetURL(target)
			pr.Out.URL.Path = target.Path + pr.In.URL.Path
			pr.Out.URL.RawPath = ""
			pr.Out.Header.Del(cluster.Header)
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadGateway, "agent_unreachable", "Ollama of the admin node not reachable")
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := cluster.FromContext(r.Context())
		if !ok || s.opts.Cluster == nil || !s.opts.Cluster.IsAdmin() {
			writeError(w, http.StatusForbidden, "agent_not_admin", "only the admin node shares its agent")
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/request":
			writeJSON(w, http.StatusOK, map[string]string{"state": s.agentShareRequest(id.Node)})
		case r.Method == "GET" && r.URL.Path == "/state":
			writeJSON(w, http.StatusOK, map[string]string{"state": s.agentShareState(id.Node)})
		case len(r.URL.Path) > len("/ollama") && r.URL.Path[:len("/ollama/")] == "/ollama/":
			if state := s.agentShareState(id.Node); state != shareAllowed {
				writeError(w, http.StatusForbidden, "agent_not_released", "the admin node has not released this node ("+state+")")
				return
			}
			path := r.URL.Path[len("/ollama"):]
			if !slices.Contains(ollamaPaths, path) {
				writeError(w, http.StatusForbidden, "agent_not_allowed", "not shared: "+path)
				return
			}
			target, err := agent.ParseOllamaURL(s.agentSettings().Ollama)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "agent_unavailable", "no Ollama server on the admin node")
				return
			}
			r2 := r.Clone(context.WithValue(r.Context(), agentRelayKey{}, target))
			r2.URL.Path = path
			proxy.ServeHTTP(w, r2)
		default:
			writeError(w, http.StatusNotFound, "not_found", "no such route")
		}
	})
}

type agentRelayKey struct{}

// agentShareState is the state of node id on this admin node.
func (s *Server) agentShareState(id string) string {
	settings := s.agentSettings()
	if !settings.Enabled || settings.Source != agentSourceOwn || settings.Ollama == "" {
		return shareOff
	}
	for _, sh := range settings.Shared {
		if sh.Node == id {
			if sh.Allowed {
				return shareAllowed
			}
			return sharePending
		}
	}
	return shareOff
}

// agentShareRequest records the request of node id and tells the owners
// and administrators.
func (s *Server) agentShareRequest(id string) string {
	name := s.opts.Cluster.NodeName(id)
	if name == "" {
		return shareNoCluster
	}
	a := &s.agentConf
	s.agentSettings() // loaded
	a.mu.Lock()
	if !a.loaded {
		// agent.json unreadable: never write over it from a request of
		// another node.
		a.mu.Unlock()
		return sharePending
	}
	settings := a.settings
	i := slices.IndexFunc(settings.Shared, func(sh agentShare) bool { return sh.Node == id })
	if i >= 0 {
		allowed := settings.Shared[i].Allowed
		a.mu.Unlock()
		if allowed {
			return shareAllowed
		}
		return sharePending
	}
	if len(settings.Shared) >= 64 {
		a.mu.Unlock()
		return sharePending
	}
	settings.Shared = append(slices.Clone(settings.Shared), agentShare{Node: id, Name: name, Asked: time.Now().UTC().Truncate(time.Second)})
	err := s.opts.Store.WriteJSON(agentSettingsFile, settings)
	if err == nil {
		a.settings = settings
	}
	a.mu.Unlock()
	if err != nil {
		return sharePending
	}
	if s.opts.Notify != nil {
		s.opts.Notify.Notify(notify.Info, "agent_cluster_request", name)
	}
	return sharePending
}

// handleAgentShareSet releases a node or takes the release back; remove
// forgets the node (it asks anew).
func (s *Server) handleAgentShareSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Allowed bool `json:"allowed"`
		Remove  bool `json:"remove"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("node")
	a := &s.agentConf
	s.agentSettings()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "agent.json is not readable; nothing was changed")
		return
	}
	settings := a.settings
	i := slices.IndexFunc(settings.Shared, func(sh agentShare) bool { return sh.Node == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no request of this node")
		return
	}
	settings.Shared = slices.Clone(settings.Shared)
	if req.Remove {
		settings.Shared = slices.Delete(settings.Shared, i, i+1)
	} else {
		settings.Shared[i].Allowed = req.Allowed
	}
	if err := s.opts.Store.WriteJSON(agentSettingsFile, settings); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.settings = settings
	writeJSON(w, http.StatusOK, settings.Shared)
}

// agentClusterCall asks the admin node's agent end (method, path) and
// decodes the state it answers.
func (s *Server) agentClusterCall(ctx context.Context, method, path string) string {
	if s.opts.Cluster == nil || !s.opts.Cluster.InCluster() || s.opts.Cluster.IsAdmin() {
		return shareNoCluster
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, method, "http://titanos-cluster-admin"+path, nil)
	res, err := s.opts.Cluster.AgentTransport().RoundTrip(req)
	if err != nil {
		return shareUnreachable
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return shareOld
	}
	var out struct{ State string }
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&out) != nil || out.State == "" {
		return shareUnreachable
	}
	return out.State
}

// handleAgentShareRequest: a node asks the admin node (again) for its model.
func (s *Server) handleAgentShareRequest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"state": s.agentClusterCall(r.Context(), "POST", "/request")})
}

// handleAgentClusterInfo tells the settings page what the cluster offers:
// whether this node can use the admin node's model, and its state.
func (s *Server) handleAgentClusterInfo(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"member": false, "admin": false}
	c := s.opts.Cluster
	if c != nil && c.InCluster() {
		if c.IsAdmin() {
			out["admin"] = true
		} else if n, err := c.AdminNode(); err == nil {
			out["member"], out["adminNode"] = true, n.Name
			if s.agentSettings().Source == agentSourceCluster {
				out["state"] = s.agentClusterCall(r.Context(), "GET", "/state")
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// agentShareError is the error of the status when the admin node does
// not (yet) share its model.
func agentShareError(state string) error {
	switch state {
	case shareAllowed:
		return nil
	case sharePending:
		return errors.New("the admin node has not released this node yet")
	case shareOff:
		return errors.New("the agent of the admin node is switched off")
	case shareOld:
		return errors.New("the admin node runs an older version without a shared agent")
	case shareNoCluster:
		return errors.New("this node is no member of a cluster")
	}
	return fmt.Errorf("the admin node does not answer")
}
