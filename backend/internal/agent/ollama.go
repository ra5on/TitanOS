// Package agent is the AI agent of TitanOS (PLAN 44): it takes a task in a
// chat, works through it in several steps with the tools of the catalog and
// answers. The model runs on an Ollama server the owner names; nothing here
// talks to other providers.
//
// The package holds what does not need the web API: the Ollama client, the
// rules about what the agent may do (policy.go), the run of one task
// (loop.go), the files of chats, projects and memory (store.go) and the web
// search (web.go). The tools themselves and the HTTP routes live in package
// api, which hands them in.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnreachable is an Ollama server that does not answer.
var ErrUnreachable = errors.New("agent: Ollama not reachable")

// Ollama talks to one Ollama server.
type Ollama struct {
	base   *url.URL
	client *http.Client
}

// NewOllama checks the address and returns a client for it. Only http and
// https without a login are allowed; the address is set by an owner or
// administrator, usually on the home network.
func NewOllama(address string) (*Ollama, error) {
	u, err := ParseOllamaURL(address)
	if err != nil {
		return nil, err
	}
	return &Ollama{base: u, client: &http.Client{
		// No overall timeout: a chat streams as long as the model writes.
		// Each request has its own context.
		Transport: &http.Transport{
			Proxy:                 nil,
			ResponseHeaderTimeout: 5 * time.Minute,
			IdleConnTimeout:       90 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// ClusterAddress stands for the Ollama server of the admin node of a
// cluster (PLAN 47); requests go through the cluster, never to this name.
const ClusterAddress = "http://titanos-cluster-admin/ollama"

// NewOllamaVia returns a client for the Ollama server of the admin node:
// rt carries each request through the cluster.
func NewOllamaVia(rt http.RoundTripper) *Ollama {
	u, _ := url.Parse(ClusterAddress)
	return &Ollama{base: u, client: &http.Client{
		Transport:     rt,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// ParseOllamaURL checks the address of an Ollama server.
func ParseOllamaURL(address string) (*url.URL, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("no address")
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("the address must be http(s)://host:port without a login")
	}
	if u.Port() == "" && u.Scheme == "http" {
		u.Host += ":11434"
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

// Address is the normalised address of the server.
func (o *Ollama) Address() string { return o.base.String() }

func (o *Ollama) endpoint(p string) string {
	u := *o.base
	u.Path += p
	return u.String()
}

func (o *Ollama) do(ctx context.Context, method, p string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.endpoint(p), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if res.StatusCode != http.StatusOK {
		return ollamaError(res.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// StatusError is an answer of Ollama with an HTTP error status, so callers
// can tell a bad request (one picture) from a server problem.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return "Ollama: " + e.Message
	}
	return fmt.Sprintf("Ollama answered HTTP %d", e.Status)
}

func ollamaError(status int, data []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &e)
	return &StatusError{Status: status, Message: e.Error}
}

// Version is the version of the server.
func (o *Ollama) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := o.do(ctx, http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}

// Model is one installed model.
type Model struct {
	Name          string   `json:"name"`
	Size          int64    `json:"size"`
	Family        string   `json:"family,omitempty"`
	Parameters    string   `json:"parameters,omitempty"`
	Capabilities  []string `json:"capabilities"`
	ContextLength int      `json:"contextLength,omitempty"`
}

// Tools reports whether the model can call tools; the agent needs that.
func (m Model) Tools() bool { return contains(m.Capabilities, "tools") }

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Models are the installed models. Newer Ollama versions put the
// capabilities into the list; for older ones Show fills them in.
func (o *Ollama) Models(ctx context.Context) ([]Model, error) {
	var tags struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Details struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				ContextLength int    `json:"context_length"`
			} `json:"details"`
			Capabilities []string `json:"capabilities"`
		} `json:"models"`
	}
	if err := o.do(ctx, http.MethodGet, "/api/tags", nil, &tags); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(tags.Models))
	for _, t := range tags.Models {
		m := Model{Name: t.Name, Size: t.Size, Family: t.Details.Family, Parameters: t.Details.ParameterSize, Capabilities: t.Capabilities, ContextLength: t.Details.ContextLength}
		if m.Capabilities == nil {
			if info, err := o.Show(ctx, t.Name); err == nil {
				m.Capabilities, m.ContextLength = info.Capabilities, info.ContextLength
			}
		}
		if m.Capabilities == nil {
			m.Capabilities = []string{}
		}
		models = append(models, m)
	}
	return models, nil
}

// Thinking are the thinking controls a model advertises.
type Thinking struct {
	// Values are booleans (on/off) and names of levels, as the model
	// reports them; only false means it cannot think.
	Values  []any `json:"values"`
	Default any   `json:"default"`
}

// ModelInfo is what /api/show tells about a model.
type ModelInfo struct {
	Capabilities  []string  `json:"capabilities"`
	Thinking      *Thinking `json:"thinking,omitempty"`
	ContextLength int       `json:"contextLength,omitempty"`
}

// Show reads the details of one model.
func (o *Ollama) Show(ctx context.Context, model string) (ModelInfo, error) {
	var raw struct {
		Capabilities []string                   `json:"capabilities"`
		Thinking     *Thinking                  `json:"thinking"`
		ModelInfo    map[string]json.RawMessage `json:"model_info"`
	}
	if err := o.do(ctx, http.MethodPost, "/api/show", map[string]any{"model": model}, &raw); err != nil {
		return ModelInfo{}, err
	}
	info := ModelInfo{Capabilities: raw.Capabilities, Thinking: raw.Thinking}
	for key, v := range raw.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			_ = json.Unmarshal(v, &info.ContextLength)
		}
	}
	if info.Capabilities == nil {
		info.Capabilities = []string{}
	}
	return info, nil
}

// Running is a model loaded right now, with the context it was loaded
// with: what "as set in Ollama" means at this moment.
type Running struct {
	Name          string `json:"name"`
	ContextLength int    `json:"contextLength"`
	VRAM          int64  `json:"vram"`
}

// Running lists the loaded models.
func (o *Ollama) Running(ctx context.Context) ([]Running, error) {
	var ps struct {
		Models []struct {
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			SizeVRAM      int64  `json:"size_vram"`
		} `json:"models"`
	}
	if err := o.do(ctx, http.MethodGet, "/api/ps", nil, &ps); err != nil {
		return nil, err
	}
	out := []Running{}
	for _, m := range ps.Models {
		out = append(out, Running{Name: m.Name, ContextLength: m.ContextLength, VRAM: m.SizeVRAM})
	}
	return out, nil
}

// Levels are the thinking choices of a model in its own order, as strings:
// named levels as they are, "on"/"off" for booleans. Empty means the model
// cannot think or says nothing about it.
func Levels(t *Thinking) []string {
	if t == nil {
		return nil
	}
	var out []string
	for _, v := range t.Values {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case bool:
			if x {
				out = append(out, "on")
			} else {
				out = append(out, "off")
			}
		}
	}
	if len(out) == 1 && out[0] == "off" {
		return nil
	}
	return out
}

// DefaultLevel is the level the model uses when think is not set.
func DefaultLevel(t *Thinking) string {
	if t == nil {
		return ""
	}
	switch x := t.Default.(type) {
	case string:
		return x
	case bool:
		if x {
			return "on"
		}
		return "off"
	}
	return ""
}

// ThinkValue turns a chosen level into the think field of a request. A
// level the model does not list is not sent, so the model keeps its own
// default instead of falling back silently to it.
func ThinkValue(t *Thinking, level string) any {
	if level == "" {
		return nil
	}
	for _, l := range Levels(t) {
		if l != level {
			continue
		}
		switch level {
		case "on":
			return true
		case "off":
			return false
		}
		return level
	}
	return nil
}

// Message is one message of a chat with the model.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolName names the tool a "tool" message answers.
	ToolName string `json:"tool_name,omitempty"`
	// Images (base64) for a model that sees pictures (PLAN 49).
	Images []string `json:"images,omitempty"`
}

// ToolCall is a call the model wants made.
type ToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// ToolSpec offers one tool to the model.
type ToolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
	// Area and Kind are for the runner, never sent to Ollama.
	Area string `json:"-"`
	Kind string `json:"-"`
}

// ChatRequest is one round with the model.
type ChatRequest struct {
	Model    string
	Messages []Message
	Tools    []ToolSpec
	Think    any
	// Context is num_ctx; 0 keeps what Ollama has set.
	Context int
	// KeepAlive is how long Ollama keeps the model loaded ("30m", "-1");
	// empty keeps Ollama's setting.
	KeepAlive string
}

// Stats are the counters Ollama returns at the end of a round.
type Stats struct {
	PromptTokens int `json:"promptTokens"`
	OutputTokens int `json:"outputTokens"`
}

// Chunk is a piece of the streamed answer.
type Chunk struct {
	Thinking string
	Content  string
}

// Chat sends one round and streams thinking and text to onChunk. It
// returns the whole message, including the tool calls.
func (o *Ollama) Chat(ctx context.Context, r ChatRequest, onChunk func(Chunk)) (Message, Stats, error) {
	body := map[string]any{"model": r.Model, "messages": r.Messages, "stream": true}
	if len(r.Tools) > 0 {
		body["tools"] = r.Tools
	}
	if r.Think != nil {
		body["think"] = r.Think
	}
	if r.Context > 0 {
		body["options"] = map[string]any{"num_ctx": r.Context}
	}
	if r.KeepAlive != "" {
		body["keep_alive"] = r.KeepAlive
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Message{}, Stats{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint("/api/chat"), bytes.NewReader(data))
	if err != nil {
		return Message{}, Stats{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := o.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, Stats{}, ctx.Err()
		}
		return Message{}, Stats{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		return Message{}, Stats{}, ollamaError(res.StatusCode, data)
	}
	var msg Message
	var content, thinking strings.Builder
	var stats Stats
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var part struct {
			Message         Message `json:"message"`
			Done            bool    `json:"done"`
			Error           string  `json:"error"`
			PromptEvalCount int     `json:"prompt_eval_count"`
			EvalCount       int     `json:"eval_count"`
		}
		if err := json.Unmarshal(line, &part); err != nil {
			return Message{}, stats, fmt.Errorf("Ollama sent an unreadable line: %v", err)
		}
		if part.Error != "" {
			return Message{}, stats, fmt.Errorf("Ollama: %s", part.Error)
		}
		if part.Message.Thinking != "" || part.Message.Content != "" {
			thinking.WriteString(part.Message.Thinking)
			content.WriteString(part.Message.Content)
			if onChunk != nil {
				onChunk(Chunk{Thinking: part.Message.Thinking, Content: part.Message.Content})
			}
		}
		msg.ToolCalls = append(msg.ToolCalls, part.Message.ToolCalls...)
		if part.Done {
			stats = Stats{PromptTokens: part.PromptEvalCount, OutputTokens: part.EvalCount}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return Message{}, stats, ctx.Err()
		}
		return Message{}, stats, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	msg.Role = "assistant"
	msg.Content = content.String()
	msg.Thinking = thinking.String()
	return msg, stats, nil
}

// Preload asks Ollama to load a model now, so the first answer does not
// wait for it. A chat without messages only loads.
func (o *Ollama) Preload(ctx context.Context, model, keepAlive string, numCtx int) error {
	body := map[string]any{"model": model, "messages": []any{}}
	if keepAlive != "" {
		body["keep_alive"] = keepAlive
	}
	if numCtx > 0 {
		body["options"] = map[string]any{"num_ctx": numCtx}
	}
	return o.do(ctx, http.MethodPost, "/api/chat", body, nil)
}

// PullProgress is a line Ollama sends while it downloads a model.
type PullProgress struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Pull downloads a model and reports the progress.
func (o *Ollama) Pull(ctx context.Context, model string, progress func(PullProgress)) error {
	data, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint("/api/pull"), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		return ollamaError(res.StatusCode, data)
	}
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		var p PullProgress
		if json.Unmarshal(scanner.Bytes(), &p) != nil {
			continue
		}
		if p.Error != "" {
			return fmt.Errorf("Ollama: %s", p.Error)
		}
		progress(p)
	}
	return scanner.Err()
}

// Look asks a model that sees pictures about one image and returns its
// answer (PLAN 49). It does not stream: the agent waits for the whole text.
// numCtx is sent when the picture model is the main model, so that Ollama
// does not load it anew with another context.
func (o *Ollama) Look(ctx context.Context, model, prompt string, image []byte, keepAlive string, numCtx int) (string, error) {
	body := map[string]any{"model": model, "stream": false,
		"messages": []Message{{Role: "user", Content: prompt, Images: []string{base64.StdEncoding.EncodeToString(image)}}}}
	if keepAlive != "" {
		body["keep_alive"] = keepAlive
	}
	if numCtx > 0 {
		body["options"] = map[string]any{"num_ctx": numCtx}
	}
	var out struct {
		Message Message `json:"message"`
	}
	if err := o.do(ctx, http.MethodPost, "/api/chat", body, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Message.Content), nil
}
