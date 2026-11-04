package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// Recurring tasks (PLAN 44, Runde 3), triggers from Home Assistant and the
// agent's own initiative on notifications of the NAS. All of them start a
// chat of their own, with the rights of the account they belong to.

type agentReactState struct {
	mu sync.Mutex
	// last run per kind of notification, for the hourly limit.
	last map[string]time.Time
	// marks are the last seen Home Assistant transition per schedule.
	marks map[string]string
}

// agentDue reports whether a schedule should run at now, given its last run.
// It runs once in the ten minutes after its time, so a minute the ticker
// missed is no lost run.
func agentDue(sc agent.Schedule, now time.Time) bool {
	if !sc.Enabled || sc.Trigger != nil {
		return false
	}
	last := sc.LastRun
	if sc.Every == "hourly" {
		slot := now.Truncate(time.Hour)
		return now.Sub(slot) < 10*time.Minute && last.Before(slot)
	}
	hh, mm := 0, 0
	if _, err := fmt.Sscanf(sc.At, "%d:%d", &hh, &mm); err != nil {
		return false
	}
	slot := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
	switch sc.Every {
	case "weekly":
		if int(now.Weekday()) != sc.Weekday {
			return false
		}
	case "monthly":
		if now.Day() != sc.Day {
			return false
		}
	case "daily":
	default:
		return false
	}
	return !now.Before(slot) && now.Sub(slot) < 10*time.Minute && last.Before(slot)
}

// AgentScheduler runs recurring tasks and triggers until ctx ends.
func (s *Server) AgentScheduler(ctx context.Context) {
	s.AgentInterrupted(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.agentTick(now)
		}
	}
}

func (s *Server) agentTick(now time.Time) {
	settings := s.agentSettings()
	if !settings.Enabled || s.opts.Users == nil {
		return
	}
	store := s.agentStore()
	var watched []string
	for _, name := range store.Accounts() {
		user, ok := s.opts.Users.Get(name)
		if !ok || !s.agentAllowed(user) {
			continue
		}
		list, err := store.Schedules(name)
		if err != nil {
			continue
		}
		for _, sc := range list {
			if sc.Enabled && sc.Trigger != nil {
				watched = append(watched, sc.Trigger.Entity)
				if s.agentTriggered(name, sc, now) {
					s.agentStartSchedule(user, sc.ID, now)
				}
				continue
			}
			if agentDue(sc, now.In(s.agentLocation())) {
				s.agentStartSchedule(user, sc.ID, now)
			}
		}
	}
	if s.opts.HomeAssistant != nil {
		s.opts.HomeAssistant.SetWatched(watched)
	}
}

func (s *Server) agentStartSchedule(user auth.User, id string, now time.Time) {
	if _, err := s.agentRunSchedule(user, id, now); err != nil {
		slog.Warn("recurring agent task not started", "account", user.Name, "schedule", id, "err", err)
	}
}

func (s *Server) agentLocation() *time.Location {
	return time.Local
}

// agentTriggered reports whether a Home Assistant entity reached the state
// of a trigger since the last look (and kept it long enough).
func (s *Server) agentTriggered(user string, sc agent.Schedule, now time.Time) bool {
	ha := s.opts.HomeAssistant
	if ha == nil {
		return false
	}
	key := user + "/" + sc.ID
	r := &s.agentReact
	r.mu.Lock()
	if r.marks == nil {
		r.marks = map[string]string{}
	}
	mark, seen := r.marks[key]
	r.mu.Unlock()
	transitions, newest := ha.Transitions(sc.Trigger.Entity, mark)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !seen {
		// First look after a start: only what happens from now on counts.
		r.marks[key] = newest
		return false
	}
	for _, t := range transitions {
		if !strings.EqualFold(t.To, sc.Trigger.State) {
			continue
		}
		if sc.Trigger.For > 0 && now.Sub(t.Time) < time.Duration(sc.Trigger.For)*time.Minute {
			// Not long enough yet: look again next minute.
			return false
		}
		r.marks[key] = newest
		return true
	}
	if newest != "" {
		r.marks[key] = newest
	}
	return false
}

// agentRunSchedule starts a recurring task in a chat of its own.
func (s *Server) agentRunSchedule(user auth.User, id string, now time.Time) (string, error) {
	store := s.agentStore()
	list, err := store.Schedules(user.Name)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(list, func(sc agent.Schedule) bool { return sc.ID == id })
	if i < 0 {
		return "", agent.ErrNotFound
	}
	sc := list[i]
	origin := "schedule"
	chat := &agent.Chat{ID: agent.NewID(), Title: sc.Name + " · " + now.In(s.agentLocation()).Format("02.01. 15:04"), Project: sc.Project, Created: now, Updated: now, Origin: origin, State: agent.StateIdle,
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: sc.Task}}}
	if err := store.SaveChat(user.Name, chat); err != nil {
		return "", err
	}
	list[i].LastRun, list[i].Chat = now, chat.ID
	if err := store.SaveSchedules(user.Name, list); err != nil {
		return "", err
	}
	err = s.agentStart(user, chat, func(rn *agent.Runner, ctx context.Context) error {
		err := rn.Run(ctx, chat)
		s.agentScheduleResult(user.Name, id, chat)
		return err
	})
	return chat.ID, err
}

func (s *Server) agentScheduleResult(user, id string, chat *agent.Chat) {
	store := s.agentStore()
	list, err := store.Schedules(user)
	if err != nil {
		return
	}
	for i := range list {
		if list[i].ID == id {
			list[i].LastOK = chat.State == agent.StateIdle
		}
	}
	if err := store.SaveSchedules(user, list); err != nil {
		slog.Warn("result of a recurring task not saved", "schedule", id, "err", err)
	}
	s.agentHANotify(chat)
}

// agentObserve is told about warnings and errors of the NAS. With the
// agent's initiative switched on, it looks into them as the chosen account,
// at most once an hour per kind of notification.
func (s *Server) agentObserve(m notify.Message) {
	settings := s.agentSettings()
	if !settings.Enabled || !settings.React || s.opts.Users == nil {
		return
	}
	as := settings.ReactAs
	if as == "" {
		as = s.firstOwner()
	}
	user, ok := s.opts.Users.Get(as)
	if !ok || user.Disabled || !s.agentAllowed(user) {
		return
	}
	r := &s.agentReact
	r.mu.Lock()
	if r.last == nil {
		r.last = map[string]time.Time{}
	}
	if t, ok := r.last[m.Kind]; ok && time.Since(t) < time.Hour {
		r.mu.Unlock()
		return
	}
	r.last[m.Kind] = time.Now()
	r.mu.Unlock()
	now := time.Now()
	task := fmt.Sprintf("Das NAS hat gemeldet (%s, %s): %s\n%s\n\nFinde heraus, was los ist. Behebe es, soweit deine Rechte es erlauben, und berichte kurz, was du gefunden und getan hast.", m.Severity, m.Kind, m.Title, m.Text)
	chat := &agent.Chat{ID: agent.NewID(), Title: "Von selbst: " + m.Title, Created: now, Updated: now, Origin: "event", State: agent.StateIdle,
		// Notification texts can carry names others chose (USB labels,
		// containers): web tools ask from the start.
		Tainted: true,
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: task}}}
	if err := s.agentStore().SaveChat(user.Name, chat); err != nil {
		slog.Warn("agent cannot look into a notification", "err", err)
		return
	}
	if err := s.agentStart(user, chat, func(rn *agent.Runner, ctx context.Context) error {
		err := rn.Run(ctx, chat)
		s.agentHANotify(chat)
		return err
	}); err != nil && !errors.Is(err, agent.ErrBusy) {
		slog.Warn("agent cannot look into a notification", "err", err)
	}
}

func (s *Server) firstOwner() string {
	for _, u := range s.opts.Users.List() {
		if u.Role == auth.RoleOwner && !u.Disabled {
			return u.Name
		}
	}
	return ""
}

// agentHANotify sends the end of an unattended run to the notify services
// of Home Assistant the owner chose.
func (s *Server) agentHANotify(chat *agent.Chat) {
	settings := s.agentSettings()
	ha := s.opts.HomeAssistant
	if len(settings.HA.Notify) == 0 || ha == nil || !ha.Active() {
		return
	}
	text := "Fertig."
	switch chat.State {
	case agent.StateWaiting:
		text = "Wartet auf deine Freigabe in TitanOS."
	case agent.StateError:
		text = "Fehler: " + chat.Error
	default:
		for i := len(chat.Entries) - 1; i >= 0; i-- {
			if e := chat.Entries[i]; e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
				text = e.Content
				break
			}
		}
	}
	if r := []rune(text); len(r) > 500 {
		text = string(r[:499]) + "…"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, service := range settings.HA.Notify {
		if err := ha.Notify(ctx, service, "KI-Agent: "+chat.Title, text, nil); err != nil {
			slog.Warn("agent message to Home Assistant not sent", "service", service, "err", err)
		}
	}
}
