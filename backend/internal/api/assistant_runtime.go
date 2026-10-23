package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

type agentIdentity struct{}
type agentEvent struct {
	kind string
	data any
}
type agentJob struct {
	priority int
	queuedAt time.Time
	eligible bool
	started  bool
	cancel   context.CancelFunc
	done     chan struct{}
	events   chan agentEvent
	mu       sync.Mutex
	control  string
	steering []string
}

func (j *agentJob) emit(kind string, data any) {
	// Deep copy before the working chat changes again. Slow/disconnected browsers
	// may miss streaming deltas, but always recover the authoritative saved chat.
	b, _ := json.Marshal(data)
	select {
	case j.events <- agentEvent{kind, json.RawMessage(b)}:
	default:
	}
}

var errAgentConfirmation = errors.New("agent awaiting confirmation")
var errAgentPaused = errors.New("agent paused")
var errAgentWaiting = assistant.ErrWaiting

func (s *Server) startAgent(user auth.User, chat *assistant.Chat, model, page string, release func()) *agentJob {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), agentIdentity{}, user.Name))
	j := &agentJob{priority: chat.Priority, queuedAt: time.Now(), cancel: cancel, done: make(chan struct{}), events: make(chan agentEvent, 256)}
	key := user.Name + "/" + chat.ID
	s.agentMu.Lock()
	s.agentJobs[key] = j
	s.agentMu.Unlock()
	base, persistent := s.chatsDir()
	policy := s.opts.Assistant.Settings()
	execution := policy.Execution
	if chat.Automation != "" && chat.Execution != "" && chat.Execution != "inherit" {
		execution = chat.Execution
	}
	go func() {
		defer cancel()
		defer close(j.done)
		defer close(j.events)
		defer func() { s.agentMu.Lock(); delete(s.agentJobs, key); s.agentMu.Unlock(); release() }()
		if chat.Run == nil {
			chat.Run = &assistant.Run{Started: time.Now()}
		}
		chat.Run.State = "queued"
		chat.Run.Detail = ""
		save := func(c *assistant.Chat) error {
			now, ok := s.chatsDir()
			if persistent && (!ok || now != base) {
				return fmt.Errorf("chat volume unavailable or changed; no fallback storage")
			}
			return s.chats.Save(user.Name, c)
		}
		j.emit("progress", chat.Run)
		if err := save(chat); err != nil {
			j.emit("problem", map[string]string{"code": "internal", "detail": err.Error()})
			return
		}
		acquired := false
		for ctx.Err() == nil && !acquired {
			ready, detail := s.dependenciesReady(user.Name, chat)
			j.mu.Lock()
			j.eligible = ready
			j.mu.Unlock()
			if chat.Run.Detail != detail {
				chat.Run.Detail = detail
				if err := save(chat); err != nil {
					return
				}
			}
			acquired = s.claimAgent(key, j)
			if !acquired {
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}
		}
		if !acquired {
			j.mu.Lock()
			control := j.control
			j.mu.Unlock()
			chat.Run.State = "paused"
			if control == "restart" || control == "requeue" {
				chat.Run.State = "queued"
			} else if control == "stop" {
				chat.Run.State = "stopped"
				assistant.Expire(chat)
			}
			_ = save(chat)
			return
		}
		defer func() { <-s.agentSlots }()
		request, _ := http.NewRequestWithContext(ctx, "GET", "/api/v1/assistant", nil)
		before := func(ctx context.Context, c *assistant.Chat) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, ok := s.opts.Users.Get(user.Name)
			if !ok || current.Role != user.Role || !s.assistantAllowed(current) {
				return assistant.ErrOff
			}
			currentPolicy := s.opts.Assistant.Settings()
			if currentPolicy.Execution != policy.Execution || !reflect.DeepEqual(currentPolicy.Areas, policy.Areas) || currentPolicy.Users != policy.Users || currentPolicy.Web != policy.Web || currentPolicy.WebUsers != policy.WebUsers {
				return fmt.Errorf("permissions changed; resume to use the new policy")
			}
			if currentPolicy.Paused {
				return errAgentPaused
			}
			if persistent {
				now, ok := s.chatsDir()
				if !ok || now != base {
					return fmt.Errorf("chat volume unavailable; pause until it returns")
				}
			}
			j.mu.Lock()
			steering := j.steering
			j.steering = nil
			j.mu.Unlock()
			for _, text := range steering {
				c.Messages = append(c.Messages, assistant.Entry{Role: "user", Content: text, Time: time.Now(), Page: page})
				c.Updated = time.Now()
			}
			if len(steering) > 0 {
				if err := save(c); err != nil {
					return err
				}
			}
			return nil
		}
		execute := func(ctx context.Context, c *assistant.Chat, a *assistant.Action) error {
			if err := before(ctx, c); err != nil {
				return err
			}
			return s.executeAgentAction(ctx, user, c, a, request, save)
		}
		chat.Run.State = "running"
		_ = save(chat)
		j.emit("progress", chat.Run)
		err := before(ctx, chat)
		if err == nil {
			for i := range chat.Messages {
				e := &chat.Messages[i]
				if e.Action != nil && (e.Action.State == assistant.ActionRunning || e.Action.State == assistant.ActionUncertain) {
					if s.restoreAgentReceipt(user.Name, chat.ID, e) {
						continue
					}
					err = fmt.Errorf("action %s has an uncertain outcome; inspect the target before continuing, do not repeat it", e.Action.ID)
					break
				}
			}
		}
		if err == nil {
			if a := assistant.Pending(chat); a != nil {
				a.Auto = execution == "direct" || a.Approved || (chat.Task != nil && chat.Task.Approved && a.Level != assistant.LevelDestroy && a.Key != "task")
				if a.Auto {
					err = execute(ctx, chat, a)
				} else {
					err = errAgentConfirmation
				}
			}
		}
		if err == nil {
			err = s.opts.Assistant.Answer(ctx, chat, assistant.Turn{
				Execution: execution,
				Progress: func(stage, tool string) {
					chat.Run.Stage, chat.Run.Tool, chat.Run.Since = stage, tool, time.Now()
					j.emit("progress", chat.Run)
					if err := save(chat); err != nil {
						cancel()
					}
				},
				Person: assistant.Person{Name: user.Name, Role: string(user.Role), Knowledge: s.personalKnowledge(user)}, Model: model, Page: page, Web: s.assistantWeb(user),
				Tools: s.agentTools(user, request, chat, save), Save: save, Before: before, Execute: execute,
				Extra: fmt.Sprintf("TitanOS version %s. Job %s. Dry run: %v. Background execution; browser availability is irrelevant. Original user instructions remain binding.", s.opts.Version, chat.ID, chat.DryRun),
				Emit: func(e assistant.Event) {
					if e.Entry != nil {
						j.emit(e.Kind, map[string]any{"index": e.Index, "entry": e.Entry})
					} else {
						j.emit(e.Kind, map[string]string{"text": e.Text})
					}
				},
			})
		}
		j.mu.Lock()
		control := j.control
		j.mu.Unlock()
		switch {
		case control == "restart":
			chat.Run.State = "queued"
		case control == "stop":
			chat.Run.State = "stopped"
			chat.Task = nil
			assistant.Expire(chat)
		case control == "pause" || errors.Is(err, context.Canceled) || errors.Is(err, errAgentPaused):
			chat.Run.State = "paused"
		case errors.Is(err, errAgentWaiting):
			chat.Run.State = "waiting"
		case errors.Is(err, errAgentConfirmation):
			chat.Run.State = "confirmation"
		case err != nil:
			chat.Run.State = "blocked"
			chat.Run.Detail = diagnose.Secrets(err.Error())
		case assistant.Pending(chat) != nil:
			chat.Run.State = "confirmation"
		default:
			chat.Run.State = "done"
		}
		chat.Updated = time.Now()
		if err := save(chat); err != nil {
			slog.Error("agent checkpoint failed", "chat", chat.ID, "err", err)
			j.emit("problem", map[string]string{"code": "internal", "detail": err.Error()})
		}
		if chat.Run.State == "blocked" {
			code := "agent_blocked"
			if errors.Is(err, assistant.ErrUpstream) {
				code = "ollama_failed"
				if errors.Is(err, assistant.ErrModelOutput) {
					code = "model_tool_parse"
				}
			}
			if errors.Is(err, assistant.ErrNoModel) {
				code = "assistant_no_model"
			}
			j.emit("problem", map[string]string{"code": code, "detail": chat.Run.Detail})
			chat.Messages = append(chat.Messages, assistant.Entry{Role: "assistant", Problem: &assistant.Problem{Code: code, Detail: chat.Run.Detail}, Time: time.Now()})
			_ = save(chat)
		}
		// The existing notification channel is administrator-visible; never include
		// a user's private chat title, filenames or content in it.
		if (chat.Automation == "" || chat.Notify == "always" || chat.Notify != "never" && chat.Run.State == "blocked") && user.Role.CanAdminister() && s.opts.Notify != nil && (chat.Run.State == "done" || chat.Run.State == "blocked") {
			severity := "info"
			if chat.Run.State == "blocked" {
				severity = "warning"
			}
			s.opts.Notify.Notify(severity, "assistant_"+chat.Run.State, user.Name)
		}
		s.notifyOwner(user, chat)
		j.emit("snapshot", map[string]any{"chat": chat})
		j.emit("done", map[string]string{})
	}()
	return j
}

// executeAgentAction re-plans from stored arguments with current rights before
// dispatching the exact existing API route. The browser never supplies results.
func (s *Server) executeAgentAction(ctx context.Context, user auth.User, c *assistant.Chat, a *assistant.Action, r *http.Request, save func(*assistant.Chat) error) error {
	started := time.Now()
	if c.DryRun {
		return fmt.Errorf("dry run cannot execute actions")
	}
	var entry *assistant.Entry
	for i := range c.Messages {
		if c.Messages[i].Action != nil && c.Messages[i].Action.ID == a.ID {
			entry = &c.Messages[i]
			break
		}
	}
	if entry == nil || a.State != assistant.ActionPending {
		return assistant.ErrNoAction
	}
	var fresh *assistant.Action
	for _, tool := range s.agentTools(user, r, c, save) {
		if tool.Name == a.Tool && tool.Plan != nil {
			v, err := tool.Plan(ctx, entry.Args)
			if err != nil {
				var invalid *assistant.ToolError
				if errors.As(err, &invalid) {
					a.State = assistant.ActionExpired
					a.Result = "Proposal no longer valid before dispatch; nothing executed. Inspect current state: " + err.Error()
					entry.OK = false
					entry.ErrorCode = "action_changed"
					return save(c)
				}
				return err
			}
			fresh = v
			break
		}
	}
	if fresh == nil {
		return fmt.Errorf("action is no longer allowed")
	}
	oldBody, _ := json.Marshal(a.Body)
	newBody, _ := json.Marshal(fresh.Body)
	if fresh.Method != a.Method || fresh.Path != a.Path || fresh.Level != a.Level || fresh.Confirm != a.Confirm || fresh.ConfirmField != a.ConfirmField || !sameJSON(oldBody, newBody) {
		// Nothing was dispatched: expire only this proposal so the model can
		// inspect and replan, without clearing receipts or mutation attempts.
		a.State = assistant.ActionExpired
		a.Result = "Target or action changed before dispatch. Nothing executed. Inspect current state and make a new plan."
		entry.OK = false
		entry.ErrorCode = "action_changed"
		return save(c)
	}
	if a.Key == "task" {
		return assistant.Resolve(c, a.ID, assistant.ActionDone, "Plan accepted under the configured execution policy.")
	}
	a.State = assistant.ActionRunning
	if err := save(c); err != nil {
		a.State = assistant.ActionPending
		return err
	}
	// A second durable receipt records dispatch independently of model history.
	// Unknown outcomes are NEVER automatically retried after a crash.
	receipt := struct {
		ID, User, Chat, Tool, State, Result string
		Time                                time.Time
	}{a.ID, user.Name, c.ID, a.Tool, "running", "", time.Now()}
	journal := func() error {
		base, ok := s.chatsDir()
		if !ok {
			return nil
		}
		dir := filepath.Join(base, user.Name, "receipts", c.ID)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		data, _ := json.Marshal(receipt)
		f, err := os.CreateTemp(dir, ".receipt-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(data); err != nil {
			f.Close()
			return err
		}
		if err = f.Sync(); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		if err := os.Rename(f.Name(), filepath.Join(dir, a.ID+".json")); err != nil {
			return err
		}
		d, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
	if err := journal(); err != nil {
		return err
	}
	raw := a.Path
	body := map[string]any{}
	if len(oldBody) > 0 && string(oldBody) != "null" {
		decoder := json.NewDecoder(bytes.NewReader(oldBody))
		decoder.UseNumber()
		_ = decoder.Decode(&body)
	}
	if a.ConfirmField != "" {
		where, field, _ := strings.Cut(a.ConfirmField, ":")
		if where == "query" {
			u, err := url.Parse(raw)
			if err != nil {
				return err
			}
			q := u.Query()
			q.Set(field, a.Confirm)
			u.RawQuery = q.Encode()
			raw = u.String()
		} else {
			body[field] = a.Confirm
		}
	}
	data, _ := json.Marshal(body)
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, a.Method, raw, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	result := diagnose.Secrets(rec.Body.String())
	if a.Tool == "docker_check_updates" && rec.Code >= 200 && rec.Code < 300 {
		v, _ := s.toolDocker(ctx)
		b, _ := json.Marshal(v)
		result = string(b)
	}
	if rec.Code >= 200 && rec.Code < 300 {
		a.State = assistant.ActionDone
		entry.OK = true
	} else {
		a.State = assistant.ActionFailed
		entry.OK = false
	}
	a.Result = result
	if a.State == assistant.ActionDone {
		if c.Run != nil {
			c.Run.Stage, c.Run.Tool, c.Run.Since = "verify", a.Tool, time.Now()
		}
		_ = save(c)
		verification := s.verifyAgentAction(ctx, user, a, r)
		if verification != nil {
			out, _ := json.Marshal(map[string]any{"requestStatus": "succeeded", "response": json.RawMessage(validJSONResult(result)), "verification": verification})
			a.Result = diagnose.Secrets(string(out))
		}
	}
	// A canceled mutation can have reached its target. Keep its receipt uncertain.
	if ctx.Err() != nil && a.State != assistant.ActionDone {
		a.State = assistant.ActionUncertain
		entry.OK = false
	}
	entry.DurationMS = time.Since(started).Milliseconds()
	receipt.State, receipt.Result = a.State, diagnose.Secrets(a.Result)
	if err := journal(); err != nil {
		return err
	}
	slog.Info("assistant action", "id", a.ID, "chat", c.ID, "user", user.Name, "tool", a.Tool, "state", a.State)
	return save(c)
}

func (s *Server) handleAgentControl(w http.ResponseWriter, r *http.Request) {
	var req struct{ Action, Text string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	id := r.PathValue("id")
	key := user.Name + "/" + id
	s.agentMu.Lock()
	j := s.agentJobs[key]
	s.agentMu.Unlock()
	if req.Action == "steer" {
		if strings.TrimSpace(req.Text) == "" || len(req.Text) > assistant.MaxMessage {
			writeAssistantError(w, assistant.ErrInvalid)
			return
		}
		if j != nil {
			j.mu.Lock()
			j.control = "pause"
			j.mu.Unlock()
			j.cancel()
			select {
			case <-j.done:
			case <-r.Context().Done():
				return
			}
		}
		u, c, release, ok := s.reserved(w, r)
		if !ok {
			return
		}
		assistant.Expire(c) // Steering invalidates an unexecuted proposal before any dispatch.
		c.Messages = append(c.Messages, assistant.Entry{Role: "user", Content: req.Text, Time: time.Now(), Page: lastPage(c)})
		c.Updated = time.Now()
		if err := s.chats.Save(u.Name, c); err != nil {
			release()
			writeAssistantError(w, err)
			return
		}
		s.answer(w, r, u, c, c.Model, lastPage(c), release)
		return
	}
	if req.Action == "stop" || req.Action == "pause" {
		if j != nil {
			j.mu.Lock()
			j.control = req.Action
			j.mu.Unlock()
			j.cancel()
			select {
			case <-j.done:
			case <-r.Context().Done():
				return
			}
		}
		release, err := s.chats.Reserve(user.Name, id)
		if err != nil {
			writeAssistantError(w, err)
			return
		}
		defer release()
		c, err := s.chats.Get(user.Name, id)
		if err != nil {
			writeAssistantError(w, err)
			return
		}
		if c.Run == nil {
			c.Run = &assistant.Run{}
		}
		if req.Action == "stop" {
			c.Run.State = "stopped"
			assistant.Expire(c)
		} else {
			c.Run.State = "paused"
		}
		if err = s.chats.Save(user.Name, c); err != nil {
			writeAssistantError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Action != "resume" && req.Action != "reconcile" {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	user, c, release, ok := s.reserved(w, r)
	if !ok {
		return
	}
	if req.Action == "reconcile" {
		for i := range c.Messages {
			e := &c.Messages[i]
			if e.Action != nil && (e.Action.State == assistant.ActionRunning || e.Action.State == assistant.ActionUncertain) {
				e.Action.State = assistant.ActionFailed
				e.OK = false
				e.Action.Result = "Outcome unknown. User acknowledged interruption. Inspect target using read tools before deciding further steps; never assume success or automatically repeat this action."
			}
		}
		if err := s.chats.Save(user.Name, c); err != nil {
			release()
			writeAssistantError(w, err)
			return
		}
	}
	// An uncertain side effect requires inspection, not an automatic duplicate.
	s.answer(w, r, user, c, c.Model, lastPage(c), release)
}
func lastPage(c *assistant.Chat) string {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "user" {
			return c.Messages[i].Page
		}
	}
	return ""
}

// RunAssistant resumes checkpointed jobs after startup, and dependencies when
// they become due. It reads volume storage, never polls /config.
func (s *Server) RunAssistant(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	scan := func() {
		users := s.opts.Users.List()
		if s.opts.Notify != nil {
			names := make([]string, 0, len(users))
			for _, u := range users {
				names = append(names, u.Name)
			}
			s.opts.Notify.ResumePersonal(names)
		}
		settings := s.opts.Assistant.Settings()
		if !settings.Enabled || settings.Paused {
			s.watchHomeAssistant(nil)
			return
		}
		s.watchHomeAssistant(users)
		for _, user := range users {
			if !s.assistantAllowed(user) {
				continue
			}
			s.scanAutomations(ctx, user, time.Now())
			list, err := s.chats.List(user.Name, "")
			if err != nil {
				continue
			}
			for _, summary := range list {
				if settings.RetentionDays > 0 && time.Since(summary.Updated) > time.Duration(settings.RetentionDays)*24*time.Hour && (summary.Run == nil || summary.Run.State == "done" || summary.Run.State == "stopped") {
					_ = s.chats.Delete(user.Name, summary.ID)
					continue
				}
				if summary.Run == nil {
					continue
				}
				state := summary.Run.State
				if state != "running" && state != "queued" && (state != "waiting" || time.Now().Before(summary.Run.Wake)) {
					continue
				}
				release, err := s.chats.Reserve(user.Name, summary.ID)
				if err != nil {
					continue
				}
				c, err := s.chats.Get(user.Name, summary.ID)
				if err != nil {
					release()
					continue
				}
				s.startAgent(user, c, c.Model, lastPage(c), release)
			}
		}
	}
	scan()
	for {
		select {
		case <-ctx.Done():
			s.agentMu.Lock()
			jobs := []*agentJob{}
			for _, j := range s.agentJobs {
				jobs = append(jobs, j)
				j.mu.Lock()
				j.control = "restart"
				j.mu.Unlock()
				j.cancel()
			}
			s.agentMu.Unlock()
			timeout := time.NewTimer(8 * time.Second)
			defer timeout.Stop()
			for _, j := range jobs {
				select {
				case <-j.done:
				case <-timeout.C:
					return
				}
			}
			return
		case <-tick.C:
			scan()
		}
	}
}

// A completed receipt survives a crash between dispatch and the chat checkpoint.
func (s *Server) restoreAgentReceipt(user, chat string, e *assistant.Entry) bool {
	base, ok := s.chatsDir()
	if !ok {
		return false
	}
	data, err := os.ReadFile(filepath.Join(base, user, "receipts", chat, e.Action.ID+".json"))
	if err != nil {
		return false
	}
	var receipt struct{ ID, User, Chat, Tool, State, Result string }
	if json.Unmarshal(data, &receipt) != nil || receipt.ID != e.Action.ID || receipt.User != user || receipt.Chat != chat || receipt.Tool != e.Action.Tool {
		return false
	}
	if receipt.State != assistant.ActionDone && receipt.State != assistant.ActionFailed {
		return false
	}
	e.Action.State = receipt.State
	e.Action.Result = receipt.Result
	e.OK = receipt.State == assistant.ActionDone
	return true
}

// Compare JSON structurally, preserving integer precision across save/reload.
func sameJSON(a, b []byte) bool {
	decode := func(raw []byte) (any, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		err := d.Decode(&v)
		return v, err
	}
	av, ae := decode(a)
	bv, be := decode(b)
	return ae == nil && be == nil && reflect.DeepEqual(av, bv)
}

// notifyOwner tells the account that owns a task about it through its own
// channels. Unlike the shared channel this may carry the chat title and a
// short summary: nobody else reads it.
func (s *Server) notifyOwner(user auth.User, chat *assistant.Chat) {
	if s.opts.Notify == nil || chat.Run == nil {
		return
	}
	event := map[string]string{"confirmation": notify.EventConfirmation, "done": notify.EventDone, "blocked": notify.EventBlocked}[chat.Run.State]
	if event == "" {
		return
	}
	if chat.Automation != "" {
		// The automation decides; errors also covers a waiting approval,
		// without which the run cannot finish.
		switch chat.Notify {
		case "never":
			return
		case "always":
		default:
			if event == notify.EventDone {
				return
			}
		}
	}
	settings, _, err := s.opts.Notify.PersonalSettings(user.Name)
	if err != nil {
		return
	}
	key := fmt.Sprint(chat.ID, "/", event, "/", len(chat.Messages))
	text := ""
	severity := notify.Info
	switch event {
	case notify.EventConfirmation:
		if a := assistant.Pending(chat); a != nil {
			key = chat.ID + "/" + event + "/" + a.ID
			text = a.Reason
		}
	case notify.EventBlocked:
		severity = notify.Warning
		text = chat.Run.Detail
	}
	if text == "" {
		for i := len(chat.Messages) - 1; i >= 0; i-- {
			if e := chat.Messages[i]; e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
				text = e.Content
				break
			}
		}
	}
	title := chat.Title
	if title == "" {
		title = map[bool]string{true: "KI-Assistent", false: "AI assistant"}[settings.Language == "de"]
	}
	heading, _ := notify.Wording(settings.Language, "personal_"+event, title)
	message := notify.PersonalMessage{Event: event, Severity: severity, Title: heading, Text: settings.Summary(text), Path: "/assistant?chat=" + url.QueryEscape(chat.ID)}
	if event == notify.EventConfirmation {
		message.Actions = s.pushActions(user, chat, assistant.Pending(chat), settings.Origin, settings.Language)
	}
	if err := s.opts.Notify.SendPersonal(user.Name, key, message); err != nil && !errors.Is(err, notify.ErrUnreachable) {
		slog.Warn("personal notification", "user", user.Name, "err", err)
	}
}
