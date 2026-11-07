package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// scripted answers /api/chat from a function of the request, so a test can
// fail rounds or answer by what it was asked.
type scripted struct {
	mu   sync.Mutex
	got  []map[string]any
	next func(n int, body map[string]any) (status int, messages []string)
}

func (s *scripted) serve(t *testing.T) *Ollama {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.got = append(s.got, body)
		n := len(s.got) - 1
		s.mu.Unlock()
		status, msgs := s.next(n, body)
		if status != http.StatusOK {
			// An error text can come as the only message.
			text := http.StatusText(status)
			if len(msgs) == 1 {
				text = msgs[0]
			}
			b, _ := json.Marshal(map[string]string{"error": text})
			w.WriteHeader(status)
			w.Write(b)
			return
		}
		for _, m := range msgs {
			w.Write([]byte(`{"message":` + m + `,"done":false}` + "\n"))
		}
		w.Write([]byte(`{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":100,"eval_count":5}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	o, err := NewOllama(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func answer(text string) string {
	b, _ := json.Marshal(map[string]string{"role": "assistant", "content": text})
	return string(b)
}

func systemOf(body map[string]any) string {
	msgs := body["messages"].([]any)
	return msgs[0].(map[string]any)["content"].(string)
}

func toolNames(body map[string]any) []string {
	var out []string
	list, _ := body["tools"].([]any)
	for _, t := range list {
		out = append(out, t.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	return out
}

// kindTools: many tools in areas; "make" changes, "see" reads.
type kindTools struct {
	mu  sync.Mutex
	ran []string
}

func (k *kindTools) Specs(*Chat) []ToolSpec {
	var out []ToolSpec
	add := func(name, area, kind string) {
		var s ToolSpec
		s.Type = "function"
		s.Function.Name = name
		s.Area, s.Kind = area, kind
		out = append(out, s)
	}
	add("see", "files", KindRead)
	add("make", "files", KindChange)
	for i := 0; i < 40; i++ {
		add(fmt.Sprintf("docker_%d", i), "docker", KindRead)
	}
	return out
}

func (k *kindTools) Prepare(_ context.Context, _ *Chat, name string, args json.RawMessage) (Prepared, error) {
	switch {
	case name == "see":
		return Prepared{Tool: name, Args: args, Mode: ModeAllow, Kind: KindRead, Area: "files"}, nil
	case name == "make":
		return Prepared{Tool: name, Args: args, Mode: ModeAllow, Kind: KindChange, Area: "files"}, nil
	case strings.HasPrefix(name, "docker_"):
		return Prepared{Tool: name, Args: args, Mode: ModeAllow, Kind: KindRead, Area: "docker"}, nil
	}
	return Prepared{}, errors.New("unknown tool " + name)
}

func (k *kindTools) Execute(_ context.Context, _ *Chat, p Prepared) (Outcome, error) {
	k.mu.Lock()
	k.ran = append(k.ran, p.Tool)
	k.mu.Unlock()
	return Outcome{Text: "ok " + p.Tool}, nil
}

func TestCompactKeepsRulesTaskAndFits(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		if body["tools"] == nil && strings.Contains(systemOf(body), "You summarise") {
			return 200, []string{answer("- Der Hund heißt Bruno.\n- Protokolle waren unauffällig.")}
		}
		return 200, []string{answer("Freigaben: Docs. Dein Hund heißt Bruno.")}
	}}
	chat := &Chat{ID: NewID()}
	chat.Entries = append(chat.Entries, Entry{ID: NewID(), Role: "user", Content: "Mein Hund heißt Bruno."}, Entry{ID: NewID(), Role: "assistant", Content: "Gut."})
	big := strings.Repeat("Zeile ohne Befund. ", 400)
	for i := 0; i < 10; i++ {
		var c ToolCall
		c.Function.Name, c.Function.Arguments = "see", json.RawMessage(`{}`)
		chat.Entries = append(chat.Entries,
			Entry{ID: NewID(), Role: "user", Content: fmt.Sprintf("Teil %d?", i)},
			Entry{ID: NewID(), Role: "assistant", Calls: []ToolCall{c}},
			Entry{ID: NewID(), Role: "tool", Tool: "see", OK: true, Result: big},
			Entry{ID: NewID(), Role: "assistant", Content: "Unauffällig."})
	}
	chat.Entries = append(chat.Entries, Entry{ID: NewID(), Role: "user", Content: "Wie heißt mein Hund?"})
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &kindTools{}, System: "RULES", Window: 4096}
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	var summary *Entry
	for i := range chat.Entries {
		if chat.Entries[i].Note == "summary" {
			summary = &chat.Entries[i]
		}
	}
	if summary == nil || !strings.Contains(summary.Content, "Bruno") {
		t.Fatalf("no summary: %+v", summary)
	}
	last := s.got[len(s.got)-1]
	sys := systemOf(last)
	if !strings.HasPrefix(sys, "RULES") || !strings.Contains(sys, "Bruno") {
		t.Fatalf("system of the last round: %.300s", sys)
	}
	msgs := last["messages"].([]any)
	if q := msgs[len(msgs)-1].(map[string]any)["content"]; q != "Wie heißt mein Hund?" {
		t.Fatalf("task lost: %v", q)
	}
	// What was sent fits the window (estimate).
	b, _ := json.Marshal(last)
	if len(b)/3 > 4096 {
		t.Fatalf("request still %d tokens", len(b)/3)
	}
	// No call is separated from its result.
	for i, m := range msgs {
		if mm := m.(map[string]any); mm["role"] == "tool" {
			prev := msgs[i-1].(map[string]any)
			if prev["role"] != "assistant" && prev["role"] != "tool" {
				t.Fatalf("tool result without its call at %d", i)
			}
		}
	}
}

func TestPlanRetryBadCallAndLoop(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		switch n {
		case 0:
			return http.StatusServiceUnavailable, nil
		case 1:
			return 200, []string{call("plan_update", `{"steps":[{"text":"Ordner ansehen"},{"text":"Bericht"}]}`)}
		case 2:
			return http.StatusInternalServerError, nil // not a broken call: tried again
		case 3:
			return 200, []string{call("see", `{"p":1}`)}
		case 4, 5, 6, 7:
			return 200, []string{call("see", `{"p":1}`)}
		}
		return 200, []string{answer("x")}
	}}
	var resets int
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &kindTools{}, System: "S", Retry: time.Millisecond,
		Emit: func(e Event) {
			if e.Type == "reset" {
				resets++
			}
		}}
	chat := newChat("Sieh nach")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if len(chat.Plan) != 2 || chat.Plan[0].Text != "Ordner ansehen" || resets != 2 {
		t.Fatalf("plan %+v resets %d", chat.Plan, resets)
	}
	if !strings.Contains(systemOf(s.got[3]), "[ ] Ordner ansehen") {
		t.Fatalf("plan not in the prompt: %s", systemOf(s.got[3]))
	}
	ran, refused, loop := 0, 0, false
	for _, e := range chat.Entries {
		if e.Tool == "see" && e.OK {
			ran++
		}
		if e.Tool == "see" && strings.HasPrefix(e.Result, "Not run") {
			refused++
		}
		loop = loop || e.Note == "loop"
	}
	if ran != 2 || refused != 3 || !loop || chat.State != StateIdle {
		t.Fatalf("ran %d refused %d loop %v state %s", ran, refused, loop, chat.State)
	}
}

func TestBrokenToolCallGoesBackToTheModel(t *testing.T) {
	if !isBadCall(errors.New("Ollama: error parsing tool call: raw='{\"a\":'")) || isBadCall(errors.New("model not found")) {
		t.Fatal("broken calls not told apart")
	}
	if !transient(&StatusError{Status: 503}) || transient(&StatusError{Status: 500, Message: "error parsing tool call"}) || transient(&StatusError{Status: 404}) ||
		!transient(errors.New("Ollama: an error was encountered while running the model: unexpected EOF")) {
		t.Fatal("transient errors not told apart")
	}
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		if n == 0 {
			return http.StatusInternalServerError, []string{"error parsing tool call: raw='{\"path\":'"}
		}
		msgs := body["messages"].([]any)
		if last := msgs[len(msgs)-1].(map[string]any); !strings.Contains(last["content"].(string), "valid JSON") {
			t.Errorf("broken call not explained: %v", last)
		}
		return 200, []string{answer("Gut.")}
	}}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &kindTools{}, Retry: time.Millisecond}
	chat := newChat("x")
	if err := r.Run(context.Background(), chat); err != nil || chat.State != StateIdle || len(s.got) != 2 {
		t.Fatalf("err %v state %s rounds %d", err, chat.State, len(s.got))
	}
}

func TestCheckAfterChangeOnce(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		switch n {
		case 0:
			return 200, []string{call("make", `{}`)}
		case 1:
			return 200, []string{answer("Erledigt.")}
		case 2:
			return 200, []string{call("see", `{}`)}
		}
		return 200, []string{answer("Geprüft, erledigt.")}
	}}
	tools := &kindTools{}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: tools}
	chat := newChat("Mach es")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if strings.Join(tools.ran, ",") != "make,see" || chat.Entries[len(chat.Entries)-1].Content != "Geprüft, erledigt." {
		t.Fatalf("ran %v last %+v", tools.ran, chat.Entries[len(chat.Entries)-1])
	}
	// Without a change nothing is asked.
	s2 := &scripted{next: func(int, map[string]any) (int, []string) { return 200, []string{answer("Nur gelesen.")} }}
	r2 := &Runner{Ollama: s2.serve(t), Model: "m", Tools: tools}
	chat2 := newChat("Lies")
	r2.Run(context.Background(), chat2)
	if len(s2.got) != 1 {
		t.Fatalf("asked %d times without a change", len(s2.got))
	}
}

func TestToolsByAreaAndLoad(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		switch n {
		case 0:
			return 200, []string{call("tools_load", `{"area":"docker"}`)}
		case 1:
			return 200, []string{call("docker_3", `{}`)}
		}
		return 200, []string{answer("ok")}
	}}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &kindTools{}, LazyTools: true}
	chat := newChat("Docker?")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(toolNames(s.got[0]), ",")
	if strings.Contains(first, "docker_3") || !strings.Contains(first, "see") || !strings.Contains(first, "tools_load") || !strings.Contains(first, "plan_update") {
		t.Fatalf("first offer %s", first)
	}
	if !strings.Contains(strings.Join(toolNames(s.got[1]), ","), "docker_3") || len(chat.Areas) != 1 {
		t.Fatalf("not loaded: %v %v", toolNames(s.got[1]), chat.Areas)
	}
	// Area and kind never go to Ollama.
	if b, _ := json.Marshal(s.got[0]["tools"]); strings.Contains(string(b), `"Area"`) || strings.Contains(string(b), `"Kind"`) {
		t.Fatal("runner fields sent to Ollama")
	}
}

func TestDelegateOnlyReadsAndCounts(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		names := strings.Join(toolNames(body), ",")
		user := body["messages"].([]any)[1].(map[string]any)["content"].(string)
		if strings.Contains(user, "Only read") {
			// The helper: never offered changes or another helper.
			if strings.Contains(names, "make") || strings.Contains(names, "task_delegate") {
				t.Errorf("helper offered %s", names)
			}
			msgs := body["messages"].([]any)
			if len(msgs) == 2 {
				return 200, []string{call("make", `{}`)}
			}
			if len(msgs) == 4 {
				return 200, []string{call("see", `{}`)}
			}
			return 200, []string{answer("Gefunden: drei Fehler in app.log.")}
		}
		if n == 0 {
			return 200, []string{call("task_delegate", `{"task":"Sieh die Protokolle durch"}`)}
		}
		return 200, []string{answer("Der Helfer fand drei Fehler.")}
	}}
	tools := &kindTools{}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: tools, Delegate: true, MaxSteps: 10}
	chat := newChat("Prüf die Logs")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if strings.Join(tools.ran, ",") != "see" {
		t.Fatalf("ran %v", tools.ran)
	}
	var report string
	for _, e := range chat.Entries {
		if e.Tool == "task_delegate" {
			report = e.Result
		}
	}
	if !strings.Contains(report, "drei Fehler in app.log") || !strings.Contains(report, "2 steps") || r.extra != 2 {
		t.Fatalf("report %q extra %d", report, r.extra)
	}
}

func TestTimeLimitReports(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		if body["tools"] == nil {
			return 200, []string{answer("Erledigt: A. Offen: B.")}
		}
		now = now.Add(20 * time.Minute)
		return 200, []string{call("see", fmt.Sprintf(`{"n":%d}`, n))}
	}}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &kindTools{}, MaxTime: 30 * time.Minute, Now: func() time.Time { return now }}
	chat := newChat("Lange Arbeit")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	n := len(chat.Entries)
	if chat.Entries[n-1].Note != "timelimit" || chat.Entries[n-2].Content != "Erledigt: A. Offen: B." {
		t.Fatalf("end %+v / %+v", chat.Entries[n-2], chat.Entries[n-1])
	}
}

// One answer with more calls than the step limit runs only up to the
// limit; the rest is recorded as not run and the next round reports.
func TestManyCallsInOneRoundKeepTheStepLimit(t *testing.T) {
	calls := []string{}
	for i := 0; i < 10; i++ {
		calls = append(calls, fmt.Sprintf(`{"function":{"name":"look","arguments":{"n":%d}}}`, i))
	}
	fake := &fakeOllama{replies: [][]string{{`{"role":"assistant","content":"","tool_calls":[` + strings.Join(calls, ",") + `]}`}}}
	tools := &fakeTools{}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: tools, MaxSteps: 3}
	chat := newChat("x")
	r.Run(context.Background(), chat)
	if len(tools.ran) != 3 {
		t.Fatalf("ran %d calls with a limit of 3", len(tools.ran))
	}
	notRun, limit := 0, 0
	for _, e := range chat.Entries {
		if e.Role == "tool" && strings.Contains(e.Result, "step limit") {
			notRun++
		}
		if e.Note == "limit" {
			limit++
		}
	}
	if notRun != 7 || limit != 1 {
		t.Fatalf("not run %d, limit notes %d", notRun, limit)
	}
}

// An Ollama that streams without end is cut off instead of filling the
// memory of titand.
func TestChatAnswerIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		line := []byte(`{"message":{"role":"assistant","content":"` + strings.Repeat("x", 64<<10) + `"},"done":false}` + "\n")
		for i := 0; i < (MaxAnswerBytes>>16)+4; i++ {
			if _, err := w.Write(line); err != nil {
				return
			}
		}
		w.Write([]byte(`{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
	}))
	defer srv.Close()
	o, err := NewOllama(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.Chat(context.Background(), ChatRequest{Model: "m"}, nil); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v", err)
	}
	calls := []string{}
	for i := 0; i < MaxToolCalls+1; i++ {
		calls = append(calls, `{"function":{"name":"look","arguments":{}}}`)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"role":"assistant","content":"","tool_calls":[` + strings.Join(calls, ",") + `]},"done":true}` + "\n"))
	}))
	defer srv2.Close()
	o2, _ := NewOllama(srv2.URL)
	if _, _, err := o2.Chat(context.Background(), ChatRequest{Model: "m"}, nil); err == nil {
		t.Fatal("too many tool calls accepted")
	}
}
