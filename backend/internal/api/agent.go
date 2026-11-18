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
	"slices"
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
	// byPerson: started by the person in the chat; counts for the limit
	// of runs at once (agent_runs.go).
	byPerson bool
	ctx      context.Context
	cancel   context.CancelFunc
	// subs are the pages that follow the run; view is what a page that
	// comes later gets as its snapshot. Both under agentRuntime.mu.
	subs map[*agentSub]struct{}
	view agentRunView
}

type agentRuntime struct {
	mu   sync.Mutex
	runs map[string]*agentRun
	// work counts the runs still at it, including what follows the
	// answer (copy in the share, learning). Tests wait for it before
	// their folders go away.
	work sync.WaitGroup
}

func runKey(user, chat string) string { return user + "/" + chat }

// agentStore is where the chats live: .titanos/agent on the first mounted
// volume.
func (s *Server) agentStore() *agent.Store {
	s.agentStoreOnce.Do(func() { s.agentStoreOne = s.newAgentStore() })
	return s.agentStoreOne
}

func (s *Server) newAgentStore() *agent.Store {
	env := s.agentEnv()
	if s.agentOpen != nil {
		return &agent.Store{Open: func() (*os.Root, error) { return s.agentOpen() }}
	}
	return &agent.Store{Open: func() (*os.Root, error) {
		if s.agentMoving() || env.storage == nil {
			return nil, volumedata.ErrUnavailable
		}
		mounted := env.storage.Mounted()
		volume, ok := s.agentVolume(mounted)
		if !ok {
			return nil, volumedata.ErrUnavailable
		}
		point := env.storage.MountPoint(volume)
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
		user := currentUser(r)
		if !s.agentAllowed(user) {
			writeError(w, http.StatusForbidden, "agent_not_allowed", "this account may not use the KI-Agent")
			return
		}
		// PLAN 91: owners and administrators look after the profile of Home
		// Assistant (its chats, memory, instruction, reminders) through the
		// same routes with ?profile=assist: the handlers then work on the
		// profile instead of the account. It keeps the role of who asks.
		if r.URL.Query().Get("profile") == "assist" {
			if !user.Role.CanAdminister() {
				writeError(w, http.StatusForbidden, "forbidden", "owners and administrators only")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), contextKey{}, auth.User{Name: agent.AssistProfile, Role: user.Role}))
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
	s.agentStore().MarkUnread(currentUser(r).Name, list)
	// Chats of other accounts in shared projects (read-only, PLAN 51).
	list = append(list, s.agentSharedChats(currentUser(r).Name)...)
	writeJSON(w, http.StatusOK, map[string]any{"chats": list})
}

func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	if author := r.URL.Query().Get("author"); author != "" && author != currentUser(r).Name {
		chat, err := s.agentReadableChat(currentUser(r).Name, author, r.PathValue("id"))
		if err != nil {
			agentStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			*agent.Chat
			Author   string `json:"author"`
			ReadOnly bool   `json:"readOnly"`
		}{chat, author, true})
		return
	}
	user := currentUser(r).Name
	chat, err := s.agentStore().Chat(user, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	// A title of before 1.07 that ends in " …" gets one from the model once,
	// when the chat is opened (unless the person named it).
	if chat.LegacyTitle() && agentTitleDue(chat) && !s.agentRunning(user, chat.ID) {
		s.agentRetitle(user, chat.ID)
	}
	// Until then (and for chats the model does not name) the whole question.
	chat.Title = chat.ShownTitle()
	writeJSON(w, http.StatusOK, struct {
		*agent.Chat
		// Titling: the model is writing the title; the page asks again.
		Titling bool `json:"titling,omitempty"`
	}{chat, s.agentTitling(user, chat.ID)})
}

type agentMessage struct {
	Message string `json:"message"`
	// Draft makes an empty chat that does not run yet (to attach files
	// before the first message).
	Draft   bool   `json:"draft,omitempty"`
	Project string `json:"project,omitempty"`
	Model   string `json:"model,omitempty"`
	Think   string `json:"think,omitempty"`
	// Autonomy sets how the chat works ("direct", "plan", "ask"); left
	// out, the chat keeps what it has.
	Autonomy string `json:"autonomy,omitempty"`
	// Replace names a message of the person in this chat (PLAN 90-4): it
	// and everything after it leave the chat, then Message is asked instead
	// ("Bearbeiten und erneut senden"). Without a Message the same one is
	// asked again ("Neu erzeugen").
	Replace string `json:"replace,omitempty"`
	// Context names what is open in the program of the panel (1.13): a
	// reference, checked and kept with the message, never content.
	Context *agent.ChatContext `json:"context,omitempty"`
	// Origin "panel" starts a chat of the panel in Mail, Kontakte or
	// Kalender (only for a new chat; nothing else may be given).
	Origin string `json:"origin,omitempty"`
}

func (m *agentMessage) check() error {
	if m.Origin != "" && m.Origin != "panel" {
		return errors.New("invalid origin")
	}
	if err := checkContext(m.Context); err != nil {
		return err
	}
	m.Message = strings.TrimSpace(m.Message)
	if m.Message == "" && m.Replace != "" {
		return nil
	}
	if m.Message == "" || len(m.Message) > maxAgentMessage || !utf8.ValidString(m.Message) {
		return errors.New("the message is empty or too long")
	}
	if len(m.Model) > 200 || len(m.Think) > 40 {
		return errors.New("invalid model or level")
	}
	return nil
}

// fileRef is a line naming an attached file or a file of the NAS.
var fileRef = agent.FileLine

// chatTitle is the title of a new chat: its whole first question.
func chatTitle(text string) string { return agent.QuestionTitle(text) }

// chatTitleMax bounds a question taken as the title (a pasted log, a letter).
const chatTitleMax = agent.QuestionTitleMax

// handleAgentChatNew starts a chat with its first message.
func (s *Server) handleAgentChatNew(w http.ResponseWriter, r *http.Request) {
	var req agentMessage
	if !readJSON(w, r, &req) {
		return
	}
	if req.Draft && strings.TrimSpace(req.Message) == "" {
		req.Message = "-"
	}
	req.Replace = "" // only in a chat that exists
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
	chat := &agent.Chat{ID: agent.NewID(), Title: chatTitle(req.Message), Project: req.Project, Created: now, Updated: now, Autonomy: autonomyOf(req.Autonomy, ""), Origin: "web", State: agent.StateIdle, Entries: []agent.Entry{}}
	if req.Origin == "panel" {
		// The tools of mail, calendar and contacts are there from the start.
		chat.Origin, chat.Areas = "panel", slices.Clone(panelAreas)
	}
	chat.ChooseModel(req.Model)
	chat.ChooseThink(req.Think)
	if req.Draft {
		chat.Title = ""
		if err := store.SaveChat(user.Name, chat); err != nil {
			agentStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, chat)
		return
	}
	// At the limit of runs at once no chat is made (1.01).
	run, err := s.agentClaim(user.Name, chat.ID, true)
	if err != nil {
		agentStartError(w, err)
		return
	}
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: now, Role: "user", Content: req.Message, Context: req.Context})
	if err := store.SaveChat(user.Name, chat); err != nil {
		s.agentRelease(run)
		agentStoreError(w, err)
		return
	}
	// The answer is the chat as it starts: the run changes it right away
	// in its own goroutine (PLAN 59, a data race before).
	snapshot := json.RawMessage(mustJSON(chat))
	if err := s.agentLaunch(run, user, chat, s.agentWork(chat)); err != nil {
		agentStartError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, snapshot)
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
	if !agent.ValidID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	// The place of the run is taken before the message is written: a chat
	// that runs, or an account at its limit of runs, keeps the chat as it is.
	run, err := s.agentClaim(user.Name, r.PathValue("id"), true)
	if err != nil {
		agentStartError(w, err)
		return
	}
	launched := false
	defer func() {
		if !launched {
			s.agentRelease(run)
		}
	}()
	store := s.agentStore()
	chat, err := store.Chat(user.Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if req.Model != "" {
		chat.ChooseModel(req.Model)
	}
	if req.Think != "" {
		chat.ChooseThink(req.Think)
	}
	chat.Autonomy = autonomyOf(req.Autonomy, chat.Autonomy)
	if req.Replace != "" {
		// What the steps after it changed on the NAS stays changed; only
		// the chat forgets them. A waiting step is dropped unanswered.
		at := slices.IndexFunc(chat.Entries, func(e agent.Entry) bool { return e.ID == req.Replace && e.Role == "user" })
		if at < 0 {
			writeError(w, http.StatusNotFound, "not_found", "no such message in this chat")
			return
		}
		if req.Message == "" {
			req.Message = chat.Entries[at].Content
		}
		chat.Entries, chat.Pending, chat.Plan = chat.Entries[:at:at], nil, nil
	}
	// A new message answers a waiting step with "no".
	if chat.Pending != nil {
		// The "no" stands where the step's result would: before the calls
		// of that round that were not made.
		skipped := chat.TakeSkipped()
		chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: time.Now(), Role: "note", Note: "declined", Tool: chat.Pending.Tool, Summary: chat.Pending.Summary})
		chat.Entries = append(chat.Entries, skipped...)
		chat.Pending = nil
	}
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: time.Now(), Role: "user", Content: req.Message, Context: req.Context})
	chat.Updated = time.Now()
	if chat.Title == "" {
		chat.Title = chatTitle(req.Message)
	}
	if err := store.SaveChat(user.Name, chat); err != nil {
		agentStoreError(w, err)
		return
	}
	// The answer is the chat as it starts: the run changes it right away
	// in its own goroutine (PLAN 59, a data race before).
	snapshot := json.RawMessage(mustJSON(chat))
	launched = true
	if err := s.agentLaunch(run, user, chat, s.agentWork(chat)); err != nil {
		agentStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, snapshot)
}

// handleAgentDecide approves or declines the waiting step. Only the owner
// of the chat, in a web session, can do that; push messages and links
// only open the chat (PLAN 6c).
func (s *Server) handleAgentDecide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
		// Edit (1.13, "Öffnen und bearbeiten"): no, the person finishes the
		// step in the program's editor; the answer names the draft.
		Edit bool `json:"edit,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Edit {
		req.Approve = false
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
	if !agent.ValidID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	// The answer goes on in a run of this chat only: another chat that
	// waits keeps its step (1.01, several runs at once).
	run, err := s.agentClaim(user.Name, r.PathValue("id"), true)
	if err != nil {
		agentStartError(w, err)
		return
	}
	chat, err := s.agentStore().Chat(user.Name, r.PathValue("id"))
	if err != nil {
		s.agentRelease(run)
		agentStoreError(w, err)
		return
	}
	if chat.Pending == nil || chat.Pending.ID != req.ID {
		s.agentRelease(run)
		writeError(w, http.StatusConflict, "agent_nothing_pending", "this step no longer waits")
		return
	}
	if req.Edit {
		// The draft is made now, before the step stops waiting; nothing runs.
		d, err := s.agentPendingDraft(r.Context(), user, chat)
		if err != nil {
			s.agentRelease(run)
			writeError(w, http.StatusConflict, "no_draft", err.Error())
			return
		}
		note := agent.NewID()
		// Saved before the run starts: the program asks for the draft at once.
		if err := chat.DeclineToEdit(req.ID, note, d, time.Now()); err != nil {
			s.agentRelease(run)
			agentStoreError(w, err)
			return
		}
		chat.State = agent.StateRunning
		if err := s.agentStore().SaveChat(user.Name, chat); err != nil {
			s.agentRelease(run)
			agentStoreError(w, err)
			return
		}
		if err := s.agentLaunch(run, user, chat, func(rn *agent.Runner, ctx context.Context) error {
			return rn.Run(ctx, chat)
		}); err != nil {
			agentStartError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "entry": note, "draft": d})
		return
	}
	if err := s.agentLaunch(run, user, chat, func(rn *agent.Runner, ctx context.Context) error {
		return rn.Decide(ctx, chat, req.ID, req.Approve)
	}); err != nil {
		agentStartError(w, err)
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
		// Archived moves the chat to the Archiv and back (PLAN 72).
		Archived *bool   `json:"archived"`
		Model    *string `json:"model"`
		Think    *string `json:"think"`
		Autonomy *string `json:"autonomy"`
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
		// Named by the person: the model never renames it (1.07).
		chat.TitleOwn = true
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
	if req.Archived != nil {
		chat.Archived = *req.Archived
	}
	if req.Model != nil && len(*req.Model) <= 200 {
		chat.ChooseModel(*req.Model)
	}
	if req.Think != nil && len(*req.Think) <= 40 {
		chat.ChooseThink(*req.Think)
	}
	if req.Autonomy != nil {
		chat.Autonomy = autonomyOf(*req.Autonomy, chat.Autonomy)
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

// handleAgentChatsDelete deletes all chats of the account at once (PLAN
// 56), with their attachments; pinned ones stay with keepPinned, running
// ones always (they are counted as skipped). Projects, memory and skills
// stay. Chats of other accounts in a shared project are never touched.
func (s *Server) handleAgentChatsDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	keepPinned := r.URL.Query().Get("keepPinned") == "true"
	store := s.agentStore()
	list, err := store.Chats(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	deleted, skipped := 0, 0
	for _, c := range list {
		if (keepPinned && c.Pinned) || s.agentRunning(user.Name, c.ID) {
			skipped++
			continue
		}
		if err := store.DeleteChat(user.Name, c.ID); err != nil {
			agentStoreError(w, err)
			return
		}
		deleted++
	}
	slog.Info("agent chats deleted", "user", user.Name, "deleted", deleted, "kept", skipped)
	writeJSON(w, http.StatusOK, map[string]int{"deleted": deleted, "kept": skipped})
}

func (s *Server) agentRunning(user, chat string) bool {
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	_, ok := s.agentRT.runs[runKey(user, chat)]
	return ok
}

// agentStart runs a chat in the background as the account: planned tasks,
// the look into a notification, Home Assistant. They never wait for the
// limit of runs at once (agent_runs.go); a run the person starts in the
// chat claims its place with agentClaim before anything is written.
func (s *Server) agentStart(user auth.User, chat *agent.Chat, work func(*agent.Runner, context.Context) error) error {
	run, err := s.agentClaim(user.Name, chat.ID, false)
	if err != nil {
		return err
	}
	return s.agentLaunch(run, user, chat, work)
}

// agentLaunch starts a claimed run; when it cannot, the claim goes.
func (s *Server) agentLaunch(run *agentRun, user auth.User, chat *agent.Chat, work func(*agent.Runner, context.Context) error) error {
	settings := s.agentSettings()
	client, err := s.agentReady(settings)
	if err != nil {
		s.agentRelease(run)
		return err
	}
	// A copy of the chat in the share the person changed is read back first.
	s.agentCheckEdit(user, chat)
	model := chat.OwnModel()
	if model == "" {
		model = settings.Model
	}
	if model == "" {
		s.agentRelease(run)
		return errors.New("no model chosen")
	}
	s.agentRT.work.Add(1)
	ctx := run.ctx
	go func() {
		defer s.agentRT.work.Done()
		// After the first answer the model writes a short title (1.07),
		// once the run has ended; it is noted before, so the page that
		// reads the chat at the end knows to ask again.
		titling := false
		defer func() {
			if titling {
				s.agentTitleRun(user.Name, chat.ID)
			}
		}()
		defer s.agentRelease(run)
		defer func() { titling = agentTitleDue(chat) && s.agentTitleMark(user.Name, chat.ID) }()
		runner := s.agentRunner(ctx, user, settings, client, model, chat, run)
		err := work(runner, ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("agent run ended with an error", "chat", chat.ID, "err", err)
		}
		s.agentDone(user, chat)
		s.agentLearn(user.Name, chat)
	}()
	return nil
}

// agentReady is the client of the model when the agent can run now.
func (s *Server) agentReady(settings agentSettings) (*agent.Ollama, error) {
	if !settings.Enabled {
		return nil, errors.New("the agent is switched off")
	}
	if s.agentMoving() {
		return nil, errAgentMoving
	}
	return s.agentOllama()
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
	think := chat.OwnThink()
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
		Ollama: client, Model: model, Think: thinkValue, Context: settings.Context, KeepAlive: settings.KeepAlive, MaxSteps: settings.MaxSteps,
		MaxTime: time.Duration(settings.MaxMinutes) * time.Minute, Window: settings.Context, Probe: agentProbe(client, model, settings.KeepAlive),
		LazyTools: !settings.AllTools, Delegate: !settings.NoDelegate,
		System: s.agentPrompt(ctx, user, settings, chat, box),
		Tools:  box,
		Save:   func(c *agent.Chat) error { return store.SaveChat(user.Name, c) },
		Emit: func(e agent.Event) {
			s.agentRT.mu.Lock()
			defer s.agentRT.mu.Unlock()
			run.emitLocked(e)
		},
	}
}

// agentDone tells the person when a run ended without them watching
// (PLAN 48): nothing goes out while the chat is open and visible on one of
// their devices, and only one message per chat until they look at it.
func (s *Server) agentDone(user auth.User, chat *agent.Chat) {
	env := s.agentEnv()
	if env.webPush == nil || !s.agentShouldPush(user.Name, chat.ID) {
		return
	}
	var body string
	switch chat.State {
	case agent.StateWaiting:
		body = "Wartet auf deine Freigabe"
		if chat.Pending != nil {
			body += ": " + chat.Pending.Summary
		}
	case agent.StateError:
		body = "Fehler: " + chat.Error
	default:
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
	if user.Name == agent.AssistProfile {
		// The profile of Home Assistant has no devices: only a step that
		// waits goes out, to those who can approve it.
		if chat.State != agent.StateWaiting || env.users == nil {
			return
		}
		for _, u := range env.users.List() {
			if !u.Disabled && u.Role.CanAdminister() && s.agentAllowed(u) {
				env.webPush.SendTo(u.Name, webpush.Message{Title: "KI-Agent (Home Assistant)", Body: body, URL: "/agent?profile=assist&chat=" + chat.ID, Tag: "agent-" + chat.ID})
			}
		}
		return
	}
	env.webPush.SendTo(user.Name, webpush.Message{Title: "KI-Agent: " + chat.ShownTitle(), Body: body, URL: "/agent?chat=" + chat.ID, Tag: "agent-" + chat.ID})
}

// agentShouldPush decides whether the end of a run goes out as a push
// message, and notes it when it does. A chat on screen counts as read.
func (s *Server) agentShouldPush(user, chat string) bool {
	store := s.agentStore()
	if s.agentWatched(user, chat) {
		_ = store.Seen(user, chat, time.Now())
		return false
	}
	seen := store.SeenAt(user, chat)
	key := runKey(user, chat)
	s.agentWatch.mu.Lock()
	defer s.agentWatch.mu.Unlock()
	if last, sent := s.agentWatch.pushed[key]; sent && last.After(seen) {
		// The last message is still unread; it says enough.
		return false
	}
	if s.agentWatch.pushed == nil {
		s.agentWatch.pushed = map[string]time.Time{}
	}
	s.agentWatch.pushed[key] = time.Now()
	return true
}

// agentVisibleFor is how long a "visible" signal of a page holds; the page
// repeats it every 15 seconds while the chat is on screen.
const agentVisibleFor = 40 * time.Second

// agentWatched reports whether the chat is on screen on one of the
// person's devices right now.
func (s *Server) agentWatched(user, chat string) bool {
	s.agentWatch.mu.Lock()
	defer s.agentWatch.mu.Unlock()
	at, ok := s.agentWatch.visible[runKey(user, chat)]
	return ok && time.Since(at) < agentVisibleFor
}

// handleAgentSeen: the page shows a chat (visible) or no longer does. A
// visible chat counts as read on all devices.
func (s *Server) handleAgentSeen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Visible bool `json:"visible"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	id := r.PathValue("id")
	if !agent.ValidID(id) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	key := runKey(user.Name, id)
	s.agentWatch.mu.Lock()
	if s.agentWatch.visible == nil {
		s.agentWatch.visible = map[string]time.Time{}
	}
	if req.Visible {
		s.agentWatch.visible[key] = time.Now()
	} else {
		delete(s.agentWatch.visible, key)
	}
	for k, at := range s.agentWatch.visible {
		if time.Since(at) > agentVisibleFor {
			delete(s.agentWatch.visible, k)
		}
	}
	s.agentWatch.mu.Unlock()
	if req.Visible {
		if err := s.agentStore().Seen(user.Name, id, time.Now()); err != nil {
			agentStoreError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

type agentWatchers struct {
	mu   sync.Mutex
	open map[string]int
	// visible: chats on screen (handleAgentSeen); pushed: the last push
	// message per chat.
	visible map[string]time.Time
	pushed  map[string]time.Time
}

// handleAgentEvents streams the events of a chat as server-sent events
// while it runs, beginning with a snapshot of the run (agent_runs.go); the
// page reloads the chat when the stream ends.
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
	s.agentRT.mu.Lock()
	run := s.agentRT.runs[key]
	s.agentRT.mu.Unlock()
	var sub *agentSub
	var first agentSnapshot
	ok := false
	if run != nil {
		sub, first, ok = s.agentSubscribe(run)
	}
	if !ok {
		fmt.Fprint(w, "event: end\ndata: {}\n\n")
		flusher.Flush()
		return
	}
	defer s.agentUnsubscribe(run, sub)
	// 1.01: first what the run did so far, then the state line older pages
	// expect, then the events as they come.
	snapshot := func(snap agentSnapshot) {
		data, _ := json.Marshal(snap)
		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
	}
	snapshot(first)
	fmt.Fprint(w, "event: state\ndata: {\"type\":\"state\",\"state\":\"running\"}\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	// Like an open console, the stream ends when its login does: logout,
	// the lock screen, a locked or removed account.
	recheck := time.NewTicker(time.Duration(consoleRecheck.Load()))
	defer recheck.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-recheck.C:
			if _, ok := s.loginHolds(r, user); !ok {
				return
			}
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-sub.wake:
			// Events were lost: a new snapshot instead of a gap.
			snap, running := s.agentResync(run, sub)
			if !running {
				// Ended meanwhile: what is left in the channel, then the end.
				continue
			}
			snapshot(snap)
			flusher.Flush()
		case e, open := <-sub.ch:
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
// It reports whether the agent's volume was there to look; the scheduler
// tries again until it was (PLAN 59: an encrypted volume unlocked later).
func (s *Server) AgentInterrupted(ctx context.Context) bool {
	store := s.agentStore()
	if store.Available() != nil {
		return false
	}
	for _, user := range store.Accounts() {
		list, err := store.Chats(user)
		if err != nil {
			continue
		}
		for _, info := range list {
			if info.State != agent.StateRunning || s.agentRunning(user, info.ID) {
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
	return true
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

// agentProbe finds the context the model runs with right now (/api/ps),
// for when "as set in Ollama" is chosen (PLAN 55: summarise in time). A
// model that is not loaded is loaded first (the request would load it
// anyway), so the first round of a task knows the window too (PLAN 59a).
// If Ollama still reports nothing, a careful guess (agentGuessContext).
const agentGuessContext = 8192

func agentProbe(client *agent.Ollama, model, keepAlive string) func(context.Context) int {
	same := func(name string) bool {
		return name == model || strings.TrimSuffix(name, ":latest") == strings.TrimSuffix(model, ":latest")
	}
	running := func(ctx context.Context) int {
		list, err := client.Running(ctx)
		if err != nil {
			return 0
		}
		for _, m := range list {
			if same(m.Name) {
				return m.ContextLength
			}
		}
		return 0
	}
	return func(ctx context.Context) int {
		// An outside provider loads nothing: the length is what it names
		// for the model, or the careful guess where it names none.
		if client.Provider() != "" {
			sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if info, err := client.Show(sctx, model); err == nil && info.ContextLength > 0 {
				return info.ContextLength
			}
			return agentGuessContext
		}
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		n := running(pctx)
		cancel()
		if n > 0 {
			return n
		}
		lctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		if err := client.Preload(lctx, model, keepAlive, 0); err == nil {
			if n = running(lctx); n > 0 {
				return n
			}
		}
		// Not loadable now: the model's trained length is only an upper
		// bound; a careful guess summarises too early rather than too late.
		if info, err := client.Show(lctx, model); err == nil && info.ContextLength > 0 {
			return min(info.ContextLength, agentGuessContext)
		}
		return 0
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return data
}

// autonomyOf reads the way to work a request names: "direct" (or ""
// stored), "plan" or "ask"; anything else keeps what the chat has.
func autonomyOf(asked, kept string) string {
	switch asked {
	case "direct":
		return agent.AutonomyDirect
	case agent.AutonomyPlan, agent.AutonomyAsk:
		return asked
	}
	return kept
}
