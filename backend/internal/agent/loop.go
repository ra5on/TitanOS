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
// the person, the step limit is reached or the person stops it. Every step
// is saved at once, so a chat never claims more than happened; a run that
// titand did not finish (restart, update) is marked interrupted at start.

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
	// the chat state; approval: the pending step; step: a call starts.
	Text  string   `json:"text,omitempty"`
	Entry *Entry   `json:"entry,omitempty"`
	State string   `json:"state,omitempty"`
	Error string   `json:"error,omitempty"`
	Wait  *Pending `json:"pending,omitempty"`
	Step  *Entry   `json:"step,omitempty"`
}

// Runner runs one chat.
type Runner struct {
	Ollama   *Ollama
	Model    string
	Think    any
	Context  int
	MaxSteps int
	// System is the system prompt for this run (prompt.go).
	System string
	Tools  Toolbox
	Emit   func(Event)
	Save   func(*Chat) error
	// Now replaces the clock in tests.
	Now func() time.Time
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

// Messages builds what the model gets from the chat.
func Messages(system string, chat *Chat) []Message {
	msgs := []Message{{Role: "system", Content: system}}
	lastUser := -1
	for i, e := range chat.Entries {
		if e.Role == "user" {
			lastUser = i
		}
	}
	for i, e := range chat.Entries {
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
			limit := MaxResultText
			if i < lastUser {
				limit = olderResultText
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
	maxSteps := r.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 30
	}
	steps := 0
	for _, e := range chat.Entries[lastUserIndex(chat)+1:] {
		if e.Role == "tool" {
			steps++
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return r.stopped(chat)
		}
		if steps >= maxSteps {
			if err := r.add(chat, Entry{Role: "note", Note: "limit", Content: fmt.Sprintf("%d", maxSteps)}); err != nil {
				return err
			}
			r.setState(chat, StateIdle, "")
			return r.save(chat)
		}
		tools := r.Tools.Specs(chat)
		var live strings.Builder
		msg, stats, err := r.Ollama.Chat(ctx, ChatRequest{Model: r.Model, Messages: Messages(r.System, chat), Tools: tools, Think: r.Think, Context: r.Context}, func(c Chunk) {
			if c.Thinking != "" {
				r.emit(Event{Type: "thinking", Text: c.Thinking})
			}
			if c.Content != "" {
				live.WriteString(c.Content)
				r.emit(Event{Type: "content", Text: c.Content})
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				return r.stopped(chat)
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
			r.setState(chat, StateIdle, "")
			return r.save(chat)
		}
		for i, call := range msg.ToolCalls {
			steps++
			done, err := r.call(ctx, chat, call.Function.Name, call.Function.Arguments)
			if err != nil {
				return err
			}
			if done {
				// Waiting for the person or a question: the other calls
				// of this round are not made.
				for _, rest := range msg.ToolCalls[i+1:] {
					chat.Entries = append(chat.Entries, Entry{ID: NewID(), At: r.now(), Role: "tool", Tool: rest.Function.Name, Args: rest.Function.Arguments, Result: "Not run: an earlier step of this round waits for the person. Call again later if still needed."})
				}
				return r.save(chat)
			}
		}
	}
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

// call checks, plans and runs one call. done means the run ends here
// (waiting for approval or a question to the person).
func (r *Runner) call(ctx context.Context, chat *Chat, name string, args json.RawMessage) (bool, error) {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	step := Entry{ID: NewID(), At: r.now(), Role: "tool", Tool: name, Args: args}
	r.emit(Event{Type: "step", Step: &step})
	p, err := r.Tools.Prepare(ctx, chat, name, args)
	if err != nil {
		step.Result = "Error: " + err.Error()
		return false, r.add(chat, step)
	}
	if p.Mode == ModeAsk {
		chat.Pending = &Pending{ID: NewID(), Tool: p.Tool, Args: p.Args, Plan: p.Plan, Level: p.Level, Summary: p.Summary, Area: p.Area, At: r.now()}
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
	step.Level, step.Summary = p.Level, p.Summary
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
	done, err := r.execute(ctx, chat, Prepared{Tool: p.Tool, Args: p.Args, Plan: p.Plan, Level: p.Level, Summary: p.Summary, Area: p.Area, Mode: ModeAllow}, step)
	if err != nil || done {
		return err
	}
	return r.Run(ctx, chat)
}
