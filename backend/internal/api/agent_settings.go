package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// The settings of the KI-Agent (PLAN 44) live on the stick in
// system/agent.json, written only when an owner or administrator saves
// them. The name is not assistant.json: api/retired.go removes that file.

const agentSettingsFile = "system/agent.json"

var keepAlivePattern = regexp.MustCompile(`^(-1|0|[1-9][0-9]{0,3}[mh])$`)

// agentSettings are the settings of the agent. New fields must stay
// optional: older versions ignore them, and a missing field keeps its
// default.
type agentSettings struct {
	Enabled bool   `json:"enabled"`
	Ollama  string `json:"ollama,omitempty"`
	// Source: "" this node's own Ollama, "cluster" the model of the admin
	// node of the cluster (PLAN 47); chats and rights stay here.
	Source string `json:"source,omitempty"`
	// Shared are, on the admin node, the nodes that asked for its model,
	// released or not. Changed only through their own routes.
	Shared []agentShare `json:"shared,omitempty"`
	// Instruction is the owner's top instruction for every chat.
	Instruction string `json:"instruction,omitempty"`
	Model       string `json:"model,omitempty"`
	// Think is the default thinking level, as the model names it; empty
	// uses the model's default.
	Think string `json:"think,omitempty"`
	// Context is num_ctx; 0 keeps what is set in Ollama.
	Context int `json:"context,omitempty"`
	// KeepAlive: how long Ollama keeps the model loaded after a request
	// ("5m", "30m", "2h", "-1" for always); empty keeps Ollama's own.
	KeepAlive string `json:"keepAlive,omitempty"`
	MaxSteps  int    `json:"maxSteps,omitempty"`
	// Users are the accounts besides owners that may use the agent
	// ("KI-Agent erlaubt" per account). Owners always may.
	Users     []string         `json:"users,omitempty"`
	Policy    agent.Policy     `json:"policy"`
	Locations []agent.Location `json:"locations,omitempty"`
	// WeeklyReport sends on Monday what the agent changed in the week.
	WeeklyReport bool `json:"weeklyReport,omitempty"`
	// React lets the agent act on notifications of the NAS as ReactAs.
	React   bool   `json:"react,omitempty"`
	ReactAs string `json:"reactAs,omitempty"`
	// Skills switched off by name of the built-in ones.
	SkillsOff []string `json:"skillsOff,omitempty"`
	// Volume holds the chats; chosen once (the first mounted one) and
	// kept, so that a new volume does not hide them.
	Volume string `json:"volume,omitempty"`
	// Home Assistant: modes per "area:<id>", "domain:<name>" or
	// "entity:<id>"; notify services for messages; Assist.
	HA agentHASettings `json:"homeassistant"`
	// Voice: speech to text with a Whisper service, off by default; Vision:
	// a model of its own for pictures when the main model sees none
	// (PLAN 49, agent_media.go).
	Voice  agentVoice  `json:"voice"`
	Vision agentVision `json:"vision"`
	// NoSnapshots switches off the Btrfs snapshot before changes that
	// can lose data (PLAN 51, agent_snapshot.go); on by default.
	NoSnapshots bool `json:"noSnapshots,omitempty"`
	// Working like an agent (PLAN 55): MaxMinutes ends a run with a report
	// (0: agentDefaultMinutes); AllTools offers every tool at once instead
	// of the core and tools_load; NoDelegate switches off task_delegate.
	MaxMinutes int  `json:"maxMinutes,omitempty"`
	AllTools   bool `json:"allTools,omitempty"`
	NoDelegate bool `json:"noDelegate,omitempty"`
	// Code runs Python or sh in a sandbox container (agent_code.go), off by
	// default (decision of the owner 5.11.2026: the middle way).
	Code agentCode `json:"code"`
}

// agentCode are the settings of code_run.
type agentCode struct {
	Enabled bool `json:"enabled,omitempty"`
	// Timeout in seconds (0: 60), Memory in MiB (0: 512).
	Timeout int `json:"timeout,omitempty"`
	Memory  int `json:"memory,omitempty"`
}

type agentHASettings struct {
	// Release is the agent's own list of devices it may see and use
	// (decision 5.11.2026), apart from "Geräte für MCP freigeben".
	Release homeassistant.Release `json:"release"`
	// Releases are those of the further connections by their ID (PLAN
	// §45); older versions only know Release, the first connection's.
	Releases map[string]homeassistant.Release `json:"releases,omitempty"`
	Entities map[string]string                `json:"entities,omitempty"`
	Notify   []string                         `json:"notify,omitempty"`
	Assist   bool                             `json:"assist,omitempty"`
	// AssistAs is the account questions from Home Assistant run as.
	AssistAs string `json:"assistAs,omitempty"`
}

const (
	agentDefaultSteps   = 30
	agentMaxSteps       = 100
	agentDefaultMinutes = 30
	agentMaxMinutes     = 600
)

type agentSettingsState struct {
	mu       sync.Mutex
	loaded   bool
	settings agentSettings
}

func (s *Server) agentSettings() agentSettings {
	a := &s.agentConf
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		var settings agentSettings
		err := s.opts.Store.ReadJSON(agentSettingsFile, &settings)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("agent settings not readable", "err", err)
			return agentSettings{}
		}
		a.settings, a.loaded = settings, true
	}
	out := a.settings
	if out.Policy.Preset == "" {
		out.Policy.Preset = agent.PresetStandard
	}
	if out.MaxSteps <= 0 {
		out.MaxSteps = agentDefaultSteps
	}
	if out.MaxMinutes <= 0 {
		out.MaxMinutes = agentDefaultMinutes
	}
	if out.Code.Timeout <= 0 {
		out.Code.Timeout = 60
	}
	if out.Code.Memory <= 0 {
		out.Code.Memory = 512
	}
	return out
}

// agentAllowed reports whether an account may use the agent.
func (s *Server) agentAllowed(user auth.User) bool {
	if user.Name == "" || user.Disabled {
		return false
	}
	if user.Role == auth.RoleOwner {
		return true
	}
	return slices.Contains(s.agentSettings().Users, user.Name)
}

func (s *Server) handleAgentSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agentSettings())
}

func (s *Server) handleAgentSettingsSet(w http.ResponseWriter, r *http.Request) {
	var req agentSettings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.checkAgentSettings(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_agent_settings", err.Error())
		return
	}
	a := &s.agentConf
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.Volume == "" {
		req.Volume = a.settings.Volume
	}
	req.Shared = a.settings.Shared
	if err := s.opts.Store.WriteJSON(agentSettingsFile, req); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.settings, a.loaded = req, true
	s.agentClient.Store(nil)
	// The share of the agent follows (made, accounts updated).
	go s.agentSyncShare()
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) checkAgentSettings(req *agentSettings) error {
	if req.Ollama != "" {
		u, err := agent.ParseOllamaURL(req.Ollama)
		if err != nil {
			return err
		}
		req.Ollama = u.String()
	}
	switch req.Source {
	case agentSourceOwn:
		if req.Enabled && req.Ollama == "" {
			return errors.New("the agent needs the address of an Ollama server")
		}
	case agentSourceCluster:
		c := s.opts.Cluster
		if c == nil || !c.InCluster() || c.IsAdmin() {
			return errors.New("only a node of a cluster other than the admin node uses the admin node's agent")
		}
	default:
		return errors.New("unknown source " + req.Source)
	}
	if len(req.Instruction) > 8000 {
		return errors.New("the instruction has at most 8000 characters")
	}
	if len(req.Model) > 200 || len(req.Think) > 40 {
		return errors.New("model or level too long")
	}
	if req.KeepAlive != "" && !keepAlivePattern.MatchString(req.KeepAlive) {
		return errors.New("keep alive like 5m, 30m, 2h or -1")
	}
	if req.Context != 0 && (req.Context < 2048 || req.Context > 1<<20) {
		return errors.New("context length out of range")
	}
	if req.MaxSteps < 0 || req.MaxSteps > agentMaxSteps {
		return errors.New("steps per task: 1 to 100")
	}
	if req.MaxMinutes < 0 || req.MaxMinutes > agentMaxMinutes {
		return errors.New("minutes per task: 1 to 600")
	}
	if req.Code.Timeout < 0 || req.Code.Timeout > 600 || req.Code.Memory < 0 || (req.Code.Memory != 0 && (req.Code.Memory < 64 || req.Code.Memory > 8192)) {
		return errors.New("code: 1 to 600 seconds, 64 to 8192 MiB")
	}
	if err := checkAgentMedia(req); err != nil {
		return err
	}
	if err := req.Policy.Check(); err != nil {
		return err
	}
	for area := range req.Policy.Areas {
		if !agent.ValidArea(area) {
			return errors.New("unknown area " + area)
		}
	}
	if len(req.Policy.Tools) > 500 {
		return errors.New("too many tool settings")
	}
	for name := range req.Policy.Tools {
		if len(name) > 64 {
			return errors.New("invalid tool name")
		}
	}
	if len(req.Users) > 200 || len(req.Locations) > 200 || len(req.SkillsOff) > 100 {
		return errors.New("too many entries")
	}
	for _, name := range req.Users {
		if _, ok := s.opts.Users.Get(name); !ok {
			return errors.New("unknown account " + name)
		}
	}
	seen := map[string]bool{}
	for i, l := range req.Locations {
		l.Name = strings.TrimSpace(l.Name)
		if l.Name == "" || len(l.Name) > 80 || len(l.Note) > 300 || l.Root == "" || len(l.Root) > 200 || strings.Contains(l.Path, "..") || len(l.Path) > 1024 {
			return errors.New("invalid location")
		}
		key := strings.ToLower(l.Name)
		if seen[key] {
			return errors.New("two locations with the name " + l.Name)
		}
		seen[key] = true
		req.Locations[i] = l
	}
	for _, name := range []string{req.ReactAs, req.HA.AssistAs} {
		if name == "" {
			continue
		}
		if u, ok := s.opts.Users.Get(name); !ok || !u.Role.CanAdminister() {
			return errors.New("acting account must be an owner or administrator")
		}
	}
	for key, mode := range req.HA.Entities {
		if !agent.ValidMode(mode) || len(key) > 200 || !(strings.HasPrefix(key, "area:") || strings.HasPrefix(key, "domain:") || strings.HasPrefix(key, "entity:")) {
			return errors.New("invalid Home Assistant rule")
		}
	}
	if len(req.HA.Releases) >= homeassistant.MaxConnections {
		return errors.New("too many Home Assistant connections")
	}
	releases := []homeassistant.Release{req.HA.Release}
	for id, r := range req.HA.Releases {
		if !regexp.MustCompile(`^[a-z0-9]{8}$`).MatchString(id) {
			return errors.New("invalid Home Assistant connection " + id)
		}
		releases = append(releases, r)
	}
	for _, r := range releases {
		if len(r.Domains) > 50 || len(r.Entities) > 2000 {
			return errors.New("too many released devices")
		}
		for _, d := range r.Domains {
			if !slices.Contains(homeassistant.Domains(), d) {
				return errors.New("unknown Home Assistant domain " + d)
			}
		}
		for _, e := range r.Entities {
			if !homeassistant.ValidEntity(e) {
				return errors.New("invalid Home Assistant entity " + e)
			}
		}
	}
	if len(req.HA.Notify) > 20 {
		return errors.New("too many notify services")
	}
	for _, n := range req.HA.Notify {
		// "<connection>/notify.x" for a further connection.
		if conn, service, found := strings.Cut(n, "/"); found {
			if !regexp.MustCompile(`^[a-z0-9]{8}$`).MatchString(conn) {
				return errors.New("invalid Home Assistant connection " + conn)
			}
			n = service
		}
		if !strings.HasPrefix(n, "notify.") || len(n) > 120 {
			return errors.New("notify services start with notify")
		}
	}
	return nil
}

// agentOllama returns the client for the configured server.
func (s *Server) agentOllama() (*agent.Ollama, error) {
	settings := s.agentSettings()
	if settings.Source == agentSourceCluster {
		if s.opts.Cluster == nil {
			return nil, errors.New("no cluster")
		}
		if c := s.agentClient.Load(); c != nil && c.Address() == agent.ClusterAddress {
			return c, nil
		}
		c := agent.NewOllamaVia(s.opts.Cluster.AgentTransport())
		s.agentClient.Store(c)
		return c, nil
	}
	if settings.Ollama == "" {
		return nil, errors.New("no Ollama server set")
	}
	if c := s.agentClient.Load(); c != nil && c.Address() == settings.Ollama {
		return c, nil
	}
	c, err := agent.NewOllama(settings.Ollama)
	if err != nil {
		return nil, err
	}
	s.agentClient.Store(c)
	return c, nil
}

// agentModelView is one model on the settings page and in the chat.
type agentModelView struct {
	agent.Model
	Levels       []string `json:"levels"`
	DefaultLevel string   `json:"defaultLevel,omitempty"`
}

// handleAgentStatus tells the page whether the agent can work, with the
// models and the thinking levels they report. Every allowed account may
// read it; it has no secrets.
func (s *Server) handleAgentStatus(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	settings := s.agentSettings()
	out := map[string]any{
		"enabled": settings.Enabled, "allowed": s.agentAllowed(user), "admin": user.Role.CanAdminister(),
		"model": settings.Model, "think": settings.Think, "context": settings.Context,
		"volume": s.agentStore().Available() == nil,
		"web":    settings.Policy.Mode("web_search", agent.AreaWeb, agent.KindRead) != agent.ModeDeny,
	}
	if !settings.Enabled || !s.agentAllowed(user) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if settings.Source == agentSourceCluster {
		state := s.agentClusterCall(r.Context(), "GET", "/state")
		out["source"], out["share"] = agentSourceCluster, state
		if err := agentShareError(state); err != nil {
			out["reachable"], out["error"] = false, err.Error()
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	client, err := s.agentOllama()
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	version, err := client.Version(ctx)
	if err != nil {
		out["reachable"], out["error"] = false, err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["reachable"], out["version"] = true, version
	models, err := client.Models(ctx)
	if err != nil {
		out["error"] = err.Error()
	}
	views := []agentModelView{}
	for _, m := range models {
		v := agentModelView{Model: m, Levels: []string{}}
		if info, err := client.Show(ctx, m.Name); err == nil {
			v.Levels = agent.Levels(info.Thinking)
			if v.Levels == nil {
				v.Levels = []string{}
			}
			v.DefaultLevel = agent.DefaultLevel(info.Thinking)
			if info.ContextLength > 0 {
				v.ContextLength = info.ContextLength
			}
		}
		views = append(views, v)
	}
	out["models"] = views
	if running, err := client.Running(ctx); err == nil {
		out["running"] = running
	}
	out["voice"] = settings.Voice.Enabled && settings.Voice.URL != ""
	out["vision"] = agentImageModel(settings, views)
	writeJSON(w, http.StatusOK, out)
}

// handleAgentCheck tests an address before it is saved.
func (s *Server) handleAgentCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ollama string `json:"ollama"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	client, err := agent.NewOllama(req.Ollama)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_address", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	version, err := client.Version(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": err.Error(), "address": client.Address()})
		return
	}
	models, _ := client.Models(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"reachable": true, "version": version, "address": client.Address(), "models": len(models)})
}

// agentToolView is one tool on the settings page.
type agentToolView struct {
	Name      string `json:"name"`
	Area      string `json:"area"`
	Kind      string `json:"kind"`
	Dangerous bool   `json:"dangerous"`
	Mode      string `json:"mode"`
	Set       bool   `json:"set"`
}

// handleAgentTools lists every tool an owner could give the agent, with
// its mode under the current settings.
func (s *Server) handleAgentTools(w http.ResponseWriter, r *http.Request) {
	settings := s.agentSettings()
	box := s.newAgentToolbox(currentUser(r), settings)
	views := []agentToolView{}
	for _, t := range box.ordered {
		if t.kind == agent.KindAgent {
			continue
		}
		_, set := settings.Policy.Tools[t.name]
		views = append(views, agentToolView{Name: t.name, Area: t.area, Kind: t.kind, Dangerous: mcpDangerous[t.name], Mode: settings.Policy.Mode(t.name, t.area, t.kind), Set: set})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": views, "areas": agent.AgentAreas})
}

// agentVolume is the volume the chats live on: the one chosen before, or
// at first use the first mounted volume, which is then kept (one write to
// the stick). A chosen volume that is not mounted makes the agent
// unavailable instead of starting empty somewhere else.
func (s *Server) agentVolume(mounted []string) (string, bool) {
	a := &s.agentConf
	current := s.agentSettings()
	if current.Volume != "" {
		return current.Volume, slices.Contains(mounted, current.Volume)
	}
	if len(mounted) == 0 {
		return "", false
	}
	sorted := slices.Sorted(slices.Values(mounted))
	chosen := sorted[0]
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded && a.settings.Volume == "" {
		next := a.settings
		next.Volume = chosen
		if err := s.opts.Store.WriteJSON(agentSettingsFile, next); err == nil {
			a.settings = next
		} else {
			slog.Warn("agent volume not kept", "err", err)
		}
	}
	return chosen, true
}
