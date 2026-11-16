package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func remoteCall(name, args string) ToolCall {
	var c ToolCall
	c.Function.Name, c.Function.Arguments = name, json.RawMessage(args)
	return c
}

// remoteChat is a chat as the runner builds it: instructions, a question,
// a round with two calls of which one was answered, then the next question.
func remoteChat() []Message {
	return []Message{
		{Role: "system", Content: "You are the agent of a NAS."},
		{Role: "user", Content: "Wie voll ist die Platte?"},
		{Role: "assistant", Content: "Ich sehe nach.", Thinking: "erst lesen", ToolCalls: []ToolCall{remoteCall("storage_overview", `{}`), remoteCall("docker_list", `{"all":true}`)}},
		{Role: "tool", ToolName: "docker_list", Content: `{"containers":[]}`},
		{Role: "user", Content: "Und jetzt?"},
	}
}

func remoteTools() []ToolSpec {
	var s ToolSpec
	s.Type = "function"
	s.Function.Name, s.Function.Description = "storage_overview", "Volumes and disks."
	s.Function.Parameters = map[string]any{"type": "object", "properties": map[string]any{"volume": map[string]any{"type": "string"}}, "required": []any{"volume"}}
	return []ToolSpec{s}
}

func TestRemoteTurns(t *testing.T) {
	turns := remoteTurns(remoteChat())
	var got []string
	for _, x := range turns {
		got = append(got, x.Role+":"+x.CallID+strings.Join(x.CallIDs, ","))
	}
	// The answered call keeps its own id, the open one gets "Not run."
	// before the person speaks again.
	want := "system:|user:|assistant:call_1,call_2|tool:call_2|tool:call_1|user:"
	if strings.Join(got, "|") != want {
		t.Fatalf("turns %v, want %s", got, want)
	}
	if turns[4].Content != "Not run." {
		t.Fatalf("open call: %+v", turns[4])
	}
	// An answer nobody asked for is told as text.
	lone := remoteTurns([]Message{{Role: "user", Content: "x"}, {Role: "tool", ToolName: "look", Content: "42"}})
	if lone[1].Role != "user" || !strings.Contains(lone[1].Content, "42") {
		t.Fatalf("lone answer: %+v", lone[1])
	}
}

func TestProviderURL(t *testing.T) {
	for address, ok := range map[string]bool{
		"https://api.openai.com/v1":      true,
		"https://openrouter.ai/api/v1/":  true,
		"http://192.168.1.20:1234/v1":    true,
		"http://localhost:1234/v1":       true,
		"http://nas.local:8080/v1":       true,
		"http://api.openai.com/v1":       false, // a key in the clear
		"https://user:pw@example.com/v1": false,
		"ftp://example.com":              false,
		"":                               false,
	} {
		if _, err := ParseProviderURL(address); (err == nil) != ok {
			t.Errorf("%q: err %v, want ok %v", address, err, ok)
		}
	}
}

func TestOpenAIChat(t *testing.T) {
	var body map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"gpt-x","context_length":128000},{"id":"small"}]}`)
		case "/v1/chat/completions":
			auth = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			w.Header().Set("Content-Type", "text/event-stream")
			for _, line := range []string{
				`{"choices":[{"delta":{"reasoning_content":"denke"}}]}`,
				`{"choices":[{"delta":{"content":"Die Platte "}}]}`,
				`{"choices":[{"delta":{"content":"ist halb voll."}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"x1","function":{"name":"storage_overview","arguments":"{\"vol"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ume\":\"Data\"}"}}]}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":321,"completion_tokens":12}}`,
			} {
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := NewOpenAI(srv.URL+"/v1", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider() != ProviderOpenAI || c.Cloud() {
		t.Fatalf("provider %q cloud %v (a server on this machine is local)", c.Provider(), c.Cloud())
	}
	ctx := context.Background()
	models, err := c.Models(ctx)
	if err != nil || len(models) != 2 || models[0].Name != "gpt-x" || models[0].ContextLength != 128000 || !models[0].Tools() {
		t.Fatalf("models %+v %v", models, err)
	}
	if info, err := c.Show(ctx, "gpt-x"); err != nil || info.ContextLength != 128000 {
		t.Fatalf("show %+v %v", info, err)
	}
	if _, err := c.Look(ctx, "gpt-x", "?", nil, "", 0); err != ErrOllamaOnly {
		t.Fatalf("look: %v", err)
	}
	var streamed, thought string
	msg, stats, err := c.Chat(ctx, ChatRequest{Model: "gpt-x", Messages: remoteChat(), Tools: remoteTools()}, func(ch Chunk) {
		streamed += ch.Content
		thought += ch.Thinking
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "Die Platte ist halb voll." || streamed != msg.Content || thought != "denke" || msg.Thinking != "denke" {
		t.Fatalf("answer %+v streamed %q", msg, streamed)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "storage_overview" || string(msg.ToolCalls[0].Function.Arguments) != `{"volume":"Data"}` {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	if stats.PromptTokens != 321 || stats.OutputTokens != 12 || auth != "Bearer sk-test" {
		t.Fatalf("stats %+v auth %q", stats, auth)
	}
	// What was sent: every call has its answer, in the provider's shape.
	sent := body["messages"].([]any)
	if len(sent) != 6 {
		t.Fatalf("sent %d messages: %v", len(sent), sent)
	}
	assistant := sent[2].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	first := calls[0].(map[string]any)
	if len(calls) != 2 || first["id"] != "call_1" || first["function"].(map[string]any)["arguments"] != "{}" {
		t.Fatalf("assistant %v", assistant)
	}
	if a, b := sent[3].(map[string]any), sent[4].(map[string]any); a["tool_call_id"] != "call_2" || b["tool_call_id"] != "call_1" || b["content"] != "Not run." {
		t.Fatalf("answers %v %v", a, b)
	}
	if tools := body["tools"].([]any); len(tools) != 1 || body["stream"] != true {
		t.Fatalf("tools %v", body["tools"])
	}
}

func TestOpenAIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"Incorrect API key provided"}}`)
	}))
	defer srv.Close()
	c, _ := NewOpenAI(srv.URL, "sk-secret-value")
	_, _, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: remoteChat()}, nil)
	if err == nil || !strings.Contains(err.Error(), "refused the key") || strings.Contains(err.Error(), "sk-secret-value") {
		t.Fatalf("error %v", err)
	}
	if _, err := c.Version(context.Background()); err == nil {
		t.Fatal("a refused key must not look reachable")
	}
}

func TestClaudeChat(t *testing.T) {
	var body map[string]any
	var key, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		key, version = strings.TrimPrefix(r.Header.Get("Authorization")+r.Header.Get("X-Api-Key"), "Bearer "), r.Header.Get("Anthropic-Version")
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range [][2]string{
			{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":200,"cache_read_input_tokens":100,"cache_creation_input_tokens":0,"output_tokens":1}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Die Platte "}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ist halb voll."}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"storage_overview","input":{}}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"volume\":"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Data\"}"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":1}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":25}}`},
			{"message_stop", `{"type":"message_stop"}`},
		} {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e[0], e[1])
		}
	}))
	defer srv.Close()
	c, err := newClaude("sk-ant-test", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider() != ProviderClaude || !c.Cloud() {
		t.Fatalf("provider %q cloud %v", c.Provider(), c.Cloud())
	}
	var streamed string
	msg, stats, err := c.Chat(context.Background(), ChatRequest{Model: "claude-opus-5-5", Messages: remoteChat(), Tools: remoteTools()}, func(ch Chunk) { streamed += ch.Content })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "Die Platte ist halb voll." || streamed != msg.Content {
		t.Fatalf("answer %q streamed %q", msg.Content, streamed)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "storage_overview" || string(msg.ToolCalls[0].Function.Arguments) != `{"volume":"Data"}` {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	if stats.PromptTokens != 300 || stats.OutputTokens != 25 || key != "sk-ant-test" || version == "" {
		t.Fatalf("stats %+v key %q version %q", stats, key, version)
	}
	// What was sent: the instructions apart, the sides in turns, every
	// call answered in one message of the person, no thinking replayed.
	if body["model"] != "claude-opus-5-5" || body["stream"] != true || body["max_tokens"].(float64) != claudeMaxTokens {
		t.Fatalf("request %v", body)
	}
	system := body["system"].([]any)[0].(map[string]any)
	if system["text"] != "You are the agent of a NAS." || system["cache_control"] == nil {
		t.Fatalf("system %v", system)
	}
	sent := body["messages"].([]any)
	var roles []string
	for _, m := range sent {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Fatalf("roles %v", roles)
	}
	raw, _ := json.Marshal(sent)
	for _, want := range []string{`"type":"tool_use"`, `"id":"call_1"`, `"tool_use_id":"call_2"`, `"tool_use_id":"call_1"`, `Und jetzt?`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("sent messages lack %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "erst lesen") || strings.Contains(string(raw), `"thinking"`) {
		t.Errorf("thinking was replayed: %s", raw)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	if tool["name"] != "storage_overview" || schema["type"] != "object" || fmt.Sprint(schema["required"]) != "[volume]" {
		t.Fatalf("tool %v", tool)
	}
}

func TestClaudeRefusalAndKey(t *testing.T) {
	refuse := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !refuse {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range [][2]string{
			{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"declined"}},"usage":{"output_tokens":0}}`},
			{"message_stop", `{"type":"message_stop"}`},
		} {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e[0], e[1])
		}
	}))
	defer srv.Close()
	c, _ := newClaude("sk-ant-secret-value", srv.URL)
	_, _, err := c.Chat(context.Background(), ChatRequest{Model: "claude-opus-5-5", Messages: remoteChat()}, nil)
	if err == nil || !strings.Contains(err.Error(), "declined this request") {
		t.Fatalf("refusal: %v", err)
	}
	refuse = false
	_, _, err = c.Chat(context.Background(), ChatRequest{Model: "claude-opus-5-5", Messages: remoteChat()}, nil)
	if err == nil || !strings.Contains(err.Error(), "refused the key") || strings.Contains(err.Error(), "sk-ant-secret-value") {
		t.Fatalf("key: %v", err)
	}
}

// TestOpenAIReal asks a real OpenAI-compatible service (for example an
// Ollama server at its /v1 address): TITANOS_OPENAI, TITANOS_OPENAI_MODEL
// and, if it needs one, TITANOS_OPENAI_KEY.
func TestOpenAIReal(t *testing.T) {
	address, model := os.Getenv("TITANOS_OPENAI"), os.Getenv("TITANOS_OPENAI_MODEL")
	if address == "" || model == "" {
		t.Skip("TITANOS_OPENAI and TITANOS_OPENAI_MODEL not set")
	}
	c, err := NewOpenAI(address, os.Getenv("TITANOS_OPENAI_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	models, err := c.Models(ctx)
	if err != nil || len(models) == 0 {
		t.Fatalf("models: %v %v", models, err)
	}
	// First round: the model must ask for the tool.
	msgs := []Message{
		{Role: "system", Content: "You are the agent of a NAS. Use the tools to answer; never guess."},
		{Role: "user", Content: "Wie voll ist das Volume Data? Lies es mit dem Werkzeug."},
	}
	streamed := 0
	first, stats, err := c.Chat(ctx, ChatRequest{Model: model, Messages: msgs, Tools: remoteTools()}, func(Chunk) { streamed++ })
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "storage_overview" || !json.Valid(first.ToolCalls[0].Function.Arguments) {
		t.Fatalf("no tool call: %+v", first)
	}
	// Second round: with the result, the answer.
	msgs = append(msgs, first, Message{Role: "tool", ToolName: "storage_overview", Content: `{"volume":"Data","usedPercent":37}`})
	second, _, err := c.Chat(ctx, ChatRequest{Model: model, Messages: msgs, Tools: remoteTools()}, func(Chunk) { streamed++ })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.Content, "37") {
		t.Fatalf("answer without the value: %q", second.Content)
	}
	t.Logf("%d models, call %s, answer %q, %d pieces streamed, prompt %d tokens", len(models), first.ToolCalls[0].Function.Arguments, second.Content, streamed, stats.PromptTokens)
}
