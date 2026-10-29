package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
)

// Chat completions as OpenAI defines them and OpenRouter, LM Studio, vLLM
// and others repeat. See https://platform.openai.com/docs/api-reference/chat.

type openaiBackend struct {
	cloud
	base string
}

func (b openaiBackend) header() map[string]string {
	if b.key == "" {
		return nil // LM Studio and the like need none
	}
	return map[string]string{"Authorization": "Bearer " + b.key}
}

func openaiMessages(messages []ollamaMessage) []map[string]any {
	out := []map[string]any{}
	for _, msg := range pairCalls(messages) {
		switch msg.Role {
		case "system":
			out = append(out, map[string]any{"role": "system", "content": msg.Content})
		case "user":
			if len(msg.Images) == 0 {
				out = append(out, map[string]any{"role": "user", "content": msg.Content})
				continue
			}
			parts := []map[string]any{{"type": "text", "text": msg.Content}}
			for _, img := range msg.Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + imageType(img) + ";base64," + img}})
			}
			out = append(out, map[string]any{"role": "user", "content": parts})
		case "assistant":
			entry := map[string]any{"role": "assistant", "content": msg.Content}
			if len(msg.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(msg.ToolCalls))
				for _, call := range msg.ToolCalls {
					calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]string{"name": call.Function.Name, "arguments": string(callArguments(call.Function.Arguments))}})
				}
				entry["tool_calls"] = calls
				if msg.Content == "" {
					entry["content"] = nil
				}
			}
			out = append(out, entry)
		case "tool":
			out = append(out, map[string]any{"role": "tool", "tool_call_id": msg.ToolCallID, "content": msg.Content})
		}
	}
	return out
}

func (b openaiBackend) chat(ctx context.Context, req chatRequest, each func(ollamaChunk) error) error {
	body := map[string]any{"model": req.Model, "messages": openaiMessages(req.Messages), "stream": true, "stream_options": map[string]bool{"include_usage": true}}
	if req.UseTools && len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		body["tools"] = tools
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	res, err := b.do(ctx, http.MethodPost, b.base+"/chat/completions", b.header(), body)
	if err != nil {
		return err
	}
	type pending struct {
		id, name string
		args     strings.Builder
	}
	var calls []*pending
	var final ollamaChunk
	finished, done := false, false
	err = b.sse(res, func(data []byte) error {
		if string(data) == "[DONE]" {
			done = true
			return nil
		}
		if done {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return b.fail(fmt.Sprintf("sent something unexpected: %v", err))
		}
		if chunk.Error != nil {
			return b.fail(chunk.Error.Message)
		}
		if chunk.Usage != nil {
			final.PromptEvalCount, final.EvalCount = chunk.Usage.Prompt, chunk.Usage.Completion
		}
		for _, choice := range chunk.Choices {
			var out ollamaChunk
			out.Message.Content = choice.Delta.Content
			out.Message.Thinking = choice.Delta.ReasoningContent + choice.Delta.Reasoning
			for _, tc := range choice.Delta.ToolCalls {
				if tc.Index < 0 || tc.Index > 255 {
					return b.fail("sent a tool call with an invalid index")
				}
				for len(calls) <= tc.Index {
					calls = append(calls, &pending{})
				}
				p := calls[tc.Index]
				if tc.ID != "" {
					p.id = tc.ID
				}
				p.name += tc.Function.Name
				p.args.WriteString(tc.Function.Arguments)
			}
			if choice.FinishReason != "" {
				finished = true
			}
			if out.Message.Content != "" || out.Message.Thinking != "" {
				if err := each(out); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Some servers end without [DONE]; a finish reason is enough.
	if !finished && !done {
		return b.fail("stream ended before completion; no tools were executed", ErrTransient)
	}
	for _, p := range calls {
		var call ollamaToolCall
		call.ID, call.Function.Name = p.id, p.name
		call.Function.Arguments = json.RawMessage(p.args.String())
		if strings.TrimSpace(p.args.String()) == "" {
			call.Function.Arguments = json.RawMessage("{}")
		}
		final.Message.ToolCalls = append(final.Message.ToolCalls, call)
	}
	final.Done = true
	return each(final)
}

// show does not ask: most compatible servers do not say what a model can.
// Unknown capabilities mean the model is tried, as with old Ollama.
func (b openaiBackend) show(ctx context.Context, model string) ([]string, int, error) {
	return []string{}, 0, nil
}

func (b openaiBackend) models(ctx context.Context) ([]Model, error) {
	res, err := b.do(ctx, http.MethodGet, b.base+"/models", b.header(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var list struct {
		Data []struct {
			ID            string   `json:"id"`
			ContextLength int      `json:"context_length"`
			Supported     []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 16<<20)).Decode(&list); err != nil {
		return nil, b.fail(fmt.Sprintf("sent something unexpected: %v", err))
	}
	models := []Model{}
	for _, d := range list.Data {
		if !ValidModel(d.ID) {
			continue
		}
		model := Model{Name: d.ID, Capabilities: []string{}, ContextLength: d.ContextLength}
		// OpenRouter says which models take tools.
		if d.Supported != nil {
			model.Capabilities = []string{"completion"}
			if slices.Contains(d.Supported, "tools") {
				model.Capabilities = append(model.Capabilities, "tools")
			}
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}
