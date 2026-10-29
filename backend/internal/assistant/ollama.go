package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

// ErrModelOutput distinguishes malformed generated calls from connectivity.
var ErrModelOutput = errors.New("model generated an invalid tool call")

func modelResponseError(message string) error {
	if strings.Contains(strings.ToLower(message), "error parsing tool call") {
		return fmt.Errorf("%w: %w: %s", ErrUpstream, ErrModelOutput, message)
	}
	return fmt.Errorf("%w: %s", ErrUpstream, message)
}

// What titand asks Ollama for. See https://github.com/ollama/ollama/blob/main/docs/api.md.

type ollamaToolCall struct {
	// ID pairs a call with its result for the cloud providers; Ollama
	// pairs them by order and never sees it.
	ID       string `json:"-"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	// ToolCallID is the call a tool result answers (cloud providers only).
	ToolCallID string `json:"-"`
}

type ollamaChunk struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
	Error   string        `json:"error"`
	// Counts of the last chunk: tokens read and written, time in nanoseconds.
	PromptEvalCount int   `json:"prompt_eval_count"`
	EvalCount       int   `json:"eval_count"`
	TotalDuration   int64 `json:"total_duration"`
}

func (m *Manager) request(ctx context.Context, method, base, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := m.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The address is in the message already; what failed is enough.
		var failed *url.Error
		if errors.As(err, &failed) {
			err = failed.Err
		}
		return nil, fmt.Errorf("%w: %w is not reachable at %s: %v", ErrTransient, ErrUpstream, req.URL.Host, err)
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &problem)
		if problem.Error == "" {
			problem.Error = strings.TrimSpace(string(data))
		}
		if res.StatusCode == http.StatusNotFound && strings.Contains(problem.Error, "not found") {
			return nil, fmt.Errorf("%w: %s", ErrModelMissing, problem.Error)
		}
		if res.StatusCode == 429 || res.StatusCode == 502 || res.StatusCode == 503 || res.StatusCode == 504 {
			return nil, fmt.Errorf("%w: %w: HTTP %d", ErrUpstream, ErrTransient, res.StatusCode)
		}
		return nil, modelResponseError(fmt.Sprintf("Ollama answered %d: %s", res.StatusCode, problem.Error))
	}
	return res, nil
}

func (m *Manager) call(ctx context.Context, method, base, path string, body, answer any) error {
	res, err := m.request(ctx, method, base, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if answer == nil {
		return nil
	}
	if err := json.Unmarshal(data, answer); err != nil {
		return fmt.Errorf("%w sent something unexpected: %v", ErrUpstream, err)
	}
	return nil
}

// stream reads the lines of a streaming answer of Ollama.
func stream(res *http.Response, each func(line []byte) error) error {
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := each(line); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return nil
}

// Model is a model Ollama has.
type Model struct {
	Name          string   `json:"name"`
	SizeBytes     int64    `json:"sizeBytes"`
	Parameters    string   `json:"parameters,omitempty"`
	Quantization  string   `json:"quantization,omitempty"`
	Capabilities  []string `json:"capabilities"`
	ContextLength int      `json:"contextLength,omitempty"`
}

// Can reports whether a model has a capability: tools, vision, thinking.
func (m Model) Can(capability string) bool { return slices.Contains(m.Capabilities, capability) }

type capabilities struct {
	list    []string
	context int
	at      time.Time
}

// show asks Ollama what a model can, and keeps the answer for a while.
func (m *Manager) show(ctx context.Context, base, model string) ([]string, int, error) {
	m.mu.Lock()
	cached, ok := m.capabilities[base+" "+model]
	m.mu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.list, cached.context, nil
	}
	var answer struct {
		Capabilities []string       `json:"capabilities"`
		ModelInfo    map[string]any `json:"model_info"`
	}
	if err := m.call(ctx, http.MethodPost, base, "/api/show", map[string]any{"model": model}, &answer); err != nil {
		return nil, 0, err
	}
	length := 0
	for key, value := range answer.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			if n, ok := value.(float64); ok {
				length = int(n)
			}
		}
	}
	if answer.Capabilities == nil {
		answer.Capabilities = []string{}
	}
	m.mu.Lock()
	m.capabilities[base+" "+model] = capabilities{list: answer.Capabilities, context: length, at: time.Now()}
	m.mu.Unlock()
	return answer.Capabilities, length, nil
}

// Models returns the models Ollama has at address, or those of the stored
// provider if it is empty, with what each can.
func (m *Manager) Models(ctx context.Context, address string) ([]Model, error) {
	if address == "" {
		// Stored settings were chosen by an owner or administrator.
		return m.ListModels(ctx, m.Settings(), "admin")
	}
	return m.ollamaModels(ctx, address)
}

// ListModels returns the models of the provider in settings, which may
// still be unsaved. Keys left empty are the stored ones (see withKeys); role
// is who chose the address of an OpenAI-compatible server.
func (m *Manager) ListModels(ctx context.Context, settings Settings, role string) ([]Model, error) {
	if !settings.Cloud() {
		address := settings.Ollama
		if address == "" {
			address = DefaultOllama
		}
		return m.ollamaModels(ctx, address)
	}
	b, err := m.backendFor(m.withKeys(settings), role)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return b.models(ctx)
}

func (m *Manager) ollamaModels(ctx context.Context, address string) ([]Model, error) {
	address = strings.TrimRight(address, "/")
	if !ValidOllama(address) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var tags struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Details struct {
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := m.call(ctx, http.MethodGet, address, "/api/tags", nil, &tags); err != nil {
		return nil, err
	}
	models := []Model{}
	for _, tag := range tags.Models {
		model := Model{Name: tag.Name, SizeBytes: tag.Size, Parameters: tag.Details.ParameterSize, Quantization: tag.Details.QuantizationLevel, Capabilities: []string{}}
		if list, length, err := m.show(ctx, address, tag.Name); err == nil {
			model.Capabilities, model.ContextLength = list, length
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}

// PullProgress is what Ollama says while it loads a model.
type PullProgress struct {
	Status    string `json:"status"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// Pull loads a model into Ollama and tells about the progress.
func (m *Manager) Pull(ctx context.Context, model string, progress func(PullProgress)) error {
	if !modelName.MatchString(model) {
		return ErrInvalid
	}
	res, err := m.request(ctx, http.MethodPost, m.Settings().Ollama, "/api/pull", map[string]any{"model": model, "stream": true})
	if err != nil {
		return err
	}
	return stream(res, func(line []byte) error {
		var p struct {
			PullProgress
			Error string `json:"error"`
		}
		if err := json.Unmarshal(line, &p); err != nil {
			return fmt.Errorf("%w sent something unexpected: %v", ErrUpstream, err)
		}
		if p.Error != "" {
			return fmt.Errorf("%w: %s", ErrUpstream, p.Error)
		}
		progress(p.PullProgress)
		return nil
	})
}

// DeleteModel removes a model from Ollama.
func (m *Manager) DeleteModel(ctx context.Context, model string) error {
	if !modelName.MatchString(model) {
		return ErrInvalid
	}
	return m.call(ctx, http.MethodDelete, m.Settings().Ollama, "/api/delete", map[string]any{"model": model}, nil)
}

// ollamaBackend is Ollama at base, the default and original way.
type ollamaBackend struct {
	m    *Manager
	base string
}

func (b ollamaBackend) chat(ctx context.Context, req chatRequest, each func(ollamaChunk) error) error {
	body := map[string]any{"model": req.Model, "messages": req.Messages, "stream": true}
	if req.UseTools {
		definitions := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			definitions = append(definitions, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": t.Parameters,
			}})
		}
		body["tools"] = definitions
	}
	if req.Think {
		body["think"] = true
	}
	options := map[string]any{}
	if req.NumCtx > 0 {
		options["num_ctx"] = req.NumCtx
	}
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if len(options) > 0 {
		body["options"] = options
	}
	res, err := b.m.request(ctx, http.MethodPost, b.base, "/api/chat", body)
	if err != nil {
		return err
	}
	return stream(res, func(line []byte) error {
		var chunk ollamaChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("%w sent something unexpected: %v", ErrUpstream, err)
		}
		if chunk.Error != "" {
			return modelResponseError(chunk.Error)
		}
		return each(chunk)
	})
}

func (b ollamaBackend) show(ctx context.Context, model string) ([]string, int, error) {
	return b.m.show(ctx, b.base, model)
}

func (b ollamaBackend) models(ctx context.Context) ([]Model, error) {
	return b.m.ollamaModels(ctx, b.base)
}

// setupModels is what "Ollama per click" loads, by the memory the model
// runs in: the first row whose limit fits, the last one above all. Every
// model here calls tools in Ollama.
var setupModels = []struct {
	upTo  uint64 // bytes; 0 is no limit
	model string
}{
	{8 << 30, "qwen3:4b"},
	{16 << 30, "qwen3:8b"},
	{0, "qwen3:14b"},
}

// RecommendModel chooses the model for a new Ollama: by the memory of the
// GPU it gets if there is one, else by the RAM of the NAS. basis says which.
func RecommendModel(ramBytes, vramBytes uint64) (model, basis string) {
	memory, basis := ramBytes, "ram"
	if vramBytes > 0 {
		memory, basis = vramBytes, "vram"
	}
	for _, row := range setupModels {
		if row.upTo == 0 || memory <= row.upTo {
			return row.model, basis
		}
	}
	return setupModels[len(setupModels)-1].model, basis
}
