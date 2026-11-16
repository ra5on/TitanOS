package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// ClaudeAddress is where Claude is asked.
const ClaudeAddress = "https://api.anthropic.com"

// claudeMaxTokens is the room for one answer. Answers are streamed, so a
// large value costs nothing but lets a long document be written in one go.
// A model with a smaller limit gets its own (claudeAnswerRoom): the API
// refuses a request that asks for more than the model can write (1.01).
const claudeMaxTokens = 64000

// claudeLeastTokens is what every Claude model can write in one answer; it is
// the second try when the limit of a model could not be read and the API
// refused the first value.
const claudeLeastTokens = 4096

type claude struct {
	client anthropic.Client
	// base is the address asked, part of the key of the limits below.
	base string
}

// claudeLimits keeps the answer limit (max_tokens) of each model as the API
// reported it, per address: it is a property of the model, not of a chat,
// and asking for it before every answer would cost a request each time.
var claudeLimits sync.Map // base + "\x00" + model → int

// claudeLimitMiss marks a model whose limit could not be read, with the
// time, so a failing lookup is not repeated before every answer.
var claudeLimitMiss sync.Map // base + "\x00" + model → time.Time

// NewClaude returns a client for Claude with the owner's API key.
func NewClaude(key string) (*Ollama, error) { return newClaude(key, "") }

// newClaude asks another address instead (tests).
func newClaude(key, base string) (*Ollama, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("no API key")
	}
	// Nothing from the environment of titand: the key and the address are
	// the ones set here, whatever ANTHROPIC_* variables may exist.
	opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithAPIKey(key), option.WithHTTPClient(remoteClient()), option.WithMaxRetries(1)}
	if base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	u, _ := ParseProviderURL(ClaudeAddress)
	if base == "" {
		base = ClaudeAddress
	}
	return &Ollama{base: u, remote: &claude{client: anthropic.NewClient(opts...), base: base}}, nil
}

func (p *claude) kind() string    { return ProviderClaude }
func (p *claude) address() string { return ClaudeAddress }

// claudeError turns an error of the API into words for the person.
func claudeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var api *anthropic.Error
	if errors.As(err, &api) {
		text := api.Error()
		if r := []rune(text); len(r) > 400 {
			text = string(r[:400])
		}
		return providerStatus(api.StatusCode, text)
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, err)
}

func (p *claude) models(ctx context.Context) ([]Model, error) {
	var out []Model
	pager := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pager.Next() {
		m := pager.Current()
		out = append(out, Model{Name: m.ID, Capabilities: []string{"completion", "tools", "vision"}, ContextLength: int(m.MaxInputTokens)})
		if m.MaxTokens > 0 {
			claudeLimits.Store(p.base+"\x00"+m.ID, int(m.MaxTokens))
		}
		if len(out) >= 500 {
			break
		}
	}
	if err := pager.Err(); err != nil {
		return nil, claudeError(ctx, err)
	}
	return out, nil
}

func (p *claude) chat(ctx context.Context, r ChatRequest, onChunk func(Chunk)) (Message, Stats, error) {
	var system []string
	// The API wants the person and the model to take turns: neighbours of
	// the same side are put into one message.
	type side struct {
		model  bool
		blocks []anthropic.ContentBlockParamUnion
	}
	var msgs []side
	add := func(model bool, blocks ...anthropic.ContentBlockParamUnion) {
		if n := len(msgs); n > 0 && msgs[n-1].model == model {
			msgs[n-1].blocks = append(msgs[n-1].blocks, blocks...)
			return
		}
		msgs = append(msgs, side{model, blocks})
	}
	for _, t := range remoteTurns(r.Messages) {
		switch t.Role {
		case "system":
			if len(msgs) == 0 {
				system = append(system, t.Content)
			} else if strings.TrimSpace(t.Content) != "" {
				add(false, anthropic.NewTextBlock(t.Content))
			}
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if strings.TrimSpace(t.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(t.Content))
			}
			for i, c := range t.ToolCalls {
				var input any = map[string]any{}
				if len(c.Function.Arguments) > 0 {
					var parsed map[string]any
					if json.Unmarshal(c.Function.Arguments, &parsed) == nil && parsed != nil {
						input = parsed
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(t.CallIDs[i], input, c.Function.Name))
			}
			if len(blocks) > 0 {
				add(true, blocks...)
			}
		case "tool":
			content := t.Content
			if content == "" {
				content = "(empty)"
			}
			add(false, anthropic.NewToolResultBlock(t.CallID, content, strings.HasPrefix(content, "Error:")))
		default:
			var blocks []anthropic.ContentBlockParamUnion
			for _, img := range t.Images {
				blocks = append(blocks, anthropic.NewImageBlockBase64(imageType(img), img))
			}
			if strings.TrimSpace(t.Content) != "" || len(blocks) == 0 {
				text := t.Content
				if strings.TrimSpace(text) == "" {
					text = "-"
				}
				blocks = append(blocks, anthropic.NewTextBlock(text))
			}
			add(false, blocks...)
		}
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(r.Model),
		MaxTokens: int64(p.answerRoom(ctx, r.Model)),
	}
	for _, m := range msgs {
		if m.model {
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(m.blocks...))
		} else {
			params.Messages = append(params.Messages, anthropic.NewUserMessage(m.blocks...))
		}
	}
	if text := strings.TrimSpace(strings.Join(system, "\n\n")); text != "" {
		// The instructions and tools are the same for a whole chat: cached.
		params.System = []anthropic.TextBlockParam{{Text: text, CacheControl: anthropic.NewCacheControlEphemeralParam()}}
	}
	for _, spec := range r.Tools {
		tool := anthropic.ToolParam{Name: spec.Function.Name, Description: anthropic.String(spec.Function.Description)}
		tool.InputSchema.Properties = spec.Function.Parameters["properties"]
		if req, ok := spec.Function.Parameters["required"].([]string); ok {
			tool.InputSchema.Required = req
		} else if list, ok := spec.Function.Parameters["required"].([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); ok {
					tool.InputSchema.Required = append(tool.InputSchema.Required, s)
				}
			}
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}

	message, err := p.stream(ctx, params, onChunk)
	var api *anthropic.Error
	if err != nil && errors.As(err, &api) && api.StatusCode == http.StatusBadRequest && strings.Contains(api.Error(), "max_tokens") && params.MaxTokens > claudeLeastTokens {
		// The limit of the model was not known and the default is too much
		// for it: once more with what every model can write. Nothing was
		// streamed yet, the API refuses such a request before it starts.
		params.MaxTokens = claudeLeastTokens
		message, err = p.stream(ctx, params, onChunk)
	}
	if err != nil {
		if errors.As(err, &api) {
			return Message{}, Stats{}, claudeError(ctx, err)
		}
		return Message{}, Stats{}, err
	}
	return claudeAnswer(message)
}

// answerRoom is max_tokens for a model: claudeMaxTokens, or less when the
// model cannot write that much. The limit comes from the models API once
// (or from the list of models); without it the default is tried.
func (p *claude) answerRoom(ctx context.Context, model string) int {
	key := p.base + "\x00" + model
	if v, ok := claudeLimits.Load(key); ok {
		return min(claudeMaxTokens, v.(int))
	}
	if at, ok := claudeLimitMiss.Load(key); ok && time.Since(at.(time.Time)) < 10*time.Minute {
		return claudeMaxTokens
	}
	lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := p.client.Models.Get(lctx, model, anthropic.ModelGetParams{})
	if err != nil || info == nil || info.MaxTokens <= 0 {
		claudeLimitMiss.Store(key, time.Now())
		return claudeMaxTokens
	}
	claudeLimits.Store(key, int(info.MaxTokens))
	return min(claudeMaxTokens, int(info.MaxTokens))
}

// stream asks for one answer and passes its pieces on. An error of the API
// comes back as it is (*anthropic.Error), so the caller can try again.
func (p *claude) stream(ctx context.Context, params anthropic.MessageNewParams, onChunk func(Chunk)) (anthropic.Message, error) {
	stream := p.client.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	var message anthropic.Message
	size := 0
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return message, claudeError(ctx, err)
		}
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			chunk := Chunk{}
			switch d := delta.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				chunk.Content = d.Text
			case anthropic.ThinkingDelta:
				chunk.Thinking = d.Thinking
			case anthropic.InputJSONDelta:
				size += len(d.PartialJSON)
			}
			size += len(chunk.Content) + len(chunk.Thinking)
			if size > MaxAnswerBytes {
				return message, errors.New("the answer of the provider is too large")
			}
			if onChunk != nil && (chunk.Content != "" || chunk.Thinking != "") {
				onChunk(chunk)
			}
		}
	}
	if err := stream.Err(); err != nil {
		var api *anthropic.Error
		if errors.As(err, &api) && ctx.Err() == nil {
			return message, err
		}
		return message, claudeError(ctx, err)
	}
	return message, nil
}

// claudeAnswer turns the whole answer into a message of the agent.
func claudeAnswer(message anthropic.Message) (Message, Stats, error) {
	out := Message{Role: "assistant"}
	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			out.Content += b.Text
		case anthropic.ThinkingBlock:
			out.Thinking += b.Thinking
		case anthropic.ToolUseBlock:
			if len(out.ToolCalls) >= MaxToolCalls {
				return Message{}, Stats{}, errors.New("the provider sent too many tool calls")
			}
			args := json.RawMessage(b.JSON.Input.Raw())
			if len(args) == 0 || !json.Valid(args) {
				args = json.RawMessage("{}")
			}
			var call ToolCall
			call.Function.Name, call.Function.Arguments = b.Name, args
			out.ToolCalls = append(out.ToolCalls, call)
		}
	}
	stats := Stats{
		PromptTokens: int(message.Usage.InputTokens + message.Usage.CacheReadInputTokens + message.Usage.CacheCreationInputTokens),
		OutputTokens: int(message.Usage.OutputTokens),
	}
	switch message.StopReason {
	case anthropic.StopReasonRefusal:
		// The provider's safety checks declined: say so instead of an empty answer.
		why := message.StopDetails.Category
		if why == "" {
			why = "no category given"
		}
		return Message{}, stats, fmt.Errorf("the provider declined this request (%s)", why)
	case anthropic.StopReasonMaxTokens:
		if len(out.ToolCalls) > 0 {
			// A call cut off in the middle cannot be run.
			out.ToolCalls = nil
		}
		out.Content += "\n\n[The answer was cut off at the provider's limit for one answer.]"
	}
	return out, stats, nil
}
