// Package tasks is what TitanOS does with reminders (VTODO, 1.13) beyond
// CalDAV: the lists and reminders of an account for the web API and, later,
// the agent's tools (tasks_list/create/update/complete/delete call Service
// directly, no HTTP in between). The reminders are stored by pim/store
// (kind Tasks) with the bytes as they came; editing works on the component
// tree, so what the page does not know (X-APPLE-…, alarms of other
// clients, parameters) stays as it is. Times follow pim/tz, repetitions
// pim/recur.
package tasks

import (
	"errors"
	"fmt"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// ProdID names TitanOS in the reminders it writes.
const ProdID = "-//TitanOS//Erinnerungen 1.13//DE"

// Bounds of the fields of a reminder.
const (
	MaxTitle  = calweb.MaxTitle
	MaxNotes  = calweb.MaxNotes
	MaxParent = 255
	// MaxAlarms bounds the alarms of one reminder.
	MaxAlarms = recur.MaxAlarms
)

// DefaultListNames are the names of the virtual default list in the
// languages of the pages; DAV shows the German one.
var DefaultListNames = map[string]string{"de": store.DefaultTaskListName, "en": "Reminders"}

// ErrInvalid marks a reminder, list or time the API refuses (400).
var ErrInvalid = errors.New("tasks: invalid reminder")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Opt is a field that may be left out (unchanged) or set, null included.
type Opt[T any] = calweb.Opt[T]

// Input is a reminder as the page (or the agent) sends it. On create only
// the fields given are written; on update a field left out stays as it
// is. Due and Start: a date "2026-11-20" (all day) or a wall clock
// "2026-11-20T09:00" (seconds and an RFC 3339 offset allowed) in Timezone
// (an IANA name or "UTC"; empty: the zone the reminder has, else the
// zone of the NAS). Null removes the date.
type Input struct {
	// List is the list of a new reminder ("" = the first list), or on
	// update the list to move it to.
	List  string      `json:"list"`
	Title *string     `json:"title"`
	Notes *string     `json:"notes"`
	Due   Opt[string] `json:"due"`
	// Start is DTSTART; left out on create (or on an update that moves
	// DUE of a reminder whose DTSTART equals it) it follows DUE, as iOS
	// writes it.
	Start    Opt[string] `json:"start"`
	Timezone string      `json:"timezone"`
	// Priority 0–9: 1 high, 5 medium, 9 low, 0 none.
	Priority *int `json:"priority"`
	// Alarms are absolute times (like Due) at which to remind; null or []
	// = none. Alarms TitanOS cannot place (no date to relate to) stay.
	Alarms Opt[[]string] `json:"alarms"`
	// Repeat: null = no repetition; a rule with custom:true keeps the
	// rule the reminder has. Counted from Start, else Due.
	Repeat Opt[recur.Simple] `json:"repeat"`
	// Parent is the UID of the reminder this one is a sub-task of
	// (RELATED-TO); null or "" = none.
	Parent Opt[string] `json:"parent"`
	// Lang names the virtual default list when a write makes it real
	// ("de", "en").
	Lang string `json:"lang"`
}

// Item is a reminder in a list, from the index (no disk).
type Item struct {
	ID      string `json:"id"` // the file name (URL-encode it)
	List    string `json:"list"`
	UID     string `json:"uid"`
	Title   string `json:"title"`
	Clipped bool   `json:"clipped,omitempty"`
	// Due is a date (DueAllDay) or an RFC 3339 time in the zone of the
	// reminder (floating: the NAS zone).
	Due       string `json:"due,omitempty"`
	DueAllDay bool   `json:"dueAllDay,omitempty"`
	Done      bool   `json:"done,omitempty"`
	// Completed is when it was done (RFC 3339, UTC).
	Completed string `json:"completed,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Recurring bool   `json:"recurring,omitempty"`
	Alarm     bool   `json:"alarm,omitempty"`
	ETag      string `json:"etag"`
}

// Task is one reminder with what the page edits (one GET, create,
// update).
type Task struct {
	Item
	Notes       string `json:"notes,omitempty"`
	Start       string `json:"start,omitempty"`
	StartAllDay bool   `json:"startAllDay,omitempty"`
	// Timezone is the zone of DUE (else DTSTART): an IANA name, "UTC", or
	// "" for dates and floating times.
	Timezone string `json:"timezone,omitempty"`
	Status   string `json:"status,omitempty"`
	Percent  int    `json:"percent,omitempty"`
	// Alarms are the times of the alarms TitanOS can place (RFC 3339 in
	// the zone of the reminder).
	Alarms           []string         `json:"alarms,omitempty"`
	Rule             *calweb.RuleView `json:"rule,omitempty"`
	RecurUnsupported bool             `json:"recurUnsupported,omitempty"`
}

// List is a reminder list.
type List struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Order int    `json:"order"`
	// Count is the number of reminders, Open of those not done.
	Count int `json:"count"`
	Open  int `json:"open"`
	// Virtual: the default list before its first write (its name in the
	// language asked for).
	Virtual bool `json:"virtual,omitempty"`
}

// ListInput is a new list or a change of one; nil fields stay.
type ListInput struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
	Order *int    `json:"order"`
}

// Filter chooses reminders.
type Filter struct {
	// List is one list; "" = all.
	List string
	// Status is "open" (default), "done" or "all".
	Status string
	// DueBefore keeps reminders with DUE before it (exclusive; those
	// without DUE drop out); zero = any.
	DueBefore time.Time
}

// Done is the outcome of ticking a reminder off or opening it again.
type Done struct {
	// Task is the reminder as it is now: for a series the next
	// occurrence, still open.
	Task Item `json:"task"`
	// Copy is, for a series, the done copy of the occurrence ticked off
	// (a reminder of its own, as iOS keeps it).
	Copy *Item `json:"copy,omitempty"`
}

// formatDue writes a time of the index for the page.
func formatTime(at time.Time, allDay, floating bool, zone string, local *time.Location) string {
	if at.IsZero() {
		return ""
	}
	if allDay {
		return at.Format(time.DateOnly)
	}
	loc := local
	switch {
	case floating:
		y, m, d := at.Date()
		h, mi, s := at.Clock()
		return time.Date(y, m, d, h, mi, s, 0, local).Format(time.RFC3339)
	case zone != "":
		if l := calweb.Zone(zone); l != nil {
			loc = l
		}
	}
	return at.In(loc).Format(time.RFC3339)
}

// itemOf is the list form of an index entry.
func itemOf(list string, o store.ObjectInfo, local *time.Location) Item {
	it := Item{ID: o.Name, List: list, UID: o.UID, Title: o.Summary, Clipped: o.Clipped, ETag: o.ETag}
	ti := o.Task
	if ti == nil {
		return it
	}
	it.Due = formatTime(ti.Due, ti.DueAllDay, ti.DueFloating, ti.DueTZ, local)
	it.DueAllDay = ti.DueAllDay
	it.Done = ti.Done()
	if !ti.Completed.IsZero() {
		it.Completed = ti.Completed.UTC().Format(time.RFC3339)
	}
	it.Priority, it.Parent, it.Recurring, it.Alarm = ti.Priority, ti.Parent, ti.Recurs, ti.Alarm
	return it
}
