package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/mcp"
)

// The chat in the web UI. Owners and administrators only: the tools show
// what only they see. See docs/AGENT-API.md.

func writeAssistantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, assistant.ErrOff):
		writeError(w, http.StatusConflict, "assistant_off", err.Error())
	case errors.Is(err, assistant.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_assistant", err.Error())
	case errors.Is(err, assistant.ErrUpstream):
		writeError(w, http.StatusBadGateway, "ollama_failed", err.Error())
	default:
		slog.Error("assistant failed", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
	}
}

func (s *Server) handleAssistant(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Assistant.Settings())
}

func (s *Server) handleAssistantChange(w http.ResponseWriter, r *http.Request) {
	var settings assistant.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.Assistant.SetSettings(settings); err != nil {
		writeAssistantError(w, err)
		return
	}
	saved := s.opts.Assistant.Settings()
	slog.Info("assistant settings changed", "enabled", saved.Enabled, "ollama", saved.Ollama, "model", saved.Model, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, saved)
}

// handleAssistantModels asks Ollama which models it has, at the address
// given or else at the stored one, so that the settings can be tried
// before they are saved.
func (s *Server) handleAssistantModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ollama string `json:"ollama"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	models, err := s.opts.Assistant.Models(r.Context(), req.Ollama)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// assistantTools are the read-only tools of the MCP server, for the chat.
// The account is an owner or administrator, so it gets all of them.
func (s *Server) assistantTools(user string) []assistant.Tool {
	var tools []assistant.Tool
	for _, t := range s.allAgentTools() {
		tool := t.tool
		tools = append(tools, assistant.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
			Call: func(ctx context.Context, args json.RawMessage) (string, bool) {
				data, err := tool.Call(ctx, args)
				if err != nil {
					var toolErr *mcp.ToolError
					if errors.As(err, &toolErr) {
						return toolErr.Message, false
					}
					slog.Warn("assistant tool call failed", "tool", tool.Name, "user", user, "err", err)
					return "The tool failed.", false
				}
				slog.Info("assistant tool call", "tool", tool.Name, "user", user)
				text, err := json.Marshal(data)
				if err != nil {
					return "The result could not be encoded.", false
				}
				return string(text), true
			},
		})
	}
	return tools
}

// handleAssistantChat answers the last message of a conversation the
// browser keeps. What happens comes as server-sent events: one per tool
// call, then the answer or a problem.
func (s *Server) handleAssistantChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string              `json:"model"`
		Messages []assistant.Message `json:"messages"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !readJSONBody(w, r, &req) {
		return
	}
	history, err := assistant.Clean(req.Messages)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	if req.Model != "" && !assistant.ValidModel(req.Model) {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	if !s.opts.Assistant.Settings().Enabled {
		writeAssistantError(w, assistant.ErrOff)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flush := http.NewResponseController(w)
	send := func(event string, data any) {
		encoded, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
		_ = flush.Flush()
	}
	user := currentUser(r).Name
	err = s.opts.Assistant.Chat(r.Context(), req.Model, history, s.assistantTools(user), func(event assistant.Event) {
		switch event.Kind {
		case "tool":
			send("tool", map[string]any{"name": event.Tool, "ok": event.OK})
		case "answer":
			send("answer", map[string]any{"text": event.Text})
		}
	})
	switch {
	case err == nil:
	case errors.Is(err, assistant.ErrNoModel):
		send("problem", map[string]string{"code": "assistant_no_model"})
	case errors.Is(err, assistant.ErrUpstream):
		send("problem", map[string]string{"code": "ollama_failed", "detail": err.Error()})
	case errors.Is(err, context.Canceled):
		// The browser went away; nobody is left to tell.
	default:
		slog.Error("assistant chat failed", "user", user, "err", err)
		send("problem", map[string]string{"code": "internal"})
	}
}
