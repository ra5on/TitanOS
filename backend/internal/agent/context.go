package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Keeping the work within the model (PLAN 55): a small local model has a
// small window, and Ollama cuts a too long request at the front, where the
// rules are. So the runner measures, summarises older parts in time, keeps
// a plan, offers tools by area and runs subtasks on a fresh context.

// PlanStep is one point of the plan the agent keeps for a task.
type PlanStep struct {
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"`
}

// Built-in tools of the runner; package api has none of these names.
const (
	toolPlan     = "plan_update"
	toolLoad     = "tools_load"
	toolDelegate = "task_delegate"
)

// coreAreas are always offered when tools come by area.
var coreAreas = []string{AreaAgent, AreaWeb, "files", "system"}

// lazyFrom: with fewer tools than this, all are offered anyway.
const lazyFrom = 30

// compactAt is the share of the window from which older parts are
// summarised; keepShare what stays word for word.
const (
	compactAt = 0.70
	keepShare = 0.35
)

// tokens estimates the tokens of a request. German text with JSON is about
// three characters a token; erring high only summarises a bit earlier.
func tokens(msgs []Message, tools []ToolSpec) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content) + len(m.Thinking) + 16
		for _, c := range m.ToolCalls {
			n += len(c.Function.Name) + len(c.Function.Arguments)
		}
	}
	if len(tools) > 0 {
		b, _ := json.Marshal(tools)
		n += len(b)
	}
	return n / 3
}

// resultLimit is how much of one tool result the model sees: at most a
// quarter of the window, so one big listing cannot push out the rest.
func (r *Runner) resultLimit() int {
	if r.Window <= 0 {
		return MaxResultText
	}
	return max(1500, min(MaxResultText, r.Window*3/4))
}

// compact summarises the older part of the chat when the next request
// would fill more than compactAt of the window. The summary is a note in
// the chat ("Zusammengefasst"); the person still sees every line.
func (r *Runner) compact(ctx context.Context, chat *Chat) {
	if r.Window <= 0 && r.Probe != nil {
		r.Window = r.Probe(ctx)
	}
	if r.Window <= 0 {
		return
	}
	limit := r.resultLimit()
	if tokens(buildMessages(r.System, chat, limit), r.offered(chat)) < int(float64(r.Window)*compactAt) {
		return
	}
	from := 0
	prev := ""
	for i := len(chat.Entries) - 1; i >= 0; i-- {
		if e := chat.Entries[i]; e.Role == "note" && e.Note == "summary" {
			from, prev = i+1, e.Content
			break
		}
	}
	// Cut where a round begins (never between a call and its result), as
	// late as needed for the rest to fit keepShare of the window.
	keep := int(float64(r.Window) * keepShare)
	cut := -1
	size := 0
	for i := len(chat.Entries) - 1; i > from; i-- {
		e := chat.Entries[i]
		size += (len(e.Content) + min(len(e.Result), limit) + len(e.Args)) / 3
		if size > keep {
			break
		}
		if e.Role == "user" || (e.Role == "assistant" && i > 0 && chat.Entries[i-1].Role != "assistant") {
			cut = i
		}
	}
	if cut < 0 {
		// Even the last round is too big: summarise up to it.
		for i := len(chat.Entries) - 1; i > from; i-- {
			if e := chat.Entries[i]; e.Role == "assistant" && len(e.Calls) > 0 {
				cut = i
				break
			}
		}
	}
	if cut <= from {
		return
	}
	text := r.summarise(ctx, prev, chat.Entries[from:cut])
	note := Entry{ID: NewID(), At: r.now(), Role: "note", Note: "summary", Content: text}
	chat.Entries = slices.Insert(chat.Entries, cut, note)
	_ = r.save(chat)
}

// summarise asks the model for a short summary; without an answer the
// questions and the last answers are kept as they are.
func (r *Runner) summarise(ctx context.Context, prev string, part []Entry) string {
	var b strings.Builder
	budget := max(4000, r.Window*3/2)
	if prev != "" {
		b.WriteString("Earlier summary:\n" + prev + "\n\n")
	}
	for _, e := range part {
		line := ""
		switch e.Role {
		case "user":
			line = "Person: " + e.Content
		case "assistant":
			line = "Agent: " + e.Content
			for _, c := range e.Calls {
				line += fmt.Sprintf(" [calls %s %s]", c.Function.Name, clip(string(c.Function.Arguments), 200))
			}
		case "tool":
			line = fmt.Sprintf("Result of %s: %s", e.Tool, clip(e.Result, 600))
		case "note":
			if e.Note == "summary" || e.Content == "" {
				continue
			}
			line = "Note " + e.Note + ": " + e.Content
		}
		if b.Len()+len(line) > budget {
			b.WriteString("[…]\n")
			continue
		}
		b.WriteString(line + "\n")
	}
	msgs := []Message{
		{Role: "system", Content: "You summarise a part of a conversation between a person and the TitanOS agent so that the agent can go on without it. Keep: what the person wants, facts they told (names, preferences), decisions, what was done and changed (with paths and names), results that are still needed, open points. Drop chatter and long listings. At most 250 words, in the language of the conversation, as a short list."},
		{Role: "user", Content: b.String()},
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	msg, _, err := r.Ollama.Chat(sctx, ChatRequest{Model: r.Model, Messages: msgs, Context: r.Context, KeepAlive: r.KeepAlive}, nil)
	if err == nil && strings.TrimSpace(msg.Content) != "" {
		return strings.TrimSpace(msg.Content)
	}
	// Mechanical: the questions of the person and the answers.
	var m strings.Builder
	if prev != "" {
		m.WriteString(prev + "\n")
	}
	for _, e := range part {
		if (e.Role == "user" || e.Role == "assistant") && strings.TrimSpace(e.Content) != "" {
			who := "Agent"
			if e.Role == "user" {
				who = "Person"
			}
			m.WriteString("- " + who + ": " + clip(e.Content, 300) + "\n")
		}
	}
	return clip(m.String(), 4000)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// planText is the plan for the system prompt.
func planText(plan []PlanStep) string {
	if len(plan) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Your plan for the current task (update it with plan_update as you go):\n")
	for _, s := range plan {
		mark := "[ ]"
		if s.Done {
			mark = "[x]"
		}
		b.WriteString(mark + " " + s.Text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// offered are the tools of this round: all, or with LazyTools the core
// areas, the areas loaded in this chat and the built-in ones.
func (r *Runner) offered(chat *Chat) []ToolSpec {
	all := r.Tools.Specs(chat)
	out := []ToolSpec{builtinSpec(toolPlan, "Write down or update your plan for a task with several steps: the whole list each time, with done set for finished steps. Use it at the start of a bigger task and after each finished step.",
		map[string]any{"type": "object", "properties": map[string]any{"steps": map[string]any{"type": "array", "maxItems": 12, "items": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}, "done": map[string]any{"type": "boolean"}}, "required": []string{"text"}}}}, "required": []string{"steps"}})}
	if r.Delegate {
		out = append(out, builtinSpec(toolDelegate, "Hand an investigation to a helper that starts with an empty context and only reads (lists, files, logs, states), e.g. \"look through the logs of all apps for errors\". You get its short report. Use it for work that would read much; it changes nothing.",
			map[string]any{"type": "object", "properties": map[string]any{"task": map[string]any{"type": "string", "description": "The task, complete in itself."}}, "required": []string{"task"}}))
	}
	if !r.LazyTools || len(all) < lazyFrom {
		return append(out, all...)
	}
	loaded := map[string]bool{}
	for _, a := range coreAreas {
		loaded[a] = true
	}
	for _, a := range chat.Areas {
		loaded[a] = true
	}
	if chat.Origin == "homeassistant" {
		loaded["home"] = true
	}
	rest := map[string][]string{}
	for _, s := range all {
		if s.Area == "" || loaded[s.Area] {
			out = append(out, s)
		} else {
			rest[s.Area] = append(rest[s.Area], s.Function.Name)
		}
	}
	if len(rest) > 0 {
		areas := make([]string, 0, len(rest))
		for a := range rest {
			areas = append(areas, a)
		}
		sort.Strings(areas)
		var desc strings.Builder
		desc.WriteString("Load more tools of an area for this chat. Areas not loaded yet: ")
		for i, a := range areas {
			if i > 0 {
				desc.WriteString("; ")
			}
			names := rest[a]
			if len(names) > 6 {
				names = append(slices.Clone(names[:6]), "…")
			}
			desc.WriteString(a + " (" + strings.Join(names, ", ") + ")")
		}
		out = append(out, builtinSpec(toolLoad, desc.String(),
			map[string]any{"type": "object", "properties": map[string]any{"area": map[string]any{"type": "string", "enum": areas}}, "required": []string{"area"}}))
	}
	return out
}

func builtinSpec(name, description string, params map[string]any) ToolSpec {
	var s ToolSpec
	s.Type = "function"
	s.Function.Name = name
	s.Function.Description = description
	s.Function.Parameters = params
	s.Area = AreaAgent
	s.Kind = KindAgent
	return s
}

// loadArea remembers an area once a tool of it was used.
func (r *Runner) loadArea(chat *Chat, area string) {
	if !r.LazyTools || area == "" || slices.Contains(coreAreas, area) || slices.Contains(chat.Areas, area) {
		return
	}
	chat.Areas = append(chat.Areas, area)
}

// builtin runs a tool of the runner itself; ok is false for other tools.
func (r *Runner) builtin(ctx context.Context, chat *Chat, name string, args json.RawMessage) (string, bool, error) {
	switch name {
	case toolPlan:
		var req struct {
			Steps []PlanStep `json:"steps"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return "", true, fmt.Errorf("steps must be a list of {text, done}: %v", err)
		}
		plan := []PlanStep{}
		for _, s := range req.Steps {
			if t := strings.TrimSpace(s.Text); t != "" && len(plan) < 12 {
				plan = append(plan, PlanStep{Text: clip(t, 200), Done: s.Done})
			}
		}
		chat.Plan = plan
		r.emit(Event{Type: "plan", Plan: plan})
		open := 0
		for _, s := range plan {
			if !s.Done {
				open++
			}
		}
		return fmt.Sprintf("Plan saved: %d steps, %d open.", len(plan), open), true, nil
	case toolLoad:
		var req struct {
			Area string `json:"area"`
		}
		_ = json.Unmarshal(args, &req)
		var names []string
		for _, s := range r.Tools.Specs(chat) {
			if s.Area == req.Area {
				names = append(names, s.Function.Name)
			}
		}
		if len(names) == 0 {
			return "", true, fmt.Errorf("there is no area %q with tools for you", req.Area)
		}
		if !slices.Contains(chat.Areas, req.Area) {
			chat.Areas = append(chat.Areas, req.Area)
		}
		return "Loaded for this chat: " + strings.Join(names, ", "), true, nil
	case toolDelegate:
		if !r.Delegate {
			return "", false, nil
		}
		var req struct {
			Task string `json:"task"`
		}
		if err := json.Unmarshal(args, &req); err != nil || strings.TrimSpace(req.Task) == "" {
			return "", true, errors.New("task is missing")
		}
		out, err := r.delegate(ctx, chat, req.Task)
		return out, true, err
	}
	return "", false, nil
}

// delegate runs a subtask: a fresh chat with only the task, reading tools
// only (it cannot wait for approval), its steps count for the parent.
func (r *Runner) delegate(ctx context.Context, parent *Chat, task string) (string, error) {
	sub := &Chat{ID: NewID(), Origin: parent.Origin, Tainted: parent.Tainted, Areas: slices.Clone(parent.Areas), State: StateIdle,
		Entries: []Entry{{ID: NewID(), At: r.now(), Role: "user", Content: task + "\n\nOnly read and look; you cannot change anything. End with a short report of what you found (with names and paths)."}}}
	budget := 12
	if left := r.MaxSteps - r.extra; r.MaxSteps > 0 && left < budget {
		budget = max(3, left)
	}
	child := &Runner{Ollama: r.Ollama, Model: r.Model, Think: r.Think, Context: r.Context, KeepAlive: r.KeepAlive, MaxSteps: budget, Window: r.Window, LazyTools: r.LazyTools,
		System: r.System, Tools: readOnly{r.Tools}, Now: r.Now, Retry: r.Retry,
		Emit: func(e Event) {
			// The person sees the steps of the helper as they happen.
			if e.Type == "step" {
				r.emit(e)
			}
		}}
	if r.MaxTime > 0 {
		child.MaxTime = max(r.MaxTime-r.now().Sub(r.start), time.Minute)
	}
	err := child.Run(ctx, sub)
	if sub.Tainted {
		parent.Tainted = true
	}
	n := 0
	answer := ""
	for _, e := range sub.Entries {
		if e.Role == "tool" {
			n++
		}
		if e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
			answer = e.Content
		}
	}
	r.extra += n
	if err != nil && answer == "" {
		return "", fmt.Errorf("the helper failed: %v", err)
	}
	if answer == "" {
		answer = "The helper found nothing to report."
	}
	return fmt.Sprintf("Report of the helper (%d steps):\n%s", n, answer), nil
}

// readOnly offers and allows only reading tools.
type readOnly struct{ Toolbox }

func (b readOnly) Specs(chat *Chat) []ToolSpec {
	var out []ToolSpec
	for _, s := range b.Toolbox.Specs(chat) {
		if s.Kind == KindRead {
			out = append(out, s)
		}
	}
	return out
}

func (b readOnly) Prepare(ctx context.Context, chat *Chat, name string, args json.RawMessage) (Prepared, error) {
	p, err := b.Toolbox.Prepare(ctx, chat, name, args)
	if err != nil {
		return p, err
	}
	if p.Kind != KindRead {
		return Prepared{}, errors.New("a helper only reads; report back what should be changed")
	}
	if p.Mode == ModeAsk {
		return Prepared{}, errors.New("this needs the approval of the person; report back instead")
	}
	return p, nil
}
