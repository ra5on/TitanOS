package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// The KI-Agent (PLAN 44, docs/AGENT.md): chats, projects and runs. A run
// belongs to the backend: it goes on when the page is closed and sends a
// push message when it is done or waits for the person. Runs live in the
// RAM of titand; every step is saved on the data volume, and a run that
// titand did not finish is marked "interrupted" at the next start.

const (
	maxAgentMessage = 32 << 10
	maxProjects     = 100
)

type agentRun struct {
	user, chat string
	cancel     context.CancelFunc
	subs       map[chan agent.Event]struct{}
}

type agentRuntime struct {
	mu   sync.Mutex
	runs map[string]*agentRun
}

func runKey(user, chat string) string { return user + "/" + chat }

// agentStore is where the chats live: .titanos/agent on the first mounted
// volume.
func (s *Server) agentStore() *agent.Store {
	if s.agentOpen != nil {
		return &agent.Store{Open: s.agentOpen}
	}
	return &agent.Store{Open: func() (*os.Root, error) {
		if s.opts.Storage == nil {
			return nil, volumedata.ErrUnavailable
		}
		mounted := s.opts.Storage.Mounted()
		volume, ok := s.agentVolume(mounted)
		if !ok {
			return nil, volumedata.ErrUnavailable
		}
		point := s.opts.Storage.MountPoint(volume)
		return volumedata.Open(filepath.Dir(point), volume, "agent", mounted)
	}}
}

func (s *Server) agentWeb() *agent.Web {
	if s.agentWebFactory != nil {
		return s.agentWebFactory()
	}
	return agent.NewWeb()
}

// requireAgent lets an account through that may use the agent.
func (s *Server) requireAgent(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !s.agentAllowed(currentUser(r)) {
			writeError(w, http.StatusForbidden, "agent_not_allowed", "this account may not use the KI-Agent")
			return
		}
		next(w, r)
	})
}

func agentStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, agent.ErrNoVolume):
		writeError(w, http.StatusServiceUnavailable, "agent_no_volume", "the agent needs a mounted data volume for its chats")
	default:
		writeError(w, http.StatusInternalServerError, "agent_store", err.Error())
	}
}

func (s *Server) handleAgentChats(w http.ResponseWriter, r *http.Request) {
	list, err := s.agentStore().Chats(currentUser(r).Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	s.agentRT.mu.Lock()
	for i := range list {
		if _, running := s.agentRT.runs[runKey(currentUser(r).Name, list[i].ID)]; running && list[i].State != agent.StateWaiting {
			list[i].State = agent.StateRunning
		}
	}
	s.agentRT.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"chats": list})
}

func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	chat, err := s.agentStore().Chat(currentUser(r).Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chat)
}

type agentMessage struct {
	Message string `json:"message"`
	Project string `json:"project,omitempty"`
	Model   string `json:"model,omitempty"`
	Think   string `json:"think,omitempty"`
}

func (m *agentMessage) check() error {
	m.Message = strings.TrimSpace(m.Message)
	if m.Message == "" || len(m.Message) > maxAgentMessage || !utf8.ValidString(m.Message) {
		return errors.New("the message is empty or too long")
	}
	if len(m.Model) > 200 || len(m.Think) > 40 {
		return errors.New("invalid model or level")
	}
	return nil
}

func chatTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 60 {
		cut := string(r[:60])
		if i := strings.LastIndex(cut, " "); i > 30 {
			cut = cut[:i]
		}
		return cut + " …"
	}
	return text
}

// handleAgentChatNew starts a chat with its first message.
func (s *Server) handleAgentChatNew(w http.ResponseWriter, r *http.Request) {
	var req agentMessage
	if !readJSON(w, r, &req) {
		return
	}
	if err := req.check(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_message", err.Error())
		return
	}
	user := currentUser(r)
	store := s.agentStore()
	if req.Project != "" {
		if _, err := s.agentProject(user.Name, req.Project); err != nil {
			agentStoreError(w, err)
			return
		}
	}
	now := time.Now()
	chat := &agent.Chat{ID: agent.NewID(), Title: chatTitle(req.Message), Project: req.Project, Created: now, Updated: now, Model: req.Model, Think: req.Think, Origin: "web", State: agent.StateIdle, Entries: []agent.Entry{}}
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: now, Role: "user", Content: req.Message})
	if err := store.SaveChat(user.Name, chat); err != nil {
		agentStoreError(w, err)
		return
	}
	if err := s.agentStart(user, chat, func(rn *agent.Runner, ctx context.Context) error { return rn.Run(ctx, chat) }); err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, chat)
}

// handleAgentMessage adds a message to a chat and runs it.
func (s *Server) handleAgentMessage(w http.ResponseWriter, r *http.Request) {
	var req agentMessage
	if !readJSON(w, r, &req) {
		return
	}
	if err := req.check(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_message", err.Error())
		return
	}
	user := currentUser(r)
	if s.agentRunning(user.Name, r.PathValue("id")) {
		writeError(w, http.StatusConflict, "agent_busy", "the agent is still working on this chat; stop it first")
		return
	}
	store := s.agentStore()
	chat, err := store.Chat(user.Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if req.Model != "" {
		chat.Model = req.Model
	}
	if req.Think != "" {
		chat.Think = req.Think
	}
	// A new message answers a waiting step with "no".
	if chat.Pending != nil {
		chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: time.Now(), Role: "note", Note: "declined", Tool: chat.Pending.Tool, Summary: chat.Pending.Summary})
		chat.Pending = nil
	}
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: time.Now(), Role: "user", Content: req.Message})
	chat.Updated = time.Now()
	if err := store.SaveChat(user.Name, chat); err != nil {
		agentStoreError(w, err)
		return
	}
	if err := s.agentStart(user, chat, func(rn *agent.Runner, ctx context.Context) error { return rn.Run(ctx, chat) }); err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, chat)
}

// handleAgentDecide approves or declines the waiting step. Only the owner
// of the chat, in a web session, can do that; push messages and links
// only open the chat (PLAN 6c).
func (s *Server) handleAgentDecide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if _, inProcess := r.Context().Value(inProcessIdentity{}).(string); inProcess {
		writeError(w, http.StatusForbidden, "web_session_required", "only the person can approve")
		return
	}
	if _, token := r.Context().Value(apiTokenIdentity{}).(string); token {
		writeError(w, http.StatusForbidden, "web_session_required", "only the person can approve")
		return
	}
	user := currentUser(r)
	if s.agentRunning(user.Name, r.PathValue("id")) {
		writeError(w, http.StatusConflict, "agent_busy", "the agent is still working on this chat")
		return
	}
	chat, err := s.agentStore().Chat(user.Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if chat.Pending == nil || chat.Pending.ID != req.ID {
		writeError(w, http.StatusConflict, "agent_nothing_pending", "this step no longer waits")
		return
	}
	if err := s.agentStart(user, chat, func(rn *agent.Runner, ctx context.Context) error {
		return rn.Decide(ctx, chat, req.ID, req.Approve)
	}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	s.agentRT.mu.Lock()
	run := s.agentRT.runs[runKey(user.Name, r.PathValue("id"))]
	s.agentRT.mu.Unlock()
	if run == nil {
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}
	run.cancel()
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

func (s *Server) handleAgentChatPatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title   *string `json:"title"`
		Project *string `json:"project"`
		Pinned  *bool   `json:"pinned"`
		Model   *string `json:"model"`
		Think   *string `json:"think"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if s.agentRunning(user.Name, r.PathValue("id")) {
		writeError(w, http.StatusConflict, "agent_busy", "the agent is still working on this chat")
		return
	}
	store := s.agentStore()
	chat, err := store.Chat(user.Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" || len(t) > 200 {
			writeError(w, http.StatusBadRequest, "invalid_title", "1 to 200 characters")
			return
		}
		chat.Title = t
	}
	if req.Project != nil {
		if *req.Project != "" {
			if _, err := s.agentProject(user.Name, *req.Project); err != nil {
				agentStoreError(w, err)
				return
			}
		}
		chat.Project = *req.Project
	}
	if req.Pinned != nil {
		chat.Pinned = *req.Pinned
	}
	if req.Model != nil && len(*req.Model) <= 200 {
		chat.Model = *req.Model
	}
	if req.Think != nil && len(*req.Think) <= 40 {
		chat.Think = *req.Think
	}
	if err := store.SaveChat(user.Name, chat); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chat.Info())
}

func (s *Server) handleAgentChatDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if s.agentRunning(user.Name, r.PathValue("id")) {
		writeError(w, http.StatusConflict, "agent_busy", "stop the agent first")
		return
	}
	if err := s.agentStore().DeleteChat(user.Name, r.PathValue("id")); err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentRunning(user, chat string) bool {
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	_, ok := s.agentRT.runs[runKey(user, chat)]
	return ok
}

// agentStart runs a chat in the background as the account.
func (s *Server) agentStart(user auth.User, chat *agent.Chat, work func(*agent.Runner, context.Context) error) error {
	settings := s.agentSettings()
	if !settings.Enabled {
		return errors.New("the agent is switched off")
	}
	client, err := s.agentOllama()
	if err != nil {
		return err
	}
	model := chat.Model
	if model == "" {
		model = settings.Model
	}
	if model == "" {
		return errors.New("no model chosen")
	}
	key := runKey(user.Name, chat.ID)
	s.agentRT.mu.Lock()
	if s.agentRT.runs == nil {
		s.agentRT.runs = map[string]*agentRun{}
	}
	if _, busy := s.agentRT.runs[key]; busy {
		s.agentRT.mu.Unlock()
		return agent.ErrBusy
	}
	ctx, cancel := context.WithCancel(s.agentBase())
	run := &agentRun{user: user.Name, chat: chat.ID, cancel: cancel, subs: map[chan agent.Event]struct{}{}}
	s.agentRT.runs[key] = run
	s.agentRT.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.agentRT.mu.Lock()
			delete(s.agentRT.runs, key)
			subs := run.subs
			run.subs = nil
			s.agentRT.mu.Unlock()
			for ch := range subs {
				close(ch)
			}
		}()
		runner := s.agentRunner(ctx, user, settings, client, model, chat, run)
		err := work(runner, ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("agent run ended with an error", "chat", chat.ID, "err", err)
		}
		s.agentDone(user, chat)
	}()
	return nil
}

// agentBase is the context runs live in: it ends when titand stops.
func (s *Server) agentBase() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *Server) agentRunner(ctx context.Context, user auth.User, settings agentSettings, client *agent.Ollama, model string, chat *agent.Chat, run *agentRun) *agent.Runner {
	store := s.agentStore()
	box := s.newAgentToolbox(user, settings)
	think := chat.Think
	if think == "" {
		think = settings.Think
	}
	var thinkValue any
	if think != "" {
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if info, err := client.Show(sctx, model); err == nil {
			thinkValue = agent.ThinkValue(info.Thinking, think)
		}
		cancel()
	}
	return &agent.Runner{
		Ollama: client, Model: model, Think: thinkValue, Context: settings.Context, MaxSteps: settings.MaxSteps,
		System: s.agentPrompt(ctx, user, settings, chat, box),
		Tools:  box,
		Save:   func(c *agent.Chat) error { return store.SaveChat(user.Name, c) },
		Emit: func(e agent.Event) {
			s.agentRT.mu.Lock()
			defer s.agentRT.mu.Unlock()
			for ch := range run.subs {
				select {
				case ch <- e:
				default:
					// A slow page loses streamed pieces, never saved lines:
					// it reloads the chat when the run ends.
				}
			}
		},
	}
}

// agentDone tells the person when a run ended without them watching.
func (s *Server) agentDone(user auth.User, chat *agent.Chat) {
	if s.opts.WebPush == nil {
		return
	}
	var body string
	switch chat.State {
	case agent.StateWaiting:
		body = "Wartet auf deine Freigabe: " + chat.Pending.Summary
	case agent.StateError:
		body = "Fehler: " + chat.Error
	default:
		if chat.Origin == "web" && s.agentWatched(user.Name, chat.ID) {
			return
		}
		body = "Fertig."
		for i := len(chat.Entries) - 1; i >= 0; i-- {
			if e := chat.Entries[i]; e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
				body = e.Content
				break
			}
		}
	}
	if r := []rune(body); len(r) > 180 {
		body = string(r[:179]) + "…"
	}
	s.opts.WebPush.SendTo(user.Name, webpush.Message{Title: "KI-Agent: " + chat.Title, Body: body, URL: "/agent?chat=" + chat.ID, Tag: "agent-" + chat.ID})
}

// agentWatched reports whether a page of the chat is open right now.
func (s *Server) agentWatched(user, chat string) bool {
	s.agentWatch.mu.Lock()
	defer s.agentWatch.mu.Unlock()
	return s.agentWatch.open[runKey(user, chat)] > 0
}

type agentWatchers struct {
	mu   sync.Mutex
	open map[string]int
}

// handleAgentEvents streams the events of a chat as server-sent events
// while it runs; the page reloads the chat when the stream ends.
func (s *Server) handleAgentEvents(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := r.PathValue("id")
	if !agent.ValidID(id) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	// The writers of the handler chain (activity log) unwrap to the real one.
	rc := http.NewResponseController(w)
	flusher := flushFunc(func() { _ = rc.Flush() })
	key := runKey(user.Name, id)
	s.agentWatch.mu.Lock()
	if s.agentWatch.open == nil {
		s.agentWatch.open = map[string]int{}
	}
	s.agentWatch.open[key]++
	s.agentWatch.mu.Unlock()
	defer func() {
		s.agentWatch.mu.Lock()
		s.agentWatch.open[key]--
		if s.agentWatch.open[key] <= 0 {
			delete(s.agentWatch.open, key)
		}
		s.agentWatch.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	ch := make(chan agent.Event, 256)
	s.agentRT.mu.Lock()
	run := s.agentRT.runs[key]
	if run != nil && run.subs != nil {
		run.subs[ch] = struct{}{}
	}
	s.agentRT.mu.Unlock()
	if run == nil {
		fmt.Fprint(w, "event: end\ndata: {}\n\n")
		flusher.Flush()
		return
	}
	defer func() {
		s.agentRT.mu.Lock()
		if run.subs != nil {
			delete(run.subs, ch)
		}
		s.agentRT.mu.Unlock()
	}()
	fmt.Fprint(w, "event: state\ndata: {\"type\":\"state\",\"state\":\"running\"}\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case e, open := <-ch:
			if !open {
				fmt.Fprint(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			flusher.Flush()
		}
	}
}

// AgentInterrupted marks runs that titand did not finish (restart, app
// update) as interrupted; it never claims they go on.
func (s *Server) AgentInterrupted(ctx context.Context) {
	store := s.agentStore()
	for _, user := range store.Accounts() {
		list, err := store.Chats(user)
		if err != nil {
			continue
		}
		for _, info := range list {
			if info.State != agent.StateRunning {
				continue
			}
			chat, err := store.Chat(user, info.ID)
			if err != nil || chat.State != agent.StateRunning {
				continue
			}
			chat.State = agent.StateInterrupted
			chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: time.Now(), Role: "note", Note: "interrupted"})
			if err := store.SaveChat(user, chat); err != nil {
				slog.Warn("interrupted agent run not marked", "chat", chat.ID, "err", err)
			}
		}
	}
}

// AgentRunning reports whether the agent works on something right now, so
// that an app update can wait for it.
func (s *Server) AgentRunning() bool {
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	return len(s.agentRT.runs) > 0
}

type flushFunc func()

func (f flushFunc) Flush() { f() }
