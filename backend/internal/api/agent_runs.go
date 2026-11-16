package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// The runs of the KI-Agent (1.01): what a page gets when it follows a run,
// and how many runs one account may have at once.
//
// A page that follows a run gets a snapshot first – the entries the run
// wrote so far, the text that streamed in since its last line, the step
// that runs, the state, a waiting step and the plan – and then the events
// as they come. Before 1.01 it only got what came after it subscribed: a
// chat opened while it ran began its answer in the middle of a sentence
// and lacked steps until the run ended. A page that reads too slowly gets
// a new snapshot instead of silent gaps. Older pages ignore the event.
//
// Owner 16 Nov 2026 ("Ja, einstellbar"): an account may run 1 to 4 chats
// at once (maxRuns, 1 from the factory). Only runs the person starts in the
// chat count and are refused at the limit – a new chat, a message, "Neu
// erzeugen", an approval. Planned tasks, the look into a notification and
// questions from Home Assistant start as before and do not count: in 1.00
// they ran beside the chats, and a limit of 1 must not make them fail.

const (
	agentRunsDefault = 1
	agentRunsMax     = 4
)

// agentSubBuffer is how many events wait for a slow page before it gets a
// new snapshot instead (a variable for tests).
var agentSubBuffer = 256

// errAgentLimit: the account runs as many chats as the settings allow.
var errAgentLimit = errors.New("agent: as many chats run as allowed")

// agentSub is one page that follows a run.
type agentSub struct {
	ch chan agent.Event
	// wake tells the page that events were lost: it sends a snapshot.
	wake chan struct{}
}

// agentRunView is what a run has written and streamed so far; it is kept
// from the events only (the runner's chat belongs to its goroutine).
type agentRunView struct {
	entries  []agent.Entry
	thinking strings.Builder
	content  strings.Builder
	step     *agent.Entry
	state    string
	err      string
	pending  *agent.Pending
	plan     *[]agent.PlanStep
}

// agentSnapshot is the event "snapshot".
type agentSnapshot struct {
	Type     string            `json:"type"`
	Entries  []agent.Entry     `json:"entries"`
	Thinking string            `json:"thinking,omitempty"`
	Content  string            `json:"content,omitempty"`
	Step     *agent.Entry      `json:"step,omitempty"`
	State    string            `json:"state"`
	Error    string            `json:"error,omitempty"`
	Pending  *agent.Pending    `json:"pending,omitempty"`
	Plan     *[]agent.PlanStep `json:"plan,omitempty"`
}

// maxRuns is the number of chats an account may run at once.
func (a agentSettings) maxRuns() int {
	if a.MaxRuns < 1 {
		return agentRunsDefault
	}
	return min(a.MaxRuns, agentRunsMax)
}

// agentClaim takes the place of a run before anything of it is written: a
// message refused at the limit is not saved, and two requests at the same
// moment cannot both take the last place. byPerson: the person starts it
// in the chat (it counts and can be refused).
func (s *Server) agentClaim(user, chat string, byPerson bool) (*agentRun, error) {
	limit := s.agentSettings().maxRuns()
	rt := &s.agentRT
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.runs == nil {
		rt.runs = map[string]*agentRun{}
	}
	key := runKey(user, chat)
	if _, busy := rt.runs[key]; busy {
		return nil, agent.ErrBusy
	}
	if byPerson && rt.countLocked(user) >= limit {
		return nil, errAgentLimit
	}
	ctx, cancel := context.WithCancel(s.agentBase())
	run := &agentRun{user: user, chat: chat, byPerson: byPerson, ctx: ctx, cancel: cancel, subs: map[*agentSub]struct{}{}}
	run.view.state = agent.StateRunning
	run.view.entries = []agent.Entry{}
	rt.runs[key] = run
	return run, nil
}

// countLocked counts the runs of an account that the person started.
func (rt *agentRuntime) countLocked(user string) int {
	n := 0
	for _, run := range rt.runs {
		if run.user == user && run.byPerson {
			n++
		}
	}
	return n
}

// agentRelease ends a claimed run: it goes from the list and the pages that
// follow it get the end.
func (s *Server) agentRelease(run *agentRun) {
	run.cancel()
	s.agentRT.mu.Lock()
	if s.agentRT.runs[runKey(run.user, run.chat)] == run {
		delete(s.agentRT.runs, runKey(run.user, run.chat))
	}
	subs := run.subs
	run.subs = nil
	s.agentRT.mu.Unlock()
	for sub := range subs {
		close(sub.ch)
	}
}

// agentLimitReached says whether the person may start no further chat now
// (checked before a message is written; agentClaim decides).
func (s *Server) agentLimitReached(user string) bool {
	limit := s.agentSettings().maxRuns()
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	return s.agentRT.countLocked(user) >= limit
}

// emitLocked hands an event to the pages and keeps what a snapshot needs.
// The caller holds agentRT.mu.
func (run *agentRun) emitLocked(e agent.Event) {
	v := &run.view
	switch e.Type {
	case "thinking":
		v.thinking.WriteString(e.Text)
	case "content":
		v.content.WriteString(e.Text)
	case "step":
		if e.Step != nil {
			// The runner goes on changing its step; the pages get a copy.
			step := *e.Step
			e.Step = &step
			v.step = &step
		}
	case "entry":
		if e.Entry != nil {
			v.entries = append(v.entries, *e.Entry)
			switch e.Entry.Role {
			case "assistant":
				v.thinking.Reset()
				v.content.Reset()
			case "tool":
				v.step = nil
			}
		}
	case "reset":
		// The round is asked again; what streamed in does not count.
		v.thinking.Reset()
		v.content.Reset()
	case "plan":
		plan := slices.Clone(e.Plan)
		if plan == nil {
			plan = []agent.PlanStep{}
		}
		e.Plan = plan
		v.plan = &plan
	case "approval":
		v.pending = e.Wait
	case "state":
		v.state, v.err = e.State, e.Error
	}
	for sub := range run.subs {
		select {
		case sub.ch <- e:
		default:
			// Too slow: the page gets a snapshot instead of a gap.
			select {
			case sub.wake <- struct{}{}:
			default:
			}
		}
	}
}

// snapshotLocked is the run as a page that comes now needs it. The caller
// holds agentRT.mu.
func (run *agentRun) snapshotLocked() agentSnapshot {
	v := &run.view
	out := agentSnapshot{Type: "snapshot", Entries: slices.Clone(v.entries), Thinking: v.thinking.String(), Content: v.content.String(),
		State: v.state, Error: v.err, Pending: v.pending}
	if out.Entries == nil {
		out.Entries = []agent.Entry{}
	}
	if v.step != nil {
		step := *v.step
		out.Step = &step
	}
	if v.plan != nil {
		plan := slices.Clone(*v.plan)
		out.Plan = &plan
	}
	return out
}

// subscribe adds a page to the run and gives it the snapshot to start with;
// ok is false when the run has ended meanwhile.
func (s *Server) agentSubscribe(run *agentRun) (*agentSub, agentSnapshot, bool) {
	sub := &agentSub{ch: make(chan agent.Event, agentSubBuffer), wake: make(chan struct{}, 1)}
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	if run.subs == nil {
		return nil, agentSnapshot{}, false
	}
	run.subs[sub] = struct{}{}
	return sub, run.snapshotLocked(), true
}

// resync is the snapshot for a page that lost events: what still waits in
// its channel is older than the snapshot and goes. ok is false once the run
// has ended (the channel is closed then).
func (s *Server) agentResync(run *agentRun, sub *agentSub) (agentSnapshot, bool) {
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	if run.subs == nil {
		return agentSnapshot{}, false
	}
	for {
		select {
		case <-sub.ch:
			continue
		default:
		}
		break
	}
	return run.snapshotLocked(), true
}

func (s *Server) agentUnsubscribe(run *agentRun, sub *agentSub) {
	s.agentRT.mu.Lock()
	if run.subs != nil {
		delete(run.subs, sub)
	}
	s.agentRT.mu.Unlock()
}

// handleAgentRuns lists the chats of the account that run right now, from
// the RAM only: the page asks it while a chat it does not show works, to
// learn when that run ends.
func (s *Server) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	limit := s.agentSettings().maxRuns()
	s.agentRT.mu.Lock()
	runs := []string{}
	for _, run := range s.agentRT.runs {
		if run.user == user {
			runs = append(runs, run.chat)
		}
	}
	s.agentRT.mu.Unlock()
	slices.Sort(runs)
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "max": limit})
}

// agentStartError: a chat that already runs is a conflict, not a failure;
// so is an account that runs as many chats as allowed.
func agentStartError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrBusy):
		writeError(w, http.StatusConflict, "agent_busy", "the agent is still working on this chat; stop it first")
	case errors.Is(err, errAgentLimit):
		writeError(w, http.StatusConflict, "agent_limit", "this account runs as many chats at once as the settings allow; wait for one or stop it")
	default:
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
	}
}
