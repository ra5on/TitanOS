package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const testKey = "sk-ant-test-0123456789"

// fakeCloud answers like a cloud provider: each reply is a list of SSE data
// lines for one streamed request, and it keeps the requests it got.
type fakeCloud struct {
	mu      sync.Mutex
	replies [][]string
	got     []map[string]any
	headers []http.Header
	models  string
	status  int // answers every request with this status if set
}

func (f *fakeCloud) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"type":"authentication_error","message":"invalid x-api-key %s"}}`, r.Header.Get("x-api-key"))
			return
		}
		if r.Method == http.MethodGet {
			w.Write([]byte(f.models))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.got = append(f.got, body)
		reply := f.replies[0]
		f.replies = f.replies[1:]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, line := range reply {
			fmt.Fprintf(w, "event: x\ndata: %s\n\n", line)
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// cloudManager is a manager whose cloud clients may reach the test servers
// on loopback, which the real rule forbids.
func cloudManager(t *testing.T, settings Settings) *Manager {
	t.Helper()
	m := NewManager(config.Open(t.TempDir()))
	m.reach = func(netip.Addr, string) bool { return true }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	return m
}

func anthropicTurn(text string) []string {
	return []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":120}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	}
}

func TestAnthropicToolCallsAndStreaming(t *testing.T) {
	fake := &fakeCloud{replies: [][]string{
		{
			`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":5}}}`,
			`{"type":"ping"}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Ich sehe nach."}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"storage_overview","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"detail\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":" true}"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30}}`,
			`{"type":"message_stop"}`,
		},
		anthropicTurn("Das Volume tank ist beeinträchtigt."),
	}}
	base := fake.serve(t)
	m := cloudManager(t, Settings{Enabled: true, Provider: ProviderAnthropic, AnthropicKey: testKey})
	m.anthropicURL = base
	if m.Settings().Model != DefaultAnthropicModel {
		t.Fatalf("default model: %q", m.Settings().Model)
	}
	var calls []string
	var events []Event
	chat := newChat(t, "Alles gut?")
	temp := 1.5
	m.settings.Temperature = &temp
	if err := m.Answer(context.Background(), chat, Turn{Person: Person{Name: "alex", Role: "owner"}, Tools: testTools(&calls), Emit: collect(&events)}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != `storage_overview {"detail": true}` {
		t.Fatalf("calls: %v", calls)
	}
	last := chat.Messages[len(chat.Messages)-1]
	if last.Content != "Das Volume tank ist beeinträchtigt." || chat.Model != DefaultAnthropicModel {
		t.Fatalf("answer: %+v", last)
	}
	if chat.Stats.Prompt != 225 || chat.Stats.Generated != 37 || chat.Stats.Rounds != 2 {
		t.Fatalf("stats: %+v", chat.Stats)
	}
	h := fake.headers[0]
	if h.Get("x-api-key") != testKey || h.Get("anthropic-version") != "2023-06-01" || h.Get("Authorization") != "" {
		t.Fatalf("headers: %v", h)
	}
	first := fake.got[0]
	if first["model"] != DefaultAnthropicModel || first["stream"] != true || first["max_tokens"] == nil || first["temperature"] != 1.0 {
		t.Fatalf("request: %v", first)
	}
	if system, _ := first["system"].(string); !strings.Contains(system, "logged in as alex") {
		t.Fatalf("system: %v", first["system"])
	}
	tools := first["tools"].([]any)
	if tools[0].(map[string]any)["input_schema"] == nil || first["tool_choice"] != nil {
		t.Fatalf("tools: %v / %v", tools[0], first["tool_choice"])
	}
	// The second round carries the call and its result under the same ID.
	messages := fake.got[1]["messages"].([]any)
	call := messages[1].(map[string]any)
	result := messages[2].(map[string]any)
	if call["role"] != "assistant" || result["role"] != "user" {
		t.Fatalf("roles: %v", messages)
	}
	use := call["content"].([]any)[1].(map[string]any)
	answer := result["content"].([]any)[0].(map[string]any)
	if use["type"] != "tool_use" || answer["type"] != "tool_result" || use["id"] == "" || use["id"] != answer["tool_use_id"] || !strings.Contains(answer["content"].(string), "degraded") {
		t.Fatalf("pairing: %v / %v", use, answer)
	}
	if input := use["input"].(map[string]any); input["detail"] != true {
		t.Fatalf("input: %v", input)
	}
}

func TestAnthropicIncompleteStreamRunsNoTool(t *testing.T) {
	cut := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"storage_overview","input":{}}}`,
		`{"type":"content_block_stop","index":0}`,
	}
	fake := &fakeCloud{replies: [][]string{cut, cut, cut}}
	m := cloudManager(t, Settings{Enabled: true, Provider: ProviderAnthropic, AnthropicKey: testKey})
	m.anthropicURL = fake.serve(t)
	var calls []string
	err := m.Answer(context.Background(), newChat(t, "?"), Turn{Person: Person{Name: "alex", Role: "owner"}, Tools: testTools(&calls), Emit: func(Event) {}})
	if err == nil || !errors.Is(err, ErrProvider) || len(calls) != 0 {
		t.Fatalf("err %v, calls %v", err, calls)
	}
	// Retried as transient, never executed.
	if len(fake.got) != 3 {
		t.Fatalf("attempts: %d", len(fake.got))
	}
}

func TestAnthropicRefusedKeyStaysOutOfErrors(t *testing.T) {
	fake := &fakeCloud{status: http.StatusUnauthorized}
	m := cloudManager(t, Settings{Provider: ProviderAnthropic, AnthropicKey: testKey})
	m.anthropicURL = fake.serve(t)
	_, err := m.Models(context.Background(), "")
	if !errors.Is(err, ErrAuth) || !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err: %v", err)
	}
	err = m.Answer(context.Background(), newChat(t, "?"), Turn{Person: Person{Name: "alex", Role: "owner"}, Emit: func(Event) {}})
	if !errors.Is(err, ErrOff) {
		t.Fatalf("off: %v", err)
	}
}

func TestAnthropicModels(t *testing.T) {
	fake := &fakeCloud{models: `{"data":[{"id":"claude-sonnet-5-5","max_input_tokens":200000},{"id":"claude-haiku-5"}],"has_more":false}`}
	m := cloudManager(t, Settings{Provider: ProviderAnthropic, AnthropicKey: testKey})
	m.anthropicURL = fake.serve(t)
	models, err := m.Models(context.Background(), "")
	if err != nil || len(models) != 2 || models[1].Name != "claude-sonnet-5-5" || models[1].ContextLength != 200000 || !models[1].Can("tools") {
		t.Fatalf("models %+v, %v", models, err)
	}
}

func TestOpenAIToolCallsAndStreaming(t *testing.T) {
	fake := &fakeCloud{replies: [][]string{
		{
			`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"storage_","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"overview","arguments":"{\"detail\""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":false}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":50,"completion_tokens":9}}`,
			`[DONE]`,
		},
		{
			`{"choices":[{"delta":{"reasoning_content":"kurz"}}]}`,
			`{"choices":[{"delta":{"content":"Alles "}}]}`,
			`{"choices":[{"delta":{"content":"geprüft."},"finish_reason":"stop"}]}`,
			`[DONE]`,
		},
	}}
	base := fake.serve(t)
	m := cloudManager(t, Settings{Enabled: true, Provider: ProviderOpenAI, OpenAIBase: base, OpenAIKey: "sk-open", Model: "gpt-test"})
	if m.Settings().OpenAIBase != base+"/v1" {
		t.Fatalf("base: %q", m.Settings().OpenAIBase)
	}
	var calls []string
	chat := newChat(t, "Prüf das")
	if err := m.Answer(context.Background(), chat, Turn{Person: Person{Name: "alex", Role: "owner"}, Tools: testTools(&calls), Emit: func(Event) {}}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != `storage_overview {"detail":false}` {
		t.Fatalf("calls: %v", calls)
	}
	last := chat.Messages[len(chat.Messages)-1]
	if last.Content != "Alles geprüft." || last.Thinking != "kurz" || chat.Stats.Prompt != 50 || chat.Stats.Generated != 9 {
		t.Fatalf("answer %+v, stats %+v", last, chat.Stats)
	}
	if fake.headers[0].Get("Authorization") != "Bearer sk-open" {
		t.Fatalf("auth: %v", fake.headers[0])
	}
	if fake.got[0]["tools"] == nil || fake.got[0]["model"] != "gpt-test" {
		t.Fatalf("request: %v", fake.got[0])
	}
	messages := fake.got[1]["messages"].([]any)
	call := messages[2].(map[string]any)
	tool := messages[3].(map[string]any)
	calls0 := call["tool_calls"].([]any)[0].(map[string]any)
	if messages[0].(map[string]any)["role"] != "system" || tool["role"] != "tool" || tool["tool_call_id"] != calls0["id"] || calls0["function"].(map[string]any)["arguments"] != `{"detail":false}` {
		t.Fatalf("pairing: %v / %v", call, tool)
	}
}

func TestOpenAIWithoutKeyAndModels(t *testing.T) {
	fake := &fakeCloud{models: `{"data":[{"id":"local-model"},{"id":"openai/gpt-x","context_length":128000,"supported_parameters":["tools","temperature"]},{"id":"tiny","supported_parameters":["temperature"]}]}`}
	base := fake.serve(t)
	m := cloudManager(t, Settings{Provider: ProviderOpenAI, OpenAIBase: base + "/v1/"})
	models, err := m.Models(context.Background(), "")
	if err != nil || len(models) != 3 {
		t.Fatalf("models %+v, %v", models, err)
	}
	if fake.headers[0].Get("Authorization") != "" {
		t.Fatal("authorization without a key")
	}
	byName := map[string]Model{}
	for _, model := range models {
		byName[model.Name] = model
	}
	if len(byName["local-model"].Capabilities) != 0 || !byName["openai/gpt-x"].Can("tools") || byName["tiny"].Can("tools") || byName["openai/gpt-x"].ContextLength != 128000 {
		t.Fatalf("capabilities: %+v", byName)
	}
}

func TestKeysAreStoredApartAndOnlyReportedAsSet(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(config.Open(dir))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSettings(Settings{Provider: ProviderAnthropic, AnthropicKey: " " + testKey + " ", OpenAIBase: "https://openrouter.ai/api/v1", OpenAIKey: "sk-or"}); err != nil {
		t.Fatal(err)
	}
	settingsData, _ := os.ReadFile(filepath.Join(dir, "system", "assistant.json"))
	if strings.Contains(string(settingsData), testKey) || strings.Contains(string(settingsData), "sk-or") || strings.Contains(string(settingsData), "KeySet") {
		t.Fatalf("settings file: %s", settingsData)
	}
	keyData, _ := os.ReadFile(filepath.Join(dir, "assistant", "keys.json"))
	if info, err := os.Stat(filepath.Join(dir, "assistant", "keys.json")); err != nil || info.Mode().Perm()&0o077 != 0 || !strings.Contains(string(keyData), testKey) {
		t.Fatalf("keys file: %v %s", err, keyData)
	}
	got := m.Settings()
	encoded, _ := json.Marshal(got)
	if !got.AnthropicKeySet || !got.OpenAIKeySet || strings.Contains(string(encoded), testKey) || strings.Contains(string(encoded), "sk-or") {
		t.Fatalf("API settings: %s", encoded)
	}
	// Empty keeps the key; after a restart it is still there.
	next := got
	if err := m.SetSettings(next); err != nil {
		t.Fatal(err)
	}
	again := NewManager(config.Open(dir))
	if err := again.Load(); err != nil || again.withKeys(again.Settings()).AnthropicKey != testKey {
		t.Fatalf("reloaded: %v", err)
	}
	// The OpenAI-compatible key never goes to another server.
	other := got
	other.OpenAIBase = "https://example.com/v1"
	if err := m.SetSettings(other); !errors.Is(err, ErrKeyServer) {
		t.Fatalf("other server: %v", err)
	}
	if k := m.withKeys(other); k.OpenAIKey != "" {
		t.Fatal("stored key offered to another server")
	}
	if k := m.withKeys(got); k.OpenAIKey != "sk-or" {
		t.Fatal("stored key not used for its own server")
	}
	other.ClearOpenAIKey = true
	if err := m.SetSettings(other); err != nil || m.Settings().OpenAIKeySet {
		t.Fatalf("clear: %v", err)
	}
	// Anthropic switched on without a key is refused.
	off := m.Settings()
	off.ClearAnthropicKey, off.Enabled = true, true
	if err := m.SetSettings(off); !errors.Is(err, ErrKeyMissing) {
		t.Fatalf("missing key: %v", err)
	}
	off.Enabled = false
	if err := m.SetSettings(off); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "assistant", "keys.json")); !os.IsNotExist(err) {
		t.Fatalf("keys file left after removing every key: %v", err)
	}
	for _, bad := range []Settings{{Provider: "gemini"}, {Provider: ProviderOpenAI}, {Provider: ProviderOpenAI, OpenAIBase: "ftp://x"}, {Provider: ProviderOpenAI, OpenAIBase: "https://u:p@x/v1"}, {AnthropicKey: "a b"}, {AnthropicKey: strings.Repeat("k", MaxKey+1)}} {
		if err := m.SetSettings(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestSettingsOfOlderVersionsStillWork(t *testing.T) {
	dir := t.TempDir()
	store := config.Open(dir)
	// A file of 0.35.0: no provider.
	if err := store.Write("system/assistant.json", []byte(`{"enabled":true,"ollama":"http://10.0.0.2:11434","model":"qwen3:8b","users":false,"areas":{"files":"read"}}`)); err != nil {
		t.Fatal(err)
	}
	m := NewManager(store)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	s := m.Settings()
	if s.Cloud() || s.Ollama != "http://10.0.0.2:11434" || s.Model != "qwen3:8b" || s.Access(AreaFiles) != AccessRead {
		t.Fatalf("old file: %+v", s)
	}
	// What this version writes, the older one reads with its own fields.
	s.Provider, s.OpenAIBase, s.Model = ProviderOpenAI, "http://192.168.1.5:1234", "local"
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	var old struct {
		Enabled bool              `json:"enabled"`
		Ollama  string            `json:"ollama"`
		Model   string            `json:"model"`
		Areas   map[string]string `json:"areas"`
	}
	data, _ := os.ReadFile(filepath.Join(dir, "system", "assistant.json"))
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&old); err != nil || !old.Enabled || old.Ollama != "http://10.0.0.2:11434" || old.Areas["files"] != "read" {
		t.Fatalf("read by an older version: %v %+v", err, old)
	}
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if raw["provider"] != "openai" || raw["openaiBase"] != "http://192.168.1.5:1234/v1" {
		t.Fatalf("new fields: %v", raw)
	}
	// Ollama as the provider is written as before: without the field.
	s = m.Settings()
	s.Provider = ProviderOllama
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "system", "assistant.json"))
	if strings.Contains(string(data), `"provider"`) {
		t.Fatalf("ollama written as a provider: %s", data)
	}
}

func TestEndpointRule(t *testing.T) {
	for addr, want := range map[string][2]bool{ // admin, user
		"8.8.8.8":         {true, true},
		"192.168.1.5":     {true, false},
		"100.100.1.1":     {true, false},
		"169.254.169.254": {true, false},
		"127.0.0.1":       {false, false},
		"::1":             {false, false},
		"0.0.0.0":         {false, false},
	} {
		a := netip.MustParseAddr(addr)
		if EndpointAllowed(a, "admin") != want[0] || EndpointAllowed(a, "owner") != want[0] || EndpointAllowed(a, "user") != want[1] {
			t.Errorf("%s: want %v", addr, want)
		}
	}
	// The real clients check the address of the connection: a server on
	// loopback (titand's own ports) is refused before anything is sent.
	fake := &fakeCloud{models: `{"data":[]}`}
	base := fake.serve(t)
	m := NewManager(config.Open(t.TempDir()))
	for _, role := range []string{"admin", "user"} {
		_, err := m.ListModels(context.Background(), Settings{Provider: ProviderOpenAI, OpenAIBase: base, OpenAIKey: "sk"}, role)
		if !errors.Is(err, ErrAddress) {
			t.Fatalf("%s: %v", role, err)
		}
	}
	m.anthropicURL = base
	if _, err := m.ListModels(context.Background(), Settings{Provider: ProviderAnthropic, AnthropicKey: testKey}, "admin"); !errors.Is(err, ErrAddress) {
		t.Fatalf("anthropic on loopback: %v", err)
	}
	if len(fake.headers) != 0 {
		t.Fatal("a request reached the blocked server")
	}
}

func TestCloudFollowsNoRedirect(t *testing.T) {
	target := &fakeCloud{models: `{"data":[{"id":"stolen"}]}`}
	to := target.serve(t)
	srv := httptest.NewServer(http.RedirectHandler(to+"/v1/models", http.StatusTemporaryRedirect))
	t.Cleanup(srv.Close)
	m := cloudManager(t, Settings{})
	_, err := m.ListModels(context.Background(), Settings{Provider: ProviderOpenAI, OpenAIBase: srv.URL, OpenAIKey: "sk-secret"}, "admin")
	if err == nil || len(target.headers) != 0 {
		t.Fatalf("redirect followed: %v", err)
	}
}

func TestProbeUsesStoredCloudKey(t *testing.T) {
	fake := &fakeCloud{replies: [][]string{
		{
			`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_p","name":"probe_status","input":{"target":"probe"}}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		},
		anthropicTurn("Die Prüfung schlug fehl; ob es Updates gibt, ist unbekannt."),
	}}
	m := cloudManager(t, Settings{Provider: ProviderAnthropic, AnthropicKey: testKey})
	m.anthropicURL = fake.serve(t)
	result, err := m.Probe(context.Background(), Settings{Provider: ProviderAnthropic, Model: "claude-sonnet-5-5"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result["provider"] != ProviderAnthropic || result["calls"] != 1 || !strings.Contains(result["answer"].(string), "unbekannt") {
		t.Fatalf("probe: %v", result)
	}
	if fake.headers[0].Get("x-api-key") != testKey {
		t.Fatal("stored key not used")
	}
	if encoded, _ := json.Marshal(result); strings.Contains(string(encoded), testKey) {
		t.Fatal("key in the probe result")
	}
}

func TestPairCallsDropsUnansweredCalls(t *testing.T) {
	call := func(id, name string) ollamaToolCall {
		var c ollamaToolCall
		c.ID, c.Function.Name, c.Function.Arguments = id, name, json.RawMessage(`{}`)
		return c
	}
	got := pairCalls([]ollamaMessage{
		{Role: "user", Content: "a"},
		{Role: "assistant", ToolCalls: []ollamaToolCall{call("1", "x"), call("2", "y")}},
		{Role: "tool", ToolName: "x", ToolCallID: "1", Content: "ok"},
		{Role: "tool", ToolName: "z", ToolCallID: "9", Content: "stray"},
		{Role: "user", Content: "b"},
		{Role: "assistant", ToolCalls: []ollamaToolCall{call("", "x")}},
		{Role: "tool", ToolName: "x", Content: "old chat"},
	})
	if len(got) != 6 || len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "1" || got[2].Content != "ok" || got[3].Content != "b" {
		t.Fatalf("pairs: %+v", got)
	}
	if got[4].ToolCalls[0].ID == "" || got[5].ToolCallID != got[4].ToolCalls[0].ID {
		t.Fatalf("old chat: %+v / %+v", got[4], got[5])
	}
}

func TestRecommendModel(t *testing.T) {
	const gib = 1 << 30
	for _, c := range []struct {
		ram, vram    uint64
		model, basis string
	}{
		{4 * gib, 0, "qwen3:4b", "ram"},
		{7 * gib, 0, "qwen3:4b", "ram"},
		{15 * gib, 0, "qwen3:8b", "ram"},
		{64 * gib, 0, "qwen3:14b", "ram"},
		{64 * gib, 8 * gib, "qwen3:4b", "vram"},
		{8 * gib, 16 * gib, "qwen3:8b", "vram"},
		{8 * gib, 24 * gib, "qwen3:14b", "vram"},
	} {
		if model, basis := RecommendModel(c.ram, c.vram); model != c.model || basis != c.basis {
			t.Errorf("%d/%d: %s %s", c.ram, c.vram, model, basis)
		}
	}
}

func TestSetupChoices(t *testing.T) {
	const gib = 1 << 30
	for _, c := range []struct{ ram, vram uint64 }{{4 * gib, 0}, {15 * gib, 0}, {64 * gib, 0}, {8 * gib, 24 * gib}} {
		model, _ := RecommendModel(c.ram, c.vram)
		found := false
		for _, choice := range SetupChoices(c.ram, c.vram) {
			if !ValidModel(choice.Name) || choice.DiskBytes == 0 || choice.MemoryBytes == 0 {
				t.Errorf("choice %+v", choice)
			}
			if choice.Name == model {
				found = true
				if !choice.Fits {
					t.Errorf("recommended %s does not fit %d/%d", model, c.ram, c.vram)
				}
			}
		}
		if !found {
			t.Errorf("recommended %s not among the choices", model)
		}
	}
	// The GPU's memory decides when there is one.
	for _, choice := range SetupChoices(64*gib, 8*gib) {
		if choice.Name == "qwen3:32b" && choice.Fits || choice.Name == "qwen3:4b" && !choice.Fits {
			t.Errorf("fits by VRAM: %+v", choice)
		}
	}
}
