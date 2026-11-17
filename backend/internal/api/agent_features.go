package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 54: the functions of the agent, in four groups, each switched on
// by itself and off by default (decision of the owner 6.11.2026: "alles
// einstellbar", every way of a question a setting, the answer the value
// ab Werk):
//
//	Dokumente und Fristen  agent_docs.go      (Dokumenten-Gedächtnis, Fristenwächter, Papierkram)
//	Schutz                 agent_protect.go   (Ransomware-Wächter, Notfallhandbuch, Notfallzugang)
//	Vorausschau            agent_foresight.go (Was-wäre-wenn, Hardware-Berater)
//	Gemeinschaft, Familie  agent_family54.go  (Backup-Buddy, Skill-Katalog; the Familien-Tagebuch went with 0.69)
//
// What applies to the whole NAS lives in agent.json (Features, owners and
// administrators); what an account sets for itself (its folders, its
// mailbox, its sources) in features.json in its private agent folder on
// the agent's volume, never on the stick.

// agentFeatures are the NAS-wide settings of the functions, one block per
// function (Docs holds the three of Dokumente und Fristen).
type agentFeatures struct {
	Docs      agentDocsNAS   `json:"docs"`
	Guard     agentGuard     `json:"guard"`
	Handbook  agentHandbook  `json:"handbook"`
	Emergency agentEmergency `json:"emergency"`
	WhatIf    agentWhatIf    `json:"whatIf"`
	Advisor   agentAdvisor   `json:"advisor"`
	Buddy     agentBuddy     `json:"buddy"`
	Catalog   agentCatalog   `json:"catalog"`
}

// agentFeaturesMine are an account's own settings of the functions.
type agentFeaturesMine struct {
	Docs  agentDocsMine  `json:"docs"`
	Paper agentPaperMine `json:"paper"`
}

const agentFeaturesFile = "features.json"

// checkAgentFeatures validates and normalises the NAS-wide settings; old
// is what is stored, for secrets that the page does not send back.
func (s *Server) checkAgentFeatures(f *agentFeatures, old agentFeatures) error {
	for _, check := range []func() error{
		func() error { return s.checkDocsNAS(&f.Docs, old.Docs) },
		func() error { return s.checkProtect(f, old) },
		func() error { return s.checkForesight(f, old) },
		func() error { return s.checkFamilyNAS(f, old) },
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

// checkAgentFeaturesMine validates an account's own settings.
func (s *Server) checkAgentFeaturesMine(user string, m *agentFeaturesMine, old agentFeaturesMine) error {
	if err := s.checkDocsMine(user, &m.Docs, old.Docs); err != nil {
		return err
	}
	return s.checkPaperMine(user, &m.Paper, old.Paper)
}

// agentFeaturesRevised is the NAS-wide settings as the page gets and sends
// them, with the version they were read in (A-30, 1.03). The version is
// not stored: it is a hash of what is stored, so every write – also by the
// routes of a group, or by an older titand – gives a new one, and a restart
// of titand keeps it.
type agentFeaturesRevised struct {
	agentFeatures
	Rev string `json:"rev"`
}

// agentFeaturesPut is the body of PUT /agent/features. Rev is optional:
// without it the save replaces the settings as before 1.03.
type agentFeaturesPut struct {
	agentFeatures
	Rev *string `json:"rev"`
}

// featuresRev is the version of the stored settings: the start of the
// SHA-256 of their JSON. Only owners and administrators see it, who may
// read agent.json anyway.
func featuresRev(f agentFeatures) string {
	data, _ := json.Marshal(f)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func revised(f agentFeatures) agentFeaturesRevised {
	return agentFeaturesRevised{agentFeatures: f.view(), Rev: featuresRev(f)}
}

func (s *Server) handleAgentFeatures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, revised(s.agentSettings().Features))
}

func (s *Server) handleAgentFeaturesSet(w http.ResponseWriter, r *http.Request) {
	env := s.agentEnv()
	var put agentFeaturesPut
	if !readJSON(w, r, &put) {
		return
	}
	req := put.agentFeatures
	a := &s.agentConf
	s.agentSettings() // loaded
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "agent.json is not readable; nothing was changed")
		return
	}
	// Saved from a page that read an older state: another tab or another
	// administrator changed the functions in between. Without the check
	// the whole block would silently set their change back.
	if put.Rev != nil && *put.Rev != featuresRev(a.settings.Features) {
		writeError(w, http.StatusConflict, "agent_features_changed", "the functions were changed elsewhere in the meantime; load them again")
		return
	}
	if err := s.checkAgentFeatures(&req, a.settings.Features); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_agent_features", err.Error())
		return
	}
	next := a.settings
	next.Features = req
	if err := env.store.WriteJSON(agentSettingsFile, next); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.settings = next
	writeJSON(w, http.StatusOK, revised(req))
}

// agentFeaturesOf reads an account's own settings (empty when none yet).
func (s *Server) agentFeaturesOf(user string) (agentFeaturesMine, error) {
	var m agentFeaturesMine
	err := s.agentStore().ReadPrivateLimit(user, agentFeaturesFile, &m, maxAgentPrivate)
	if errors.Is(err, agent.ErrNotFound) {
		err = nil
	}
	return m, err
}

func (s *Server) handleAgentFeaturesMine(w http.ResponseWriter, r *http.Request) {
	m, err := s.agentFeaturesOf(currentUser(r).Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mine": m.view(), "nas": s.agentSettings().Features.public()})
}

func (s *Server) handleAgentFeaturesMineSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	var req agentFeaturesMine
	if !readJSON(w, r, &req) {
		return
	}
	old, err := s.agentFeaturesOf(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if err := s.checkAgentFeaturesMine(user, &req, old); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_agent_features", err.Error())
		return
	}
	if err := s.agentStore().WritePrivate(user, agentFeaturesFile, req); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req.view())
}

// view is what the page sees: secrets are never sent, only whether one is set.
func (f agentFeatures) view() agentFeatures {
	f.Docs = f.Docs.view()
	f.Guard, f.Handbook, f.Emergency = f.Guard.view(), f.Handbook.view(), f.Emergency.view()
	f.WhatIf, f.Advisor = f.WhatIf.view(), f.Advisor.view()
	f.Buddy, f.Catalog = f.Buddy.view(), f.Catalog.view()
	return f
}

// public is what every account of the agent may know of the NAS-wide
// settings: which functions are there for it.
func (f agentFeatures) public() map[string]bool {
	return map[string]bool{
		"docs": f.Docs.Enabled, "deadlines": f.Docs.Enabled && f.Docs.Deadlines, "paper": f.Docs.Enabled && f.Docs.Paper,
		"guard": f.Guard.Enabled, "handbook": f.Handbook.Enabled, "emergency": f.Emergency.Enabled,
		"whatIf": f.WhatIf.Enabled, "advisor": f.Advisor.Enabled,
		"buddy": f.Buddy.Enabled, "catalog": f.Catalog.Enabled,
	}
}

func (m agentFeaturesMine) view() agentFeaturesMine {
	m.Docs, m.Paper = m.Docs.view(), m.Paper.view()
	return m
}

// registerAgentFeatures: the settings routes and the routes of the groups.
func (s *Server) registerAgentFeatures(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/agent/features", s.requireAdmin(s.handleAgentFeatures))
	mux.HandleFunc("PUT /api/v1/agent/features", s.requireAdmin(s.handleAgentFeaturesSet))
	mux.HandleFunc("GET /api/v1/agent/features/me", s.requireAgent(s.handleAgentFeaturesMine))
	mux.HandleFunc("PUT /api/v1/agent/features/me", s.requireAgent(s.handleAgentFeaturesMineSet))
	s.registerAgentDocs(mux)
	s.registerAgentProtect(mux)
	s.registerAgentForesight(mux)
	s.registerAgentFamily54(mux)
}

// agentFeaturesTick runs the background work of the functions once a minute
// (from agentTick; the protection runs from there also while the agent is
// off). Each group decides itself what is due; none may wake a
// sleeping disk (PLAN 53) or write to the stick.
func (s *Server) agentFeaturesTick(now agentNow) {
	s.agentDocsTick(now)
	s.agentForesightTick(now)
	s.agentFamily54Tick(now)
}

// featureTools are the agent's tools of the functions that are switched on.
func (b *agentToolbox) featureTools() []*agentTool {
	var out []*agentTool
	out = append(out, b.docsTools()...)
	out = append(out, b.protectTools()...)
	out = append(out, b.foresightTools()...)
	return out
}
