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
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
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
	// mapTried is when the nightly NAS map was last started; reportSent
	// when the weekly report went out.
	mapTried   time.Time
	reportSent time.Time
}

// AgentScheduler runs recurring tasks and triggers until ctx ends.
func (s *Server) AgentScheduler(ctx context.Context) {
	marked := s.AgentInterrupted(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if !marked {
				marked = s.AgentInterrupted(ctx)
			}
			s.agentTick(now)
		}
	}
}

func (s *Server) agentTick(now time.Time) {
	settings := s.agentSettings()
	// Protection (ransomware guard, ends of emergency access) works also
	// while the agent itself is off (PLAN 54-S); each part checks its switch.
	if s.opts.Users != nil {
		s.agentProtectTick(now.In(s.agentLocation()))
	}
	if !settings.Enabled || s.opts.Users == nil {
		return
	}
	s.nightlyNASMap(now)
	s.agentSyncShare()
	s.agentWeeklyReport(now.In(s.agentLocation()))
	s.agentForecastTick(now.In(s.agentLocation()))
	s.agentReflectTick(now.In(s.agentLocation()))
	s.agentFeaturesTick(now.In(s.agentLocation()))
	store := s.agentStore()
	// The entities of triggers, per connection.
	watched := map[string][]string{}
	for _, name := range store.ScheduleAccounts() {
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
				conn := sc.Trigger.Connection
				if conn == "" {
					conn = homeassistant.PrimaryID
				}
				watched[conn] = append(watched[conn], sc.Trigger.Entity)
				if s.agentTriggered(name, sc, now) {
					s.agentStartSchedule(user, sc.ID, now, false)
				}
				continue
			}
			if due, late := agentDueAt(sc, now.In(s.agentLocation())); due {
				s.agentStartSchedule(user, sc.ID, now, late)
			}
		}
	}
	if hub := s.haHub(); hub != nil {
		for _, ha := range hub.All() {
			ha.SetWatched(watched[ha.ID()])
		}
	}
}

func (s *Server) agentStartSchedule(user auth.User, id string, now time.Time, late bool) {
	if _, err := s.agentRunSchedule(user, id, now, late); err != nil {
		slog.Warn("recurring agent task not started", "account", user.Name, "schedule", id, "err", err)
	}
}

func (s *Server) agentLocation() *time.Location {
	return time.Local
}

// agentTriggered reports whether a Home Assistant entity reached the state
// of a trigger since the last look (and kept it long enough).
func (s *Server) agentTriggered(user string, sc agent.Schedule, now time.Time) bool {
	ha := s.haConnection(sc.Trigger.Connection)
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

// agentRunSchedule starts a planned task in a chat of its own. A reminder
// only becomes a chat entry and a push message; late marks a missed run
// that is caught up after a start of the NAS.
func (s *Server) agentRunSchedule(user auth.User, id string, now time.Time, late bool) (string, error) {
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
	title := sc.Name + " · " + now.In(s.agentLocation()).Format("02.01. 15:04")
	if late {
		title += " (nachgeholt)"
	}
	task := sc.Task
	if sc.Kind == "morning" {
		task = agentMorningTask(sc.Report) + nightNote(user.Name)
	}
	chat := &agent.Chat{ID: agent.NewID(), Title: title, Project: sc.Project, Created: now, Updated: now, Origin: origin, Schedule: sc.ID, State: agent.StateIdle,
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: task}}}
	if sc.Kind == "reminder" {
		chat.Origin = "reminder"
		chat.Entries = []agent.Entry{{ID: agent.NewID(), At: now, Role: "assistant", Content: "⏰ **Erinnerung:** " + sc.Task}}
	}
	if err := store.SaveChat(user.Name, chat); err != nil {
		return "", err
	}
	list[i].LastRun, list[i].Chat = now, chat.ID
	if sc.Kind == "reminder" {
		list[i].LastOK = true
	}
	if err := store.SaveSchedules(user.Name, list); err != nil {
		return "", err
	}
	if sc.Kind == "reminder" {
		s.agentRemind(user, chat, sc)
		return chat.ID, nil
	}
	err = s.agentStart(user, chat, func(rn *agent.Runner, ctx context.Context) error {
		err := rn.Run(ctx, chat)
		s.agentScheduleResult(user.Name, id, chat)
		return err
	})
	return chat.ID, err
}

// agentRemind sends a reminder to the person's devices and, if set, to the
// notify services of Home Assistant. A reminder always goes out, also when
// a page of the agent is open: it is what the person asked for.
func (s *Server) agentRemind(user auth.User, chat *agent.Chat, sc agent.Schedule) {
	if s.opts.WebPush != nil {
		body := sc.Task
		if r := []rune(body); len(r) > 180 {
			body = string(r[:179]) + "…"
		}
		s.opts.WebPush.SendTo(user.Name, webpush.Message{Title: "⏰ " + sc.Name, Body: body, URL: "/agent?chat=" + chat.ID, Tag: "agent-" + chat.ID})
	}
	s.agentHANotify(chat)
}

// agentMorningTask is the task of the morning report from what the person
// chose (PLAN 49).
func agentMorningTask(r *agent.Report) string {
	if r == nil {
		r = &agent.Report{NAS: true, Backups: true, Today: true}
	}
	parts := []string{}
	if r.NAS {
		parts = append(parts, "den Zustand des NAS (Volumes, Platten mit SMART und Temperatur, freier Platz, Auffälliges der Nacht)")
	}
	if r.Backups {
		parts = append(parts, "ob die Sicherungen gelaufen sind und welche Updates es gibt (TitanOS, Apps, Container)")
	}
	if r.Today {
		parts = append(parts, "meine Erinnerungen und geplanten Aufträge für heute")
	}
	if r.Home {
		parts = append(parts, "das Wichtigste aus Home Assistant (z. B. Strom von gestern, offene Fenster, schwache Batterien)")
	}
	text := "Morgenbericht: Gib mir einen kurzen Überblick für heute"
	if len(parts) > 0 {
		text += " über " + strings.Join(parts, "; ")
	}
	text += ". Kurz und übersichtlich, mit dem Wichtigsten zuerst; ändere nichts."
	if extra := strings.TrimSpace(r.Extra); extra != "" {
		text += "\nAußerdem: " + extra
	}
	return text
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
	s.agentInitiativeObserve(m)
	settings := s.agentSettings()
	if m.Severity != notify.Warning && m.Severity != notify.Error {
		return
	}
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
	s.agentObserveAs(user, m)
}

// agentObserveAs looks into a message as the account, at most once an hour
// for each kind of message.
func (s *Server) agentObserveAs(user auth.User, m notify.Message) {
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
	if len(settings.HA.Notify) == 0 || s.haHub() == nil {
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
	for _, entry := range settings.HA.Notify {
		// "notify.x" goes to the first connection, "<id>/notify.x" to another.
		conn, service, found := strings.Cut(entry, "/")
		if !found {
			conn, service = "", entry
		}
		ha := s.haConnection(conn)
		if ha == nil || !ha.Active() {
			continue
		}
		if err := ha.Notify(ctx, service, "KI-Agent: "+chat.Title, text, nil); err != nil {
			slog.Warn("agent message to Home Assistant not sent", "service", service, "err", err)
		}
	}
}

// agentButtons are the recurring tasks that have a button in Home Assistant.
func (s *Server) agentButtons() []homeassistant.AgentTaskValues {
	settings := s.agentSettings()
	if !settings.Enabled || s.opts.Users == nil {
		return nil
	}
	store := s.agentStore()
	var out []homeassistant.AgentTaskValues
	for _, name := range store.ScheduleAccounts() {
		user, ok := s.opts.Users.Get(name)
		if !ok || !s.agentAllowed(user) {
			continue
		}
		list, err := store.Schedules(name)
		if err != nil {
			continue
		}
		for _, sc := range list {
			if sc.Enabled && sc.Button {
				out = append(out, homeassistant.AgentTaskValues{Key: name + "/" + sc.ID, Name: sc.Name})
			}
		}
	}
	return out
}

// agentButton runs the task behind a button pressed in Home Assistant.
func (s *Server) agentButton(key, action string) error {
	name, id, ok := strings.Cut(key, "/")
	if !ok || action != "run" {
		return fmt.Errorf("unknown agent command")
	}
	user, found := s.opts.Users.Get(name)
	if !found || !s.agentAllowed(user) {
		return fmt.Errorf("account may not use the agent")
	}
	list, err := s.agentStore().Schedules(name)
	if err != nil {
		return err
	}
	for _, sc := range list {
		if sc.ID == id && sc.Enabled && sc.Button {
			_, err := s.agentRunSchedule(user, id, time.Now(), false)
			return err
		}
	}
	return fmt.Errorf("no such task with a button")
}
