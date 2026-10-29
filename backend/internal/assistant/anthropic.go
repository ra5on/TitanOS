package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// The Messages API of Anthropic. See https://docs.anthropic.com/en/api/messages.

const anthropicVersion = "2023-06-01"

// anthropicMaxTokens bounds one answer; tool rounds are much shorter.
const anthropicMaxTokens = 16384

type anthropicBackend struct {
	cloud
	base string
}

func (b anthropicBackend) header() map[string]string {
	return map[string]string{"x-api-key": b.key, "anthropic-version": anthropicVersion}
}

// anthropicMessages turns the history into a system text and messages of
// content blocks. Tool results go into the user turn after the call; turns of
// the same role are joined.
func anthropicMessages(messages []ollamaMessage) (string, []map[string]any) {
	var system []string
	var out []map[string]any
	add := func(role string, blocks []map[string]any) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1]["role"] == role {
			out[n-1]["content"] = append(out[n-1]["content"].([]map[string]any), blocks...)
			return
		}
		out = append(out, map[string]any{"role": role, "content": blocks})
	}
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	for _, msg := range pairCalls(messages) {
		switch msg.Role {
		case "system":
			system = append(system, msg.Content)
		case "user":
			var blocks []map[string]any
			for _, img := range msg.Images {
				blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": imageType(img), "data": img}})
			}
			if strings.TrimSpace(msg.Content) != "" || len(blocks) == 0 {
				content := msg.Content
				if strings.TrimSpace(content) == "" {
					content = "(empty message)"
				}
				blocks = append(blocks, text(content))
			}
			add("user", blocks)
		case "assistant":
			var blocks []map[string]any
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, text(msg.Content))
			}
			for _, call := range msg.ToolCalls {
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": callArguments(call.Function.Arguments)})
			}
			add("assistant", blocks)
		case "tool":
			content := msg.Content
			if content == "" {
				content = "(empty result)"
			}
			add("user", []map[string]any{{"type": "tool_result", "tool_use_id": msg.ToolCallID, "content": content}})
		}
	}
	return strings.Join(system, "\n\n"), out
}

func (b anthropicBackend) chat(ctx context.Context, req chatRequest, each func(ollamaChunk) error) error {
	system, messages := anthropicMessages(req.Messages)
	body := map[string]any{"model": req.Model, "max_tokens": anthropicMaxTokens, "messages": messages, "stream": true}
	if system != "" {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
		}
		body["tools"] = tools
		if !req.UseTools {
			body["tool_choice"] = map[string]any{"type": "none"}
		}
	}
	if req.Temperature != nil {
		body["temperature"] = min(*req.Temperature, 1)
	}
	res, err := b.do(ctx, http.MethodPost, b.base+"/v1/messages", b.header(), body)
	if err != nil {
		return err
	}
	type block struct {
		tool       bool
		id, name   string
		input      strings.Builder
		startInput json.RawMessage
	}
	blocks := map[int]*block{}
	var final ollamaChunk
	done := false
	err = b.sse(res, func(data []byte) error {
		var event struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				Usage struct {
					Input  int `json:"input_tokens"`
					Cached int `json:"cache_read_input_tokens"`
					Write  int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Block struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Text  string          `json:"text"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				Output int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return b.fail(fmt.Sprintf("sent something unexpected: %v", err))
		}
		var chunk ollamaChunk
		switch event.Type {
		case "message_start":
			final.PromptEvalCount = event.Message.Usage.Input + event.Message.Usage.Cached + event.Message.Usage.Write
			return nil
		case "content_block_start":
			bl := &block{tool: event.Block.Type == "tool_use", id: event.Block.ID, name: event.Block.Name, startInput: event.Block.Input}
			blocks[event.Index] = bl
			chunk.Message.Content = event.Block.Text
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				chunk.Message.Content = event.Delta.Text
			case "thinking_delta":
				chunk.Message.Thinking = event.Delta.Thinking
			case "input_json_delta":
				if bl := blocks[event.Index]; bl != nil {
					bl.input.WriteString(event.Delta.PartialJSON)
				}
				if n := len(event.Delta.PartialJSON); n > 0 {
					return each(ollamaChunk{pending: n})
				}
				return nil
			default:
				return nil
			}
		case "content_block_stop":
			bl := blocks[event.Index]
			if bl == nil || !bl.tool {
				return nil
			}
			var call ollamaToolCall
			call.ID, call.Function.Name = bl.id, bl.name
			switch {
			case bl.input.Len() > 0:
				call.Function.Arguments = json.RawMessage(bl.input.String())
			case len(bl.startInput) > 0:
				call.Function.Arguments = bl.startInput
			default:
				call.Function.Arguments = json.RawMessage("{}")
			}
			chunk.Message.ToolCalls = []ollamaToolCall{call}
		case "message_delta":
			final.EvalCount = event.Usage.Output
			if event.Delta.StopReason == "max_tokens" {
				for _, bl := range blocks {
					if bl.tool {
						// The arguments may be cut off; even a complete
						// looking call may lack fields that were to follow.
						return b.fail("the answer hit the length limit during a tool call; no tools were executed")
					}
				}
			}
			return nil
		case "message_stop":
			done = true
			final.Done = true
			return each(final)
		case "error":
			kinds := []error{}
			if event.Error.Type == "overloaded_error" || event.Error.Type == "rate_limit_error" || event.Error.Type == "api_error" {
				kinds = append(kinds, ErrTransient)
			}
			return b.fail(event.Error.Type+": "+event.Error.Message, kinds...)
		default:
			return nil // ping and future events
		}
		return each(chunk)
	})
	if err == nil && !done {
		return b.fail("stream ended before completion; no tools were executed", ErrTransient)
	}
	return err
}

// show needs no request: every current Claude model calls tools and sees
// images. A model that does not exist fails on its first round.
func (b anthropicBackend) show(ctx context.Context, model string) ([]string, int, error) {
	return []string{"completion", "tools", "vision"}, 0, nil
}

func (b anthropicBackend) models(ctx context.Context) ([]Model, error) {
	models := []Model{}
	after := ""
	for page := 0; page < 10; page++ {
		address := b.base + "/v1/models?limit=1000"
		if after != "" {
			address += "&after_id=" + url.QueryEscape(after)
		}
		res, err := b.do(ctx, http.MethodGet, address, b.header(), nil)
		if err != nil {
			return nil, err
		}
		var list struct {
			Data []struct {
				ID             string `json:"id"`
				MaxInputTokens int    `json:"max_input_tokens"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		err = json.NewDecoder(http.MaxBytesReader(nil, res.Body, 8<<20)).Decode(&list)
		res.Body.Close()
		if err != nil {
			return nil, b.fail(fmt.Sprintf("sent something unexpected: %v", err))
		}
		for _, d := range list.Data {
			if ValidModel(d.ID) {
				models = append(models, Model{Name: d.ID, Capabilities: []string{"completion", "tools", "vision"}, ContextLength: d.MaxInputTokens})
			}
		}
		if !list.HasMore || list.LastID == "" {
			break
		}
		after = list.LastID
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}
