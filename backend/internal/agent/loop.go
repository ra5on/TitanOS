package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A run takes the chat as it is, asks the model, makes the calls it wants
// and asks again, until the model answers without a call, a call waits for
// the person, the step or time limit is reached or the person stops it.
// Every step is saved at once, so a chat never claims more than happened;
// a run that titand did not finish (restart, update) is marked interrupted
// at start.
//
// What makes it an agent rather than a question-answer loop (PLAN 55):
// the context is kept within the model's window by summarising older parts
// (context.go), the model keeps a plan (plan_update), errors of Ollama and
// broken calls are retried or handed back, repeated calls are caught, at a
// limit the agent reports where it stands, a change is checked before the
// answer, tools come by area (tools_load) and a subtask runs on a fresh
// context (task_delegate).

// Prepared is a call that was checked and planned but not run.
type Prepared struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
	// Plan is what runs on approval, exactly; tools without a plan run
	// again with the same arguments.
	Plan      json.RawMessage `json:"plan,omitempty"`
	Level     string          `json:"level,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Area      string          `json:"area,omitempty"`
	Mode      string          `json:"mode"`
	Untrusted bool            `json:"untrusted,omitempty"`
	// Kind is read, create, change or agent (policy.go).
	Kind string `json:"kind,omitempty"`
}

// Outcome is what a call returned.
type Outcome struct {
	Text    string
	Sources []Source
	Files   []FileRef
	// Note is shown as its own line ("memory": something remembered).
	Note, NoteText string
	// Choices end the turn with a question and buttons.
	Choices []string
	// Question is the text that goes with the choices.
	Question string
}

// Toolbox is what package api hands in: the tools of the account with the
// policy applied.
type Toolbox interface {
	Specs(chat *Chat) []ToolSpec
	Prepare(ctx context.Context, chat *Chat, name string, args json.RawMessage) (Prepared, error)
	Execute(ctx context.Context, chat *Chat, p Prepared) (Outcome, error)
}

// Event goes to the open pages of a chat while it runs.
type Event struct {
	Type string `json:"type"`
	// thinking/content: a streamed piece; entry: a saved line; state:
	// the chat state; approval: the pending step; step: a call starts;
	// reset: forget what streamed in (the round is asked again); plan:
	// the plan changed.
	Text  string     `json:"text,omitempty"`
	Entry *Entry     `json:"entry,omitempty"`
	State string     `json:"state,omitempty"`
	Error string     `json:"error,omitempty"`
	Wait  *Pending   `json:"pending,omitempty"`
	Step  *Entry     `json:"step,omitempty"`
	Plan  []PlanStep `json:"plan,omitempty"`
}

// Runner runs one chat.
type Runner struct {
	Ollama    *Ollama
	Model     string
	Think     any
	Context   int
	KeepAlive string
	MaxSteps  int
	// MaxTime ends a run after this long with a report (0: no limit).
	MaxTime time.Duration
	// Window is the context length the model really has (num_ctx, or what
	// Ollama loaded it with); 0 is unknown and keeps the whole chat.
	Window int
	// Probe finds the window when it is not known yet (what Ollama loaded
	// the model with); asked until it answers.
	Probe func(context.Context) int
	// LazyTools offers the core tools and tools_load instead of all.
	LazyTools bool
	// Delegate offers task_delegate (never inside a subtask).
	Delegate bool
	// System is the system prompt for this run (prompt.go).
	System string
	Tools  Toolbox
	Emit   func(Event)
	Save   func(*Chat) error
	// Now replaces the clock in tests.
	Now func() time.Time
	// Retry is the wait before a new try (tests make it short).
	Retry time.Duration

	start time.Time
	// extra are steps of subtasks, counted against the limit.
	extra int
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) emit(e Event) {
	if r.Emit != nil {
		r.Emit(e)
	}
}

func (r *Runner) add(chat *Chat, e Entry) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.At.IsZero() {
		e.At = r.now()
	}
	chat.Entries = append(chat.Entries, e)
	chat.Updated = e.At
	r.emit(Event{Type: "entry", Entry: &chat.Entries[len(chat.Entries)-1]})
	return r.save(chat)
}

func (r *Runner) save(chat *Chat) error {
	if r.Save == nil {
		return nil
	}
	return r.Save(chat)
}

func (r *Runner) setState(chat *Chat, state, msg string) {
	chat.State, chat.Error = state, msg
	r.emit(Event{Type: "state", State: state, Error: msg})
}

// MaxResultText is how much of a tool result goes back to the model.
const MaxResultText = 12000

// olderResultText is how much of a result of an earlier turn it sees.
const olderResultText = 1500

// Messages builds what the model gets from the chat, with the whole chat
// and the default result sizes.
func Messages(system string, chat *Chat) []Message {
	return buildMessages(system, chat, MaxResultText)
}

// Hints the runner gives the model as notes; they are part of the
// conversation for the model, short lines for the person.
const (
	hintVerify = "[TitanOS] Before you answer: check with a reading tool that your change really worked (list the folder, read the file or the state). Then answer briefly. / Prüfe kurz, ob die Änderung wirkt."
	hintLimit  = "[TitanOS] The step limit of this run is reached. Do not call tools now. Report in a few lines: what is done, what is still open, what the next step would be. The person can say \"Weiter\" to go on."
	hintTime   = "[TitanOS] The time limit of this run is reached. Do not call tools now. Report in a few lines: what is done, what is still open, what the next step would be. The person can say \"Weiter\" to go on."
)

func buildMessages(system string, chat *Chat, resultLimit int) []Message {
	if p := planText(chat.Plan); p != "" {
		system += "\n\n" + p
	}
	start, summary := 0, ""
	for i := len(chat.Entries) - 1; i >= 0; i-- {
		if e := chat.Entries[i]; e.Role == "note" && e.Note == "summary" {
			start, summary = i+1, e.Content
			break
		}
	}
	if summary != "" {
		system += "\n\nSummary of the earlier part of this chat (older messages are not shown any more):\n" + summary
	}
	msgs := []Message{{Role: "system", Content: system}}
	lastUser := lastUserIndex(chat)
	// The task of the running turn stays, even when it was summarised.
	if lastUser >= 0 && lastUser < start {
		msgs = append(msgs, Message{Role: "user", Content: chat.Entries[lastUser].Content})
	}
	for i := start; i < len(chat.Entries); i++ {
		e := chat.Entries[i]
		switch e.Role {
		case "user":
			msgs = append(msgs, Message{Role: "user", Content: e.Content})
		case "assistant":
			m := Message{Role: "assistant", Content: e.Content, ToolCalls: e.Calls}
			// Thinking of the running turn helps the model continue after
			// a tool call; older thoughts only cost context.
			if i > lastUser {
				m.Thinking = e.Thinking
			}
			if m.Content == "" && len(m.ToolCalls) == 0 && m.Thinking == "" {
				continue
			}
			msgs = append(msgs, m)
		case "tool":
			text := e.Result
			limit := resultLimit
			if i < lastUser {
				limit = min(olderResultText, resultLimit)
			}
			if r := []rune(text); len(r) > limit {
				text = string(r[:limit]) + "\n[… gekürzt / cut]"
			}
			msgs = append(msgs, Message{Role: "tool", ToolName: e.Tool, Content: text})
		case "note":
			// A decision of the person is part of the conversation.
			switch e.Note {
			case "declined":
				msgs = append(msgs, Message{Role: "tool", ToolName: e.Tool, Content: "The person declined this step. Do not try it another way; ask what they want instead. / Die Person hat abgelehnt."})
			case "stopped":
				msgs = append(msgs, Message{Role: "user", Content: "[Stopped by the person / von der Person angehalten]"})
			case "verify":
				msgs = append(msgs, Message{Role: "user", Content: hintVerify})
			case "badcall":
				msgs = append(msgs, Message{Role: "user", Content: "[TitanOS] Your last tool call could not be read (" + e.Content + "). Call the tool again with valid JSON arguments, or answer without a tool."})
			case "limitask":
				msgs = append(msgs, Message{Role: "user", Content: hintLimit})
			case "timeask":
				msgs = append(msgs, Message{Role: "user", Content: hintTime})
			}
		}
	}
	return msgs
}

// ErrBusy is a chat that already runs.
var ErrBusy = errors.New("agent: chat is running")

// Run works on the chat until it is done or waits.
func (r *Runner) Run(ctx context.Context, chat *Chat) error {
	r.setState(chat, StateRunning, "")
	if err := r.save(chat); err != nil {
		return err
	}
	if r.start.IsZero() {
		r.start = r.now()
	}
	// A new question starts without the plan of the last task.
	if i := lastUserIndex(chat); i >= 0 && i == len(chat.Entries)-1 && len(chat.Plan) > 0 {
		chat.Plan = nil
	}
	maxSteps := r.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 30
	}
	// Keeping the plan and loading tools are bookkeeping, not steps of the
	// task (PLAN 59: small tasks were twice as long because of them).
	// Beyond freeBookkeeping per task they count again, so that a model
	// that only rewrites its plan still ends.
	steps, kept := 0, 0
	count := func(tool string) {
		if bookkeeping(tool) && kept < freeBookkeeping {
			kept++
			return
		}
		steps++
	}
	for _, e := range chat.Entries[lastUserIndex(chat)+1:] {
		if e.Role == "tool" {
			count(e.Tool)
		}
	}
	badCalls := 0
	for {
		if err := ctx.Err(); err != nil {
			return r.stopped(chat)
		}
		if steps+r.extra >= maxSteps {
			return r.report(ctx, chat, "limitask", "limit", fmt.Sprintf("%d", maxSteps))
		}
		if r.MaxTime > 0 && r.now().Sub(r.start) >= r.MaxTime {
			return r.report(ctx, chat, "timeask", "timelimit", fmt.Sprintf("%d", int(r.MaxTime.Minutes())))
		}
		r.compact(ctx, chat)
		msg, stats, err := r.ask(ctx, chat, r.offered(chat))
		if err != nil {
			if ctx.Err() != nil {
				return r.stopped(chat)
			}
			// A call the model wrote broken goes back to it, twice at most.
			if isBadCall(err) && badCalls < 2 {
				badCalls++
				if err := r.add(chat, Entry{Role: "note", Note: "badcall", Content: shortError(err)}); err != nil {
					return err
				}
				continue
			}
			// The error of the model is what counts; saving is best effort.
			_ = r.add(chat, Entry{Role: "note", Note: "error", Content: err.Error()})
			r.setState(chat, StateError, err.Error())
			_ = r.save(chat)
			return err
		}
		st := stats
		if err := r.add(chat, Entry{Role: "assistant", Content: msg.Content, Thinking: msg.Thinking, Calls: msg.ToolCalls, Stats: &st}); err != nil {
			return err
		}
		if len(msg.ToolCalls) == 0 {
			// A change without a look afterwards: check once before the end.
			if needsCheck(chat) && steps+r.extra < maxSteps {
				if err := r.add(chat, Entry{Role: "note", Note: "verify"}); err != nil {
					return err
				}
				continue
			}
			r.setState(chat, StateIdle, "")
			return r.save(chat)
		}
		for i, call := range msg.ToolCalls {
			count(call.Function.Name)
			done, err := r.call(ctx, chat, call.Function.Name, call.Function.Arguments)
			if err != nil {
				return err
			}
			if done {
				// Waiting for the person, a question or a stop: the other
				// calls of this round are not made.
				if chat.State == StateWaiting {
					for _, rest := range msg.ToolCalls[i+1:] {
						chat.Entries = append(chat.Entries, Entry{ID: NewID(), At: r.now(), Role: "tool", Tool: rest.Function.Name, Args: rest.Function.Arguments, Result: "Not run: an earlier step of this round waits for the person. Call again later if still needed."})
					}
				}
				return r.save(chat)
			}
		}
	}
}

// ask sends one round, and tries again when Ollama is not reachable or
// fails on its side (5xx), up to three times. What streamed in before is
// forgotten by the pages (event reset).
func (r *Runner) ask(ctx context.Context, chat *Chat, tools []ToolSpec) (Message, Stats, error) {
	wait := r.Retry
	if wait <= 0 {
		wait = 2 * time.Second
	}
	var lastErr error
	for try := 0; try < 3; try++ {
		if try > 0 {
			r.emit(Event{Type: "reset"})
			select {
			case <-ctx.Done():
				return Message{}, Stats{}, ctx.Err()
			case <-time.After(wait):
			}
			wait *= 3
		}
		msg, stats, err := r.Ollama.Chat(ctx, ChatRequest{Model: r.Model, Messages: buildMessages(r.System, chat, r.resultLimit()), Tools: tools, Think: r.Think, Context: r.Context, KeepAlive: r.KeepAlive}, func(c Chunk) {
			if c.Thinking != "" {
				r.emit(Event{Type: "thinking", Text: c.Thinking})
			}
			if c.Content != "" {
				r.emit(Event{Type: "content", Text: c.Content})
			}
		})
		if err == nil || ctx.Err() != nil || !transient(err) {
			return msg, stats, err
		}
		lastErr = err
	}
	return Message{}, Stats{}, lastErr
}

// transient errors are worth a new try: no answer, or a failure of the
// server that is not about the call the model wrote.
func transient(err error) bool {
	if errors.Is(err, ErrUnreachable) {
		return true
	}
	var se *StatusError
	if errors.As(err, &se) && se.Status >= 500 && !isBadCall(err) {
		return true
	}
	// The model runner of Ollama died in the middle of the answer.
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "error was encountered while running the model") || strings.Contains(s, "runner has unexpectedly stopped")
}

// isBadCall is Ollama failing to read the tool call the model wrote.
func isBadCall(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "tool call") && (strings.Contains(s, "pars") || strings.Contains(s, "invalid") || strings.Contains(s, "unmarshal"))
}

func shortError(err error) string {
	s := err.Error()
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// report ends a run at a limit: one round without tools for the state of
// the work, then the note with "Weiter".
func (r *Runner) report(ctx context.Context, chat *Chat, ask, note, value string) error {
	if err := r.add(chat, Entry{Role: "note", Note: ask}); err != nil {
		return err
	}
	if msg, stats, err := r.ask(ctx, chat, nil); err == nil && strings.TrimSpace(msg.Content) != "" {
		st := stats
		if err := r.add(chat, Entry{Role: "assistant", Content: msg.Content, Thinking: msg.Thinking, Stats: &st}); err != nil {
			return err
		}
	} else if ctx.Err() != nil {
		return r.stopped(chat)
	}
	if err := r.add(chat, Entry{Role: "note", Note: note, Content: value}); err != nil {
		return err
	}
	r.setState(chat, StateIdle, "")
	return r.save(chat)
}

const freeBookkeeping = 8

func bookkeeping(tool string) bool { return tool == toolPlan || tool == toolLoad }

// needsCheck: in this turn something was changed and nothing was read
// after it, and the runner has not asked for a check yet.
func needsCheck(chat *Chat) bool {
	changed := false
	for _, e := range chat.Entries[lastUserIndex(chat)+1:] {
		switch {
		case e.Role == "note" && e.Note == "verify":
			return false
		case e.Role == "tool" && e.OK && (e.Kind == KindChange || e.Kind == KindCreate):
			changed = true
		case e.Role == "tool" && e.OK && e.Kind == KindRead:
			changed = false
		}
	}
	return changed
}

func lastUserIndex(chat *Chat) int {
	for i := len(chat.Entries) - 1; i >= 0; i-- {
		if chat.Entries[i].Role == "user" {
			return i
		}
	}
	return -1
}

func (r *Runner) stopped(chat *Chat) error {
	chat.Pending = nil
	if err := r.add(chat, Entry{Role: "note", Note: "stopped"}); err != nil {
		return err
	}
	r.setState(chat, StateIdle, "")
	return r.save(chat)
}

// repeats counts the calls of this turn with exactly this tool and these
// arguments.
func repeats(chat *Chat, name string, args json.RawMessage) int {
	key := canonical(args)
	n := 0
	for _, e := range chat.Entries[lastUserIndex(chat)+1:] {
		if e.Role == "tool" && e.Tool == name && canonical(e.Args) == key {
			n++
		}
	}
	return n
}

func canonical(args json.RawMessage) string {
	var v any
	if json.Unmarshal(args, &v) != nil {
		return string(args)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// call checks, plans and runs one call. done means the run ends here
// (waiting for approval, a question to the person, a stop).
func (r *Runner) call(ctx context.Context, chat *Chat, name string, args json.RawMessage) (bool, error) {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	step := Entry{ID: NewID(), At: r.now(), Role: "tool", Tool: name, Args: args}
	r.emit(Event{Type: "step", Step: &step})
	// The same call again and again: say so, and stop at the fifth.
	switch n := repeats(chat, name, args); {
	case n >= 4:
		step.Result = "Not run: the same call was made 4 times in this task."
		if err := r.add(chat, step); err != nil {
			return false, err
		}
		if err := r.add(chat, Entry{Role: "note", Note: "loop", Tool: name}); err != nil {
			return false, err
		}
		r.setState(chat, StateIdle, "")
		return true, r.save(chat)
	case n >= 2:
		step.Result = fmt.Sprintf("Not run: you already made exactly this call %d times in this task. Use the result you have, try another way, or ask the person with ask_user.", n)
		return false, r.add(chat, step)
	}
	if out, ok, err := r.builtin(ctx, chat, name, args); ok {
		if err != nil {
			step.Result = "Error: " + err.Error()
			return false, r.add(chat, step)
		}
		step.OK, step.Result, step.Kind = true, out, KindAgent
		return false, r.add(chat, step)
	}
	p, err := r.Tools.Prepare(ctx, chat, name, args)
	if err != nil {
		step.Result = "Error: " + err.Error()
		return false, r.add(chat, step)
	}
	r.loadArea(chat, p.Area)
	if p.Mode == ModeAsk {
		chat.Pending = &Pending{ID: NewID(), Tool: p.Tool, Args: p.Args, Plan: p.Plan, Level: p.Level, Summary: p.Summary, Area: p.Area, Kind: p.Kind, At: r.now()}
		r.setState(chat, StateWaiting, "")
		r.emit(Event{Type: "approval", Wait: chat.Pending})
		return true, r.save(chat)
	}
	return r.execute(ctx, chat, p, step)
}

func (r *Runner) execute(ctx context.Context, chat *Chat, p Prepared, step Entry) (bool, error) {
	start := time.Now()
	out, err := r.Tools.Execute(ctx, chat, p)
	step.Duration = time.Since(start).Milliseconds()
	step.Level, step.Summary, step.Kind = p.Level, p.Summary, p.Kind
	if p.Untrusted {
		chat.Tainted = true
	}
	if err != nil {
		if ctx.Err() != nil {
			return true, r.stopped(chat)
		}
		step.Result = "Error: " + err.Error()
		return false, r.add(chat, step)
	}
	step.OK, step.Result, step.Sources, step.Files = true, out.Text, out.Sources, out.Files
	if err := r.add(chat, step); err != nil {
		return false, err
	}
	if out.Note != "" {
		if err := r.add(chat, Entry{Role: "note", Note: out.Note, Content: out.NoteText}); err != nil {
			return false, err
		}
	}
	if len(out.Choices) > 0 {
		if err := r.add(chat, Entry{Role: "assistant", Content: out.Question, Choices: out.Choices}); err != nil {
			return false, err
		}
		r.setState(chat, StateIdle, "")
		return true, r.save(chat)
	}
	return false, nil
}

// Decide answers a pending step: approve runs exactly what was planned,
// decline tells the model. Then the run goes on.
func (r *Runner) Decide(ctx context.Context, chat *Chat, id string, approve bool) error {
	p := chat.Pending
	if p == nil || p.ID != id {
		return ErrNotFound
	}
	chat.Pending = nil
	if !approve {
		if err := r.add(chat, Entry{Role: "note", Note: "declined", Tool: p.Tool, Summary: p.Summary}); err != nil {
			return err
		}
		return r.Run(ctx, chat)
	}
	if err := r.add(chat, Entry{Role: "note", Note: "approved", Tool: p.Tool, Summary: p.Summary}); err != nil {
		return err
	}
	r.setState(chat, StateRunning, "")
	step := Entry{ID: NewID(), At: r.now(), Role: "tool", Tool: p.Tool, Args: p.Args}
	r.emit(Event{Type: "step", Step: &step})
	done, err := r.execute(ctx, chat, Prepared{Tool: p.Tool, Args: p.Args, Plan: p.Plan, Level: p.Level, Summary: p.Summary, Area: p.Area, Kind: p.Kind, Mode: ModeAllow}, step)
	if err != nil || done {
		return err
	}
	return r.Run(ctx, chat)
}
