package api

import (
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// agentEvalTune sets the runner up as agentRunner does (PLAN 55).
func agentEvalTune(r *agent.Runner) {
	r.LazyTools, r.Delegate = true, true
	r.Window, r.Probe = r.Context, agentProbe(r.Ollama, r.Model)
	r.MaxTime = agentDefaultMinutes * time.Minute
}
