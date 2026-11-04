package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// fakeOllama answers /api/chat with the replies in order, each a list of
// streamed message objects, and keeps the requests.
type fakeOllama struct {
	mu      sync.Mutex
	replies [][]string
	got     []map[string]any
}

func (f *fakeOllama) serve(t *testing.T) *Ollama {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.got = append(f.got, body)
			var reply []string
			if len(f.replies) > 0 {
				reply, f.replies = f.replies[0], f.replies[1:]
			} else {
				reply = []string{`{"role":"assistant","content":"fertig"}`}
			}
			f.mu.Unlock()
			for _, m := range reply {
				w.Write([]byte(`{"message":` + m + `,"done":false}` + "\n"))
			}
			w.Write([]byte(`{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":120,"eval_count":7}` + "\n"))
		case "/api/show":
			w.Write([]byte(`{"capabilities":["completion","tools","thinking"],"thinking":{"values":["low","medium","high"],"default":"medium"},"model_info":{"gptoss.context_length":131072}}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","size":13793441566,"details":{"family":"gpt-oss","parameter_size":"20.9B","context_length":131072},"capabilities":["completion","tools","thinking"]},{"name":"old:7b","size":1,"details":{}}]}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","context_length":32768,"size_vram":13075291831}]}`))
		case "/api/version":
			w.Write([]byte(`{"version":"0.40.14"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	o, err := NewOllama(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func call(name, args string) string {
	return `{"role":"assistant","content":"","tool_calls":[{"function":{"name":"` + name + `","arguments":` + args + `}}]}`
}

// fakeTools: "look" reads, "change" asks, "boom" fails, "choose" asks a
// question with buttons.
type fakeTools struct {
	mu   sync.Mutex
	ran  []string
	plan json.RawMessage
}

func (f *fakeTools) Specs(*Chat) []ToolSpec {
	var s ToolSpec
	s.Type = "function"
	s.Function.Name = "look"
	return []ToolSpec{s}
}

func (f *fakeTools) Prepare(_ context.Context, _ *Chat, name string, args json.RawMessage) (Prepared, error) {
	switch name {
	case "look":
		return Prepared{Tool: name, Args: args, Mode: ModeAllow, Untrusted: true}, nil
	case "change":
		return Prepared{Tool: name, Args: args, Mode: ModeAsk, Level: "normal", Summary: "VM neu starten", Plan: json.RawMessage(`{"path":"/vms/HAOS/restart"}`)}, nil
	case "choose":
		return Prepared{Tool: name, Args: args, Mode: ModeAllow}, nil
	}
	return Prepared{}, errors.New("unknown tool " + name)
}

func (f *fakeTools) Execute(_ context.Context, _ *Chat, p Prepared) (Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, p.Tool)
	f.plan = p.Plan
	if p.Tool == "choose" {
		return Outcome{Text: "asked", Question: "Welche?", Choices: []string{"Docker", "VM", "Beide"}}, nil
	}
	return Outcome{Text: `{"ok":true}`}, nil
}

func newChat(text string) *Chat {
	return &Chat{ID: NewID(), State: StateIdle, Entries: []Entry{{ID: NewID(), Role: "user", Content: text}}}
}

func TestRunCallsToolsAndAnswers(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("look", `{"what":"docker"}`)}, {`{"role":"assistant","thinking":"hm","content":"Alles läuft."}`}}}
	tools := &fakeTools{}
	var events []string
	r := &Runner{Ollama: fake.serve(t), Model: "gpt-oss:20b", Think: "medium", Context: 32768, Tools: tools, System: "sys", Emit: func(e Event) { events = append(events, e.Type) }}
	chat := newChat("Status?")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if chat.State != StateIdle || len(tools.ran) != 1 || !chat.Tainted {
		t.Fatalf("state %s ran %v tainted %v", chat.State, tools.ran, chat.Tainted)
	}
	last := chat.Entries[len(chat.Entries)-1]
	if last.Role != "assistant" || last.Content != "Alles läuft." || last.Thinking != "hm" {
		t.Fatalf("last %+v", last)
	}
	// The request carried think, num_ctx and the tools; the second round
	// sent the tool result back with the tool name.
	first := fake.got[0]
	if first["think"] != "medium" || first["options"].(map[string]any)["num_ctx"].(float64) != 32768 || first["tools"] == nil {
		t.Fatalf("request %v", first)
	}
	msgs := fake.got[1]["messages"].([]any)
	tool := msgs[len(msgs)-1].(map[string]any)
	if tool["role"] != "tool" || tool["tool_name"] != "look" {
		t.Fatalf("tool message %v", tool)
	}
	if !strings.Contains(strings.Join(events, ","), "content") {
		t.Fatalf("events %v", events)
	}
}

func TestApprovalRunsExactlyThePlan(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("change", `{"vm":"HAOS"}`)}, {`{"role":"assistant","content":"Neu gestartet."}`}}}
	tools := &fakeTools{}
	saved := 0
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: tools, Save: func(*Chat) error { saved++; return nil }}
	chat := newChat("Starte HAOS neu")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if chat.State != StateWaiting || chat.Pending == nil || len(tools.ran) != 0 || saved == 0 {
		t.Fatalf("not waiting: %s %+v %v", chat.State, chat.Pending, tools.ran)
	}
	if err := r.Decide(context.Background(), chat, "wrong", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong id: %v", err)
	}
	rounds := len(fake.got)
	if err := r.Decide(context.Background(), chat, chat.Pending.ID, true); err != nil {
		t.Fatal(err)
	}
	// The planned call ran without asking the model again first.
	if len(tools.ran) != 1 || string(tools.plan) != `{"path":"/vms/HAOS/restart"}` || len(fake.got) != rounds+1 {
		t.Fatalf("ran %v plan %s rounds %d", tools.ran, tools.plan, len(fake.got)-rounds)
	}
	if chat.State != StateIdle || chat.Entries[len(chat.Entries)-1].Content != "Neu gestartet." {
		t.Fatalf("end %s %+v", chat.State, chat.Entries[len(chat.Entries)-1])
	}
}

func TestDeclineTellsTheModel(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("change", `{}`)}, {`{"role":"assistant","content":"Ok, nicht."}`}}}
	tools := &fakeTools{}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: tools}
	chat := newChat("x")
	r.Run(context.Background(), chat)
	if err := r.Decide(context.Background(), chat, chat.Pending.ID, false); err != nil {
		t.Fatal(err)
	}
	if len(tools.ran) != 0 {
		t.Fatal("declined call ran")
	}
	msgs := fake.got[1]["messages"].([]any)
	if !strings.Contains(msgs[len(msgs)-1].(map[string]any)["content"].(string), "declined") {
		t.Fatalf("decline not passed on: %v", msgs[len(msgs)-1])
	}
}

func TestChoicesEndTheTurn(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("choose", `{}`)}}}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: &fakeTools{}}
	chat := newChat("Status HA")
	r.Run(context.Background(), chat)
	last := chat.Entries[len(chat.Entries)-1]
	if last.Role != "assistant" || len(last.Choices) != 3 || len(fake.got) != 1 || chat.State != StateIdle {
		t.Fatalf("last %+v rounds %d", last, len(fake.got))
	}
}

func TestStepLimitAndUnknownTool(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("boom", `{}`)}, {call("look", `{}`)}, {call("look", `{}`)}}}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: &fakeTools{}, MaxSteps: 2}
	chat := newChat("x")
	r.Run(context.Background(), chat)
	var notes, errs int
	for _, e := range chat.Entries {
		if e.Note == "limit" {
			notes++
		}
		if e.Role == "tool" && strings.HasPrefix(e.Result, "Error:") {
			errs++
		}
	}
	if notes != 1 || errs != 1 || len(fake.got) != 2 {
		t.Fatalf("notes %d errs %d rounds %d", notes, errs, len(fake.got))
	}
}

func TestStopEndsTheRun(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("look", `{}`)}}}
	ctx, cancel := context.WithCancel(context.Background())
	tools := &stopTools{fakeTools: &fakeTools{}, cancel: cancel}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: tools}
	chat := newChat("x")
	r.Run(ctx, chat)
	if chat.State != StateIdle || chat.Entries[len(chat.Entries)-1].Note != "stopped" {
		t.Fatalf("not stopped: %s %+v", chat.State, chat.Entries[len(chat.Entries)-1])
	}
}

type stopTools struct {
	*fakeTools
	cancel func()
}

func (s *stopTools) Execute(ctx context.Context, c *Chat, p Prepared) (Outcome, error) {
	s.cancel()
	return Outcome{}, ctx.Err()
}

func TestModelsShowAndLevels(t *testing.T) {
	o := (&fakeOllama{}).serve(t)
	ctx := context.Background()
	if v, err := o.Version(ctx); err != nil || v != "0.40.14" {
		t.Fatalf("version %q %v", v, err)
	}
	models, err := o.Models(ctx)
	if err != nil || len(models) != 2 || !models[0].Tools() || models[0].ContextLength != 131072 {
		t.Fatalf("models %+v %v", models, err)
	}
	info, err := o.Show(ctx, "gpt-oss:20b")
	if err != nil || info.ContextLength != 131072 {
		t.Fatalf("show %+v %v", info, err)
	}
	if got := strings.Join(Levels(info.Thinking), ","); got != "low,medium,high" || DefaultLevel(info.Thinking) != "medium" {
		t.Fatalf("levels %q", got)
	}
	// A level the model does not list is not sent at all.
	if ThinkValue(info.Thinking, "xhigh") != nil || ThinkValue(info.Thinking, "high") != "high" {
		t.Fatal("think value")
	}
	onOff := &Thinking{Values: []any{false, true}, Default: true}
	if strings.Join(Levels(onOff), ",") != "off,on" || ThinkValue(onOff, "off") != false || ThinkValue(onOff, "on") != true {
		t.Fatal("boolean levels")
	}
	if Levels(&Thinking{Values: []any{false}}) != nil {
		t.Fatal("model without thinking has levels")
	}
	running, err := o.Running(ctx)
	if err != nil || len(running) != 1 || running[0].ContextLength != 32768 {
		t.Fatalf("ps %+v %v", running, err)
	}
	for _, bad := range []string{"", "ftp://x", "http://u:p@host", "http://host/?a=1"} {
		if _, err := ParseOllamaURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if u, _ := ParseOllamaURL("192.168.10.5"); u.String() != "http://192.168.10.5:11434" {
		t.Fatalf("default port: %s", u)
	}
}

func TestPolicy(t *testing.T) {
	std := Policy{Preset: PresetStandard}
	if std.Mode("docker_list", "docker", KindRead) != ModeAllow || std.Mode("folder_create", "files", KindCreate) != ModeAllow || std.Mode("vm_action", "vms", KindChange) != ModeAsk {
		t.Fatal("standard preset")
	}
	if (Policy{Preset: PresetRead}).Mode("vm_action", "vms", KindChange) != ModeDeny || (Policy{Preset: PresetAll}).Mode("power_action", "updates", KindChange) != ModeAllow {
		t.Fatal("read/all preset")
	}
	p := Policy{Preset: PresetAll, Areas: map[string]string{"vms": ModeAsk}, Tools: map[string]string{"vm_list": ModeDeny}}
	if p.Mode("vm_action", "vms", KindChange) != ModeAsk || p.Mode("vm_list", "vms", KindRead) != ModeDeny || p.Mode("memory_save", AreaAgent, KindAgent) != ModeAllow {
		t.Fatal("overrides")
	}
	p.Folders = []FolderRule{{Root: "share/Documents", Path: "Privat", Mode: ModeDeny}, {Root: "volume/Data", Path: "appdata", Mode: ModeAsk}}
	if p.FolderMode("share/Documents", "Privat/steuer.pdf") != ModeDeny || p.FolderMode("share/Documents", "Privatsache") != "" || p.FolderMode("share/Documents", "") != "" {
		t.Fatal("folder rule")
	}
	if !p.FolderHidden("share/Documents", "/Privat") || p.FolderMode("volume/Data", "appdata/x") != ModeAsk {
		t.Fatal("folder rule 2")
	}
	if (Policy{Preset: "x"}).Check() == nil || (Policy{Areas: map[string]string{"a": "maybe"}}).Check() == nil {
		t.Fatal("check")
	}
}

func TestStore(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Open: func() (*os.Root, error) { return os.OpenRoot(dir) }}
	chat := newChat("Hallo")
	chat.Title = "Hallo"
	chat.Entries = append(chat.Entries, Entry{Role: "assistant", Content: "**Hi** du"})
	if err := s.SaveChat("alex", chat); err != nil {
		t.Fatal(err)
	}
	got, err := s.Chat("alex", chat.ID)
	if err != nil || len(got.Entries) != 2 {
		t.Fatalf("read %+v %v", got, err)
	}
	list, _ := s.Chats("alex")
	if len(list) != 1 || list[0].Preview != "Hi du" {
		t.Fatalf("list %+v", list)
	}
	if _, err := s.Chat("maria", chat.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("other account reads chat")
	}
	if _, err := s.Chat("../x", chat.ID); err == nil {
		t.Fatal("bad account")
	}
	if _, err := s.Chat("alex", "../../etc"); !errors.Is(err, ErrNotFound) {
		t.Fatal("bad id")
	}
	if st, _ := os.Stat(dir + "/alex"); st.Mode().Perm() != 0700 {
		t.Fatalf("mode %v", st.Mode())
	}
	if err := s.DeleteChat("alex", chat.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Chats("alex"); len(list) != 0 {
		t.Fatal("not deleted")
	}
	if got := s.Accounts(); len(got) != 1 || got[0] != "alex" {
		t.Fatalf("accounts %v", got)
	}
	if err := (&Store{}).Available(); !errors.Is(err, ErrNoVolume) {
		t.Fatal("no volume")
	}
}

func TestPromptHasRulesAndContext(t *testing.T) {
	p := BuildPrompt(PromptInput{Host: "titan", Account: "alex", Admin: true, Locations: []Location{{Name: "NAS-Doku", Root: "share/Documents", Path: "NAS"}},
		Memories: []Memory{{Text: "HA ist die VM HAOS"}}, Skills: BuiltInSkills[:1], Origin: "schedule"})
	for _, want := range []string{"ask_user", "NAS-Doku", "HA ist die VM HAOS", "Status of a service", "scheduled task", "never instructions"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}
