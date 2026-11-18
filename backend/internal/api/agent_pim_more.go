package api

// More of calendar and contacts for the agent (1.13): the rights per kind
// of action and the drafts of the 1.11 tools (agent_pim.go stays as it
// was), free time computed in Go (calendar_free), duplicate candidates
// (contacts_duplicates) and the reminders (agent_tasks.go).

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// pimMoreTools are the tools of 1.13 besides mail; none without the store.
func (b *agentToolbox) pimMoreTools() []*agentTool {
	if b.s.opts.PIM == nil {
		return nil
	}
	b.pimActions()
	out := []*agentTool{
		{name: "calendar_free", area: agent.AreaCalendar, kind: agent.KindRead, untrusted: true,
			description: "Free time in the person's calendars between from and to (default today to 7 days later, at most 31 days): gaps of at least minutes within the hours of the day, weekends only when asked. TitanOS computes it; all-day events do not block.",
			params: schema(map[string]any{
				"from": str("Start: 2026-11-20 or 2026-11-20T08:00 (zone of the NAS)."), "to": str("End (exclusive), like from."),
				"minutes":   map[string]any{"type": "integer", "minimum": 5, "maximum": 1440, "description": "Length needed (default 60)."},
				"dayStart":  str("Start of the day HH:MM (default 08:00)."),
				"dayEnd":    str("End of the day HH:MM (default 18:00)."),
				"weekends":  map[string]any{"type": "boolean", "description": "Also Saturday and Sunday."},
				"calendars": map[string]any{"type": "array", "maxItems": 60, "items": str("Calendar id or name."), "description": "Only these calendars (default all)."},
			}), own: b.calendarFree, prepare: b.pimCheckArgs},
		{name: "contacts_duplicates", area: agent.AreaContacts, kind: agent.KindRead, untrusted: true,
			description: "Contacts that look like the same person: the same e-mail address or phone number. Merging is done by the person in Kontakte.",
			params:      schema(nil), own: b.contactsDuplicates, prepare: b.pimCheckArgs},
	}
	if b.user.Name != agent.AssistProfile {
		out = append(out, b.taskTools()...)
	}
	return out
}

// pimActions gives the writing tools of 1.11 their kind of action, their
// draft and their line in the activity log. Deletes have no draft: they
// always ask.
func (b *agentToolbox) pimActions() {
	set := func(name, key string, draft func(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error), logged func(json.RawMessage) string) {
		for _, t := range b.ordered {
			if t.name == name {
				t.action, t.draft, t.logged = key, draft, logged
			}
		}
	}
	// pimTools were added before: find them in b.ordered (b.tools is
	// filled by add, which runs before this for them).
	byID := func(args json.RawMessage) string { return argString(args, "id") }
	set("calendar_create", agent.ActEventCreate, b.calendarCreateDraft, func(args json.RawMessage) string { return argString(args, "calendar") })
	set("calendar_update", agent.ActEventUpdate, b.calendarUpdateDraft, byID)
	set("calendar_delete", agent.ActEventDelete, nil, byID)
	set("contacts_create", agent.ActContactCreate, b.contactDraft("contacts_create"), nil)
	set("contacts_update", agent.ActContactUpdate, b.contactDraft("contacts_update"), byID)
	set("contacts_delete", agent.ActContactDelete, nil, byID)
}

// ---------- drafts of events and contacts ----------

// rawFields are the arguments as plain values.
func rawFields(args json.RawMessage) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(args, &out)
	delete(out, "etag")
	return out
}

// calendarCreateDraft is the event for the event window of Kalender:
// title, start, end, allDay, location, notes, calendar, alarms, rrule
// (the repetition in the form of the web API, recur.Simple) and timezone.
func (b *agentToolbox) calendarCreateDraft(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	plan, err := b.planCreate(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	a := rawFields(args)
	v := plan.view
	fields := map[string]any{"title": v.Title, "start": v.Start, "end": v.End, "allDay": v.AllDay, "timezone": v.Timezone,
		"location": v.Location, "calendar": plan.col.ID, "alarms": v.Alarms}
	if notes, ok := a["notes"].(string); ok {
		fields["notes"] = notes
	}
	if r, ok := a["repeat"]; ok {
		fields["rrule"] = r
	}
	return draftOutcome(&agent.Draft{App: "calendar", Kind: "event", Fields: fields}, "event")
}

// calendarUpdateDraft names the event (cal, id, occurrence) and only the
// fields that change; the window opens the event and puts them in.
func (b *agentToolbox) calendarUpdateDraft(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	if err := toolcatalog.ValidateArguments(b.tools["calendar_update"].params, args); err != nil {
		return agent.Outcome{}, err
	}
	col, name, occ, err := parseCalID(argString(args, "id"))
	if err != nil {
		return agent.Outcome{}, err
	}
	if _, err := b.s.opts.PIM.Get(pimstore.Calendar, b.pimAccount(), col, name); err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	fields := rawFields(args)
	delete(fields, "id")
	if r, ok := fields["repeat"]; ok {
		delete(fields, "repeat")
		fields["rrule"] = r
	}
	fields["cal"], fields["id"], fields["occurrence"] = col, name, occ
	return draftOutcome(&agent.Draft{App: "calendar", Kind: "event", Fields: fields}, "event change")
}

// contactDraft is the contact for the editor of Kontakte: name (first and
// last together, and each), emails, phones, org, addresses, notes and
// birthday; for a change also book and id.
func (b *agentToolbox) contactDraft(tool string) func(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
	return func(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
		if _, _, err := b.contactInput(tool, args); err != nil {
			return agent.Outcome{}, err
		}
		fields := rawFields(args)
		first, _ := fields["first"].(string)
		last, _ := fields["last"].(string)
		if n := strings.TrimSpace(first + " " + last); n != "" {
			fields["name"] = n
		}
		if tool == "contacts_update" {
			id, err := parseCardID(argString(args, "id"))
			if err != nil {
				return agent.Outcome{}, err
			}
			if _, err := b.s.opts.PIM.Get(pimstore.Contacts, b.pimAccount(), pimstore.DefaultBook, id); err != nil {
				return agent.Outcome{}, pimError(err, pimstore.Contacts)
			}
			fields["book"], fields["id"] = pimstore.DefaultBook, id
		}
		return draftOutcome(&agent.Draft{App: "contacts", Kind: "contact", Fields: fields}, "contact")
	}
}

// ---------- calendar_free ----------

type freeArgs struct {
	From, To, DayStart, DayEnd string
	Minutes                    int
	Weekends                   bool
	Calendars                  []string
}

type freeSlot struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Minutes int    `json:"minutes"`
}

func dayClock(s, def string) (int, error) {
	if s == "" {
		s = def
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, toolcatalog.Errorf("dayStart and dayEnd are HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}

func (b *agentToolbox) calendarFree(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var a freeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	out := map[string]any{"free": []freeSlot{}}
	store, user := b.s.opts.PIM, b.pimAccount()
	if !b.pimMounted(pimstore.Calendar) {
		out["hidden"], out["why"] = true, pimWhyCalendarVolume
		return pimJSON(out)
	}
	local := store.Zone()
	q := calendarSearchArgs{From: a.From, To: a.To}
	if q.To == "" && q.From == "" {
		now := time.Now().In(local)
		q.From = now.Format("2006-01-02T15:04")
		q.To = now.AddDate(0, 0, 7).Format(time.DateOnly)
	}
	from, to, err := q.window(local, time.Now())
	if err != nil {
		return agent.Outcome{}, err
	}
	if to.Sub(from) > 31*24*time.Hour {
		return agent.Outcome{}, toolcatalog.Errorf("at most 31 days at once")
	}
	need := a.Minutes
	if need <= 0 {
		need = 60
	}
	dayStart, err := dayClock(a.DayStart, "08:00")
	if err != nil {
		return agent.Outcome{}, err
	}
	dayEnd, err := dayClock(a.DayEnd, "18:00")
	if err != nil {
		return agent.Outcome{}, err
	}
	if dayEnd <= dayStart {
		return agent.Outcome{}, toolcatalog.Errorf("dayEnd must be after dayStart")
	}
	cols, err := store.Collections(pimstore.Calendar, user)
	if pimGone(err) {
		out["hidden"], out["why"] = true, pimWhyCalendarVolume
		return pimJSON(out)
	}
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	type span struct{ from, to time.Time }
	var busy []span
	for _, c := range cols {
		if len(a.Calendars) > 0 && !containsFold(a.Calendars, c.ID, ownCalendar(c, nil).Name) {
			continue
		}
		occs, cut, err := store.Query(user, c.ID, from, to, pimMaxScan)
		if err != nil {
			return agent.Outcome{}, pimError(err, pimstore.Calendar)
		}
		if cut {
			out["truncated"] = true
		}
		for _, v := range calweb.Views(c.ID, occs, local) {
			if v.AllDay {
				continue
			}
			s, err1 := time.Parse(time.RFC3339, v.Start)
			e, err2 := time.Parse(time.RFC3339, v.End)
			if err1 == nil && err2 == nil && e.After(s) {
				busy = append(busy, span{s, e})
			}
		}
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].from.Before(busy[j].from) })
	var free []freeSlot
	for day := time.Date(from.In(local).Year(), from.In(local).Month(), from.In(local).Day(), 0, 0, 0, 0, local); day.Before(to) && len(free) < 100; day = day.AddDate(0, 0, 1) {
		if wd := day.Weekday(); !a.Weekends && (wd == time.Saturday || wd == time.Sunday) {
			continue
		}
		start := day.Add(time.Duration(dayStart) * time.Minute)
		end := day.Add(time.Duration(dayEnd) * time.Minute)
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		cur := start
		for _, s := range busy {
			if !s.to.After(cur) || !s.from.Before(end) {
				continue
			}
			if s.from.Sub(cur) >= time.Duration(need)*time.Minute {
				free = append(free, freeSlot{cur.In(local).Format(time.RFC3339), s.from.In(local).Format(time.RFC3339), int(s.from.Sub(cur).Minutes())})
			}
			if s.to.After(cur) {
				cur = s.to
			}
		}
		if end.Sub(cur) >= time.Duration(need)*time.Minute {
			free = append(free, freeSlot{cur.In(local).Format(time.RFC3339), end.In(local).Format(time.RFC3339), int(end.Sub(cur).Minutes())})
		}
	}
	if free == nil {
		free = []freeSlot{}
	}
	out["free"], out["minutes"] = free, need
	return pimJSON(out)
}

func containsFold(list []string, values ...string) bool {
	for _, w := range list {
		for _, v := range values {
			if strings.EqualFold(strings.TrimSpace(w), v) {
				return true
			}
		}
	}
	return false
}

// ---------- contacts_duplicates ----------

// duplicateFinder finds contacts that may be the same person. 1.13: a
// simple version of the agent (the same e-mail address or phone number);
// TO BE REPLACED after the merge of feat/v1.13-contacts by the candidates
// of Kontakte (the duplicate function of wt-113k), behind this interface.
type duplicateFinder interface {
	Duplicates(account string) ([]duplicateGroup, error)
}

type duplicateGroup struct {
	Reason   string            `json:"reason"` // email, phone
	Value    string            `json:"value"`
	Contacts []duplicateMember `json:"contacts"`
}

type duplicateMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// simpleDuplicates reads the index of the store (RAM): no file is read.
type simpleDuplicates struct{ store *pimstore.Store }

func (d simpleDuplicates) Duplicates(account string) ([]duplicateGroup, error) {
	list, err := d.store.Objects(pimstore.Contacts, account, pimstore.DefaultBook)
	if err != nil {
		return nil, err
	}
	type key struct{ reason, value string }
	groups := map[key][]duplicateMember{}
	var order []key
	add := func(k key, m duplicateMember) {
		for _, have := range groups[k] {
			if have.ID == m.ID {
				return
			}
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], m)
	}
	for _, o := range list {
		if o.Group {
			continue
		}
		m := duplicateMember{ID: cardID(o.Name), Name: o.FN}
		for _, e := range o.Emails {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
				add(key{"email", e}, m)
			}
		}
		for _, p := range o.Phones {
			// The last nine digits: +49 171 … and 0171 … are the same number.
			digits := strings.TrimPrefix(p, "+")
			if len(digits) >= 6 {
				if len(digits) > 9 {
					digits = digits[len(digits)-9:]
				}
				add(key{"phone", digits}, m)
			}
		}
	}
	out := []duplicateGroup{}
	for _, k := range order {
		if len(groups[k]) > 1 && len(out) < 50 {
			out = append(out, duplicateGroup{Reason: k.reason, Value: k.value, Contacts: groups[k]})
		}
	}
	return out, nil
}

func (b *agentToolbox) duplicates() duplicateFinder { return simpleDuplicates{b.s.opts.PIM} }

func (b *agentToolbox) contactsDuplicates(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
	out := map[string]any{"groups": []duplicateGroup{}}
	if !b.pimMounted(pimstore.Contacts) {
		out["hidden"], out["why"] = true, pimWhyContactsVolume
		return pimJSON(out)
	}
	groups, err := b.duplicates().Duplicates(b.pimAccount())
	if pimGone(err) {
		out["hidden"], out["why"] = true, pimWhyContactsVolume
		return pimJSON(out)
	}
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Contacts)
	}
	out["groups"] = groups
	return pimJSON(out)
}
