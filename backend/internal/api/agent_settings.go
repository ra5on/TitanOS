package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
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

// agentSettings are the settings of the agent. New fields must stay
// optional: older versions ignore them, and a missing field keeps its
// default.
type agentSettings struct {
	Enabled bool   `json:"enabled"`
	Ollama  string `json:"ollama,omitempty"`
	Model   string `json:"model,omitempty"`
	// Think is the default thinking level, as the model names it; empty
	// uses the model's default.
	Think string `json:"think,omitempty"`
	// Context is num_ctx; 0 keeps what is set in Ollama.
	Context  int `json:"context,omitempty"`
	MaxSteps int `json:"maxSteps,omitempty"`
	// Users are the accounts besides owners that may use the agent
	// ("KI-Agent erlaubt" per account). Owners always may.
	Users     []string         `json:"users,omitempty"`
	Policy    agent.Policy     `json:"policy"`
	Locations []agent.Location `json:"locations,omitempty"`
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
}

type agentHASettings struct {
	// Release is the agent's own list of devices it may see and use
	// (decision 5.11.2026), apart from "Geräte für MCP freigeben".
	Release  homeassistant.Release `json:"release"`
	Entities map[string]string     `json:"entities,omitempty"`
	Notify   []string              `json:"notify,omitempty"`
	Assist   bool                  `json:"assist,omitempty"`
	// AssistAs is the account questions from Home Assistant run as.
	AssistAs string `json:"assistAs,omitempty"`
}

const (
	agentDefaultSteps = 30
	agentMaxSteps     = 100
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
	if err := s.opts.Store.WriteJSON(agentSettingsFile, req); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.settings, a.loaded = req, true
	s.agentClient.Store(nil)
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
	if req.Enabled && req.Ollama == "" {
		return errors.New("the agent needs the address of an Ollama server")
	}
	if len(req.Model) > 200 || len(req.Think) > 40 {
		return errors.New("model or level too long")
	}
	if req.Context != 0 && (req.Context < 2048 || req.Context > 1<<20) {
		return errors.New("context length out of range")
	}
	if req.MaxSteps < 0 || req.MaxSteps > agentMaxSteps {
		return errors.New("steps per task: 1 to 100")
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
	if len(req.HA.Release.Domains) > 50 || len(req.HA.Release.Entities) > 2000 {
		return errors.New("too many released devices")
	}
	for _, d := range req.HA.Release.Domains {
		if !slices.Contains(homeassistant.Domains(), d) {
			return errors.New("unknown Home Assistant domain " + d)
		}
	}
	for _, e := range req.HA.Release.Entities {
		if !homeassistant.ValidEntity(e) {
			return errors.New("invalid Home Assistant entity " + e)
		}
	}
	if len(req.HA.Notify) > 20 {
		return errors.New("too many notify services")
	}
	for _, n := range req.HA.Notify {
		if !strings.HasPrefix(n, "notify.") || len(n) > 120 {
			return errors.New("notify services start with notify")
		}
	}
	return nil
}

// agentOllama returns the client for the configured server.
func (s *Server) agentOllama() (*agent.Ollama, error) {
	settings := s.agentSettings()
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
