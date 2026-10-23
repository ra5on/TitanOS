package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
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
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &q) != nil || len(q.ID) > 100 {
		return
	}
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" || len(q.Text) > assistant.MaxMessage {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), assistWait+15*time.Second)
		defer cancel()
		answer := s.assist(ctx, q.Text)
		if err := s.opts.HomeAssistant.FireEvent(ctx, "titanos_answer", map[string]string{"id": q.ID, "text": answer}); err != nil {
			slog.Warn("answer to Home Assistant not delivered", "err", err)
		}
	}()
}

// assist runs a question and returns what Home Assistant should say.
func (s *Server) assist(ctx context.Context, text string) string {
	settings, _ := s.opts.HomeAssistant.Settings()
	user, ok := s.opts.Users.Get(settings.Account)
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
	code, body := s.internalAs(ctx, user, http.MethodPost, "/api/v1/assistant/chats", nil)
	var created struct{ ID string }
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &created) != nil || created.ID == "" {
		return say("Der KI-Assistent ist gerade nicht erreichbar.", "The AI assistant cannot be reached right now.")
	}
	waitCtx, cancel := context.WithTimeout(ctx, assistWait)
	s.internalAs(waitCtx, user, http.MethodPost, "/api/v1/assistant/chats/"+url.PathEscape(created.ID)+"/messages", map[string]string{"text": text, "page": "/assistant"})
	cancel()
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
	if len(runes) > 1500 {
		return string(runes[:1500]) + " …"
	}
	return string(runes)
}
