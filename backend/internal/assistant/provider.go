package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/httpfetch"
	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// Where the model runs. Ollama is the default and what older versions know;
// the cloud providers get chat contents, documents and tool results.
const (
	ProviderOllama    = "ollama"
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// DefaultAnthropicModel is chosen when Anthropic is picked without a model.
const DefaultAnthropicModel = "claude-sonnet-5-5"

// anthropicAPI is fixed; only tests point it elsewhere.
const anthropicAPI = "https://api.anthropic.com"

// keysFile keeps the API keys apart from the settings, in a folder the file
// manager hides, so that older versions never see or rewrite them.
const keysFile = "assistant/keys.json"

// MaxKey is the longest API key accepted.
const MaxKey = 512

var (
	// ErrProvider marks failures of a cloud provider, as opposed to Ollama.
	ErrProvider = errors.New("assistant: model provider")
	// ErrAuth means the provider refused the API key.
	ErrAuth = errors.New("assistant: the provider refused the API key")
	// ErrKeyMissing means a cloud provider was chosen without a key.
	ErrKeyMissing = errors.New("assistant: no API key for the provider")
	// ErrKeyServer means the stored key belongs to another server; it is
	// never sent anywhere else and has to be entered again or removed.
	ErrKeyServer = errors.New("assistant: the stored API key belongs to another server")
	// ErrAddress means the server may not be reached from here (see
	// EndpointAllowed).
	ErrAddress = errors.New("assistant: this address may not be used")
)

// Keys are the API keys on the stick; the OpenAI-compatible key belongs to
// the server it was entered for.
type Keys struct {
	Anthropic  string `json:"anthropic,omitempty"`
	OpenAI     string `json:"openai,omitempty"`
	OpenAIBase string `json:"openaiBase,omitempty"`
}

// providerOf is the provider of settings; empty is Ollama.
func providerOf(s Settings) string {
	if s.Provider == "" {
		return ProviderOllama
	}
	return s.Provider
}

// Cloud reports whether chats go to a provider outside the home network.
func (s Settings) Cloud() bool { return providerOf(s) != ProviderOllama }

// NormalizeBase checks the address of an OpenAI-compatible API and returns
// it without a trailing slash; a bare host gets /v1.
func NormalizeBase(address string) (string, bool) {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	u, err := url.Parse(address)
	if err != nil || len(address) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", false
	}
	if u.Path == "" {
		address += "/v1"
	}
	return address, true
}

func validKey(key string) bool {
	if len(key) > MaxKey {
		return false
	}
	for _, r := range key {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// EndpointAllowed is the rule for OpenAI-compatible servers: owners and
// administrators may use their home network, other accounts only the
// internet; nobody this host's loopback (titand's own ports).
func EndpointAllowed(addr netip.Addr, role string) bool {
	if role == "owner" || role == "admin" {
		return netguard.LAN(addr)
	}
	return netguard.Public(addr)
}

// cloudClient connects only where reach allows, checked at every connection,
// and follows no redirect: a key must not travel to another server.
func cloudClient(reach func(netip.Addr) bool) *http.Client {
	client := httpfetch.NewClient(httpfetch.Policy{Allow: func(a netip.AddrPort) bool { return reach(a.Addr()) }})
	if tr, ok := client.Transport.(*http.Transport); ok {
		// Long prompts take a while before the first byte.
		tr.ResponseHeaderTimeout = 10 * time.Minute
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// toolDef is a tool as every provider gets it.
type toolDef struct {
	Name        string
	Description string
	Parameters  any
}

// chatRequest is one round with the model, in the terms of the agent loop.
type chatRequest struct {
	Model    string
	Messages []ollamaMessage
	Tools    []toolDef
	// UseTools says whether the model may call tools in this round. Tools
	// still go along, because Anthropic needs them for earlier calls.
	UseTools    bool
	Think       bool
	NumCtx      int
	Temperature *float64
}

// backend is where the agent loop sends its rounds. Every backend answers in
// chunks of Ollama's shape; the last one has Done set.
type backend interface {
	chat(ctx context.Context, req chatRequest, each func(ollamaChunk) error) error
	show(ctx context.Context, model string) ([]string, int, error)
	models(ctx context.Context) ([]Model, error)
}

// withKeys fills in the stored keys for settings that bring none: the
// Anthropic key always, the OpenAI-compatible key only for its own server.
func (m *Manager) withKeys(s Settings) Settings {
	m.mu.Lock()
	keys := m.keys
	m.mu.Unlock()
	if s.AnthropicKey == "" && !s.ClearAnthropicKey {
		s.AnthropicKey = keys.Anthropic
	}
	if s.OpenAIKey == "" && !s.ClearOpenAIKey && keys.OpenAIBase != "" {
		if base, ok := NormalizeBase(s.OpenAIBase); ok && base == keys.OpenAIBase {
			s.OpenAIKey = keys.OpenAI
		}
	}
	return s
}

// backendFor returns the backend of settings that carry their keys. role is
// who chose the OpenAI-compatible address.
func (m *Manager) backendFor(s Settings, role string) (backend, error) {
	switch providerOf(s) {
	case ProviderOllama:
		return ollamaBackend{m: m, base: s.Ollama}, nil
	case ProviderAnthropic:
		if s.AnthropicKey == "" {
			return nil, ErrKeyMissing
		}
		return anthropicBackend{cloud{name: "Anthropic", client: m.anthropicClient, key: s.AnthropicKey}, m.anthropicURL}, nil
	case ProviderOpenAI:
		base, ok := NormalizeBase(s.OpenAIBase)
		if !ok {
			return nil, ErrInvalid
		}
		client := m.lanClient
		if role != "owner" && role != "admin" {
			client = m.publicClient
		}
		return openaiBackend{cloud{name: "OpenAI-compatible server", client: client, key: s.OpenAIKey}, base}, nil
	}
	return nil, ErrInvalid
}

// providerError is a failure of a cloud provider. Its text never holds the
// key; kinds say what the agent loop and the API make of it.
type providerError struct {
	text  string
	kinds []error
}

func (e *providerError) Error() string   { return e.text }
func (e *providerError) Unwrap() []error { return e.kinds }

func (c cloud) fail(text string, kinds ...error) error {
	if c.key != "" {
		text = strings.ReplaceAll(text, c.key, "***")
	}
	return &providerError{text: c.name + ": " + text, kinds: append([]error{ErrUpstream, ErrProvider}, kinds...)}
}

// cloud is the HTTP side shared by the cloud providers.
type cloud struct {
	name   string
	client *http.Client
	key    string
}

// do sends a request and returns an answer with status 200, or a failure
// that says what it was without the body of a refused key.
func (c cloud) do(ctx context.Context, method, address string, header map[string]string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return nil, ErrInvalid
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, httpfetch.ErrBlocked) {
			return nil, c.fail("this address may not be used ("+req.URL.Host+")", ErrAddress)
		}
		var failed *url.Error
		if errors.As(err, &failed) {
			err = failed.Err
		}
		return nil, c.fail(fmt.Sprintf("not reachable at %s: %v", req.URL.Host, err), ErrTransient)
	}
	if res.StatusCode == http.StatusOK {
		return res, nil
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var problem struct {
		Error json.RawMessage `json:"error"`
	}
	message := strings.TrimSpace(string(data))
	if json.Unmarshal(data, &problem) == nil && len(problem.Error) > 0 {
		var detail struct {
			Type    string `json:"type"`
			Code    any    `json:"code"`
			Message string `json:"message"`
		}
		var text string
		if json.Unmarshal(problem.Error, &detail) == nil && detail.Message != "" {
			message = detail.Message
		} else if json.Unmarshal(problem.Error, &text) == nil && text != "" {
			message = text
		}
	}
	message = truncate(message, 500)
	switch code := res.StatusCode; {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		// Some providers repeat part of the key; the text stays out.
		return nil, c.fail(fmt.Sprintf("answered %d: the API key was refused", code), ErrAuth)
	case code == http.StatusNotFound && strings.Contains(strings.ToLower(message), "model"):
		return nil, c.fail(fmt.Sprintf("answered %d: %s", code, message), ErrModelMissing)
	case code == 408 || code == 409 || code == 425 || code == 429 || code >= 500:
		return nil, c.fail(fmt.Sprintf("answered %d: %s", code, message), ErrTransient)
	case code >= 300 && code < 400:
		return nil, c.fail(fmt.Sprintf("answered %d: redirects are not followed", code))
	}
	return nil, c.fail(fmt.Sprintf("answered %d: %s", res.StatusCode, message))
}

// sse reads the data lines of a server-sent event stream.
func (c cloud) sse(res *http.Response, each func(data []byte) error) error {
	err := stream(res, func(line []byte) error {
		if !strings.HasPrefix(string(line), "data:") {
			return nil // event:, id:, comments
		}
		return each([]byte(strings.TrimSpace(string(line[len("data:"):]))))
	})
	var pe *providerError
	if err != nil && errors.Is(err, ErrUpstream) && !errors.As(err, &pe) {
		return c.fail(err.Error(), ErrTransient)
	}
	return err
}

// pairCalls keeps tool calls and their results together, as both cloud APIs
// demand: a call without a result (an interrupted run) or a result without
// its call is left out. Calls without an ID (older chats) get one.
func pairCalls(messages []ollamaMessage) []ollamaMessage {
	out := make([]ollamaMessage, 0, len(messages))
	n := 0
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "tool" {
			continue // only directly after its call, below
		}
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			out = append(out, msg)
			continue
		}
		calls := append([]ollamaToolCall(nil), msg.ToolCalls...)
		for j := range calls {
			if calls[j].ID == "" {
				n++
				calls[j].ID = fmt.Sprintf("call_%d", n)
			}
		}
		answered := map[string]bool{}
		var results []ollamaMessage
		for ; i+1 < len(messages) && messages[i+1].Role == "tool"; i++ {
			result := messages[i+1]
			if result.ToolCallID == "" {
				for _, call := range calls {
					if !answered[call.ID] && call.Function.Name == result.ToolName {
						result.ToolCallID = call.ID
						break
					}
				}
			}
			known := false
			for _, call := range calls {
				known = known || call.ID == result.ToolCallID
			}
			if known && !answered[result.ToolCallID] {
				answered[result.ToolCallID] = true
				results = append(results, result)
			}
		}
		kept := calls[:0]
		for _, call := range calls {
			if answered[call.ID] {
				kept = append(kept, call)
			}
		}
		msg.ToolCalls = kept
		if len(kept) > 0 || msg.Content != "" {
			out = append(out, msg)
		}
		out = append(out, results...)
	}
	return out
}

// imageType names the format of a base64 image for the cloud APIs.
func imageType(data string) string {
	head := data
	if len(head) > 64 {
		head = head[:64]
	}
	switch {
	case strings.HasPrefix(head, "/9j/"):
		return "image/jpeg"
	case strings.HasPrefix(head, "R0lGOD"):
		return "image/gif"
	case strings.HasPrefix(head, "UklGR"):
		return "image/webp"
	}
	return "image/png"
}

// callArguments is what a provider gets for the arguments of an earlier
// call: an object, also when the model emitted something invalid (the tool
// result that follows says why it was not run).
func callArguments(raw json.RawMessage) json.RawMessage {
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return json.RawMessage("{}")
	}
	return raw
}
