package assistant

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

func resumable(r *Run) bool {
	return r != nil && (r.State == "running" || r.State == "waiting" || r.State == "paused" || r.State == "queued" || r.State == "confirmation" || r.State == "blocked")
}

// compact preserves the original task, latest steering and recent tool pairs.
// It records earlier observations verbatim, not as model-invented facts. Action
// receipts are independently journalled by the executor before any mutation.
func compact(c *Chat, contextSize int) {
	budget := 100000
	if contextSize > 0 {
		budget = max(8000, min(budget, contextSize*2))
	}
	data, _ := json.Marshal(c.Messages)
	if len(data) < budget || len(c.Messages) < 12 {
		return
	}
	cut := len(c.Messages) - 8
	for cut < len(c.Messages)-2 {
		recent, _ := json.Marshal(c.Messages[cut:])
		if len(recent) <= budget {
			break
		}
		cut++
	}
	for cut > 1 && c.Messages[cut].Role == "tool" {
		cut--
	}
	if cut < 2 {
		return
	}
	var b strings.Builder
	b.WriteString(c.Summary)
	for _, e := range c.Messages[1:cut] {
		switch {
		case e.Role == "user":
			fmt.Fprintf(&b, "\nUser instruction: %s", truncate(e.Content, 4000))
		case e.Action != nil:
			fmt.Fprintf(&b, "\nAction %s [%s]: %s", e.Action.ID, e.Action.State, truncate(e.Action.Result, 1500))
		case e.Role == "tool":
			fmt.Fprintf(&b, "\nObservation %s (success=%v): %s", e.Tool, e.OK, truncate(e.Content, 1000))
		}
	}
	summary := b.String()
	if len(summary) > 16000 {
		summary = "Earlier record shortened; consult action receipts; do not infer success.\n" + summary[len(summary)-16000:]
	}
	c.Summary = strings.ToValidUTF8(summary, "")
	c.Archived += cut - 1
	c.Messages = append(c.Messages[:1:1], c.Messages[cut:]...)
}

// ContextPreview shows the exact policy and dynamic context sent to Ollama.
func (m *Manager) ContextPreview(person Person, page string) string {
	return m.prompt(person, page, m.Settings().Web, nil)
}

func repeatedAction(c *Chat, a *Action) bool {
	if a.Key == "task" {
		return false
	}
	if a.Key == "docker.check" || a.Key == "update.check" {
		for i := len(c.Messages) - 1; i >= 0; i-- {
			e := c.Messages[i]
			if e.Tool == "agent_wait" || e.Role == "user" {
				return false
			}
			if e.Action != nil && e.Action.Key == a.Key {
				return true
			}
		}
		return false
	}
	b, _ := json.Marshal(a.Body)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(a.Method+"\n"+a.Path+"\n"+string(b))))
	if c.Attempts[fingerprint] {
		return true
	}
	if c.Attempts == nil {
		c.Attempts = map[string]bool{}
	}
	c.Attempts[fingerprint] = true
	for i := len(c.Messages) - 1; i >= 0; i-- {
		e := c.Messages[i]
		if e.Role == "user" {
			break
		}
		if e.Action == nil {
			continue
		}
		x := e.Action
		old, _ := json.Marshal(x.Body)
		if x.Method == a.Method && x.Path == a.Path && string(old) == string(b) {
			return true
		}
	}
	return false
}
