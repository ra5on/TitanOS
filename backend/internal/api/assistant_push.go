package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// Approving from a notification: a message about a waiting action carries
// buttons with a one-time token. The token lives only in memory (a restart
// of titand ends it), is bound to account, chat and action, and lasts 15
// minutes. Actions that need the typed name are never approved this way.

const pushTokenLife = 15 * time.Minute

type pushToken struct {
	user, chat, action string
	expires            time.Time
}

type pushTokens struct {
	mu   sync.Mutex
	list map[string]pushToken
}

func (p *pushTokens) add(t pushToken) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.list == nil {
		p.list = map[string]pushToken{}
	}
	now := time.Now()
	for k, v := range p.list {
		if now.After(v.expires) {
			delete(p.list, k)
		}
	}
	p.list[token] = t
	return token
}

// take returns a token once; a second use finds nothing.
func (p *pushTokens) take(token string) (pushToken, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.list[token]
	delete(p.list, token)
	if !ok || time.Now().After(t.expires) {
		return pushToken{}, false
	}
	return t, true
}

// pushActions are the buttons for a waiting action, if approving from
// notifications is switched on and the action allows it.
func (s *Server) pushActions(user auth.User, chat *assistant.Chat, a *assistant.Action, origin, language string) []notify.PushAction {
	if !s.opts.Assistant.Settings().PushApproval || a == nil || a.Level == assistant.LevelDestroy {
		return nil
	}
	token := s.push.add(pushToken{user: user.Name, chat: chat.ID, action: a.ID, expires: time.Now().Add(pushTokenLife)})
	labels := map[string][2]string{"de": {"Freigeben", "Ablehnen"}, "en": {"Approve", "Reject"}}[language]
	if labels[0] == "" {
		labels = [2]string{"Approve", "Reject"}
	}
	var actions []notify.PushAction
	for i, decision := range []string{"approve", "reject"} {
		action := notify.PushAction{Label: labels[i], HAAction: "TITANOS_" + strings.ToUpper(decision) + "_" + token}
		if origin != "" {
			action.URL = origin + "/api/v1/assistant/push/" + token + "/" + decision
		}
		actions = append(actions, action)
	}
	return actions
}

var errPushToken = fmt.Errorf("%w: the link is unknown, used or expired", assistant.ErrNoAction)

// decidePush approves or rejects the action of a token, as its account and
// through the same route as the web interface.
func (s *Server) decidePush(ctx context.Context, token, decision string) error {
	if decision != "approve" && decision != "reject" {
		return errPushToken
	}
	t, ok := s.push.take(token)
	if !ok {
		return errPushToken
	}
	user, ok := s.opts.Users.Get(t.user)
	if !ok || !s.assistantAllowed(user) || !s.opts.Assistant.Settings().PushApproval {
		return errPushToken
	}
	chat, err := s.chats.Get(user.Name, t.chat)
	if err != nil {
		return errPushToken
	}
	pending := assistant.Pending(chat)
	if pending == nil || pending.ID != t.action || pending.Level == assistant.LevelDestroy {
		return errPushToken
	}
	body := map[string]any{"state": "approved", "confirmed": true}
	if decision == "reject" {
		body = map[string]any{"state": "rejected"}
	}
	// The request ends after a moment; the task goes on in the background,
	// as when a browser closes after approving.
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	route := "/api/v1/assistant/chats/" + url.PathEscape(chat.ID) + "/actions/" + url.PathEscape(pending.ID)
	code, answer := s.internalAs(callCtx, user, http.MethodPost, route, body)
	if code >= 300 {
		return fmt.Errorf("%d: %s", code, strings.TrimSpace(answer))
	}
	slog.Info("assistant action decided from a notification", "decision", decision, "chat", chat.ID, "user", user.Name)
	return nil
}

// handlePushDecision is the target of the buttons in ntfy. It needs no
// login: the token is the permission.
func (s *Server) handlePushDecision(w http.ResponseWriter, r *http.Request) {
	if err := s.decidePush(r.Context(), r.PathValue("token"), r.PathValue("decision")); err != nil {
		writeError(w, http.StatusNotFound, "push_token", "the link is unknown, used or expired")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHANotificationAction gets the buttons pressed in the Home Assistant app.
func (s *Server) handleHANotificationAction(data json.RawMessage) {
	var ev struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	for _, decision := range []string{"approve", "reject"} {
		prefix := "TITANOS_" + strings.ToUpper(decision) + "_"
		if token, ok := strings.CutPrefix(ev.Action, prefix); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := s.decidePush(ctx, token, decision); err != nil {
					slog.Warn("decision from the Home Assistant app refused", "err", err)
				}
			}()
		}
	}
}
