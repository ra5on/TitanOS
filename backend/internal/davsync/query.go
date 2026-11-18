package davsync

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// filterError is a filter a query cannot use: status and the precondition
// of the DAV:error (none: plain text).
type filterError struct {
	status int
	cond   xml.Name
	text   string
}

func (rq *request) refuseFilter(fe *filterError) {
	if fe.cond.Local != "" {
		writeXMLError(rq.w, fe.status, fe.cond)
		return
	}
	http.Error(rq.w, fe.text, fe.status)
}

// textMatch is a text-match of a filter (RFC 4791 9.7.5, RFC 6352
// 10.5.4): the text folded by its collation, compared by match-type.
// CalDAV knows only substring matches; a match-type there is taken as
// CardDAV defines it.
type textMatch struct {
	text   string
	fold   func(string) string
	mode   string
	negate bool
}

// Collations (RFC 4790): i;octet, i;ascii-casemap and i;unicode-casemap
// (RFC 5051: NFKD and case folding; titlecase there, folding here – the
// same for comparing).
func collation(name string) (func(string) string, bool) {
	switch name {
	case "i;octet":
		return func(s string) string { return s }, true
	case "i;ascii-casemap":
		return func(s string) string {
			b := []byte(s)
			for i, c := range b {
				if c >= 'A' && c <= 'Z' {
					b[i] = c + 'a' - 'A'
				}
			}
			return string(b)
		}, true
	case "i;unicode-casemap":
		fold := cases.Fold() // per request: a Caser is not safe for concurrent use
		return func(s string) string { return fold.String(norm.NFKD.String(s)) }, true
	}
	return nil, false
}

// parseTextMatch reads a text-match; defaultCollation is the one of the
// protocol (CalDAV i;ascii-casemap, CardDAV i;unicode-casemap), ns the
// namespace of its preconditions.
func parseTextMatch(n *node, defaultCollation string, precondition func(string) xml.Name) (textMatch, *filterError) {
	name := n.attr("collation")
	if name == "" {
		name = defaultCollation
	}
	fold, ok := collation(name)
	if !ok {
		return textMatch{}, &filterError{status: http.StatusForbidden, cond: precondition("supported-collation")}
	}
	tm := textMatch{fold: fold, text: fold(n.Text), mode: n.attr("match-type")}
	switch tm.mode {
	case "":
		tm.mode = "contains"
	case "contains", "equals", "starts-with", "ends-with":
	default:
		return textMatch{}, &filterError{status: http.StatusBadRequest, text: "unknown match-type"}
	}
	switch n.attr("negate-condition") {
	case "", "no":
	case "yes":
		tm.negate = true
	default:
		return textMatch{}, &filterError{status: http.StatusBadRequest, text: "negate-condition must be yes or no"}
	}
	return tm, nil
}

// matches compares one value (already in the form of the index).
func (tm textMatch) matches(value string) bool {
	v := tm.fold(value)
	var ok bool
	switch tm.mode {
	case "equals":
		ok = v == tm.text
	case "starts-with":
		ok = strings.HasPrefix(v, tm.text)
	case "ends-with":
		ok = strings.HasSuffix(v, tm.text)
	default:
		ok = strings.Contains(v, tm.text)
	}
	return ok != tm.negate
}

// calFilter is what TitanOS evaluates of a calendar-query filter (RFC
// 4791 9.7): comp-filter VCALENDAR/VEVENT, time-range on VEVENT (start
// and/or end, open sides allowed: iOS sends only start) and prop-filter
// UID with text-match. Anything else (other prop-filters, param-filters,
// VALARM, time-range on a property) matches every entry: allowed, the
// client filters again.
type calFilter struct {
	none   bool
	ranges [][2]time.Time
	uids   []textMatch
	// noUID: prop-filter UID is-not-defined.
	noUID bool
	// The components asked for (1.13): a comp-filter VEVENT or VTODO,
	// or one of them with is-not-defined. A calendar holds only VEVENT, a
	// reminder list only VTODO (excludes, tasks.go).
	event, noEvent, todo, noTodo bool
	// task are the filters below comp-filter VTODO (tasks.go).
	task todoFilter
}

const utcFormat = "20060102T150405Z"

func parseUTC(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(utcFormat, s)
	return t, err == nil
}

func validFilter() *filterError {
	return &filterError{status: http.StatusForbidden, cond: calName("valid-filter")}
}

// maxFilterTerms bounds the prop-filters and text-matches of one query: each
// is evaluated against every entry of the collection (20 000), so a body of
// 256 KiB with thousands of them cost minutes of CPU (security review 1.11,
// S-3). iOS sends one or two. A query has at most one time-range: each
// one expands every recurring entry again.
const maxFilterTerms = 64

func tooComplex(precondition func(string) xml.Name) *filterError {
	return &filterError{status: http.StatusForbidden, cond: precondition("supported-filter")}
}

func parseCalFilter(f *node) (calFilter, *filterError) {
	var out calFilter
	if f == nil {
		return out, validFilter()
	}
	top := f.child(calName("comp-filter"))
	if top == nil || len(f.all(calName("comp-filter"))) > 1 {
		return out, validFilter()
	}
	if !strings.EqualFold(top.attr("name"), "VCALENDAR") || top.child(calName("is-not-defined")) != nil {
		out.none = true
		return out, nil
	}
	for _, c := range top.all(calName("comp-filter")) {
		name := strings.ToUpper(c.attr("name"))
		undefined := c.child(calName("is-not-defined")) != nil
		switch {
		case name == "VEVENT" && undefined:
			out.noEvent = true
		case name == "VEVENT":
			out.event = true
			if fe := out.eventFilters(c); fe != nil {
				return out, fe
			}
			if len(out.ranges) > 1 || len(out.uids) > maxFilterTerms {
				return out, tooComplex(calName)
			}
		case name == "VTODO" && undefined:
			out.noTodo = true
		case name == "VTODO":
			out.todo = true
			if fe := out.todoFilters(c); fe != nil {
				return out, fe
			}
		case name == "VJOURNAL" || name == "VFREEBUSY":
			// Neither is stored: nothing has one.
			if !undefined {
				out.none = true
			}
		}
	}
	return out, nil
}

// eventFilters reads the filters below comp-filter VEVENT.
func (f *calFilter) eventFilters(c *node) *filterError {
	ranges := c.all(calName("time-range"))
	if len(ranges) > 1 {
		// RFC 4791 9.7.1: a comp-filter has at most one time-range.
		return validFilter()
	}
	for _, tr := range ranges {
		start, ok1 := parseUTC(tr.attr("start"))
		end, ok2 := parseUTC(tr.attr("end"))
		if !ok1 || !ok2 || (start.IsZero() && end.IsZero()) || (!start.IsZero() && !end.IsZero() && !end.After(start)) {
			return validFilter()
		}
		f.ranges = append(f.ranges, [2]time.Time{start, end})
	}
	for _, p := range c.all(calName("prop-filter")) {
		if !strings.EqualFold(p.attr("name"), "UID") {
			continue
		}
		if p.child(calName("is-not-defined")) != nil {
			f.noUID = true
			continue
		}
		for _, m := range p.all(calName("text-match")) {
			tm, fe := parseTextMatch(m, "i;ascii-casemap", calName)
			if fe != nil {
				return fe
			}
			f.uids = append(f.uids, tm)
		}
	}
	return nil
}

func (f *calFilter) uidOK(uid string) bool {
	if f.noUID && uid != "" {
		return false
	}
	for _, tm := range f.uids {
		if uid == "" || !tm.matches(uid) {
			return false
		}
	}
	return true
}

// calendarQuery answers calendar-query (RFC 4791 7.8) from the index in
// RAM; a time-range goes through pim/store Overlapping (own expansion of
// recurrences with the length of each occurrence; an event running at the
// start of the range matches; an EXDATE'd occurrence does not). The
// properties are those of the prop framework plus calendar-data, the
// stored bytes: comp and expand below calendar-data are not applied (the
// whole object comes back; RFC 4791 allows a client to ask, iOS does not).
func (rq *request) calendarQuery(body *node) {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	f, fe := parseCalFilter(body.child(calName("filter")))
	if fe != nil {
		rq.refuseFilter(fe)
		return
	}
	pr := parsePropRequest(body)
	objs, err := st.Objects(t.kind, t.account, t.collection)
	if err != nil {
		rq.fail(err)
		return
	}
	none := f.excludes(t.kind)
	var allowed []map[string]bool
	if !none {
		for _, r := range f.ranges {
			names, err := st.Overlapping(t.account, t.collection, r[0], r[1])
			if err != nil {
				rq.fail(err)
				return
			}
			set := make(map[string]bool, len(names))
			for _, n := range names {
				set[n] = true
			}
			allowed = append(allowed, set)
		}
	}
	var members []member
	for _, o := range objs {
		if none || !f.uidOK(o.UID) || (f.todo && !f.task.match(o.Entry, st.Zone())) {
			continue
		}
		in := true
		for _, set := range allowed {
			in = in && set[o.Name]
		}
		if in {
			members = append(members, member{href: rq.memberHref(o.Name), name: o.Name, info: o})
		}
	}
	ms := newMultistatus(rq.w)
	if rq.streamMembers(st, ms, members, pr) {
		ms.finish()
	}
}

// cardPropFilter is a prop-filter of an addressbook-query (RFC 6352
// 10.5.1) on FN, EMAIL, TEL or UID, compared with the index (TEL by its
// digits); any other property matches every card (the client filters).
type cardPropFilter struct {
	name      string
	known     bool
	undefined bool
	allof     bool
	matches   []textMatch
}

type cardFilter struct {
	allof bool
	props []cardPropFilter
}

func testAttr(n *node) (bool, *filterError) {
	switch n.attr("test") {
	case "", "anyof":
		return false, nil
	case "allof":
		return true, nil
	}
	return false, &filterError{status: http.StatusBadRequest, text: "test must be anyof or allof"}
}

func parseCardFilter(f *node) (cardFilter, *filterError) {
	var out cardFilter
	if f == nil {
		return out, &filterError{status: http.StatusBadRequest, text: "addressbook-query needs a filter"}
	}
	var fe *filterError
	if out.allof, fe = testAttr(f); fe != nil {
		return out, fe
	}
	terms := 0
	for _, p := range f.all(cardName("prop-filter")) {
		terms += 1 + len(p.all(cardName("text-match")))
		if terms > maxFilterTerms {
			return out, tooComplex(cardName)
		}
		pf := cardPropFilter{name: strings.ToUpper(p.attr("name"))}
		switch pf.name {
		case "FN", "EMAIL", "TEL", "UID":
			pf.known = true
		}
		if pf.allof, fe = testAttr(p); fe != nil {
			return out, fe
		}
		pf.undefined = p.child(cardName("is-not-defined")) != nil
		for _, m := range p.all(cardName("text-match")) {
			tm, fe := parseTextMatch(m, "i;unicode-casemap", cardName)
			if fe != nil {
				return out, fe
			}
			if pf.name == "TEL" {
				// Numbers by their digits (the index keeps those).
				tm.text = store.PhoneDigits(m.Text)
				tm.fold = func(s string) string { return s }
			}
			pf.matches = append(pf.matches, tm)
		}
		out.props = append(out.props, pf)
	}
	return out, nil
}

func cardValues(e store.Entry, name string) []string {
	var v []string
	switch name {
	case "FN":
		v = []string{e.FN}
	case "UID":
		v = []string{e.UID}
	case "EMAIL":
		return e.Emails
	case "TEL":
		return e.Phones
	}
	if v[0] == "" {
		return nil
	}
	return v
}

func (pf cardPropFilter) match(e store.Entry) bool {
	if !pf.known {
		return true
	}
	values := cardValues(e, pf.name)
	if pf.undefined {
		return len(values) == 0
	}
	if len(pf.matches) == 0 {
		return len(values) > 0
	}
	for _, v := range values {
		hit := pf.allof
		for _, tm := range pf.matches {
			ok := tm.matches(v)
			if tm.text == "" && pf.name == "TEL" {
				ok = tm.negate // a number without digits matches none
			}
			if pf.allof {
				hit = hit && ok
			} else {
				hit = hit || ok
			}
		}
		if hit {
			return true
		}
	}
	return false
}

func (f cardFilter) match(e store.Entry) bool {
	if len(f.props) == 0 {
		return true
	}
	for _, pf := range f.props {
		ok := pf.match(e)
		if ok && !f.allof {
			return true
		}
		if !ok && f.allof {
			return false
		}
	}
	return f.allof
}

// addressbookQuery answers addressbook-query (RFC 6352 8.6) from the
// index in RAM: prop-filter FN/EMAIL/TEL/UID with text-match
// (i;unicode-casemap by default; contains, equals, starts-with,
// ends-with; negate-condition), test anyof/allof, limit/nresults (a cut
// answer ends with 507 number-of-matches-within-limits for the address
// book). Group vCards are part of the address book (iOS needs them; only
// the TitanOS views hide them). address-data is the stored card; a list of
// properties below it is not applied.
func (rq *request) addressbookQuery(body *node) {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	f, fe := parseCardFilter(body.child(cardName("filter")))
	if fe != nil {
		rq.refuseFilter(fe)
		return
	}
	limit, ok := rq.readLimit(body, cardName)
	if !ok {
		return
	}
	pr := parsePropRequest(body)
	objs, err := st.Objects(t.kind, t.account, t.collection)
	if err != nil {
		rq.fail(err)
		return
	}
	var members []member
	cut := false
	for _, o := range objs {
		if !f.match(o.Entry) {
			continue
		}
		if limit > 0 && len(members) == limit {
			cut = true
			break
		}
		members = append(members, member{href: rq.memberHref(o.Name), name: o.Name, info: o})
	}
	ms := newMultistatus(rq.w)
	if !rq.streamMembers(st, ms, members, pr) {
		return
	}
	if cut {
		ms.add(rq.truncated())
	}
	ms.finish()
}
