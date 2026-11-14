package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The start page of the KI-Agent (PLAN 90-4) shows how the NAS is doing
// without asking a model: numbers titand already has. Only owners and
// administrators get them; the page writes the sentences in the person's
// language.

type agentHomeVolume struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Mounted bool   `json:"mounted"`
	Percent int    `json:"percent"`
	// DaysLeft: full in about that many days by the trend of the last
	// weeks (the daily samples of the forecast); 0 when unknown or far away.
	DaysLeft int `json:"daysLeft,omitempty"`
}

type agentHomeBackups struct {
	Jobs    int        `json:"jobs"`
	Failing int        `json:"failing"`
	Running int        `json:"running"`
	LastOK  *time.Time `json:"lastOK,omitempty"`
}

type agentHomeApps struct {
	Running int `json:"running"`
	Stopped int `json:"stopped"`
}

type agentHomeState struct {
	At      time.Time         `json:"at"`
	Volumes []agentHomeVolume `json:"volumes"`
	Backups *agentHomeBackups `json:"backups,omitempty"`
	Apps    *agentHomeApps    `json:"apps,omitempty"`
}

// agentHomeCache keeps the state for a minute: the page is opened often
// and nothing here is worth asking the disks or Docker for each time.
var agentHomeCache struct {
	sync.Mutex
	state *agentHomeState
}

func (s *Server) agentHomeState(ctx context.Context) *agentHomeState {
	agentHomeCache.Lock()
	defer agentHomeCache.Unlock()
	now := time.Now()
	if c := agentHomeCache.state; c != nil && now.Sub(c.At) < time.Minute {
		return c
	}
	env := s.agentEnv()
	state := &agentHomeState{At: now, Volumes: []agentHomeVolume{}}
	if env.storage != nil {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		st, err := env.storage.Status(ctx)
		cancel()
		if err == nil {
			s.loadForecast()
			agentForecast.Lock()
			for _, v := range st.Volumes {
				row := agentHomeVolume{Name: v.Name, State: v.State, Mounted: v.Mounted}
				if v.SizeBytes > 0 {
					row.Percent = int(100 * v.UsedBytes / v.SizeBytes)
				}
				if days := daysUntilFull(agentForecast.samples[v.Name]); days > 0 && days <= 60 {
					row.DaysLeft = days
				}
				state.Volumes = append(state.Volumes, row)
			}
			agentForecast.Unlock()
		}
	}
	if env.backup != nil {
		b := &agentHomeBackups{}
		for _, v := range env.backup.List() {
			if !v.Job.Enabled {
				continue
			}
			b.Jobs++
			if v.State.Running {
				b.Running++
			}
			if v.State.FailingSince != nil {
				b.Failing++
			}
			if ok := v.State.LastOK; ok != nil && (b.LastOK == nil || ok.After(*b.LastOK)) {
				b.LastOK = ok
			}
		}
		if b.Jobs > 0 {
			state.Backups = b
		}
	}
	if env.docker != nil && env.servicesOn != nil && env.servicesOn()["docker"] {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		st := env.docker.Status(ctx)
		cancel()
		if st.Running {
			a := &agentHomeApps{}
			for _, c := range st.Containers {
				if c.State == "running" {
					a.Running++
				} else {
					a.Stopped++
				}
			}
			state.Apps = a
		}
	}
	agentHomeCache.state = state
	return state
}

func (s *Server) handleAgentHome(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user.Role != auth.RoleOwner && user.Role != auth.RoleAdmin {
		writeJSON(w, http.StatusOK, map[string]any{"state": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": s.agentHomeState(r.Context())})
}
