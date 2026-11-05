package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Assist (PLAN 44, Runde 4): Home Assistant talks to the KI-Agent through
// its built-in Ollama integration, so no integration of our own is needed.
// TitanOS answers the few Ollama routes that integration uses under
// /agent-ollama, only when Assist is switched on and only to the address of
// the Home Assistant set up under Settings › Home Assistant (that
// integration sends no credentials). A question runs as the account chosen
// for it, with the agent's permissions, in the chat "Home Assistant
// (Assist)" of that account; an answer that takes long is cut short and
// the result follows as a message.

const (
	assistPrefix = "/agent-ollama"
	assistModel  = "titanos-agent:latest"
	assistWait   = 45 * time.Second
	// assistChatGap: questions closer together continue the same chat.
	assistChatGap = 10 * time.Minute
)

// assistAllowed reports whether a request may use Assist.
func (s *Server) assistAllowed(r *http.Request) bool {
	settings := s.agentSettings()
	if !settings.Enabled || !settings.HA.Assist || s.haHub() == nil {
		return false
	}
	remote := net.ParseIP(remoteIP(r))
	if remote == nil {
		return false
	}
	// Any Home Assistant set up as a connection may ask.
	for _, conn := range s.haHub().All() {
		if ha, _ := conn.Settings(); s.assistFrom(r, ha.URL, remote) {
			return true
		}
	}
	return false
}

// assistFrom says whether remote is the Home Assistant at address.
func (s *Server) assistFrom(r *http.Request, address string, remote net.IP) bool {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a.IP.Equal(remote) {
			return true
		}
	}
	// Home Assistant on this NAS itself (host network, "localhost").
	if remote.IsLoopback() {
		for _, a := range addrs {
			if a.IP.IsLoopback() {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleAgentOllama(w http.ResponseWriter, r *http.Request) {
	if !s.assistAllowed(r) {
		writeError(w, http.StatusForbidden, "assist_not_allowed", "Assist is off or this is not the Home Assistant set up in TitanOS")
		return
	}
	switch strings.TrimPrefix(r.URL.Path, assistPrefix) {
	case "/api/version":
		writeJSON(w, http.StatusOK, map[string]string{"version": "0.40.0"})
	case "/api/tags":
		writeJSON(w, http.StatusOK, map[string]any{"models": []map[string]any{{
			"name": assistModel, "model": assistModel, "size": 0, "modified_at": time.Now().UTC().Format(time.RFC3339),
			"details": map[string]any{"family": "titanos", "format": "agent"},
		}}})
	case "/api/show":
		writeJSON(w, http.StatusOK, map[string]any{"capabilities": []string{"completion"}, "details": map[string]any{"family": "titanos"}, "model_info": map[string]any{}})
	case "/api/pull":
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	case "/api/chat":
		s.assistChat(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (s *Server) assistChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream *bool `json:"stream"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	question := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			question = strings.TrimSpace(req.Messages[i].Content)
			break
		}
	}
	answer := s.assistAnswer(r.Context(), question)
	stream := req.Stream == nil || *req.Stream
	now := time.Now().UTC().Format(time.RFC3339Nano)
	msg := map[string]any{"role": "assistant", "content": answer}
	if !stream {
		writeJSON(w, http.StatusOK, map[string]any{"model": assistModel, "created_at": now, "message": msg, "done": true, "done_reason": "stop"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	enc.Encode(map[string]any{"model": assistModel, "created_at": now, "message": msg, "done": false})
	enc.Encode(map[string]any{"model": assistModel, "created_at": now, "message": map[string]any{"role": "assistant", "content": ""}, "done": true, "done_reason": "stop"})
}

// assistAnswer runs a question and waits for the answer, at most a while.
func (s *Server) assistAnswer(ctx context.Context, question string) string {
	if question == "" || len(question) > maxAgentMessage {
		return "Ich habe keine Frage verstanden."
	}
	settings := s.agentSettings()
	as := settings.HA.AssistAs
	if as == "" {
		as = s.firstOwner()
	}
	user, ok := s.opts.Users.Get(as)
	if !ok || user.Disabled || !s.agentAllowed(user) {
		return "Der KI-Agent ist für Home Assistant nicht eingerichtet."
	}
	store := s.agentStore()
	now := time.Now()
	var chat *agent.Chat
	if list, err := store.Chats(user.Name); err == nil {
		for _, info := range list {
			if info.Origin == "homeassistant" && now.Sub(info.Updated) < assistChatGap && !s.agentRunning(user.Name, info.ID) {
				chat, _ = store.Chat(user.Name, info.ID)
				break
			}
		}
	}
	if chat == nil {
		chat = &agent.Chat{ID: agent.NewID(), Title: "Home Assistant (Assist) · " + now.Format("02.01. 15:04"), Created: now, Origin: "homeassistant", State: agent.StateIdle, Entries: []agent.Entry{}}
		if p := s.assistProject(user.Name); p != "" {
			chat.Project = p
		}
	}
	chat.Pending = nil
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: now, Role: "user", Content: question})
	chat.Updated = now
	if err := store.SaveChat(user.Name, chat); err != nil {
		return "Der KI-Agent kann gerade nicht speichern."
	}
	done := make(chan struct{})
	if err := s.agentStart(user, chat, func(rn *agent.Runner, runCtx context.Context) error {
		defer close(done)
		return rn.Run(runCtx, chat)
	}); err != nil {
		return fmt.Sprintf("Der KI-Agent ist nicht bereit: %v", err)
	}
	select {
	case <-done:
	case <-time.After(assistWait):
		return "Ich kümmere mich darum und schicke dir das Ergebnis als Mitteilung."
	case <-ctx.Done():
		return ""
	}
	fresh, err := store.Chat(user.Name, chat.ID)
	if err != nil {
		return "Fertig."
	}
	if fresh.State == agent.StateWaiting && fresh.Pending != nil {
		return "Dafür brauche ich deine Freigabe in TitanOS: " + fresh.Pending.Summary
	}
	for i := len(fresh.Entries) - 1; i >= 0; i-- {
		if e := fresh.Entries[i]; e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
			return plainSpeech(e.Content)
		}
	}
	return "Fertig."
}

// assistProject is the project named "Home Assistant", if the account has one.
func (s *Server) assistProject(user string) string {
	list, err := s.agentStore().Projects(user)
	if err != nil {
		return ""
	}
	for _, p := range list {
		if strings.EqualFold(p.Name, "Home Assistant") {
			return p.ID
		}
	}
	return ""
}

// plainSpeech removes Markdown so a voice reads the text well.
func plainSpeech(s string) string {
	r := strings.NewReplacer("**", "", "__", "", "`", "", "### ", "", "## ", "", "# ", "")
	s = r.Replace(s)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.Trim(line, "-|: ") == "" {
			continue
		}
		if strings.HasPrefix(line, "|") {
			// A table row is read as a list: "a, b."
			var cells []string
			for _, c := range strings.Split(line, "|") {
				if c = strings.TrimSpace(c); c != "" {
					cells = append(cells, c)
				}
			}
			out = append(out, strings.Join(cells, ", ")+".")
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimLeft(line, "-*")))
	}
	return strings.Join(out, " ")
}
