package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
)

func (s *Server) handleAgentPrompt(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	usage, _ := s.chats.Usage(u.Name)
	writeJSON(w, http.StatusOK, map[string]any{"historyBytes": usage, "persistent": s.chats.Persistent(), "defaultPrompt": assistant.DefaultSystemPrompt, "version": assistant.PromptVersion, "context": s.opts.Assistant.ContextPreview(assistant.Person{Name: u.Name, Role: string(u.Role)}, ""), "nasVersion": s.opts.Version})
}
func (s *Server) handleAgentProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings assistant.Settings `json:"settings"`
		Question string             `json:"question"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	result, err := s.opts.Assistant.Probe(ctx, req.Settings, req.Question)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleAgentDiagnostics(w http.ResponseWriter, r *http.Request) {
	c, err := s.chats.Get(currentUser(r).Name, r.PathValue("id"))
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	include := r.URL.Query().Get("content") == "true"
	steps := []map[string]any{}
	for _, e := range c.Messages {
		step := map[string]any{"time": e.Time, "role": e.Role, "tool": e.Tool, "ok": e.OK}
		if e.Action != nil {
			step["actionId"] = e.Action.ID
			step["state"] = e.Action.State
		}
		if e.Problem != nil {
			step["errorCode"] = e.Problem.Code
		}
		if include {
			step["text"] = e.Content
			step["arguments"] = e.Args
			if e.Action != nil {
				step["result"] = e.Action.Result
			}
			if e.Problem != nil {
				step["error"] = e.Problem.Detail
			}
		}
		steps = append(steps, step)
	}
	run := c.Run
	if !include && run != nil {
		copy := *run
		copy.Detail = ""
		run = &copy
	}
	report := map[string]any{"version": s.opts.Version, "model": c.Model, "run": run, "steps": steps, "contentIncluded": include}
	data, _ := json.MarshalIndent(report, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="assistant-diagnostics.json"`)
	w.Write([]byte(s.redactor().Text(string(data))))
}

func (s *Server) agentTools(user auth.User, r *http.Request, c *assistant.Chat, save func(*assistant.Chat) error) []assistant.Tool {
	tools := s.assistantTools(user, r)
	tools = append(tools, assistant.Tool{Name: "agent_wait", Description: "Wait before rechecking an observed unavailable dependency. Resumes without a browser; never use to delay a completed answer.", Parameters: schema(map[string]any{"seconds": map[string]any{"type": "integer", "minimum": 5, "maximum": 3600}, "reason": str("Observed dependency.")}, "seconds", "reason"), Read: func(ctx context.Context, args json.RawMessage) (assistant.Result, error) {
		var req struct {
			Seconds int
			Reason  string
		}
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return assistant.Result{}, err
		}
		if req.Seconds < 5 || req.Seconds > 3600 || strings.TrimSpace(req.Reason) == "" {
			return assistant.Result{}, assistant.ErrInvalid
		}
		c.Run.Wake = time.Now().Add(time.Duration(req.Seconds) * time.Second)
		c.Run.Detail = diagnose.Secrets(req.Reason)
		return assistant.Result{}, assistant.ErrWaiting
	}})
	if s.opts.Assistant.Settings().Access(assistant.AreaFiles) != "off" {
		tools = append(tools, reading("file_undo_list", "Supported reversible file operations of this account. Expire after 30 minutes or service restart.", nil, false, func(ctx context.Context, args json.RawMessage) (any, error) {
			data, err := s.internalGet(ctx, r, "/files/undo")
			return json.RawMessage(data), err
		}))
		if s.opts.Assistant.Settings().Access(assistant.AreaFiles) == "write" {
			tools = append(tools, actionTool("agent_undo", "Undo a supported file action only when the person requests it. Read file_undo_list first.", map[string]any{"id": str("Exact ID from file_undo_list.")}, []string{"id"}, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
				var q struct{ ID string }
				if err := assistant.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				if q.ID == "" || strings.ContainsAny(q.ID, "/?.\\") {
					return nil, assistant.ErrInvalid
				}
				return withLevel(card("api.change", map[string]string{"summary": "Undo file action"}, "POST", "/files/undo/"+q.ID, nil), assistant.LevelDanger, "", ""), nil
			}))
		}
	}
	return tools
}
