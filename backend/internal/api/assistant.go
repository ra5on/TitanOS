package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
)

// The chat in the web UI. Owners and administrators chat when it is on;
// users only if the settings allow it, with tools for what they see.
// Chats are kept per account on a volume. See docs/ASSISTANT.md.

func writeAssistantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, assistant.ErrOff):
		writeError(w, http.StatusConflict, "assistant_off", err.Error())
	case errors.Is(err, assistant.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_assistant", err.Error())
	case errors.Is(err, assistant.ErrChatNotFound):
		writeError(w, http.StatusNotFound, "chat_not_found", err.Error())
	case errors.Is(err, assistant.ErrChatFull):
		writeError(w, http.StatusConflict, "chat_full", err.Error())
	case errors.Is(err, assistant.ErrBusy):
		writeError(w, http.StatusConflict, "chat_busy", err.Error())
	case errors.Is(err, assistant.ErrNoAction):
		writeError(w, http.StatusConflict, "action_gone", err.Error())
	case errors.Is(err, assistant.ErrModelMissing):
		writeError(w, http.StatusNotFound, "model_missing", err.Error())
	case errors.Is(err, assistant.ErrUpstream):
		writeError(w, http.StatusBadGateway, "ollama_failed", err.Error())
	default:
		slog.Error("assistant failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "the assistant failed")
	}
}

// chatsDir is where chats are kept: on the volume of the settings, or the
// first mounted one. Without it, chats stay in memory.
func (s *Server) chatsDir() (string, bool) {
	mounted := s.opts.Storage.Mounted()
	volume := s.opts.Assistant.Settings().Volume
	if volume == "" && len(mounted) > 0 {
		volume = mounted[0]
	}
	if volume == "" || !slices.Contains(mounted, volume) {
		return "", false
	}
	return filepath.Join(s.opts.Storage.MountPoint(volume), ".titanos", "assistant"), true
}

// requireAssistant lets accounts in that may chat.
func (s *Server) requireAssistant(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !s.assistantAllowed(currentUser(r)) {
			writeError(w, http.StatusForbidden, "assistant_off", "the assistant is off for this account")
			return
		}
		next(w, r)
	})
}

// handleAssistantStatus tells every account whether it may chat.
func (s *Server) handleAssistantStatus(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	settings := s.opts.Assistant.Settings()
	writeJSON(w, http.StatusOK, map[string]any{
		"available":  s.assistantAllowed(user),
		"enabled":    settings.Enabled,
		"model":      settings.Model,
		"web":        s.assistantWeb(user),
		"persistent": s.chats.Persistent(),
	})
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
		if errors.Is(err, assistant.ErrInvalid) {
			writeAssistantError(w, err)
			return
		}
		slog.Error("assistant settings not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	saved := s.opts.Assistant.Settings()
	slog.Info("assistant settings changed", "enabled", saved.Enabled, "ollama", saved.Ollama, "model", saved.Model, "users", saved.Users, "web", saved.Web, "webUsers", saved.WebUsers, "by", currentUser(r).Name, "ip", remoteIP(r))
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

// handleAssistantChatModels lists the models the chat can use: those that
// call tools, or of an Ollama that does not tell.
func (s *Server) handleAssistantChatModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.opts.Assistant.Models(r.Context(), "")
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	models = slices.DeleteFunc(models, func(m assistant.Model) bool { return len(m.Capabilities) > 0 && !m.Can("tools") })
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func startEvents(w http.ResponseWriter) func(event string, data any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush := http.NewResponseController(w)
	return func(event string, data any) {
		encoded, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
		_ = flush.Flush()
	}
}

// handleAssistantPull loads a model into Ollama; the progress comes as
// server-sent events.
func (s *Server) handleAssistantPull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !assistant.ValidModel(req.Model) {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	slog.Info("assistant model pull", "model", req.Model, "by", currentUser(r).Name, "ip", remoteIP(r))
	send := startEvents(w)
	last := time.Time{}
	err := s.opts.Assistant.Pull(r.Context(), req.Model, func(p assistant.PullProgress) {
		// Ollama tells many times a second; the browser needs less.
		if time.Since(last) > 300*time.Millisecond || p.Total == 0 {
			last = time.Now()
			send("progress", p)
		}
	})
	switch {
	case err == nil:
		send("done", map[string]string{})
	case errors.Is(err, context.Canceled):
	default:
		send("problem", map[string]string{"code": "ollama_failed", "detail": err.Error()})
	}
}

func (s *Server) handleAssistantModelDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Assistant.DeleteModel(r.Context(), req.Model); err != nil {
		writeAssistantError(w, err)
		return
	}
	slog.Info("assistant model deleted", "model", req.Model, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleAssistantOllamaApps suggests the address of an Ollama app that runs
// on this NAS.
func (s *Server) handleAssistantOllamaApps(w http.ResponseWriter, r *http.Request) {
	type found struct {
		Container string `json:"container"`
		Address   string `json:"address"`
		Running   bool   `json:"running"`
	}
	list := []found{}
	for _, c := range s.opts.Docker.Status(r.Context()).Containers {
		image := strings.ToLower(c.Image)
		if !strings.Contains(image, "ollama/ollama") {
			continue
		}
		port := 0
		for _, p := range c.Ports {
			if p.Container == 11434 && p.Host > 0 {
				port = p.Host
			}
		}
		if port == 0 && len(c.Ports) == 0 {
			// Host network: Ollama listens on its own port.
			port = 11434
		}
		if port > 0 {
			list = append(list, found{Container: c.Name, Address: "http://127.0.0.1:" + strconv.Itoa(port), Running: c.State == "running"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": list})
}

func (s *Server) handleAssistantChats(w http.ResponseWriter, r *http.Request) {
	list, err := s.chats.List(currentUser(r).Name, r.URL.Query().Get("search"))
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": list, "persistent": s.chats.Persistent()})
}

func (s *Server) handleAssistantChatCreate(w http.ResponseWriter, r *http.Request) {
	chat, err := s.chats.New(currentUser(r).Name, "")
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, chat)
}

func (s *Server) handleAssistantChatGet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	chat, err := s.chats.Get(user.Name, r.PathValue("id"))
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chat": chat, "busy": s.chats.Busy(user.Name, chat.ID)})
}

func (s *Server) handleAssistantChatRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	title := strings.Join(strings.Fields(req.Title), " ")
	if title == "" || len([]rune(title)) > 120 {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	user := currentUser(r)
	release, err := s.chats.Reserve(user.Name, r.PathValue("id"))
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	defer release()
	chat, err := s.chats.Get(user.Name, r.PathValue("id"))
	if err == nil {
		chat.Title = title
		err = s.chats.Save(user.Name, chat)
	}
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAssistantChatDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.chats.Delete(currentUser(r).Name, r.PathValue("id")); err != nil {
		writeAssistantError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAssistantChatsDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := s.chats.ForgetUser(user.Name); err != nil {
		writeAssistantError(w, err)
		return
	}
	slog.Info("assistant chats deleted", "user", user.Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// answer lets the assistant work on a chat that is reserved, and tells the
// browser about it as server-sent events: delta and thinking for the text
// as it comes, entry for every message that is added or final, action
// when a card waits for the person, problem, and done at the end.
func (s *Server) answer(w http.ResponseWriter, r *http.Request, user auth.User, chat *assistant.Chat, model, page string) {
	send := startEvents(w)
	save := func(c *assistant.Chat) error { return s.chats.Save(user.Name, c) }
	err := s.opts.Assistant.Answer(r.Context(), chat, assistant.Turn{
		Person: assistant.Person{Name: user.Name, Role: string(user.Role)},
		Model:  model,
		Tools:  s.assistantTools(user, r),
		Page:   page,
		Web:    s.assistantWeb(user),
		Save:   save,
		Emit: func(e assistant.Event) {
			switch e.Kind {
			case "delta", "thinking":
				send(e.Kind, map[string]string{"text": e.Text})
			case "entry":
				send("entry", map[string]any{"index": e.Index, "entry": e.Entry})
			case "action":
				send("action", map[string]any{"index": e.Index})
			}
		},
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		problem := assistant.Problem{Code: "internal"}
		switch {
		case errors.Is(err, assistant.ErrNoModel):
			problem.Code = "assistant_no_model"
		case errors.Is(err, assistant.ErrNoTools):
			problem.Code = "assistant_no_tools"
		case errors.Is(err, assistant.ErrModelMissing):
			problem = assistant.Problem{Code: "model_missing", Detail: err.Error()}
		case errors.Is(err, assistant.ErrUpstream):
			problem = assistant.Problem{Code: "ollama_failed", Detail: err.Error()}
		case errors.Is(err, assistant.ErrOff):
			problem.Code = "assistant_off"
		case errors.Is(err, assistant.ErrChatFull):
			problem.Code = "chat_full"
		default:
			slog.Error("assistant chat failed", "user", user.Name, "err", err)
		}
		// The problem stays in the chat, so that it is there after a reload.
		chat.Messages = append(chat.Messages, assistant.Entry{Role: "assistant", Problem: &problem, Time: time.Now()})
		if !errors.Is(err, assistant.ErrChatFull) {
			_ = save(chat)
		}
		send("entry", map[string]any{"index": len(chat.Messages) - 1, "entry": chat.Messages[len(chat.Messages)-1]})
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	send("done", map[string]string{})
}

// reserved gets a chat for a change and marks it as answering.
func (s *Server) reserved(w http.ResponseWriter, r *http.Request) (auth.User, *assistant.Chat, func(), bool) {
	user := currentUser(r)
	release, err := s.chats.Reserve(user.Name, r.PathValue("id"))
	if err != nil {
		writeAssistantError(w, err)
		return user, nil, nil, false
	}
	chat, err := s.chats.Get(user.Name, r.PathValue("id"))
	if err != nil {
		release()
		writeAssistantError(w, err)
		return user, nil, nil, false
	}
	return user, chat, release, true
}

// handleAssistantMessage adds a message of the person and answers it.
// With edit, the message at that index and all after it are replaced.
func (s *Server) handleAssistantMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text        string                 `json:"text"`
		Page        string                 `json:"page"`
		Model       string                 `json:"model"`
		Attachments []assistant.Attachment `json:"attachments"`
		Edit        *int                   `json:"edit"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*assistant.MaxImageAttachment*4/3+(1<<20))
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Model != "" && !assistant.ValidModel(req.Model) {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	user := currentUser(r)
	for i, a := range req.Attachments {
		if a.Kind != "nas" {
			continue
		}
		resolved, err := s.nasAttachment(user, a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "attachment_unreadable", err.Error())
			return
		}
		req.Attachments[i] = resolved
	}
	user, chat, release, ok := s.reserved(w, r)
	if !ok {
		return
	}
	defer release()
	if req.Edit != nil {
		if err := assistant.CutBefore(chat, *req.Edit); err != nil {
			writeAssistantError(w, err)
			return
		}
	}
	if err := assistant.NewMessage(chat, req.Text, req.Page, req.Attachments, time.Now()); err != nil {
		writeAssistantError(w, err)
		return
	}
	if err := s.chats.Save(user.Name, chat); err != nil {
		writeAssistantError(w, err)
		return
	}
	s.answer(w, r, user, chat, req.Model, req.Page)
}

// handleAssistantRegenerate answers the newest message of the person again.
func (s *Server) handleAssistantRegenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Model != "" && !assistant.ValidModel(req.Model) {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	user, chat, release, ok := s.reserved(w, r)
	if !ok {
		return
	}
	defer release()
	page, err := assistant.LastQuestion(chat)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	s.answer(w, r, user, chat, req.Model, page)
}

// handleAssistantAction records what happened to a card in the browser
// and lets the model go on.
func (s *Server) handleAssistantAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		State  string `json:"state"`
		Result string `json:"result"`
		Model  string `json:"model"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Model != "" && !assistant.ValidModel(req.Model) {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	user, chat, release, ok := s.reserved(w, r)
	if !ok {
		return
	}
	defer release()
	action := assistant.Pending(chat)
	// What a route answered may hold a secret (a new backup job answers
	// with its password); the model never sees it.
	if err := assistant.Resolve(chat, r.PathValue("action"), req.State, diagnose.Secrets(req.Result)); err != nil {
		writeAssistantError(w, err)
		return
	}
	slog.Info("assistant action "+req.State, "action", action.Key, "params", action.Params, "chat", chat.ID, "user", user.Name, "ip", remoteIP(r))
	if err := s.chats.Save(user.Name, chat); err != nil {
		writeAssistantError(w, err)
		return
	}
	page := ""
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if chat.Messages[i].Role == "user" {
			page = chat.Messages[i].Page
			break
		}
	}
	s.answer(w, r, user, chat, req.Model, page)
}
