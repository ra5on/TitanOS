package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
)

// Questions from Home Assistant Assist: a blueprint in Home Assistant fires
// the event titanos_ask with {id, text}; TitanOS answers as the account chosen
// under Settings › Home Assistant and fires titanos_answer with {id, text}.
// Each question is a chat of that account, so its rights and the execution
// policy apply as in the web interface.

// assistWait is how long Home Assistant is kept waiting; the blueprint waits 60 s.
const assistWait = 50 * time.Second

func (s *Server) handleAssist(data json.RawMessage) {
	var q struct {
		ID           string `json:"id"`
		Text         string `json:"text"`
		Conversation string `json:"conversation_id"`
		NAS          string `json:"nas_id"`
	}
	if json.Unmarshal(data, &q) != nil || len(q.ID) > 100 || q.ID == "" || len(q.Conversation) > 100 {
		return
	}
	settings, _ := s.opts.HomeAssistant.Settings()
	if q.NAS != "" && q.NAS != settings.MQTT.ID {
		return
	}
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" || len(q.Text) > assistant.MaxMessage {
		return
	}
	// Duplicate event IDs must not start another physical action.
	s.assistSessions.mu.Lock()
	if s.assistSessions.requests == nil {
		s.assistSessions.requests = map[string]time.Time{}
	}
	now := time.Now()
	for key, at := range s.assistSessions.requests {
		if now.Sub(at) > 30*time.Minute {
			delete(s.assistSessions.requests, key)
		}
	}
	requestKey := settings.URL + "/" + settings.Account + "/" + q.ID
	if _, seen := s.assistSessions.requests[requestKey]; seen || len(s.assistSessions.requests) >= 1000 {
		s.assistSessions.mu.Unlock()
		return
	}
	s.assistSessions.requests[requestKey] = now
	s.assistSessions.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), assistWait+15*time.Second)
		defer cancel()
		answer := s.assistConversation(ctx, q.Text, q.Conversation)
		if err := s.opts.HomeAssistant.FireEvent(ctx, "titanos_answer", map[string]string{"id": q.ID, "text": answer, "conversation_id": q.Conversation, "nas_id": settings.MQTT.ID}); err != nil {
			slog.Warn("answer to Home Assistant not delivered", "err", err)
		}
	}()
}

func (s *Server) assistConversation(ctx context.Context, text, conversation string) string {
	settings, _ := s.opts.HomeAssistant.Settings()
	user, ok := s.activeUser(settings.Account)
	german := true
	if ok {
		if p, _, err := s.opts.Notify.PersonalSettings(user.Name); err == nil {
			german = p.Language == "de"
		}
	}
	say := func(de, en string) string {
		if german {
			return de
		}
		return en
	}
	if !settings.Assist || !ok || !user.Role.CanAdminister() || !s.assistantAllowed(user) {
		return say("TitanOS beantwortet keine Fragen aus Home Assistant. Das lässt sich unter Einstellungen › Home Assistant einschalten.", "TitanOS does not answer questions from Home Assistant. Switch it on under Settings › Home Assistant.")
	}
	// Only conversation keys map to chat IDs. Never accept a chat ID from HA.
	var created struct{ ID string }
	sessionKey := settings.URL + "/" + user.Name + "/" + conversation
	if conversation != "" {
		s.assistSessions.mu.Lock()
		if s.assistSessions.busy == nil {
			s.assistSessions.busy = map[string]bool{}
		}
		if s.assistSessions.busy[sessionKey] {
			s.assistSessions.mu.Unlock()
			return say("Die vorige Frage wird noch bearbeitet.", "The previous question is still being processed.")
		}
		s.assistSessions.busy[sessionKey] = true
		s.assistSessions.mu.Unlock()
		defer func() {
			s.assistSessions.mu.Lock()
			delete(s.assistSessions.busy, sessionKey)
			s.assistSessions.mu.Unlock()
		}()
		s.assistSessions.mu.Lock()
		if entry, ok := s.assistSessions.entries[sessionKey]; ok && time.Since(entry.Used) < 30*time.Minute {
			created.ID = entry.Chat
		}
		s.assistSessions.mu.Unlock()
		if created.ID != "" {
			if _, err := s.chats.Get(user.Name, created.ID); err != nil {
				created.ID = ""
			}
		}
	}
	if created.ID == "" {
		code, body := s.internalAs(ctx, user, http.MethodPost, "/api/v1/assistant/chats", nil)
		if code != http.StatusCreated || json.Unmarshal([]byte(body), &created) != nil || created.ID == "" {
			return say("Der KI-Assistent ist gerade nicht erreichbar.", "The AI assistant cannot be reached right now.")
		}
	}
	if conversation != "" {
		s.assistSessions.mu.Lock()
		if s.assistSessions.entries == nil {
			s.assistSessions.entries = map[string]assistSession{}
		}
		for key, entry := range s.assistSessions.entries {
			if time.Since(entry.Used) >= 30*time.Minute {
				delete(s.assistSessions.entries, key)
			}
		}
		_, exists := s.assistSessions.entries[sessionKey]
		if exists || len(s.assistSessions.entries) < 200 {
			s.assistSessions.entries[sessionKey] = assistSession{created.ID, time.Now()}
		}
		s.assistSessions.mu.Unlock()
	}
	waitCtx, cancel := context.WithTimeout(ctx, assistWait)
	code, _ := s.internalAs(waitCtx, user, http.MethodPost, "/api/v1/assistant/chats/"+url.PathEscape(created.ID)+"/messages", map[string]string{"text": text, "page": "/assistant", "model": s.opts.Assistant.Settings().VoiceModel})
	cancel()
	if code >= 400 {
		return say("Die Nachricht wurde nicht angenommen. Prüfe den laufenden Auftrag im KI-Assistenten.", "The message was not accepted. Check the current task in the assistant.")
	}
	chat, err := s.chats.Get(user.Name, created.ID)
	if err != nil || chat.Run == nil {
		return say("Der KI-Assistent ist gerade nicht erreichbar.", "The AI assistant cannot be reached right now.")
	}
	switch chat.Run.State {
	case "confirmation":
		reason := ""
		if a := assistant.Pending(chat); a != nil {
			reason = " " + a.Reason
		}
		return say("Dafür brauche ich deine Freigabe in TitanOS."+reason, "I need your approval in TitanOS for this."+reason)
	case "running", "queued", "waiting":
		return say("Ich arbeite noch daran. Das Ergebnis steht gleich im KI-Assistenten von TitanOS.", "I am still working on it. The result will be in the TitanOS assistant shortly.")
	case "blocked":
		return say("Das hat nicht geklappt. Einzelheiten stehen im KI-Assistenten von TitanOS.", "That did not work. Details are in the TitanOS assistant.")
	}
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if e := chat.Messages[i]; e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
			return speakable(e.Content)
		}
	}
	return say("Erledigt.", "Done.")
}

// speakable keeps an answer short and drops Markdown that a voice would read out.
func speakable(text string) string {
	text = strings.NewReplacer("**", "", "__", "", "`", "", "#", "").Replace(text)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 600 {
		return string(runes[:600]) + " …"
	}
	return string(runes)
}

type assistSession struct {
	Chat string
	Used time.Time
}
type assistSessions struct {
	mu       sync.Mutex
	entries  map[string]assistSession
	requests map[string]time.Time
	busy     map[string]bool
}
