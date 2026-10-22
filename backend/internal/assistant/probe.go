package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Probe uses a synthetic tool only. It cannot inspect or change NAS objects.
// It checks actual calls and error handling, not only advertised capabilities.
func (m *Manager) Probe(ctx context.Context, settings Settings, question string) (map[string]any, error) {
	if !ValidOllama(settings.Ollama) || !ValidModel(settings.Model) || len(settings.SystemPrompt) > MaxPrompt || len(question) > MaxMessage {
		return nil, ErrInvalid
	}
	scratch := NewManager(nil)
	scratch.client = m.client
	scratch.settings = settings
	scratch.settings.Enabled = true
	scratch.settings.MaxSteps = 6
	if question == "" {
		question = "Run probe_status for target probe. It returns a failed update check. Explain whether any update conclusion is possible. Do not invent facts."
	}
	calls := 0
	chat := &Chat{Messages: []Entry{{Role: "user", Content: question}}}
	err := scratch.Answer(ctx, chat, Turn{Person: Person{Name: "probe", Role: "owner"}, Model: settings.Model,
		Tools: []Tool{{Name: "probe_status", Description: "Synthetic update check; target must be probe. No real NAS access.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"target": map[string]string{"type": "string"}}, "required": []string{"target"}}, Read: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var a struct{ Target string }
			if err := DecodeArgs(args, &a); err != nil {
				return Result{}, err
			}
			if a.Target != "probe" {
				return Result{}, Errorf("target must be exactly probe")
			}
			calls++
			return Result{}, Errorf("registry unreachable; update status unknown, not zero updates")
		}}},
		Emit: func(Event) {},
	})
	if err != nil {
		return nil, err
	}
	var answer strings.Builder
	for _, e := range chat.Messages {
		if e.Role == "assistant" {
			answer.WriteString(e.Content)
		}
	}
	return map[string]any{"toolCalled": calls > 0, "calls": calls, "answer": answer.String(), "note": fmt.Sprintf("Synthetic test only (%d calls). Check the answer's treatment of the failure; this is not a blanket model certification.", calls)}, nil
}
