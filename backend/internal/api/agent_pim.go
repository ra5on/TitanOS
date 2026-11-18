package api

// Calendar and contacts for the KI-Agent (1.11, TECHNIK 6.1): nine tools
// of the agent's own, never in the MCP catalog and never in publicRoutes
// (no tokens: personal data, like T-Mail). They run as the account of the
// chat (the profile of Home Assistant: as "Assist als") on its own data
// only; ids name a collection and a file of that account, never another
// account. Reading tools return outside content (invitations of others),
// so the chat counts as tainted afterwards. What may run is decided by
// Policy.DataRights (two axes); deleting a whole series or cutting it
// down always asks, and so does any delete after outside content.
// The addresses T-Mail learned from sent mail are T-Mail's: no tool here
// reads them.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The words hidden results carry (TECHNIK 6.1), so that the model never
// says "no events" when it may not or cannot look.
const (
	pimWhyBirthdays      = "birthdays come from the contacts, which the person has not opened to the agent (Einstellungen › KI-Agent › Rechte › Kontakte); this is no sign that there are none"
	pimWhyBirthdaysGone  = "birthdays come from the contacts, which are on a data volume that is not mounted right now; this is no sign that there are none"
	pimWhyCalendarVolume = "the calendar is on a data volume that is not mounted right now; this is no sign that there are no events"
	pimWhyContactsVolume = "the contacts are on a data volume that is not mounted right now; this is no sign that there are none"
)

// Bounds of the tools.
const (
	pimDefaultDays  = 14
	pimMaxEvents    = 100
	pimMaxContacts  = 50
	pimMaxNotes     = 2000
	pimMaxFileReads = 400
	// pimMaxScan bounds the occurrences calendar_search looks through for
	// a text (S-20): the earliest of the window; truncated past it.
	pimMaxScan = 2000
)

// pimTools are the tools of calendar and contacts; none without the store
// (titand -dev, tests without it). Without the volume they exist and say
// so (hidden/why), so the model never reads "nothing" as "none".
func (b *agentToolbox) pimTools() []*agentTool {
	if b.s.opts.PIM == nil {
		return nil
	}
	cal, con := agent.AreaCalendar, agent.AreaContacts
	idCal := str("Id from calendar_search, e.g. cal:privat/abc.ics@20261120T080000Z (an occurrence of a series ends with @…). Never make one up.")
	idCard := str("Id from contacts_search, e.g. card:kontakte/abc.vcf. Never make one up.")
	scope := enum("For an event of a series: this (only this occurrence), following (this and all after it) or all (the whole series). Required for a series.", "this", "following", "all")
	etag := str("Leave out; TitanOS fills it in.")
	event := map[string]any{
		"title":    map[string]any{"type": "string", "maxLength": 2000, "description": "Title."},
		"start":    str("Start: 2026-11-20T09:00 (wall clock in timezone) or 2026-11-20 for an all-day event."),
		"end":      str("End like start; all-day: the day after the last day (exclusive). Leave out for duration or the default (1 hour, 1 day)."),
		"duration": map[string]any{"type": "integer", "minimum": 1, "maximum": 40320, "description": "Length in minutes instead of end (timed events)."},
		"allDay":   map[string]any{"type": "boolean", "description": "All-day event (start and end are dates)."},
		"timezone": str("IANA zone such as Europe/Berlin; default the zone of the NAS (a change keeps the event's zone)."),
		"location": map[string]any{"type": "string", "maxLength": 2000, "description": "Place (\"\" clears it)."},
		"notes":    map[string]any{"type": "string", "maxLength": 32000, "description": "Notes (\"\" clears them)."},
		"repeat": map[string]any{"type": "object", "additionalProperties": false, "description": "Repetition; freq none removes it.",
			"properties": map[string]any{
				"freq":       enum("daily, weekly, monthly, yearly or none.", "daily", "weekly", "monthly", "yearly", "none"),
				"interval":   map[string]any{"type": "integer", "minimum": 1, "maximum": 999, "description": "Every n days/weeks/months/years (default 1)."},
				"byDay":      map[string]any{"type": "array", "maxItems": 7, "items": str("MO … SU, monthly with a number: 3TU, -1FR."), "description": "Weekdays."},
				"byMonthDay": map[string]any{"type": "array", "maxItems": 31, "items": map[string]any{"type": "integer", "minimum": -1, "maximum": 31}, "description": "Days of the month (-1 = last)."},
				"until":      str("Last day YYYY-MM-DD (included)."),
				"count":      map[string]any{"type": "integer", "minimum": 1, "maximum": 5000, "description": "Number of occurrences."},
			}, "required": []string{"freq"}},
		"alarms": map[string]any{"type": "array", "maxItems": 20, "items": map[string]any{"type": "integer", "minimum": 0, "maximum": calweb.MaxAlarmMinutes}, "description": "Reminders in minutes before the start ([] = none)."},
	}
	with := func(base map[string]any, more map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range more {
			out[k] = v
		}
		return out
	}
	value := func(kinds ...string) map[string]any {
		return map[string]any{"type": "array", "maxItems": 100, "items": map[string]any{"type": "object", "additionalProperties": false,
			"properties": map[string]any{"type": enum("Kind.", kinds...), "value": map[string]any{"type": "string", "maxLength": 1000}}, "required": []string{"value"}}}
	}
	contact := map[string]any{
		"first":  map[string]any{"type": "string", "maxLength": 2000, "description": "First name."},
		"last":   map[string]any{"type": "string", "maxLength": 2000, "description": "Last name."},
		"org":    map[string]any{"type": "string", "maxLength": 2000, "description": "Company."},
		"phones": value("mobile", "home", "work", "other"),
		"emails": value("home", "work", "other"),
		"addresses": map[string]any{"type": "array", "maxItems": 100, "items": map[string]any{"type": "object", "additionalProperties": false,
			"properties": map[string]any{"type": enum("Kind.", "home", "work", "other"), "street": str("Street and number."), "city": str("City."),
				"postcode": str("Postcode."), "region": str("Region."), "country": str("Country.")}}},
		"birthday": str("YYYY-MM-DD, --MM-DD without year, \"\" removes it."),
		"notes":    map[string]any{"type": "string", "maxLength": 32000, "description": "Notes."},
	}
	return []*agentTool{
		{name: "calendar_list", area: cal, kind: agent.KindRead, untrusted: true,
			description: "The person's calendars in T-Kalender (id, name, color, number of events), and the birthdays from the contacts.",
			params:      schema(nil), own: b.calendarList, prepare: b.pimCheckArgs},
		{name: "calendar_search", area: cal, kind: agent.KindRead, untrusted: true,
			description: "Events of the person's calendars between from and to (default today 00:00 to 14 days later, at most 400 days), optionally only those whose title, place or notes contain query. Use it for \"Was steht morgen an?\" and before changing or deleting an event (its id). truncated:true means not all were looked at (with query only the earliest 2000 of the span): search a shorter span before saying there is none.",
			params: schema(map[string]any{
				"from":      str("Start of the time span: 2026-11-20 or 2026-11-20T08:00 (zone of the NAS) or RFC 3339."),
				"to":        str("End of the time span (exclusive), like from."),
				"query":     str("Words in title, place or notes (case does not matter)."),
				"calendars": map[string]any{"type": "array", "maxItems": 60, "items": str("Calendar id or name from calendar_list."), "description": "Only these calendars."},
				"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": pimMaxEvents, "description": "At most this many events (default 50)."},
			}), own: b.calendarSearch, prepare: b.calendarSearchCheck},
		{name: "calendar_create", area: cal, kind: agent.KindChange,
			description: "Add an event to a calendar of the person (default the first one).",
			params:      schema(with(event, map[string]any{"calendar": str("Calendar id or name from calendar_list; default the first.")}), "title", "start"),
			own:         b.calendarCreate, prepare: b.calendarCreatePrepare},
		{name: "calendar_update", area: cal, kind: agent.KindChange,
			description: "Change an event: only the fields given change. For a series give scope; a new repeat rule for the whole series always asks the person.",
			params:      schema(with(event, map[string]any{"id": idCal, "scope": scope, "etag": etag}), "id"),
			own:         b.calendarUpdate, prepare: b.calendarUpdatePrepare},
		{name: "calendar_delete", area: cal, kind: agent.KindChange,
			description: "Delete an event, only when asked to. For a series give scope; deleting a whole series always asks the person, and so does any delete after outside content.",
			params:      schema(map[string]any{"id": idCal, "scope": scope, "etag": etag}, "id"),
			own:         b.calendarDelete, prepare: b.calendarDeletePrepare},
		{name: "contacts_search", area: con, kind: agent.KindRead, untrusted: true,
			description: "The person's contacts in Kontakte whose name, company, number, address or e-mail address contains query (empty: all), with numbers, addresses, birthday and notes. Not the addresses T-Mail learned.",
			params: schema(map[string]any{
				"query": str("Name, company, number, address or e-mail address."),
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": pimMaxContacts, "description": "At most this many (default 20)."},
			}), own: b.contactsSearch, prepare: b.pimCheckArgs},
		{name: "contacts_create", area: con, kind: agent.KindChange,
			description: "Add a contact to the person's address book.",
			params:      schema(contact), own: b.contactsCreate, prepare: b.contactsCreatePrepare},
		{name: "contacts_update", area: con, kind: agent.KindChange,
			description: "Change a contact: only the fields given change; a list (phones, emails, addresses) given replaces the whole list.",
			params:      schema(with(contact, map[string]any{"id": idCard, "etag": etag}), "id"),
			own:         b.contactsUpdate, prepare: b.contactsUpdatePrepare},
		{name: "contacts_delete", area: con, kind: agent.KindChange,
			description: "Delete a contact, only when asked to.",
			params:      schema(map[string]any{"id": idCard, "etag": etag}, "id"),
			own:         b.contactsDelete, prepare: b.contactsDeletePrepare},
	}
}

// ---------- shared ----------

// pimAccount is the account the tools act as: the chat's, or "Assist als"
// for a question from Home Assistant.
func (b *agentToolbox) pimAccount() string { return b.session.user.Name }

// pimArea is the store part of the web API without T-Mail: the agent
// never sees T-Mail's learned addresses.
func (b *agentToolbox) pimArea() pimDataArea { return pimDataArea{store: b.s.opts.PIM} }

// pimMounted reports whether the data of a kind can be read now (RAM only).
func (b *agentToolbox) pimMounted(kind pimstore.Kind) bool {
	st := b.s.opts.PIM
	return st != nil && (st.Current == nil || st.Current(string(kind)) != "")
}

// pimMayRead reports whether the agent may read an area in this run (the
// rights, and for Home Assistant its own narrower ones).
func (b *agentToolbox) pimMayRead(area string) bool {
	return b.modeIn(&agentTool{name: area + "_search", area: area}, area, agent.KindRead) != agent.ModeDeny
}

func pimGone(err error) bool {
	return errors.Is(err, pimstore.ErrUnavailable) || errors.Is(err, volumedata.ErrHeld)
}

// pimError words an error of the store or of calweb/cardweb for the model.
func pimError(err error, kind pimstore.Kind) error {
	what := "event"
	if kind == pimstore.Contacts {
		what = "contact"
	}
	switch {
	case pimGone(err):
		if kind == pimstore.Contacts {
			return toolcatalog.Errorf("the contacts are on a data volume that is not mounted right now; nothing was changed")
		}
		return toolcatalog.Errorf("the calendar is on a data volume that is not mounted right now; nothing was changed")
	case errors.Is(err, errCalendarMissing), errors.Is(err, pimstore.ErrInvalidCollection):
		return toolcatalog.Errorf("there is no such calendar; calendar_list names them")
	case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidName), errors.Is(err, calweb.ErrNoOccurrence):
		return toolcatalog.Errorf("there is no such %s; take the id from the search", what)
	case errors.Is(err, pimstore.ErrPrecondition):
		return toolcatalog.Errorf("the %s was changed meanwhile; read it again and ask the person once more", what)
	case errors.Is(err, pimstore.ErrCollectionFull):
		return toolcatalog.Errorf("the calendar or address book is full")
	case errors.Is(err, pimstore.ErrTooLarge):
		return toolcatalog.Errorf("the %s is too large", what)
	case errors.Is(err, calweb.ErrInvalid), errors.Is(err, cardweb.ErrInvalid), errors.Is(err, pimstore.ErrInvalidData), errors.Is(err, recur.ErrNotSimple):
		return toolcatalog.Errorf("%v", err)
	}
	return fmt.Errorf("the %s could not be read or written", what)
}

func pimJSON(v any) (agent.Outcome, error) {
	data, err := json.Marshal(v)
	return agent.Outcome{Text: string(data)}, err
}

// pimCheckArgs checks the arguments against the schema; the tools of the
// agent's own are not checked by Prepare otherwise.
func (b *agentToolbox) pimCheckArgs(_ context.Context, _ *agent.Chat, name string, args json.RawMessage, _ *agent.Prepared) (string, error) {
	return "", toolcatalog.ValidateArguments(b.tools[name].params, args)
}

// withETag puts the etag read in Prepare into the arguments: the step that
// runs (now or after the person's yes) changes exactly what was shown, and
// a change meanwhile fails instead.
func withETag(p *agent.Prepared, etag string) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(p.Args, &fields) != nil {
		return
	}
	if _, set := fields["etag"]; set {
		return
	}
	fields["etag"], _ = json.Marshal(etag)
	p.Args, _ = json.Marshal(fields)
}

func pimClip(s string, n int) (string, bool) {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…", true
	}
	return s, false
}

// ---------- ids ----------

func calID(col, name, occurrence string) string {
	id := "cal:" + col + "/" + name
	if occurrence != "" {
		id += "@" + occurrence
	}
	return id
}

// parseCalID reads cal:<collection>/<file>[@<occurrence>]; a file name may
// hold "@" itself (Google UIDs), so only a valid occurrence after the
// last one counts.
func parseCalID(id string) (col, name, occurrence string, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(id), "cal:")
	col, name, ok2 := strings.Cut(rest, "/")
	if i := strings.LastIndex(name, "@"); i >= 0 {
		if _, perr := recur.ParseOccurrenceID(name[i+1:]); perr == nil {
			name, occurrence = name[:i], name[i+1:]
		}
	}
	if !ok || !ok2 || !pimstore.ValidCollection(col) || col == calweb.BirthdaysID || name == "" || strings.Contains(name, "/") {
		return "", "", "", toolcatalog.Errorf("id %q is not an event id from calendar_search (cal:<calendar>/<file>[@occurrence]); birthdays cannot be changed here", id)
	}
	return col, name, occurrence, nil
}

func cardID(name string) string { return "card:" + pimstore.DefaultBook + "/" + name }

func parseCardID(id string) (string, error) {
	name, ok := strings.CutPrefix(strings.TrimSpace(id), "card:"+pimstore.DefaultBook+"/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return "", toolcatalog.Errorf("id %q is not a contact id from contacts_search (card:kontakte/<file>)", id)
	}
	return name, nil
}

// ---------- calendar_list ----------

type pimCalendarRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	ReadOnly bool   `json:"readOnly"`
	Kind     string `json:"kind"`
	Events   int    `json:"events"`
}

func (b *agentToolbox) calendarList(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
	out := map[string]any{"calendars": []pimCalendarRow{}}
	store, user := b.s.opts.PIM, b.pimAccount()
	if !b.pimMounted(pimstore.Calendar) {
		out["hidden"], out["why"] = true, pimWhyCalendarVolume
		return pimJSON(out)
	}
	cols, err := store.Collections(pimstore.Calendar, user)
	if pimGone(err) {
		out["hidden"], out["why"] = true, pimWhyCalendarVolume
		return pimJSON(out)
	}
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	rows := []pimCalendarRow{}
	for _, c := range cols {
		v := ownCalendar(c, nil)
		rows = append(rows, pimCalendarRow{ID: v.ID, Name: v.Name, Color: v.Color, Kind: "own", Events: v.Count})
	}
	switch {
	case !b.pimMayRead(agent.AreaContacts):
		out["hidden"], out["why"] = true, pimWhyBirthdays
	default:
		contacts, err := store.Objects(pimstore.Contacts, user, pimstore.DefaultBook)
		if err != nil {
			out["hidden"], out["why"] = true, pimWhyBirthdaysGone
			break
		}
		rows = append(rows, pimCalendarRow{ID: calweb.BirthdaysID, Name: "Geburtstage", Color: birthdayColor, ReadOnly: true, Kind: "birthdays", Events: calweb.CountBirthdays(contacts)})
	}
	out["calendars"] = rows
	return pimJSON(out)
}

// ---------- calendar_search ----------

type calendarSearchArgs struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Query     string   `json:"query"`
	Calendars []string `json:"calendars"`
	Limit     int      `json:"limit"`
}

type pimRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type pimEventView struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind,omitempty"` // "birthday"
	Calendar     pimRef           `json:"calendar"`
	Title        string           `json:"title"`
	Start        string           `json:"start"`
	End          string           `json:"end"`
	AllDay       bool             `json:"allDay"`
	Timezone     string           `json:"timezone"`
	Location     string           `json:"location"`
	Notes        string           `json:"notes,omitempty"`
	NotesClipped bool             `json:"notesClipped,omitempty"`
	Recurring    bool             `json:"recurring"`
	Rule         *calweb.RuleView `json:"rule,omitempty"`
	Occurrence   string           `json:"occurrence"`
	Alarms       []int            `json:"alarms"`
	Exception    bool             `json:"exception"`
	Birthday     *calweb.Birthday `json:"birthday,omitempty"`
}

// window reads from and to: today 00:00 and 14 days later by default, at
// most 400 days.
func (q calendarSearchArgs) window(local *time.Location, now time.Time) (time.Time, time.Time, error) {
	now = now.In(local)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, local)
	if q.From != "" {
		t, ok := parseInstant(q.From, local)
		if !ok {
			return from, from, toolcatalog.Errorf("from: a date (2026-11-20), a time (2026-11-20T08:00) or RFC 3339")
		}
		from = t
	}
	to := from.AddDate(0, 0, pimDefaultDays)
	if q.To != "" {
		t, ok := parseInstant(q.To, local)
		if !ok {
			return from, to, toolcatalog.Errorf("to: a date (2026-11-20), a time (2026-11-20T08:00) or RFC 3339")
		}
		to = t
	}
	if !to.After(from) {
		return from, to, toolcatalog.Errorf("to must come after from")
	}
	if to.After(from.AddDate(0, 0, recur.MaxWindowDays)) {
		return from, to, toolcatalog.Errorf("at most %d days at once; search in parts", recur.MaxWindowDays)
	}
	return from, to, nil
}

func (b *agentToolbox) calendarSearchCheck(ctx context.Context, chat *agent.Chat, name string, args json.RawMessage, p *agent.Prepared) (string, error) {
	if err := toolcatalog.ValidateArguments(b.tools[name].params, args); err != nil {
		return "", err
	}
	var q calendarSearchArgs
	if err := json.Unmarshal(args, &q); err != nil {
		return "", toolcatalog.Errorf("invalid arguments")
	}
	_, _, err := q.window(b.pimZone(), time.Now())
	return "", err
}

func (b *agentToolbox) pimZone() *time.Location {
	if b.s.opts.PIM == nil {
		return time.Local
	}
	return b.s.opts.PIM.Zone()
}

// pimFile is what a calendar search needs of a file and the index does
// not hold: the whole title and place (the index cuts them) and the notes,
// for the event itself and its exceptions.
type pimFile struct {
	master    [3]string
	overrides map[string][3]string
}

func readPimFile(data []byte, local *time.Location) *pimFile {
	f, err := recur.Read(data, recur.Options{Local: local})
	if err != nil || len(f.Series) != 1 {
		return nil
	}
	texts := func(c *ical.Component) [3]string {
		title, _ := c.Props.Text(ical.PropSummary)
		place, _ := c.Props.Text(ical.PropLocation)
		notes, _ := c.Props.Text(ical.PropDescription)
		return [3]string{title, place, notes}
	}
	out := &pimFile{overrides: map[string][3]string{}}
	sr := f.Series[0]
	if sr.Master != nil {
		out.master = texts(sr.Master.Component)
	}
	for _, o := range sr.Overrides {
		if o.RecurrenceID != nil {
			out.overrides[recur.OccurrenceID(*o.RecurrenceID)] = texts(o.Component)
		}
	}
	return out
}

func (f *pimFile) of(occurrence string) [3]string {
	if t, ok := f.overrides[occurrence]; ok {
		return t
	}
	return f.master
}

func (b *agentToolbox) calendarSearch(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var q calendarSearchArgs
	if err := json.Unmarshal(args, &q); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	out := map[string]any{"events": []pimEventView{}, "truncated": false}
	store, user := b.s.opts.PIM, b.pimAccount()
	if !b.pimMounted(pimstore.Calendar) {
		out["hidden"], out["why"] = true, pimWhyCalendarVolume
		return pimJSON(out)
	}
	local := store.Zone()
	from, to, err := q.window(local, time.Now())
	if err != nil {
		return agent.Outcome{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, pimMaxEvents)
	cols, err := store.Collections(pimstore.Calendar, user)
	if pimGone(err) {
		out["hidden"], out["why"] = true, pimWhyCalendarVolume
		return pimJSON(out)
	}
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	names := map[string]string{}
	var chosen []pimstore.Collection
	birthdays := len(q.Calendars) == 0
	if len(q.Calendars) == 0 {
		chosen = cols
	}
	for _, want := range q.Calendars {
		w := strings.TrimSpace(want)
		if strings.EqualFold(w, calweb.BirthdaysID) || strings.EqualFold(w, "Geburtstage") || strings.EqualFold(w, "birthdays") {
			birthdays = true
			continue
		}
		i := slices.IndexFunc(cols, func(c pimstore.Collection) bool { return c.ID == w || strings.EqualFold(c.Name, w) })
		if i < 0 {
			return agent.Outcome{}, toolcatalog.Errorf("there is no calendar %q; calendar_list names them", w)
		}
		if !slices.ContainsFunc(chosen, func(c pimstore.Collection) bool { return c.ID == cols[i].ID }) {
			chosen = append(chosen, cols[i])
		}
	}
	// The occurrences are bounded before they are expanded (S-20): the
	// earliest limit+1 tell whether there are more; with a text the
	// earliest pimMaxScan are searched.
	needle := cardweb.Fold(strings.TrimSpace(q.Query))
	scan := limit + 1
	if needle != "" {
		scan = pimMaxScan
	}
	var views []calweb.View
	for _, c := range chosen {
		names[c.ID] = ownCalendar(c, nil).Name
		occs, cut, err := store.Query(user, c.ID, from, to, scan)
		if pimGone(err) {
			out["hidden"], out["why"] = true, pimWhyCalendarVolume
			return pimJSON(out)
		}
		if err != nil {
			return agent.Outcome{}, pimError(err, pimstore.Calendar)
		}
		for _, o := range occs {
			if o.Truncated {
				out["truncated"] = true
			}
		}
		views = append(views, calweb.Views(c.ID, occs, local)...)
		var merged bool
		views, merged = earliestViews(views, scan, local)
		if (cut || merged) && needle != "" {
			out["truncated"] = true // a match may be past the scan
		}
	}
	if birthdays {
		switch {
		case !b.pimMayRead(agent.AreaContacts):
			// Nothing of the contacts, not even a count (TECHNIK 8.3 q. 25).
			out["hidden"], out["why"] = true, pimWhyBirthdays
		default:
			contacts, err := store.Objects(pimstore.Contacts, user, pimstore.DefaultBook)
			if err != nil {
				out["hidden"], out["why"] = true, pimWhyBirthdaysGone
				break
			}
			names[calweb.BirthdaysID] = "Geburtstage"
			views = append(views, calweb.Birthdays(contacts, from, to, local)...)
		}
	}
	views, cut := earliestViews(views, scan, local)
	if cut && needle != "" {
		out["truncated"] = true
	}
	files := map[string]*pimFile{}
	reads := 0
	events := []pimEventView{}
	for _, v := range views {
		if len(events) == limit {
			out["truncated"] = true
			break
		}
		occ := v.Occurrence
		if !v.Recurring && v.Kind != "birthday" {
			occ = "" // a single event: its file is its id
		}
		e := pimEventView{ID: calID(v.Calendar, v.ID, occ), Calendar: pimRef{v.Calendar, names[v.Calendar]}, Title: v.Title,
			Start: v.Start, End: v.End, AllDay: v.AllDay, Timezone: v.Timezone, Location: v.Location, Recurring: v.Recurring,
			Rule: v.Rule, Occurrence: v.Occurrence, Alarms: v.Alarms, Exception: v.Exception, Birthday: v.Birthday}
		if e.Alarms == nil {
			e.Alarms = []int{}
		}
		if v.Kind == "birthday" {
			e.Kind = "birthday"
			if needle != "" && !strings.Contains(cardweb.Fold(v.Title), needle) {
				continue
			}
			events = append(events, e)
			continue
		}
		key := v.Calendar + "/" + v.ID + "\x00" + v.ETag
		f, seen := files[key]
		if !seen {
			if reads >= pimMaxFileReads {
				out["truncated"] = true
				break
			}
			reads++
			if obj, err := store.Get(pimstore.Calendar, user, v.Calendar, v.ID); err == nil {
				f = readPimFile(obj.Data, local)
			} else if pimGone(err) {
				out["hidden"], out["why"] = true, pimWhyCalendarVolume
				out["events"] = []pimEventView{}
				return pimJSON(out)
			}
			files[key] = f
		}
		if f != nil {
			t := f.of(v.Occurrence)
			e.Title, e.Location = t[0], t[1]
			e.Notes, e.NotesClipped = pimClip(t[2], pimMaxNotes)
			if needle != "" && !strings.Contains(cardweb.Fold(t[0]+"\n"+t[1]+"\n"+t[2]), needle) {
				continue
			}
		} else if needle != "" && !strings.Contains(cardweb.Fold(v.Title+"\n"+v.Location), needle) {
			continue
		}
		events = append(events, e)
	}
	out["events"] = events
	out["from"], out["to"] = from.Format(time.RFC3339), to.Format(time.RFC3339)
	return pimJSON(out)
}

// ---------- calendar_create / update / delete ----------

type pimEventArgs struct {
	ID       string        `json:"id"`
	Scope    string        `json:"scope"`
	ETag     string        `json:"etag"`
	Calendar string        `json:"calendar"`
	Title    *string       `json:"title"`
	Start    *string       `json:"start"`
	End      *string       `json:"end"`
	Duration *int          `json:"duration"`
	AllDay   *bool         `json:"allDay"`
	Timezone *string       `json:"timezone"`
	Location *string       `json:"location"`
	Notes    *string       `json:"notes"`
	Repeat   *recur.Simple `json:"repeat"`
	Alarms   *[]int        `json:"alarms"`
}

func (a pimEventArgs) times() bool {
	return a.Start != nil || a.End != nil || a.Duration != nil || a.AllDay != nil || a.Timezone != nil
}

// input is the calweb form of the fields given.
func (a pimEventArgs) input() calweb.Input {
	in := calweb.Input{Title: a.Title, Location: a.Location, Notes: a.Notes}
	if a.Timezone != nil {
		in.Timezone = *a.Timezone
	}
	if a.Repeat != nil {
		in.Repeat.Set = true
		if a.Repeat.Freq != "none" {
			s := *a.Repeat
			s.Custom = false
			if s.Interval == 0 {
				s.Interval = 1
			}
			in.Repeat.Value = &s
		}
	}
	if a.Alarms != nil {
		in.Alarms = calweb.Opt[[]int]{Set: true, Value: a.Alarms}
	}
	return in
}

// later adds a length to a start as calweb takes it: RFC 3339, a wall
// clock, or a date (days).
func later(start string, d time.Duration, days int) (string, error) {
	if t, err := time.Parse(time.RFC3339, start); err == nil {
		return t.Add(d).Format(time.RFC3339), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, start); err == nil {
			return t.Add(d).Format("2006-01-02T15:04:05"), nil
		}
	}
	if t, err := time.Parse("2006-01-02", start); err == nil {
		return t.AddDate(0, 0, days).Format("2006-01-02"), nil
	}
	return "", toolcatalog.Errorf("start: 2026-11-20T09:00 or 2026-11-20")
}

// pimFindCalendar is a calendar by id or name; "" is the first.
func (b *agentToolbox) pimFindCalendar(want string) (pimstore.Collection, error) {
	cols, err := b.s.opts.PIM.Collections(pimstore.Calendar, b.pimAccount())
	if err != nil {
		return pimstore.Collection{}, pimError(err, pimstore.Calendar)
	}
	want = strings.TrimSpace(want)
	if strings.EqualFold(want, calweb.BirthdaysID) || strings.EqualFold(want, "Geburtstage") {
		return pimstore.Collection{}, toolcatalog.Errorf("the birthdays come from the contacts and cannot be written")
	}
	for _, c := range cols {
		if want == "" || c.ID == want || strings.EqualFold(c.Name, want) {
			return c, nil
		}
	}
	return pimstore.Collection{}, toolcatalog.Errorf("there is no calendar %q; calendar_list names them", want)
}

type pimCreatePlan struct {
	col  pimstore.Collection
	in   calweb.Input
	data []byte
	uid  string
	view calweb.View
}

func (b *agentToolbox) planCreate(args json.RawMessage) (*pimCreatePlan, error) {
	if err := toolcatalog.ValidateArguments(b.tools["calendar_create"].params, args); err != nil {
		return nil, err
	}
	var a pimEventArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, toolcatalog.Errorf("invalid arguments")
	}
	if !b.pimMounted(pimstore.Calendar) {
		return nil, pimError(pimstore.ErrUnavailable, pimstore.Calendar)
	}
	col, err := b.pimFindCalendar(a.Calendar)
	if err != nil {
		return nil, err
	}
	in := a.input()
	if a.AllDay != nil {
		in.AllDay = *a.AllDay
	}
	in.Start = strings.TrimSpace(*a.Start)
	switch {
	case a.End != nil:
		in.End = *a.End
	case a.Duration != nil && in.AllDay:
		return nil, toolcatalog.Errorf("duration is for timed events; give end (the day after the last day) for an all-day event")
	case a.Duration != nil:
		if in.End, err = later(in.Start, time.Duration(*a.Duration)*time.Minute, 0); err != nil {
			return nil, err
		}
	}
	store := b.s.opts.PIM
	p := &pimCreatePlan{col: col, in: in, uid: calweb.NewUID()}
	if p.data, err = calweb.Build(in, p.uid, time.Now(), store.Zone()); err != nil {
		return nil, pimError(err, pimstore.Calendar)
	}
	if p.view, err = calweb.Open(p.data, "", store.Zone()); err != nil {
		return nil, pimError(err, pimstore.Calendar)
	}
	return p, nil
}

func (b *agentToolbox) calendarCreatePrepare(_ context.Context, chat *agent.Chat, _ string, args json.RawMessage, p *agent.Prepared) (string, error) {
	plan, err := b.planCreate(args)
	if err != nil {
		return "", err
	}
	en := pimEnglish(chat)
	p.Summary = pick(en, "Termin anlegen in ", "Add an event to ") + calName(plan.col) + ": " + eventLine(plan.view, en, true)
	return "", nil
}

func (b *agentToolbox) calendarCreate(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	plan, err := b.planCreate(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	name := plan.uid + ".ics"
	if _, err := b.s.opts.PIM.Put(pimstore.Calendar, b.pimAccount(), plan.col.ID, name, plan.data, pimstore.PutOptions{IfNoneMatch: true}); err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	v := plan.view
	return pimJSON(map[string]any{"id": calID(plan.col.ID, name, ""), "calendar": pimRef{plan.col.ID, calName(plan.col)},
		"title": v.Title, "start": v.Start, "end": v.End, "allDay": v.AllDay, "timezone": v.Timezone, "rule": v.Rule})
}

func calName(c pimstore.Collection) string { return ownCalendar(c, nil).Name }

// pimOpened is an event of the account as a tool finds it.
type pimOpened struct {
	col        pimstore.Collection
	name, occ  string
	obj        pimstore.Object
	view       calweb.View // the occurrence named, or the event itself
	series     bool
	args       pimEventArgs
	scope      recur.Scope
	scopeGiven bool
}

// fileRecurs reports whether a file is a series: a rule or extra dates
// (also one TitanOS cannot expand), or exceptions.
func fileRecurs(data []byte, local *time.Location) bool {
	f, err := recur.Read(data, recur.Options{Local: local})
	if err != nil || len(f.Series) != 1 {
		return false
	}
	sr := f.Series[0]
	if sr.Master == nil || len(sr.Overrides) > 0 || sr.Master.Rule != nil || len(sr.Master.RDates) > 0 {
		return true
	}
	props := sr.Master.Component.Props
	return props.Get(ical.PropRecurrenceRule) != nil || props.Get(ical.PropRecurrenceDates) != nil
}

// openForChange reads the event an update or delete names, with its
// scope checked. The etag of the arguments (Prepare put it there) must
// still be the file's.
func (b *agentToolbox) openForChange(tool string, args json.RawMessage) (*pimOpened, error) {
	if err := toolcatalog.ValidateArguments(b.tools[tool].params, args); err != nil {
		return nil, err
	}
	var a pimEventArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, toolcatalog.Errorf("invalid arguments")
	}
	col, name, occ, err := parseCalID(a.ID)
	if err != nil {
		return nil, err
	}
	if !b.pimMounted(pimstore.Calendar) {
		return nil, pimError(pimstore.ErrUnavailable, pimstore.Calendar)
	}
	store := b.s.opts.PIM
	c, err := store.GetCollection(pimstore.Calendar, b.pimAccount(), col)
	if err != nil {
		if errors.Is(err, pimstore.ErrNotFound) {
			err = errCalendarMissing
		}
		return nil, pimError(err, pimstore.Calendar)
	}
	v, obj, err := b.pimArea().openEvent(store, b.pimAccount(), col, name, occ)
	if err != nil {
		return nil, pimError(err, pimstore.Calendar)
	}
	if a.ETag != "" && !sameETag(a.ETag, obj.ETag) {
		return nil, pimError(pimstore.ErrPrecondition, pimstore.Calendar)
	}
	o := &pimOpened{col: c, name: name, occ: occ, obj: obj, view: v, series: fileRecurs(obj.Data, store.Zone()), args: a, scope: recur.ScopeAll}
	if o.series {
		s, ok := parseScope(a.Scope)
		if !ok {
			return nil, toolcatalog.Errorf("scope: this, following or all")
		}
		o.scope, o.scopeGiven = s, a.Scope != ""
		if s != recur.ScopeAll && occ == "" {
			return nil, toolcatalog.Errorf("scope %s needs the id of one occurrence (ending with @…) from calendar_search", a.Scope)
		}
	}
	return o, nil
}

// changeInput turns the fields given into the calweb form of a change:
// times are given together, so what was not given comes from the event
// (its length stays unless end or duration is given).
func (o *pimOpened) changeInput() (calweb.Input, error) {
	a := o.args
	in := a.input()
	if !a.times() {
		return in, nil
	}
	old := o.view
	in.AllDay = old.AllDay
	if a.AllDay != nil {
		in.AllDay = *a.AllDay
	}
	switch {
	case a.Start != nil:
		in.Start = strings.TrimSpace(*a.Start)
	case in.AllDay != old.AllDay:
		return in, toolcatalog.Errorf("give start when an event becomes all-day or timed")
	default:
		in.Start = old.Start
	}
	var err error
	switch {
	case a.End != nil:
		in.End = *a.End
	case a.Duration != nil && in.AllDay:
		return in, toolcatalog.Errorf("duration is for timed events; give end (the day after the last day) for an all-day event")
	case a.Duration != nil:
		in.End, err = later(in.Start, time.Duration(*a.Duration)*time.Minute, 0)
	case in.AllDay == old.AllDay && in.AllDay:
		s, _ := time.Parse("2006-01-02", old.Start)
		e, _ := time.Parse("2006-01-02", old.End)
		in.End, err = later(in.Start, 0, max(1, int(e.Sub(s).Hours()/24+0.5)))
	case in.AllDay == old.AllDay:
		s, _ := time.Parse(time.RFC3339, old.Start)
		e, _ := time.Parse(time.RFC3339, old.End)
		in.End, err = later(in.Start, e.Sub(s), 0)
	}
	return in, err
}

type pimUpdatePlan struct {
	*pimOpened
	res     recur.EditResult
	changed bool
	after   calweb.View
}

func (b *agentToolbox) planUpdate(args json.RawMessage) (*pimUpdatePlan, error) {
	o, err := b.openForChange("calendar_update", args)
	if err != nil {
		return nil, err
	}
	if o.series && !o.scopeGiven {
		return nil, toolcatalog.Errorf("this event is part of a series: give scope this, following or all (ask the person which)")
	}
	in, err := o.changeInput()
	if err != nil {
		return nil, err
	}
	local := b.s.opts.PIM.Zone()
	res, changed, err := calweb.Edit(o.obj.Data, o.occ, o.scope, in, calweb.EditOptions{Local: local, Now: time.Now(), NewUID: calweb.NewUID()})
	if err != nil {
		return nil, pimError(err, pimstore.Calendar)
	}
	p := &pimUpdatePlan{pimOpened: o, res: res, changed: changed, after: o.view}
	if changed {
		data := res.Data
		if res.New != nil {
			data = res.New
		}
		occ := o.occ
		if res.New != nil {
			occ = ""
		}
		if v, err := calweb.Open(data, occ, local); err == nil {
			p.after = v
		} else if v, err := calweb.Open(data, "", local); err == nil {
			p.after = v
		}
	}
	return p, nil
}

func (b *agentToolbox) calendarUpdatePrepare(_ context.Context, chat *agent.Chat, _ string, args json.RawMessage, p *agent.Prepared) (string, error) {
	plan, err := b.planUpdate(args)
	if err != nil {
		return "", err
	}
	withETag(p, plan.obj.ETag)
	en := pimEnglish(chat)
	head := pick(en, "Termin ändern in ", "Change an event in ") + calName(plan.col) + scopeWords(plan.pimOpened, plan.res.Scope, en) + ": " + plan.view.Title
	diff := eventDiff(plan.view, plan.after, en)
	switch {
	case !plan.changed || len(diff) == 0:
		p.Summary = head + pick(en, " – keine Änderung", " – nothing changes")
	default:
		p.Summary = head + " – " + strings.Join(diff, "; ")
	}
	if plan.res.DroppedExceptions > 0 {
		p.Summary += pick(en, fmt.Sprintf(" (%d geänderte Einzeltermine der Reihe gehen dabei verloren)", plan.res.DroppedExceptions),
			fmt.Sprintf(" (%d changed occurrences of the series are dropped)", plan.res.DroppedExceptions))
	}
	// A new rule for the whole series can cut it down to one event (repeat
	// none, count 1, until its first day): losing a series asks like
	// deleting it, over every right and work mode (Sicherheitsdurchsicht
	// S-13). "following" at the first occurrence is the whole series too.
	if plan.series && plan.changed && plan.res.Scope == recur.ScopeAll && plan.args.Repeat != nil {
		whole, _ := calweb.Open(plan.obj.Data, "", b.s.opts.PIM.Zone())
		p.Summary += pick(en, " – die Wiederholung der ganzen Reihe ändert sich (bisher ", " – the repetition of the whole series changes (so far ") +
			seriesCount(whole, en) + ", " + ruleWords(whole, en) + ")"
		return agent.ModeAsk, nil
	}
	return "", nil
}

// seriesCount is the number of occurrences of a series in words.
func seriesCount(whole calweb.View, en bool) string {
	if whole.Occurrences == nil {
		return pick(en, "ohne Ende", "without end")
	}
	return pick(en, fmt.Sprintf("%d Termine", *whole.Occurrences), fmt.Sprintf("%d occurrences", *whole.Occurrences))
}

func (b *agentToolbox) calendarUpdate(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	plan, err := b.planUpdate(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	store, user, col := b.s.opts.PIM, b.pimAccount(), plan.col.ID
	out := map[string]any{"id": calID(col, plan.name, plan.occ), "calendar": pimRef{col, calName(plan.col)}, "title": plan.after.Title,
		"start": plan.after.Start, "end": plan.after.End, "allDay": plan.after.AllDay, "rule": plan.after.Rule,
		"droppedExceptions": plan.res.DroppedExceptions, "scope": scopeName(plan.res.Scope), "changed": plan.changed}
	if !plan.changed {
		return pimJSON(out)
	}
	if plan.res.New != nil {
		newName := plan.res.NewUID + ".ics"
		if _, err := store.Put(pimstore.Calendar, user, col, newName, plan.res.New, pimstore.PutOptions{IfNoneMatch: true}); err != nil {
			return agent.Outcome{}, pimError(err, pimstore.Calendar)
		}
		out["newId"] = calID(col, newName, "")
		if _, err := store.Put(pimstore.Calendar, user, col, plan.name, plan.res.Data, pimstore.PutOptions{IfMatch: plan.obj.ETag}); err != nil {
			_ = store.DeleteObject(pimstore.Calendar, user, col, newName, "")
			return agent.Outcome{}, pimError(err, pimstore.Calendar)
		}
		return pimJSON(out)
	}
	if _, err := store.Put(pimstore.Calendar, user, col, plan.name, plan.res.Data, pimstore.PutOptions{IfMatch: plan.obj.ETag}); err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	return pimJSON(out)
}

type pimDeletePlan struct {
	*pimOpened
	res recur.EditResult
	// whole: the step removes a whole series (it always asks).
	whole bool
}

func (b *agentToolbox) planDelete(args json.RawMessage) (*pimDeletePlan, error) {
	o, err := b.openForChange("calendar_delete", args)
	if err != nil {
		return nil, err
	}
	res, err := recur.Delete(o.obj.Data, "", o.occ, o.scope, recur.EditOptions{Local: b.s.opts.PIM.Zone(), Now: time.Now(), ProdID: calweb.ProdID})
	if err != nil {
		return nil, pimError(err, pimstore.Calendar)
	}
	// "Ganze Reihe löschen fragt immer" (TECHNIK 6.2): scope all or none,
	// "following" at the first occurrence, the last one left – whatever
	// takes the whole series.
	whole := o.series && (res.Delete || res.Scope == recur.ScopeAll)
	return &pimDeletePlan{pimOpened: o, res: res, whole: whole}, nil
}

func (b *agentToolbox) calendarDeletePrepare(_ context.Context, chat *agent.Chat, _ string, args json.RawMessage, p *agent.Prepared) (string, error) {
	plan, err := b.planDelete(args)
	if err != nil {
		return "", err
	}
	withETag(p, plan.obj.ETag)
	en := pimEnglish(chat)
	p.Summary = pick(en, "Termin löschen in ", "Delete an event in ") + calName(plan.col) + ": " + eventLine(plan.view, en, false)
	if !plan.series {
		return "", nil
	}
	switch {
	case plan.whole:
		whole, _ := calweb.Open(plan.obj.Data, "", b.s.opts.PIM.Zone())
		p.Summary += pick(en, " – die ganze Reihe (", " – the whole series (") + seriesCount(whole, en) + ", " + ruleWords(whole, en) + ")"
		return agent.ModeAsk, nil
	case plan.res.Scope == recur.ScopeFollowing:
		p.Summary += pick(en, " – dieses und alle folgenden Vorkommen", " – this and all following occurrences")
	default:
		p.Summary += pick(en, " – nur dieses Vorkommen", " – only this occurrence")
	}
	return "", nil
}

func (b *agentToolbox) calendarDelete(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	plan, err := b.planDelete(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	store, user, col := b.s.opts.PIM, b.pimAccount(), plan.col.ID
	if plan.res.Delete {
		err = store.DeleteObject(pimstore.Calendar, user, col, plan.name, plan.obj.ETag)
	} else {
		_, err = store.Put(pimstore.Calendar, user, col, plan.name, plan.res.Data, pimstore.PutOptions{IfMatch: plan.obj.ETag})
	}
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	return pimJSON(map[string]any{"deleted": true, "scope": scopeName(plan.res.Scope)})
}

// ---------- contacts ----------

type pimContactView struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	First     string            `json:"first"`
	Last      string            `json:"last"`
	Org       string            `json:"org"`
	Phones    []pimValue        `json:"phones"`
	Emails    []pimValue        `json:"emails"`
	Addresses []pimAddressValue `json:"addresses"`
	Birthday  string            `json:"birthday"`
	Notes     string            `json:"notes"`
	Clipped   bool              `json:"notesClipped,omitempty"`
}

type pimValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type pimAddressValue struct {
	Type     string `json:"type"`
	Street   string `json:"street"`
	City     string `json:"city"`
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
}

// contactView is a contact for the model, without its picture.
func contactView(name string, c cardweb.Contact) pimContactView {
	v := pimContactView{ID: cardID(name), Name: c.Name, First: c.First, Last: c.Last, Org: c.Org, Birthday: c.Birthday,
		Phones: []pimValue{}, Emails: []pimValue{}, Addresses: []pimAddressValue{}}
	v.Notes, v.Clipped = pimClip(c.Notes, pimMaxNotes)
	for _, p := range c.Phones {
		v.Phones = append(v.Phones, pimValue{p.Type, p.Value})
	}
	for _, e := range c.Emails {
		v.Emails = append(v.Emails, pimValue{e.Type, e.Value})
	}
	for _, a := range c.Addresses {
		v.Addresses = append(v.Addresses, pimAddressValue{a.Type, a.Street, a.City, a.Postcode, a.Country})
	}
	return v
}

func (b *agentToolbox) contactsSearch(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var q struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &q); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	out := map[string]any{"contacts": []pimContactView{}, "truncated": false}
	if !b.pimMounted(pimstore.Contacts) {
		out["hidden"], out["why"] = true, pimWhyContactsVolume
		return pimJSON(out)
	}
	store, user := b.s.opts.PIM, b.pimAccount()
	objects, err := store.Objects(pimstore.Contacts, user, pimstore.DefaultBook)
	if pimGone(err) {
		out["hidden"], out["why"] = true, pimWhyContactsVolume
		return pimJSON(out)
	}
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Contacts)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, pimMaxContacts)
	items := cardweb.List(pimstore.DefaultBook, objects, q.Query, time.Now(), store.Zone())
	if len(items) > limit {
		items, out["truncated"] = items[:limit], true
	}
	list := []pimContactView{}
	for _, it := range items {
		c, _, err := b.pimArea().openContact(store, user, pimstore.DefaultBook, it.ID)
		if pimGone(err) {
			out["hidden"], out["why"] = true, pimWhyContactsVolume
			return pimJSON(out)
		}
		if err != nil {
			continue // gone meanwhile or unreadable
		}
		list = append(list, contactView(it.ID, c))
	}
	out["contacts"] = list
	return pimJSON(out)
}

type pimContactArgs struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}

func (b *agentToolbox) contactInput(tool string, args json.RawMessage) (cardweb.Input, pimContactArgs, error) {
	var in cardweb.Input
	var a pimContactArgs
	if err := toolcatalog.ValidateArguments(b.tools[tool].params, args); err != nil {
		return in, a, err
	}
	if json.Unmarshal(args, &in) != nil || json.Unmarshal(args, &a) != nil {
		return in, a, toolcatalog.Errorf("invalid arguments")
	}
	if !b.pimMounted(pimstore.Contacts) {
		return in, a, pimError(pimstore.ErrUnavailable, pimstore.Contacts)
	}
	return in, a, nil
}

func (b *agentToolbox) contactsCreatePrepare(_ context.Context, chat *agent.Chat, _ string, args json.RawMessage, p *agent.Prepared) (string, error) {
	in, _, err := b.contactInput("contacts_create", args)
	if err != nil {
		return "", err
	}
	data, err := cardweb.Build(in, calweb.NewUID(), time.Now())
	if err != nil {
		return "", pimError(err, pimstore.Contacts)
	}
	c, err := cardweb.Open(data)
	if err != nil {
		return "", pimError(err, pimstore.Contacts)
	}
	en := pimEnglish(chat)
	p.Summary = pick(en, "Kontakt anlegen: ", "Add a contact: ") + strings.Join(contactParts(c, en), ", ")
	return "", nil
}

func (b *agentToolbox) contactsCreate(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	in, _, err := b.contactInput("contacts_create", args)
	if err != nil {
		return agent.Outcome{}, err
	}
	w, err := b.pimArea().createContact(b.s.opts.PIM, b.pimAccount(), pimstore.DefaultBook, in)
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Contacts)
	}
	c, _, _ := b.pimArea().openContact(b.s.opts.PIM, b.pimAccount(), pimstore.DefaultBook, w.ID)
	return pimJSON(map[string]any{"id": cardID(w.ID), "name": c.Name})
}

type pimContactPlan struct {
	name    string
	obj     pimstore.Object
	before  cardweb.Contact
	data    []byte
	changed bool
}

func (b *agentToolbox) openContactFor(tool string, args json.RawMessage) (*pimContactPlan, cardweb.Input, error) {
	in, a, err := b.contactInput(tool, args)
	if err != nil {
		return nil, in, err
	}
	name, err := parseCardID(a.ID)
	if err != nil {
		return nil, in, err
	}
	c, obj, err := b.pimArea().openContact(b.s.opts.PIM, b.pimAccount(), pimstore.DefaultBook, name)
	if err != nil {
		return nil, in, pimError(err, pimstore.Contacts)
	}
	if a.ETag != "" && !sameETag(a.ETag, obj.ETag) {
		return nil, in, pimError(pimstore.ErrPrecondition, pimstore.Contacts)
	}
	return &pimContactPlan{name: name, obj: obj, before: c}, in, nil
}

func (b *agentToolbox) planContactUpdate(args json.RawMessage) (*pimContactPlan, error) {
	p, in, err := b.openContactFor("contacts_update", args)
	if err != nil {
		return nil, err
	}
	if p.data, p.changed, err = cardweb.Edit(p.obj.Data, in, time.Now()); err != nil {
		return nil, pimError(err, pimstore.Contacts)
	}
	return p, nil
}

func (b *agentToolbox) contactsUpdatePrepare(_ context.Context, chat *agent.Chat, _ string, args json.RawMessage, p *agent.Prepared) (string, error) {
	plan, err := b.planContactUpdate(args)
	if err != nil {
		return "", err
	}
	withETag(p, plan.obj.ETag)
	en := pimEnglish(chat)
	head := pick(en, "Kontakt ändern: ", "Change a contact: ") + plan.before.Name
	after := plan.before
	if plan.changed {
		if c, err := cardweb.Open(plan.data); err == nil {
			after = c
		}
	}
	diff := contactDiff(plan.before, after, en)
	if len(diff) == 0 {
		p.Summary = head + pick(en, " – keine Änderung", " – nothing changes")
	} else {
		p.Summary = head + " – " + strings.Join(diff, "; ")
	}
	return "", nil
}

func (b *agentToolbox) contactsUpdate(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	plan, err := b.planContactUpdate(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	name := plan.before.Name
	if plan.changed {
		if _, err := b.s.opts.PIM.Put(pimstore.Contacts, b.pimAccount(), pimstore.DefaultBook, plan.name, plan.data, pimstore.PutOptions{IfMatch: plan.obj.ETag}); err != nil {
			return agent.Outcome{}, pimError(err, pimstore.Contacts)
		}
		if c, err := cardweb.Open(plan.data); err == nil {
			name = c.Name
		}
	}
	return pimJSON(map[string]any{"id": cardID(plan.name), "name": name, "changed": plan.changed})
}

func (b *agentToolbox) contactsDeletePrepare(_ context.Context, chat *agent.Chat, _ string, args json.RawMessage, p *agent.Prepared) (string, error) {
	plan, _, err := b.openContactFor("contacts_delete", args)
	if err != nil {
		return "", err
	}
	withETag(p, plan.obj.ETag)
	en := pimEnglish(chat)
	p.Summary = pick(en, "Kontakt löschen: ", "Delete a contact: ") + strings.Join(contactParts(plan.before, en), ", ")
	return "", nil
}

func (b *agentToolbox) contactsDelete(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	plan, _, err := b.openContactFor("contacts_delete", args)
	if err != nil {
		return agent.Outcome{}, err
	}
	if err := b.s.opts.PIM.DeleteObject(pimstore.Contacts, b.pimAccount(), pimstore.DefaultBook, plan.name, plan.obj.ETag); err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Contacts)
	}
	return pimJSON(map[string]any{"deleted": true})
}

// ---------- summaries for the approval (whole, never cut) ----------

func pick(en bool, de, english string) string {
	if en {
		return english
	}
	return de
}

// pimEnglish decides the language of the summaries from the person's last
// message: English when it reads English, German otherwise (the page has
// no locale on the server; TitanOS's own language is German).
func pimEnglish(chat *agent.Chat) bool {
	if chat == nil {
		return false
	}
	for i := len(chat.Entries) - 1; i >= 0; i-- {
		e := chat.Entries[i]
		if e.Role != "user" {
			continue
		}
		en, de := 0, 0
		for _, w := range strings.FieldsFunc(strings.ToLower(e.Content), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r == 'ä' || r == 'ö' || r == 'ü' || r == 'ß' || r == '\'')
		}) {
			switch w {
			case "the", "a", "an", "my", "please", "add", "create", "delete", "remove", "change", "move", "what", "when", "tomorrow", "today",
				"appointment", "meeting", "event", "contact", "calendar", "at", "on", "for", "with", "is", "are", "to", "of", "and", "next", "week":
				en++
			case "der", "die", "das", "ein", "eine", "einen", "mein", "meine", "bitte", "trag", "trage", "termin", "morgen", "heute", "kontakt",
				"kalender", "um", "am", "für", "mit", "ist", "sind", "und", "was", "wann", "lösch", "lösche", "ändere", "nächste", "woche", "uhr", "im", "von":
				de++
			}
		}
		return en > de
	}
	return false
}

var (
	weekdaysDE = []string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
	monthsEN   = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

func dayWords(t time.Time, en bool) string {
	if en {
		return fmt.Sprintf("%s %d %s %d", t.Weekday().String()[:3], t.Day(), monthsEN[t.Month()-1], t.Year())
	}
	return fmt.Sprintf("%s %02d.%02d.%d", weekdaysDE[t.Weekday()], t.Day(), int(t.Month()), t.Year())
}

// whenWords is the time of an event: "Do 20.11.2026 09:00–09:45
// (Europe/Berlin)", all-day "Do 20.11.2026, ganztägig".
func whenWords(v calweb.View, en bool) string {
	if v.AllDay {
		s, err1 := time.Parse("2006-01-02", v.Start)
		e, err2 := time.Parse("2006-01-02", v.End)
		if err1 != nil || err2 != nil {
			return v.Start
		}
		day := dayWords(s, en)
		if last := e.AddDate(0, 0, -1); last.After(s) {
			day += " – " + dayWords(last, en)
		}
		return day + pick(en, ", ganztägig", ", all day")
	}
	s, err1 := time.Parse(time.RFC3339, v.Start)
	e, err2 := time.Parse(time.RFC3339, v.End)
	if err1 != nil || err2 != nil {
		return v.Start
	}
	out := dayWords(s, en) + " " + s.Format("15:04")
	if s.Format("2006-01-02") == e.Format("2006-01-02") {
		out += "–" + e.Format("15:04")
	} else {
		out += " – " + dayWords(e, en) + " " + e.Format("15:04")
	}
	if v.Timezone != "" {
		out += " (" + v.Timezone + ")"
	}
	return out
}

func ruleWords(v calweb.View, en bool) string {
	switch {
	case v.Rule != nil && v.Rule.Text != nil:
		return pick(en, "wiederholt sich ", "repeats ") + v.Rule.Text[pick(en, "de", "en")]
	case v.Recurring:
		return pick(en, "wiederholt sich (Regel hier nicht darstellbar)", "repeats (rule not shown here)")
	}
	return pick(en, "wiederholt nie", "does not repeat")
}

func minutesWords(m int, en bool) string {
	switch {
	case m == 0:
		return pick(en, "zu Beginn", "at the start")
	case m%1440 == 0:
		n := m / 1440
		if n == 1 {
			return pick(en, "1 Tag", "1 day")
		}
		return strconv.Itoa(n) + pick(en, " Tage", " days")
	case m%60 == 0:
		return strconv.Itoa(m/60) + " h"
	}
	return strconv.Itoa(m) + " min"
}

func alarmWords(alarms []int, en bool) string {
	if len(alarms) == 0 {
		return pick(en, "keine Erinnerung", "no reminder")
	}
	parts := []string{}
	for _, m := range alarms {
		w := minutesWords(m, en)
		if m > 0 {
			w += pick(en, " vorher", " before")
		}
		parts = append(parts, w)
	}
	if len(parts) == 1 {
		return pick(en, "Erinnerung ", "reminder ") + parts[0]
	}
	return pick(en, "Erinnerungen ", "reminders ") + strings.Join(parts, ", ")
}

func quoted(s string, en bool) string { return pick(en, "„"+s+"“", "\""+s+"\"") }

// eventLine is an event in one sentence; full adds repetition, reminders
// and notes (a new event appears in the chat whole).
func eventLine(v calweb.View, en, full bool) string {
	parts := []string{v.Title, whenWords(v, en)}
	if v.Title == "" {
		parts[0] = pick(en, "(ohne Titel)", "(no title)")
	}
	if v.Location != "" {
		parts = append(parts, pick(en, "Ort ", "place ")+v.Location)
	}
	if full {
		parts = append(parts, ruleWords(v, en), alarmWords(v.Alarms, en))
		if v.Notes != nil && *v.Notes != "" {
			parts = append(parts, pick(en, "Notiz: ", "notes: ")+quoted(*v.Notes, en))
		}
	}
	return strings.Join(parts, ", ")
}

func scopeWords(o *pimOpened, applied recur.Scope, en bool) string {
	if !o.series {
		return ""
	}
	switch applied {
	case recur.ScopeThis:
		return pick(en, " (nur dieses Vorkommen)", " (only this occurrence)")
	case recur.ScopeFollowing:
		return pick(en, " (dieses und alle folgenden Vorkommen)", " (this and all following occurrences)")
	}
	return pick(en, " (die ganze Reihe)", " (the whole series)")
}

// eventDiff names what changes, old → new.
func eventDiff(old, now calweb.View, en bool) []string {
	var out []string
	add := func(de, english, a, b string) {
		if a != b {
			out = append(out, pick(en, de, english)+": "+quoted(a, en)+" → "+quoted(b, en))
		}
	}
	add("Titel", "title", old.Title, now.Title)
	add("Zeit", "time", whenWords(old, en), whenWords(now, en))
	add("Ort", "place", old.Location, now.Location)
	notes := func(v calweb.View) string {
		if v.Notes == nil {
			return ""
		}
		return *v.Notes
	}
	add("Notiz", "notes", notes(old), notes(now))
	add("Wiederholung", "repetition", ruleWords(old, en), ruleWords(now, en))
	add("Erinnerung", "reminder", alarmWords(old.Alarms, en), alarmWords(now.Alarms, en))
	return out
}

func kindWords(t string, en bool) string {
	de := map[string]string{"mobile": "mobil", "home": "privat", "work": "geschäftlich", "other": "sonstige"}
	if en {
		return t
	}
	if w, ok := de[t]; ok {
		return w
	}
	return t
}

func addressWords(a cardweb.Address) string {
	parts := []string{}
	for _, s := range []string{strings.ReplaceAll(a.Street, "\n", ", "), strings.TrimSpace(a.Postcode + " " + a.City), a.Region, a.Country} {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func birthdayWords(s string, en bool) string {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if en {
			return t.Format("2 Jan 2006")
		}
		return t.Format("02.01.2006")
	}
	if strings.HasPrefix(s, "--") && len(s) == 7 {
		if en {
			return s[2:]
		}
		return s[5:] + "." + s[2:4] + "."
	}
	return s
}

// contactFields are a contact's parts as words, by field.
func contactFields(c cardweb.Contact, en bool) [][2]string {
	var out [][2]string
	add := func(field, v string) {
		if v != "" {
			out = append(out, [2]string{field, v})
		}
	}
	add(pick(en, "Name", "name"), c.Name)
	add(pick(en, "Firma", "company"), c.Org)
	for _, p := range c.Phones {
		add(pick(en, "Telefon", "phone"), "("+kindWords(p.Type, en)+") "+p.Value)
	}
	for _, e := range c.Emails {
		add(pick(en, "E-Mail", "e-mail"), "("+kindWords(e.Type, en)+") "+e.Value)
	}
	for _, a := range c.Addresses {
		add(pick(en, "Adresse", "address"), "("+kindWords(a.Type, en)+") "+addressWords(a))
	}
	if c.Birthday != "" {
		add(pick(en, "Geburtstag", "birthday"), birthdayWords(c.Birthday, en))
	}
	if c.Notes != "" {
		add(pick(en, "Notiz", "notes"), quoted(c.Notes, en))
	}
	return out
}

func contactParts(c cardweb.Contact, en bool) []string {
	var out []string
	for i, f := range contactFields(c, en) {
		if i == 0 && f[0] == pick(en, "Name", "name") {
			out = append(out, f[1])
			continue
		}
		out = append(out, f[0]+" "+f[1])
	}
	return out
}

// contactDiff names what changes per field, old → new.
func contactDiff(old, now cardweb.Contact, en bool) []string {
	group := func(c cardweb.Contact) (map[string]string, []string) {
		m, order := map[string]string{}, []string{}
		for _, f := range contactFields(c, en) {
			if _, ok := m[f[0]]; !ok {
				order = append(order, f[0])
				m[f[0]] = f[1]
			} else {
				m[f[0]] += ", " + f[1]
			}
		}
		return m, order
	}
	a, order := group(old)
	bm, order2 := group(now)
	for _, k := range order2 {
		if !slices.Contains(order, k) {
			order = append(order, k)
		}
	}
	none := pick(en, "(leer)", "(empty)")
	var out []string
	for _, k := range order {
		if a[k] == bm[k] {
			continue
		}
		x, y := a[k], bm[k]
		if x == "" {
			x = none
		}
		if y == "" {
			y = none
		}
		out = append(out, k+": "+x+" → "+y)
	}
	return out
}
