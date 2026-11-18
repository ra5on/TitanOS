package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// The reminders of events in TitanOS (1.13, docs/KALENDER.md "Hinweise zu
// Erinnerungen"): the alarms of the events (VALARM, minutes before the
// start, as the calendar lists them) are planned by titand itself, so that
// TitanOS shows a hint at their time while any page of it is open – also
// without Kalender. Only RAM: the plan covers the next reminderHorizon,
// is made again when a calendar of the account changes (Store.OnChanged),
// when the account's choice changes and every reminderReplan, and after a
// restart from the store (nothing is written to the stick or the volume).
//
// A reminder that is due becomes an entry of the account's list of the
// last hints (at most maxFiredReminders, for reminderKeep); /system names
// the newest ("remindersLatest", for every role) and the shell reads them
// from GET /api/v1/calendar/reminders and shows each once. The bell of
// owners and administrators is left alone on purpose: its messages go on
// to their personal channels (ntfy, Telegram, e-mail) – the iPhone shows
// the alarm itself, a second message there would be one too many. A push
// to the account's own devices only when the account asked for it
// ("reminderPush" of the preference kind "calendar", off by default); the
// hints in TitanOS can be switched off ("reminderHints": false).
const (
	reminderHorizon   = 48 * time.Hour
	reminderReplan    = 12 * time.Hour
	reminderGrace     = 10 * time.Minute
	reminderKeep      = 12 * time.Hour
	maxFiredReminders = 50
	// maxPlannedReminders bounds one account's plan.
	maxPlannedReminders = 2000
	// reminderRetry is how soon a plan that could not be read is tried again.
	reminderRetry = time.Minute
)

// firedReminder is one hint as the shell reads it.
type firedReminder struct {
	Seq        int       `json:"seq"`
	At         time.Time `json:"at"`
	Calendar   string    `json:"calendar"`
	ID         string    `json:"id"`
	Occurrence string    `json:"occurrence"`
	Title      string    `json:"title"`
	Location   string    `json:"location,omitempty"`
	Start      string    `json:"start"`
	AllDay     bool      `json:"allDay"`
	// Minutes before the start (negative: after it).
	Minutes int    `json:"minutes"`
	key     string // account, calendar, file, occurrence, minutes
}

type plannedReminder struct {
	at time.Time
	firedReminder
}

// reminderPrefs are the account's choices (preference kind "calendar").
type reminderPrefs struct {
	hints, push bool
}

// reminderDeps is what the planner reads: set once when titand starts
// (tests swap them whole while the loop runs).
type reminderDeps struct {
	store  *pimstore.Store
	users  func() []string
	prefs  func(user string) reminderPrefs
	hidden func(user string) []string
	push   func(user string, r firedReminder)
	now    func() time.Time
	// available: the calendars' volume is there (nil: ask the store).
	available func() bool
}

type calendarReminders struct {
	deps atomic.Pointer[reminderDeps]

	mu    sync.Mutex
	plans map[string][]plannedReminder
	fired map[string][]firedReminder
	sent  map[string]time.Time // key → when it was due (no hint twice)
	seq   int
	epoch int64
	dirty map[string]bool
	all   bool
	wake  chan struct{}
}

func newCalendarReminders() *calendarReminders {
	c := &calendarReminders{
		plans: map[string][]plannedReminder{},
		fired: map[string][]firedReminder{},
		sent:  map[string]time.Time{},
		dirty: map[string]bool{},
		wake:  make(chan struct{}, 1),
		epoch: time.Now().Unix(),
	}
	c.deps.Store(&reminderDeps{now: time.Now})
	return c
}

func (c *calendarReminders) now() time.Time { return c.deps.Load().now() }

// changed is told about every change of a collection (Store.OnChanged).
func (c *calendarReminders) changed(ch pimstore.Changed) {
	if ch.Kind != pimstore.Calendar {
		return
	}
	c.replan(ch.Account)
}

// replan makes the plan of an account again soon ("" every account).
func (c *calendarReminders) replan(user string) {
	c.mu.Lock()
	if user == "" {
		c.all = true
	} else {
		c.dirty[user] = true
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// run plans and fires until ctx ends. The first plan comes a moment after
// the start; one that cannot be read (the volume not mounted yet) is
// tried again every minute.
func (c *calendarReminders) run(ctx context.Context) {
	c.replan("")
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	nextAll := c.now().Add(reminderReplan)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-c.wake:
			// Changes come in bursts (an iPhone uploads many events).
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		if !c.now().Before(nextAll) {
			c.replan("")
			nextAll = c.now().Add(reminderReplan)
		}
		failed := c.planDirty()
		c.fire()
		wait := time.Until(nextAll)
		// The volume not there yet (a passphrase awaited): asked again each minute.
		if failed && wait > reminderRetry {
			wait = reminderRetry
		}
		if next, ok := c.nextAt(); ok && next.Sub(c.now()) < wait {
			wait = max(next.Sub(c.now()), time.Second)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
	}
}

// planDirty plans the accounts marked. One whose plan could not be read
// (the volume is not there yet) stays marked; the result says so.
func (c *calendarReminders) planDirty() (failed bool) {
	c.mu.Lock()
	var users []string
	if c.all {
		c.all = false
		c.dirty = map[string]bool{}
		if d := c.deps.Load(); d.users != nil {
			users = d.users()
		}
	} else {
		for u := range c.dirty {
			users = append(users, u)
		}
		c.dirty = map[string]bool{}
	}
	c.mu.Unlock()
	for _, u := range users {
		plan, err := c.plan(u)
		if err != nil {
			// The volume is away: kept as it was, tried again soon.
			slog.Debug("calendar reminders not planned", "account", u, "err", err)
			c.mu.Lock()
			c.dirty[u] = true
			c.mu.Unlock()
			failed = true
			continue
		}
		c.mu.Lock()
		if len(plan) == 0 {
			delete(c.plans, u)
		} else {
			c.plans[u] = plan
		}
		c.mu.Unlock()
	}
	return failed
}

// plan reads the reminders of an account due in [now − grace, now + horizon).
func (c *calendarReminders) plan(user string) ([]plannedReminder, error) {
	d := c.deps.Load()
	store := d.store
	if store == nil {
		return nil, nil
	}
	if d.prefs != nil {
		if p := d.prefs(user); !p.hints && !p.push {
			return nil, nil
		}
	}
	if d.available != nil && !d.available() ||
		d.available == nil && store.Current != nil && store.Current(string(pimstore.Calendar)) == "" {
		return nil, fmt.Errorf("calendar store unavailable")
	}
	cols, err := store.Collections(pimstore.Calendar, user)
	if err != nil {
		return nil, err
	}
	var hidden []string
	if d.hidden != nil {
		hidden = d.hidden(user)
	}
	now := d.now()
	from, to := now.Add(-reminderGrace), now.Add(reminderHorizon)
	local := store.Zone()
	var out []plannedReminder
	for _, col := range cols {
		if col.Virtual || slices.Contains(hidden, col.ID) {
			continue
		}
		// Events that start up to four weeks (the longest alarm) after the window, a day before it.
		occs, _, err := store.Query(user, col.ID, from.Add(-24*time.Hour), to.Add(calweb.MaxAlarmMinutes*time.Minute), pimstore.MaxQueryOccurrences)
		if err != nil {
			return nil, err
		}
		for _, o := range occs {
			if len(o.Alarms) == 0 {
				continue
			}
			views := calweb.Views(col.ID, []pimstore.Occurrence{o}, local)
			if len(views) == 0 {
				continue
			}
			v := views[0]
			for _, m := range o.Alarms {
				at := o.Start.Add(-time.Duration(m) * time.Minute)
				if at.Before(from) || !at.Before(to) {
					continue
				}
				r := firedReminder{Calendar: col.ID, ID: o.Name, Occurrence: v.Occurrence, Title: v.Title, Location: v.Location,
					Start: v.Start, AllDay: v.AllDay, Minutes: m}
				// The account first: two accounts may have a calendar of
				// the same id with a file of the same name (unabhängige
				// Durchsicht 1.13, A-2); forget drops by this prefix.
				r.key = reminderKeyPrefix(user) + col.ID + "\x00" + o.Name + "\x00" + v.Occurrence + "\x00" + strconv.Itoa(m)
				out = append(out, plannedReminder{at: at, firedReminder: r})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	if len(out) > maxPlannedReminders {
		out = out[:maxPlannedReminders]
	}
	return out, nil
}

func (c *calendarReminders) nextAt() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var next time.Time
	for _, plan := range c.plans {
		if len(plan) > 0 && (next.IsZero() || plan[0].at.Before(next)) {
			next = plan[0].at
		}
	}
	return next, !next.IsZero()
}

// fire turns the reminders that are due into hints. One that was due
// more than reminderGrace ago (titand was not running, the plan came
// late) is dropped; each key once.
func (c *calendarReminders) fire() {
	d := c.deps.Load()
	now := d.now()
	type out struct {
		user string
		r    firedReminder
		push bool
	}
	var pushes []out
	c.mu.Lock()
	for key, at := range c.sent {
		if now.Sub(at) > reminderHorizon {
			delete(c.sent, key)
		}
	}
	for user, plan := range c.plans {
		i := 0
		for ; i < len(plan) && !plan[i].at.After(now); i++ {
			p := plan[i]
			if now.Sub(p.at) > reminderGrace {
				continue
			}
			if _, done := c.sent[p.key]; done {
				continue
			}
			c.sent[p.key] = p.at
			prefs := reminderPrefs{hints: true}
			if d.prefs != nil {
				prefs = d.prefs(user)
			}
			c.seq++
			r := p.firedReminder
			r.Seq, r.At = c.seq, p.at
			if prefs.hints {
				list := append(c.fired[user], r)
				if len(list) > maxFiredReminders {
					list = list[len(list)-maxFiredReminders:]
				}
				c.fired[user] = list
			}
			if prefs.push && d.push != nil {
				pushes = append(pushes, out{user, r, true})
			}
		}
		if i == len(plan) {
			delete(c.plans, user)
		} else {
			c.plans[user] = plan[i:]
		}
	}
	c.mu.Unlock()
	for _, p := range pushes {
		d.push(p.user, p.r)
	}
}

// latest is "<epoch>-<seq>" of the account's newest hint ("" without).
func (c *calendarReminders) latest(user string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.fired[user]
	if len(list) == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%d", c.epoch, list[len(list)-1].Seq)
}

// recent are the account's hints of the last reminderKeep, oldest first.
func (c *calendarReminders) recent(user string) []firedReminder {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	out := []firedReminder{}
	for _, r := range c.fired[user] {
		if now.Sub(r.At) <= reminderKeep {
			out = append(out, r)
		}
	}
	return out
}

// forget drops what is kept of an account (deleted).
func (c *calendarReminders) forget(user string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.plans, user)
	delete(c.fired, user)
	prefix := reminderKeyPrefix(user)
	for key := range c.sent {
		if strings.HasPrefix(key, prefix) {
			delete(c.sent, key)
		}
	}
	c.mu.Unlock()
}

// reminderKeyPrefix begins the key of every reminder of an account.
func reminderKeyPrefix(user string) string { return user + "\x00" }

// handleReminders is GET /api/v1/calendar/reminders: the account's hints
// of the last 12 hours, {epoch, reminders: [firedReminder]}.
func (s *Server) handleReminders(w http.ResponseWriter, r *http.Request) {
	// Like every route of the calendar: without the data never an empty list.
	if _, ok := s.pimDataArea().calendarStore(w); !ok {
		return
	}
	user := currentUser(r).Name
	writeJSON(w, http.StatusOK, map[string]any{
		"epoch":     strconv.FormatInt(s.reminders.epoch, 10),
		"reminders": s.reminders.recent(user),
	})
}

// reminderPrefsOf reads "reminderHints" (default true) and "reminderPush"
// (default false) of the preference kind "calendar".
func (s *Server) reminderPrefsOf(user string) reminderPrefs {
	p := reminderPrefs{hints: true}
	if s.prefs == nil {
		return p
	}
	raw, err := s.prefs.get(user, "calendar")
	if err != nil || raw == nil {
		return p
	}
	var v struct {
		Hints *bool `json:"reminderHints"`
		Push  *bool `json:"reminderPush"`
	}
	if json.Unmarshal(raw, &v) == nil {
		if v.Hints != nil {
			p.hints = *v.Hints
		}
		if v.Push != nil {
			p.push = *v.Push
		}
	}
	return p
}

// startReminders wires the planner to the store and starts it.
func (s *Server) startReminders() {
	c := s.reminders
	if s.opts.PIM == nil {
		return
	}
	d := &reminderDeps{store: s.opts.PIM, now: time.Now, prefs: s.reminderPrefsOf, hidden: s.pimDataArea().hiddenCalendars}
	d.users = func() []string {
		if s.opts.Users == nil {
			return nil
		}
		var out []string
		for _, u := range s.opts.Users.List() {
			if !u.Disabled {
				out = append(out, u.Name)
			}
		}
		return out
	}
	if n := s.opts.Notify; n != nil {
		local := s.opts.PIM.Zone
		d.push = func(user string, r firedReminder) {
			if n.PushTo == nil {
				return
			}
			hostname, _ := os.Hostname()
			title := r.Title
			if title == "" {
				// In the account's language of its notifications.
				title = "(ohne Titel)"
				if p, _, err := n.PersonalSettings(user); err == nil && p.Language == "en" {
					title = "(no title)"
				}
			}
			n.PushTo(user, "calendar", notify.Message{Time: r.At, Severity: notify.Info, Kind: "calendar_reminder",
				Subject: title, Title: title, Text: reminderPushText(r, local()), Link: "/calendar"}, "["+hostname+"] "+title)
		}
	}
	c.deps.Store(d)
	// Not a job of goJob: it runs as long as titand (Close need not wait
	// for it; it writes nothing).
	go c.run(s.ctx)
}

// reminderPushText is the line of a push: when the event begins (in the
// zone of the NAS), and where.
func reminderPushText(r firedReminder, local *time.Location) string {
	when := r.Start
	if t, err := time.Parse(time.RFC3339, r.Start); err == nil {
		when = t.In(local).Format("02.01.2006 15:04")
	} else if t, err := time.Parse("2006-01-02", r.Start); err == nil {
		when = t.Format("02.01.2006")
	}
	if r.Location != "" {
		return when + " · " + r.Location
	}
	return when
}
