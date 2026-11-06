package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
	"github.com/ra5on/TitanOS/backend/internal/whatif"
)

// Gruppe „Vorausschau“ (PLAN 54-V): Was-wäre-wenn and Hardware-Berater,
// for the whole NAS, owners and administrators only, both off by default.
//
//	Was-wäre-wenn    computes on a model of the NAS built from what titand
//	                 holds in RAM (storage status, cached SMART values,
//	                 cached backup states); changes nothing, wakes nothing.
//	Hardware-Berater judges the SMART history and samples of memory, CPU,
//	                 volumes and graphics card, nightly (agent_foresight_advisor.go).

// agentWhatIf: the switch of Was-wäre-wenn.
type agentWhatIf struct {
	Enabled bool `json:"enabled,omitempty"`
}

func (w agentWhatIf) view() agentWhatIf { return w }

// agentAdvisor are the settings of the Hardware-Berater. Predict and
// Bottlenecks are on unless switched off (nil is on, so an agent.json
// without them means the owner's answer); Prices needs the web search and
// the monthly report a notification channel, both off by default.
type agentAdvisor struct {
	Enabled     bool  `json:"enabled,omitempty"`
	Predict     *bool `json:"predict,omitempty"`
	Bottlenecks *bool `json:"bottlenecks,omitempty"`
	Prices      bool  `json:"prices,omitempty"`
	Monthly     bool  `json:"monthly,omitempty"`
	// At is the time of the nightly run, HH:MM; empty is 03:30, the
	// agent's own default thinking time.
	At string `json:"at,omitempty"`
}

const advisorDefaultAt = "03:30"

func (a agentAdvisor) predict() bool     { return a.Predict == nil || *a.Predict }
func (a agentAdvisor) bottlenecks() bool { return a.Bottlenecks == nil || *a.Bottlenecks }
func (a agentAdvisor) at() string {
	if a.At == "" {
		return advisorDefaultAt
	}
	return a.At
}

// view shows every value as it applies.
func (a agentAdvisor) view() agentAdvisor {
	p, b := a.predict(), a.bottlenecks()
	a.Predict, a.Bottlenecks, a.At = &p, &b, a.at()
	return a
}

func (s *Server) checkForesight(f *agentFeatures, old agentFeatures) error {
	if f.Advisor.At != "" {
		if _, err := time.Parse("15:04", f.Advisor.At); err != nil || len(f.Advisor.At) != 5 {
			return errors.New("advisor.at must be HH:MM")
		}
	}
	return nil
}

func (s *Server) registerAgentForesight(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/agent/whatif", s.requireAdmin(s.handleWhatIfState))
	mux.HandleFunc("POST /api/v1/agent/whatif", s.requireAdmin(s.handleWhatIf))
	mux.HandleFunc("GET /api/v1/agent/advisor", s.requireAdmin(s.handleAdvisor))
	mux.HandleFunc("POST /api/v1/agent/advisor/run", s.requireAdmin(s.handleAdvisorRun))
}

func (s *Server) agentForesightTick(now agentNow) {
	f := s.agentSettings().Features
	if !f.WhatIf.Enabled && !f.Advisor.Enabled {
		return
	}
	s.foresightSample(now, f)
	if f.Advisor.Enabled {
		s.advisorNightly(now, f.Advisor)
	}
}

func featureOff(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "agent_feature_off", "this function of the agent is switched off")
}

// handleWhatIfState tells the storage page whether to offer "Durchspielen"
// and what it may play through.
func (s *Server) handleWhatIfState(w http.ResponseWriter, r *http.Request) {
	on := s.agentSettings().Features.WhatIf.Enabled
	writeJSON(w, http.StatusOK, map[string]any{"enabled": on, "levels": whatIfLevels})
}

func (s *Server) handleWhatIf(w http.ResponseWriter, r *http.Request) {
	if !s.agentSettings().Features.WhatIf.Enabled {
		featureOff(w)
		return
	}
	var sc whatif.Scenario
	if !readJSON(w, r, &sc) {
		return
	}
	res, err := s.playWhatIf(r.Context(), sc)
	if err != nil {
		whatIfError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func whatIfError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, whatif.ErrNoDisk), errors.Is(err, whatif.ErrNoVolume):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, whatif.ErrScenario), errors.Is(err, whatif.ErrLevel):
		writeError(w, http.StatusBadRequest, "invalid_scenario", err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, "whatif_unavailable", err.Error())
	}
}

// playWhatIf builds the model and plays the scenario through.
func (s *Server) playWhatIf(ctx context.Context, sc whatif.Scenario) (whatif.Result, error) {
	m, err := s.whatifModel(ctx)
	if err != nil {
		return whatif.Result{}, err
	}
	return whatif.Run(m, sc)
}

func (b *agentToolbox) foresightTools() []*agentTool {
	if !b.user.Role.CanAdminister() {
		return nil
	}
	f := b.settings.Features
	var out []*agentTool
	if f.WhatIf.Enabled {
		out = append(out, &agentTool{
			name: "whatif", area: toolcatalog.AreaStorage, kind: agent.KindRead,
			description: "Play through what would happen on this NAS, computed only (nothing is changed): a disk fails (kind disk, disk sdX), a volume runs full (kind full, volume), or what another RAID level would bring (kind level, volume, level raid1/raid5/raid6/raid10/raid1c3). Answers with data at risk, rebuild time, risk meanwhile, backup gaps and advice.",
			params: schema(map[string]any{
				"kind":   enum("What to play through.", whatif.KindDisk, whatif.KindFull, whatif.KindLevel),
				"disk":   str("Disk name such as sdb (kind disk)."),
				"volume": str("Volume name (kind full and level)."),
				"level":  enum("Target level (kind level).", "raid1", "raid5", "raid6", "raid10", "raid1c3"),
			}, "kind"),
			own: func(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
				var sc whatif.Scenario
				if err := json.Unmarshal(args, &sc); err != nil {
					return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
				}
				res, err := b.s.playWhatIf(ctx, sc)
				if err != nil {
					return agent.Outcome{}, toolcatalog.Errorf("%s", err.Error())
				}
				data, _ := json.Marshal(res)
				return agent.Outcome{Text: whatif.Summary(res) + "\n" + string(data)}, nil
			},
		})
	}
	if f.Advisor.Enabled {
		out = append(out, &agentTool{
			name: "hardware_advice", area: toolcatalog.AreaSystem, kind: agent.KindRead, untrusted: f.Advisor.Prices,
			description: "The report of the hardware adviser: which disk will probably fail (SMART trend), bottlenecks (memory, volumes filling up, CPU, graphics card) and temperatures. Offers with prices, if any, come from a web search and are not checked.",
			params:      schema(nil),
			own: func(ctx context.Context, _ *agent.Chat, _ json.RawMessage) (agent.Outcome, error) {
				saved, ok := b.s.advisorReport()
				if !ok {
					var err error
					if saved, err = b.s.advisorRunNow(ctx, f.Advisor); err != nil {
						return agent.Outcome{}, toolcatalog.Errorf("%s", err.Error())
					}
				}
				saved.Volumes, saved.Measured = nil, nil
				data, _ := json.Marshal(saved)
				return agent.Outcome{Text: string(data)}, nil
			},
		})
	}
	return out
}

// whatIfLevels are the targets of the level scenario.
var whatIfLevels = []string{"raid1", "raid5", "raid6", "raid10", "raid1c3"}
