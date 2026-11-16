package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// ClaudeAddress is where Claude is asked.
const ClaudeAddress = "https://api.anthropic.com"

// claudeMaxTokens is the room for one answer. Answers are streamed, so a
// large value costs nothing but lets a long document be written in one go.
const claudeMaxTokens = 64000

type claude struct {
	client anthropic.Client
}

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
	return &Ollama{base: u, remote: &claude{client: anthropic.NewClient(opts...)}}, nil
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
		switch api.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("the provider refused the key (%d): %s", api.StatusCode, text)
		case http.StatusTooManyRequests:
			return fmt.Errorf("the provider's limit is reached (429): %s", text)
		}
		return fmt.Errorf("the provider answered %d: %s", api.StatusCode, text)
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, err)
}

func (p *claude) models(ctx context.Context) ([]Model, error) {
	var out []Model
	pager := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pager.Next() {
		m := pager.Current()
		out = append(out, Model{Name: m.ID, Capabilities: []string{"completion", "tools", "vision"}, ContextLength: int(m.MaxInputTokens)})
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
		MaxTokens: claudeMaxTokens,
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

	stream := p.client.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	var message anthropic.Message
	size := 0
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return Message{}, Stats{}, claudeError(ctx, err)
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
				return Message{}, Stats{}, errors.New("the answer of the provider is too large")
			}
			if onChunk != nil && (chunk.Content != "" || chunk.Thinking != "") {
				onChunk(chunk)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return Message{}, Stats{}, claudeError(ctx, err)
	}
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
