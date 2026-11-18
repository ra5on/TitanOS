package davsync

import (
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Reminder lists (1.13): iOS keeps the lists of its Reminders app as
// CalDAV calendars with supported-calendar-component-set VTODO, side by
// side with the calendars in /caldav/<account>/calendars/. The store keeps
// them as a kind of its own (store.Tasks); here a list is a calendar
// collection like any other – same paths, properties, reports and rules –
// but for its component and the kind it is stored under. A path below the
// home names a list or a calendar by its id (store.CalDAVKind).

// family is the protocol a kind is served with: reminder lists are
// calendars.
func family(kind store.Kind) store.Kind {
	if kind == store.Tasks {
		return store.Calendar
	}
	return kind
}

// component is the component a collection of the kind holds.
func component(kind store.Kind) string {
	if kind == store.Tasks {
		return "VTODO"
	}
	return "VEVENT"
}

// resolveKind makes a CalDAV path of a collection or an entry name the
// kind that holds the id (RAM after the first read). An id neither kind
// knows stays a calendar (404, or a new one with MKCALENDAR). When a kind
// cannot be read now the answer is 503, never 404: the id might be one of
// its lists, and a 404 makes iOS delete its copy.
func (rq *request) resolveKind() bool {
	t := &rq.t
	if t.kind != store.Calendar || t.level < levelCollection || rq.h.Store == nil {
		return true
	}
	kind, err := rq.h.Store.CalDAVKind(t.account, t.collection)
	switch {
	case err == nil:
		t.kind = kind
	case notFound(err):
	default:
		rq.fail(err)
		return false
	}
	return true
}

// homeCollections are the collections of the home set: the calendars,
// and for CalDAV the reminder lists after them. Both or nothing: without
// the lists the home is 503, never a home without them.
func (rq *request) homeCollections(st *store.Store) ([]store.Collection, error) {
	cols, err := st.Collections(rq.t.kind, rq.t.account)
	if err != nil || rq.t.kind != store.Calendar {
		return cols, err
	}
	lists, err := st.Collections(store.Tasks, rq.t.account)
	if err != nil {
		return nil, err
	}
	return append(cols, lists...), nil
}

// excludes reports whether a filter matches nothing in a collection of
// the kind: a calendar holds no VTODO, a list no VEVENT.
func (f *calFilter) excludes(kind store.Kind) bool {
	if f.none {
		return true
	}
	if kind == store.Tasks {
		return f.event || f.noTodo
	}
	return f.todo || f.noEvent
}

// todoFilter is what TitanOS evaluates below comp-filter VTODO (RFC 4791
// 9.7) from the index: a time-range on the reminder (9.9, by DTSTART and
// DUE, else COMPLETED), prop-filter COMPLETED (is-not-defined, as iOS asks
// for the open ones, or a time-range), STATUS and UID with text-match.
// Anything else matches every reminder, and a recurring one matches every
// time-range (the client filters again).
type todoFilter struct {
	ranges    [][2]time.Time
	completed int // 0 any, 1 not defined, 2 defined
	doneIn    [][2]time.Time
	status    []textMatch
	noStatus  bool
}

const (
	completedAny = iota
	completedUndefined
	completedDefined
)

// todoFilters reads the filters below comp-filter VTODO.
func (f *calFilter) todoFilters(c *node) *filterError {
	parseRanges := func(n *node) ([][2]time.Time, *filterError) {
		var out [][2]time.Time
		for _, tr := range n.all(calName("time-range")) {
			start, ok1 := parseUTC(tr.attr("start"))
			end, ok2 := parseUTC(tr.attr("end"))
			if !ok1 || !ok2 || (start.IsZero() && end.IsZero()) || (!start.IsZero() && !end.IsZero() && !end.After(start)) {
				return nil, validFilter()
			}
			out = append(out, [2]time.Time{start, end})
		}
		if len(out) > 1 {
			return nil, validFilter()
		}
		return out, nil
	}
	var fe *filterError
	if f.task.ranges, fe = parseRanges(c); fe != nil {
		return fe
	}
	terms := 0
	for _, p := range c.all(calName("prop-filter")) {
		name := strings.ToUpper(p.attr("name"))
		undefined := p.child(calName("is-not-defined")) != nil
		matches := p.all(calName("text-match"))
		terms += 1 + len(matches)
		if terms > maxFilterTerms {
			return tooComplex(calName)
		}
		switch name {
		case "UID":
			if undefined {
				f.noUID = true
				continue
			}
			for _, m := range matches {
				tm, fe := parseTextMatch(m, "i;ascii-casemap", calName)
				if fe != nil {
					return fe
				}
				f.uids = append(f.uids, tm)
			}
		case "COMPLETED":
			if undefined {
				f.task.completed = completedUndefined
				continue
			}
			f.task.completed = completedDefined
			if f.task.doneIn, fe = parseRanges(p); fe != nil {
				return fe
			}
		case "STATUS":
			if undefined {
				f.task.noStatus = true
				continue
			}
			for _, m := range matches {
				tm, fe := parseTextMatch(m, "i;ascii-casemap", calName)
				if fe != nil {
					return fe
				}
				f.task.status = append(f.task.status, tm)
			}
		}
	}
	return nil
}

// match evaluates the filter on the index entry of a reminder; an entry
// the index could not read matches (the client filters again).
func (tf *todoFilter) match(e store.Entry, loc *time.Location) bool {
	ti := e.Task
	if ti == nil {
		return true
	}
	switch tf.completed {
	case completedUndefined:
		if !ti.Completed.IsZero() {
			return false
		}
	case completedDefined:
		if ti.Completed.IsZero() {
			return false
		}
		for _, r := range tf.doneIn {
			if !within(ti.Completed, r) {
				return false
			}
		}
	}
	if tf.noStatus && ti.Status != "" {
		return false
	}
	for _, tm := range tf.status {
		if ti.Status == "" || !tm.matches(ti.Status) {
			return false
		}
	}
	for _, r := range tf.ranges {
		if !ti.Recurs && !todoOverlaps(ti, loc, r) {
			return false
		}
	}
	return true
}

// within reports start <= t < end with open sides (COMPLETED, RFC 4791
// 9.9: start <= COMPLETED and end >= COMPLETED).
func within(t time.Time, r [2]time.Time) bool {
	return (r[0].IsZero() || !t.Before(r[0])) && (r[1].IsZero() || !t.After(r[1]))
}

// todoOverlaps is the table of RFC 4791 9.9 for VTODO with the dates the
// index has (no DURATION, no CREATED: those cases match).
func todoOverlaps(ti *store.TaskInfo, loc *time.Location, r [2]time.Time) bool {
	start, due := ti.StartAt(loc), ti.DueAt(loc)
	before := func(a, b time.Time) bool { return a.IsZero() || b.IsZero() || a.Before(b) } // open sides
	notAfter := func(a, b time.Time) bool { return a.IsZero() || b.IsZero() || !a.After(b) }
	switch {
	case !start.IsZero() && !due.IsZero():
		return (before(r[0], due) || notAfter(r[0], start)) && (before(start, r[1]) || notAfter(due, r[1]))
	case !start.IsZero():
		return notAfter(r[0], start) && before(start, r[1])
	case !due.IsZero():
		return before(r[0], due) && notAfter(due, r[1])
	case !ti.Completed.IsZero():
		return notAfter(r[0], ti.Completed) && notAfter(ti.Completed, r[1])
	}
	return true
}
