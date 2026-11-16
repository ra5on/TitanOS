package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Outside providers (0.96, owner 16 Nov 2026: not every NAS has a graphics
// card). Besides an Ollama server the agent can use a service that speaks
// the OpenAI chat API (OpenAI, OpenRouter, Groq, Mistral, LM Studio …) or
// Claude. The rest of TitanOS keeps working with *Ollama: a client made by
// NewOpenAI or NewClaude carries a remote and hands every call to it. What
// only an Ollama server can do (loading and pulling models, looking at
// pictures with a second model) answers with a plain error there.
//
// With such a provider everything the agent reads – file contents, names
// of devices, logs – leaves the NAS. That is why it is off from the
// factory, set only by the owner, and shown in the chat (Cloud).

// Provider kinds.
const (
	ProviderOllama = ""
	ProviderOpenAI = "openai"
	ProviderClaude = "claude"
)

// ValidProvider reports whether kind is one of the provider kinds.
func ValidProvider(kind string) bool {
	return kind == ProviderOllama || kind == ProviderOpenAI || kind == ProviderClaude
}

// remote is a provider other than Ollama.
type remote interface {
	kind() string
	address() string
	chat(ctx context.Context, r ChatRequest, onChunk func(Chunk)) (Message, Stats, error)
	models(ctx context.Context) ([]Model, error)
}

// ErrOllamaOnly is what an outside provider answers where only an Ollama
// server can help.
var ErrOllamaOnly = errors.New("this needs a model on an Ollama server; the outside provider cannot do it")

// remoteClient is the HTTP client for a provider: no proxy, no redirects,
// and no overall timeout, because an answer streams as long as it is
// written.
func remoteClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			ResponseHeaderTimeout: 5 * time.Minute,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   20 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ParseProviderURL checks the address of an OpenAI-compatible service, for
// example https://api.openai.com/v1 or http://192.168.1.20:1234/v1.
func ParseProviderURL(address string) (*url.URL, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("no address")
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("the address must be http(s)://host/path without a login")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	// A key never travels in the clear over the internet.
	if u.Scheme == "http" && !homeHost(u.Hostname()) {
		return nil, errors.New("an address outside the home network must be https")
	}
	return u, nil
}

// homeHost reports whether a host name is this NAS or a device of the home
// network, as far as the name alone can tell: a private or local address,
// or a name without a public ending.
func homeHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	// An address first: an IPv6 address has no dot either, and a public
	// one is not the home network (0.99).
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, end := range []string{".local", ".lan", ".home", ".internal", ".fritz.box", ".home.arpa"} {
		if strings.HasSuffix(host, end) {
			return true
		}
	}
	return false
}

// Provider is the kind of the client: "" for Ollama.
func (o *Ollama) Provider() string {
	if o.remote == nil {
		return ProviderOllama
	}
	return o.remote.kind()
}

// Cloud reports whether what the agent sends leaves the home network: an
// outside provider at an address that is not one of the home network.
// Ollama servers count as local; they are set by address on purpose.
func (o *Ollama) Cloud() bool {
	if o.remote == nil {
		return false
	}
	u, err := url.Parse(o.remote.address())
	return err != nil || !homeHost(u.Hostname())
}

// remoteInfo is what Show answers for a model of an outside provider:
// tools always (the agent needs them and cannot ask), the context length
// the provider names.
func (o *Ollama) remoteInfo(ctx context.Context, model string) (ModelInfo, error) {
	info := ModelInfo{Capabilities: []string{"completion", "tools"}}
	list, err := o.remoteModels(ctx)
	if err != nil {
		return info, err
	}
	for _, m := range list {
		if m.Name == model {
			info.ContextLength = m.ContextLength
			info.Capabilities = m.Capabilities
		}
	}
	return info, nil
}

var remoteCache struct {
	sync.Mutex
	key  string
	at   time.Time
	list []Model
}

// remoteModels lists the models of the provider; the list is kept for a
// minute, because pages ask for it often and providers count requests.
func (o *Ollama) remoteModels(ctx context.Context) ([]Model, error) {
	key := o.remote.kind() + "|" + o.remote.address()
	remoteCache.Lock()
	if remoteCache.key == key && time.Since(remoteCache.at) < time.Minute {
		list := remoteCache.list
		remoteCache.Unlock()
		return list, nil
	}
	remoteCache.Unlock()
	list, err := o.remote.models(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	remoteCache.Lock()
	remoteCache.key, remoteCache.at, remoteCache.list = key, time.Now(), list
	remoteCache.Unlock()
	return list, nil
}

// ----- turns: what both providers need from the chat -----

// remoteTurn is one message of the chat with the ids both APIs want for
// tool calls. Ollama has none; they are counted here, the same for the
// same chat every time.
type remoteTurn struct {
	Message
	// CallIDs are the ids of the calls of an assistant message.
	CallIDs []string
	// CallID is the call a tool message answers.
	CallID string
}

// remoteTurns pairs every tool message with a call of the assistant
// message before it, in order. A call without an answer (a step that waits
// or was not run) gets one, because both APIs refuse a call left open; an
// answer without a call becomes a plain message of the person.
func remoteTurns(msgs []Message) []remoteTurn {
	var out []remoteTurn
	var open []string // ids of calls not answered yet
	var names []string
	closeOpen := func() {
		for i, id := range open {
			out = append(out, remoteTurn{Message: Message{Role: "tool", ToolName: names[i], Content: "Not run."}, CallID: id})
		}
		open, names = nil, nil
	}
	n := 0
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			closeOpen()
			t := remoteTurn{Message: m}
			for _, c := range m.ToolCalls {
				n++
				id := fmt.Sprintf("call_%d", n)
				t.CallIDs = append(t.CallIDs, id)
				open = append(open, id)
				names = append(names, c.Function.Name)
			}
			out = append(out, t)
		case "tool":
			if len(open) == 0 {
				out = append(out, remoteTurn{Message: Message{Role: "user", Content: "Result of " + m.ToolName + ":\n" + m.Content}})
				continue
			}
			// The answer to the call of that name if there is one, else the oldest.
			at := 0
			for i, name := range names {
				if name == m.ToolName {
					at = i
					break
				}
			}
			out = append(out, remoteTurn{Message: m, CallID: open[at]})
			open = append(open[:at], open[at+1:]...)
			names = append(names[:at], names[at+1:]...)
		default:
			closeOpen()
			out = append(out, remoteTurn{Message: m})
		}
	}
	closeOpen()
	return out
}

// imageType names the kind of a picture from the start of its base64 text.
func imageType(b64 string) string {
	switch {
	case strings.HasPrefix(b64, "iVBOR"):
		return "image/png"
	case strings.HasPrefix(b64, "R0lG"):
		return "image/gif"
	case strings.HasPrefix(b64, "UklG"):
		return "image/webp"
	}
	return "image/jpeg"
}

// ----- OpenAI-compatible -----

type openAI struct {
	base   *url.URL
	key    string
	client *http.Client
}

// NewOpenAI returns a client for a service that speaks the OpenAI chat
// API. The key may be empty for a server on the home network.
func NewOpenAI(address, key string) (*Ollama, error) {
	u, err := ParseProviderURL(address)
	if err != nil {
		return nil, err
	}
	return &Ollama{base: u, remote: &openAI{base: u, key: strings.TrimSpace(key), client: remoteClient()}}, nil
}

func (p *openAI) kind() string    { return ProviderOpenAI }
func (p *openAI) address() string { return p.base.String() }

func (p *openAI) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base.String()+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.key != "" {
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if res.StatusCode/100 != 2 {
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
		return nil, providerError(res.StatusCode, raw)
	}
	return res, nil
}

// providerError makes the error of a refused request: the status and the
// provider's own words, never the key.
func providerError(status int, body []byte) error {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	text := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &e) == nil && len(e.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		var plain string
		switch {
		case json.Unmarshal(e.Error, &obj) == nil && obj.Message != "":
			text = obj.Message
		case json.Unmarshal(e.Error, &plain) == nil && plain != "":
			text = plain
		}
	}
	if r := []rune(text); len(r) > 400 {
		text = string(r[:400])
	}
	return providerStatus(status, text)
}

// ProviderError is an answer of an outside provider with an HTTP error
// status. The run tries again after a failure of the provider's server
// (5xx, overloaded), as it does with Ollama (0.99).
type ProviderError struct {
	Status int
	Text   string
}

func (e *ProviderError) Error() string { return e.Text }

func providerStatus(status int, text string) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		text = fmt.Sprintf("the provider refused the key (%d): %s", status, text)
	case http.StatusTooManyRequests:
		text = fmt.Sprintf("the provider's limit is reached (429): %s", text)
	default:
		text = fmt.Sprintf("the provider answered %d: %s", status, text)
	}
	return &ProviderError{Status: status, Text: text}
}

func (p *openAI) models(ctx context.Context) ([]Model, error) {
	res, err := p.request(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var data struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			ContextWindow int    `json:"context_window"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&data); err != nil {
		return nil, errors.New("the provider's list of models could not be read")
	}
	var out []Model
	for _, m := range data.Data {
		if m.ID == "" {
			continue
		}
		out = append(out, Model{Name: m.ID, Capabilities: []string{"completion", "tools"}, ContextLength: max(m.ContextLength, m.ContextWindow)})
	}
	return out, nil
}

func (p *openAI) chat(ctx context.Context, r ChatRequest, onChunk func(Chunk)) (Message, Stats, error) {
	type part map[string]any
	var msgs []map[string]any
	for _, t := range remoteTurns(r.Messages) {
		switch t.Role {
		case "assistant":
			m := map[string]any{"role": "assistant", "content": t.Content}
			var calls []map[string]any
			for i, c := range t.ToolCalls {
				args := string(c.Function.Arguments)
				if strings.TrimSpace(args) == "" || args == "null" {
					args = "{}"
				}
				calls = append(calls, map[string]any{"id": t.CallIDs[i], "type": "function",
					"function": map[string]any{"name": c.Function.Name, "arguments": args}})
			}
			if len(calls) > 0 {
				m["tool_calls"] = calls
			}
			msgs = append(msgs, m)
		case "tool":
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": t.CallID, "content": t.Content})
		default:
			if len(t.Images) == 0 {
				msgs = append(msgs, map[string]any{"role": t.Role, "content": t.Content})
				continue
			}
			parts := []part{{"type": "text", "text": t.Content}}
			for _, img := range t.Images {
				parts = append(parts, part{"type": "image_url", "image_url": part{"url": "data:" + imageType(img) + ";base64," + img}})
			}
			msgs = append(msgs, map[string]any{"role": t.Role, "content": parts})
		}
	}
	body := map[string]any{"model": r.Model, "messages": msgs, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	if len(r.Tools) > 0 {
		body["tools"] = r.Tools
	}
	res, err := p.request(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return Message{}, Stats{}, err
	}
	defer res.Body.Close()

	out := Message{Role: "assistant"}
	var stats Stats
	type building struct {
		name string
		args strings.Builder
	}
	var calls []*building
	size := 0
	// How the answer ended: "[DONE]" or a finish_reason. Without either the
	// connection closed in the middle; half an answer is no answer (0.99).
	ended, finish := false, ""
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 64<<10), 4<<20)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			ended = true
			break
		}
		var chunk struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					Reasoning        string `json:"reasoning"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int `json:"index"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return Message{}, Stats{}, providerError(http.StatusBadGateway, []byte(data))
		}
		if chunk.Usage != nil {
			stats.PromptTokens, stats.OutputTokens = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				ended, finish = true, choice.FinishReason
			}
			d := choice.Delta
			thinking := d.Reasoning + d.ReasoningContent
			size += len(d.Content) + len(thinking)
			if size > MaxAnswerBytes {
				return Message{}, Stats{}, errors.New("the answer of the provider is too large")
			}
			out.Content += d.Content
			out.Thinking += thinking
			if onChunk != nil && (d.Content != "" || thinking != "") {
				onChunk(Chunk{Content: d.Content, Thinking: thinking})
			}
			for _, tc := range d.ToolCalls {
				if tc.Index < 0 || tc.Index >= MaxToolCalls {
					return Message{}, Stats{}, errors.New("the provider sent too many tool calls")
				}
				for len(calls) <= tc.Index {
					calls = append(calls, &building{})
				}
				b := calls[tc.Index]
				if tc.Function.Name != "" {
					b.name = tc.Function.Name
				}
				size += len(tc.Function.Arguments)
				if size > MaxAnswerBytes {
					return Message{}, Stats{}, errors.New("the answer of the provider is too large")
				}
				b.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := scan.Err(); err != nil {
		if ctx.Err() != nil {
			return Message{}, Stats{}, ctx.Err()
		}
		return Message{}, Stats{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if !ended {
		return Message{}, Stats{}, fmt.Errorf("%w: the provider's answer ended in the middle", ErrUnreachable)
	}
	if finish == "length" {
		// A call cut off at the provider's limit cannot be run.
		calls = nil
		out.Content += "\n\n[The answer was cut off at the provider's limit for one answer.]"
	}
	for _, b := range calls {
		if b.name == "" {
			continue
		}
		args := strings.TrimSpace(b.args.String())
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			// Never run a call with other arguments than the model wrote.
			return Message{}, Stats{}, fmt.Errorf("invalid tool call: the arguments of %s could not be parsed", b.name)
		}
		var call ToolCall
		call.Function.Name, call.Function.Arguments = b.name, json.RawMessage(args)
		out.ToolCalls = append(out.ToolCalls, call)
	}
	return out, stats, nil
}
