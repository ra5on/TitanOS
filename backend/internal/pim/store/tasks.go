package store

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Reminder lists (1.13): the "Erinnerungen" of iOS are CalDAV calendar
// collections whose supported-calendar-component-set is VTODO. TitanOS
// keeps them as a kind of their own (Tasks) below .titanos/tasks:
//
//	.titanos/tasks/<account>/owner.json
//	.titanos/tasks/<account>/<list>/state.json, o/<entry>.ics
//
// so that each kind has its own virtual default ("privat" and
// "erinnerungen--titanos"), its own last collection and its own mark, and
// 1.12 – which reads every folder of .titanos/calendar/<account> as a
// calendar of events – never sees a list (it ignores .titanos/tasks). A
// collection holds one component: a list only VTODO, a calendar only
// VEVENT (as iCloud does); anything else is ErrUnsupportedComponent. The
// ids of both kinds share /caldav/<account>/calendars/: one id is either a
// calendar or a list (idFree).

// The virtual default list. Its id has a double hyphen, which no id the
// web UI ever made from a name has (calweb.CalendarID), so it never meets
// a calendar of 1.11/1.12 – iOS names its own lists with UUIDs.
const (
	DefaultTaskList     = "erinnerungen--titanos"
	DefaultTaskListName = "Erinnerungen"
	// DefaultTaskColor is --icon-square-orange of web/src/app.css.
	DefaultTaskColor = "#c2410c"
)

// TaskInfo is what the index keeps of a reminder (the master VTODO of the
// file), so lists, filters and the due ones need no disk. Times like
// Entry.Start: an instant, or for dates and floating times the wall clock
// written as UTC. Shared between copies of an entry: never changed once
// made.
type TaskInfo struct {
	Due           time.Time `json:"due,omitzero"`
	DueAllDay     bool      `json:"dueAllDay,omitempty"`
	DueFloating   bool      `json:"dueFloating,omitempty"`
	DueTZ         string    `json:"dueTz,omitempty"`
	Start         time.Time `json:"start,omitzero"`
	StartAllDay   bool      `json:"startAllDay,omitempty"`
	StartFloating bool      `json:"startFloating,omitempty"`
	// Status is STATUS in upper case (NEEDS-ACTION, IN-PROCESS,
	// COMPLETED, CANCELLED) or "".
	Status string `json:"status,omitempty"`
	// Completed is COMPLETED (UTC); zero when the property is missing.
	Completed time.Time `json:"completed,omitzero"`
	Percent   int       `json:"percent,omitempty"`
	// Priority is PRIORITY 0–9 (1 high, 5 medium, 9 low, 0 none).
	Priority int `json:"priority,omitempty"`
	// Parent is the UID of RELATED-TO with RELTYPE=PARENT (or without
	// RELTYPE): a sub-task of that reminder (clipped).
	Parent string `json:"parent,omitempty"`
	Alarm  bool   `json:"alarm,omitempty"`
	Recurs bool   `json:"recurs,omitempty"`
	// RecurUnsupported: a rule TitanOS does not expand (kept as it is).
	RecurUnsupported bool `json:"recurUnsupported,omitempty"`
}

// Done reports whether the reminder is done: STATUS:COMPLETED or a
// COMPLETED time (clients differ in which they write).
func (t *TaskInfo) Done() bool {
	return t != nil && (t.Status == "COMPLETED" || !t.Completed.IsZero())
}

// Cancelled reports STATUS:CANCELLED.
func (t *TaskInfo) Cancelled() bool { return t != nil && t.Status == "CANCELLED" }

// DueAt is DUE on the timeline, dates at 00:00 and floating times in loc;
// zero without DUE.
func (t *TaskInfo) DueAt(loc *time.Location) time.Time {
	if t == nil || t.Due.IsZero() {
		return time.Time{}
	}
	return wallIn(t.Due, t.DueAllDay || t.DueFloating, loc)
}

// StartAt is DTSTART like DueAt.
func (t *TaskInfo) StartAt(loc *time.Location) time.Time {
	if t == nil || t.Start.IsZero() {
		return time.Time{}
	}
	return wallIn(t.Start, t.StartAllDay || t.StartFloating, loc)
}

func wallIn(t time.Time, wall bool, loc *time.Location) time.Time {
	if !wall {
		return t
	}
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, m, d, h, mi, s, 0, loc)
}

// indexTime is the form of a value in the index.
func indexTime(v tz.Value) (at time.Time, allDay, floating bool) {
	switch v.Kind {
	case tz.KindDate, tz.KindFloating:
		y, m, d := v.Time.Date()
		h, mi, s := v.Time.Clock()
		return time.Date(y, m, d, h, mi, s, 0, time.UTC), v.Kind == tz.KindDate, v.Kind == tz.KindFloating
	}
	return v.Time.UTC(), false, false
}

// inspectTodo checks a reminder as a PUT does: VTODO and VTIMEZONE only,
// one UID per file, the limits of pim/recur (with MaxZoneBytes for a
// zone), and returns its index entry.
func (s *Store) inspectTodo(account, name string, data []byte) (*Entry, error) {
	if len(data) > MaxICS {
		return nil, ErrTooLarge
	}
	f, err := recur.ReadTodos(data, recur.Options{Local: s.local(), Account: account, Name: name, MaxZone: recur.MaxZoneBytes})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidData, err)
	}
	for _, c := range f.Calendar.Children {
		switch c.Name {
		case ical.CompToDo, ical.CompTimezone:
		default:
			return nil, fmt.Errorf("%w: %s in a reminder list", ErrUnsupportedComponent, c.Name)
		}
	}
	if len(f.Todos) == 0 {
		return nil, fmt.Errorf("%w: no VTODO", ErrInvalidData)
	}
	var master *recur.Todo
	for _, t := range f.Todos {
		if t.UID != f.Todos[0].UID {
			return nil, fmt.Errorf("%w: a file holds the reminders of exactly one UID", ErrInvalidData)
		}
		if t.RecurrenceID == nil {
			if master != nil {
				return nil, fmt.Errorf("%w: two VTODOs with the same UID and no RECURRENCE-ID", ErrInvalidData)
			}
			master = t
		}
	}
	if master == nil {
		master = f.Todos[0]
	}
	e := &Entry{ETag: ETag(data), UID: master.UID}
	if summary, err := master.Component.Props.Text(ical.PropSummary); err == nil {
		e.Summary, e.Clipped = clip(summary), clipped(summary)
	}
	e.Task = TodoInfo(master, len(f.Todos) > 1)
	return e, nil
}

// TodoInfo is the index form of a reminder (overrides: the file has
// overrides of single occurrences).
func TodoInfo(t *recur.Todo, overrides bool) *TaskInfo {
	c := t.Component
	ti := &TaskInfo{Recurs: t.Recurring() || overrides, RecurUnsupported: t.Unsupported}
	if t.Due != nil {
		ti.Due, ti.DueAllDay, ti.DueFloating = indexTime(*t.Due)
		ti.DueTZ = ZoneName(*t.Due)
	}
	if t.Start != nil {
		ti.Start, ti.StartAllDay, ti.StartFloating = indexTime(*t.Start)
	}
	if p := c.Props.Get(ical.PropStatus); p != nil {
		ti.Status = strings.ToUpper(strings.TrimSpace(p.Value))
		if len(ti.Status) > 32 {
			ti.Status = ti.Status[:32]
		}
	}
	if t.Completed != nil {
		ti.Completed = t.Completed.Time.UTC()
	}
	ti.Percent = smallInt(c, ical.PropPercentComplete, 100)
	ti.Priority = smallInt(c, ical.PropPriority, 9)
	ti.Parent = clip(ParentUID(c))
	for _, ch := range c.Children {
		if ch.Name == ical.CompAlarm {
			ti.Alarm = true
			break
		}
	}
	return ti
}

// ParentUID is the UID a VTODO names as its parent: RELATED-TO with
// RELTYPE=PARENT or without RELTYPE (RFC 5545 3.8.4.5); "" without one.
func ParentUID(c *ical.Component) string {
	for _, p := range c.Props.Values(ical.PropRelatedTo) {
		rel := p.Params.Get(ical.ParamRelationshipType)
		if rel == "" || strings.EqualFold(rel, "PARENT") {
			return strings.TrimSpace(p.Value)
		}
	}
	return ""
}

// smallInt reads an integer property in [0, limit]; anything else is 0.
func smallInt(c *ical.Component, name string, limit int) int {
	p := c.Props.Get(name)
	if p == nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.Value))
	if err != nil || n < 0 || n > limit {
		return 0
	}
	return n
}

// siblingKind is the kind whose collections share the CalDAV home with
// kind's ("" for contacts).
func siblingKind(kind Kind) Kind {
	switch kind {
	case Calendar:
		return Tasks
	case Tasks:
		return Calendar
	}
	return ""
}

// idFree is ErrExists when the sibling kind has a collection (or its
// virtual default) with the id. It is asked before the write's own locks,
// so two clients making the same id as a calendar and as a list in the
// same instant could both succeed; iOS names new collections with UUIDs
// and the web UI checks both kinds first.
func (s *Store) idFree(kind Kind, account, id string) error {
	other := siblingKind(kind)
	if other == "" {
		return nil
	}
	held, err := s.holds(other, account, id)
	if err != nil {
		return err
	}
	if held {
		return ErrExists
	}
	return nil
}

func (s *Store) holds(kind Kind, account, id string) (bool, error) {
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return false, err
	}
	c, err := acc.lookup(id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.deleted, nil
}

// Holds reports whether the account has a collection id of the kind, its
// virtual default included (from RAM after the first read). An error
// (no volume) counts as held: a name is never handed out unchecked.
func (s *Store) Holds(kind Kind, account, id string) bool {
	held, err := s.holds(kind, account, id)
	return held || err != nil
}

// CalDAVKind says which kind holds a collection id of the CalDAV home:
// Calendar or Tasks (a virtual default included); ErrNotFound when
// neither does, and the error of a kind that cannot be read (no volume)
// when the other does not hold it – the id might be one of its.
func (s *Store) CalDAVKind(account, id string) (Kind, error) {
	if !ValidCollection(id) {
		return Calendar, ErrInvalidCollection
	}
	held, calErr := s.holds(Calendar, account, id)
	if calErr == nil && held {
		return Calendar, nil
	}
	held, taskErr := s.holds(Tasks, account, id)
	switch {
	case taskErr == nil && held:
		return Tasks, nil
	case calErr != nil:
		return Calendar, calErr
	case taskErr != nil:
		return Calendar, taskErr
	}
	return Calendar, ErrNotFound
}
