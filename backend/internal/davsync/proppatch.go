package davsync

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// instruction is one property of a PROPPATCH set or remove, or of the set
// of a MKCALENDAR/MKCOL, in document order (RFC 4918 9.2).
type instruction struct {
	name xml.Name
	// value is the property element of a set; nil for a remove.
	value *node
}

// verdict is the status of one property of a PROPPATCH or MKCALENDAR.
type verdict struct {
	name   xml.Name
	status int
}

// writable is a property PROPPATCH (and MKCALENDAR/MKCOL) may set on a
// collection (TECHNIK 1.4).
type writable struct {
	// kind limits it to calendars or contacts ("" = both).
	kind store.Kind
	// apply puts the value (nil = remove) into the patch: 0 fine, 403
	// refused, 409 a value the property cannot hold.
	apply func(p *store.MetaPatch, v *node) int
}

var (
	appleColor = xml.Name{Space: nsApple, Local: "calendar-color"}
	appleOrder = xml.Name{Space: nsApple, Local: "calendar-order"}
)

// textValue is the text of a property element; one with child elements is
// no text value (409).
func textValue(v *node) (string, bool) {
	if len(v.Children) > 0 {
		return "", false
	}
	return v.Text, true
}

// checked applies a one-field patch if the store accepts its value.
func checked(p *store.MetaPatch, one store.MetaPatch) int {
	if !one.Valid() {
		return http.StatusConflict
	}
	if one.Name != nil {
		p.Name = one.Name
	}
	if one.Color != nil {
		p.Color = one.Color
	}
	if one.Order != nil {
		p.Order = one.Order
	}
	if one.Description != nil {
		p.Description = one.Description
	}
	return 0
}

func description(p *store.MetaPatch, v *node) int {
	s := ""
	if v != nil {
		text, ok := textValue(v)
		if !ok {
			return http.StatusConflict
		}
		s = strings.TrimSpace(text)
	}
	return checked(p, store.MetaPatch{Description: &s})
}

// writables are the properties of collections that can be written.
var writables = map[xml.Name]writable{
	davName("displayname"): {apply: func(p *store.MetaPatch, v *node) int {
		if v == nil {
			return http.StatusForbidden // a collection keeps a name
		}
		text, ok := textValue(v)
		if !ok {
			return http.StatusConflict
		}
		s := strings.TrimSpace(text)
		return checked(p, store.MetaPatch{Name: &s})
	}},
	// Apple writes #RRGGBBAA, the store keeps #RRGGBB and #RRGGBBAA.
	appleColor: {kind: store.Calendar, apply: func(p *store.MetaPatch, v *node) int {
		s := ""
		if v != nil {
			text, ok := textValue(v)
			if !ok {
				return http.StatusConflict
			}
			s = strings.TrimSpace(text)
		}
		return checked(p, store.MetaPatch{Color: &s})
	}},
	appleOrder: {kind: store.Calendar, apply: func(p *store.MetaPatch, v *node) int {
		n := 0
		if v != nil {
			text, ok := textValue(v)
			if !ok {
				return http.StatusConflict
			}
			var err error
			if n, err = strconv.Atoi(strings.TrimSpace(text)); err != nil {
				return http.StatusConflict
			}
		}
		return checked(p, store.MetaPatch{Order: &n})
	}},
	calName("calendar-description"):     {kind: store.Calendar, apply: description},
	cardName("addressbook-description"): {kind: store.Contacts, apply: description},
}

// parsePropertyUpdate reads the body of a PROPPATCH: DAV:propertyupdate
// with set and remove, each with DAV:prop (unknown elements are ignored,
// RFC 4918 17). An update without any property is an error.
func parsePropertyUpdate(root *node) ([]instruction, error) {
	if root == nil || root.Name != davName("propertyupdate") {
		return nil, errXML
	}
	var out []instruction
	for _, c := range root.Children {
		set := c.Name == davName("set")
		if !set && c.Name != davName("remove") {
			continue
		}
		for _, p := range c.all(davName("prop")) {
			for _, v := range p.Children {
				in := instruction{name: v.Name}
				if set {
					in.value = v
				}
				out = append(out, in)
			}
		}
	}
	if len(out) == 0 {
		return nil, errXML
	}
	return out, nil
}

// planUpdate checks the instructions of a PROPPATCH for a collection of
// the kind, in document order, into one patch (collection = false: a
// principal, home set or entry, where nothing is writable). Setting a
// property that cannot be written is 403, a value it cannot hold 409;
// removing a property that is not there is fine (RFC 4918 14.23). It is
// all or nothing: if one fails, the others are 424 (RFC 4918 9.2).
func planUpdate(kind store.Kind, collection bool, ins []instruction) (store.MetaPatch, []verdict, bool) {
	var patch store.MetaPatch
	var out []verdict
	index := map[xml.Name]int{}
	ok := true
	for _, in := range ins {
		status := http.StatusOK
		w, known := writables[in.name]
		switch {
		case !collection || !known || (w.kind != "" && w.kind != kind):
			if in.value != nil {
				status = http.StatusForbidden
			}
		default:
			if s := w.apply(&patch, in.value); s != 0 {
				status = s
			}
		}
		if status != http.StatusOK {
			ok = false
		}
		if i, seen := index[in.name]; seen {
			if out[i].status == http.StatusOK {
				out[i].status = status
			}
			continue
		}
		index[in.name] = len(out)
		out = append(out, verdict{name: in.name, status: status})
	}
	if !ok {
		for i := range out {
			if out[i].status == http.StatusOK {
				out[i].status = http.StatusFailedDependency
			}
		}
	}
	return patch, out, ok
}

// propstatsOf groups verdicts by status, in the order of first appearance.
func propstatsOf(vs []verdict) []Propstat {
	var out []Propstat
	at := map[int]int{}
	for _, v := range vs {
		i, ok := at[v.status]
		if !ok {
			i = len(out)
			at[v.status] = i
			out = append(out, Propstat{Status: v.status})
		}
		out[i].Props = append(out[i].Props, elem(v.name))
	}
	return out
}

// patchEmpty reports whether a patch changes nothing.
func patchEmpty(p store.MetaPatch) bool {
	return p.Name == nil && p.Color == nil && p.Order == nil && p.Description == nil
}

// proppatch changes properties of a collection (RFC 4918 9.2): 207 with a
// propstat per property, never 501. Everything not writable is 403 (also
// on a principal, the home set or an entry, which have none), atomically.
// The answer begins only after the store took the change, so a missing
// volume is still 503.
func (rq *request) proppatch() {
	t := rq.t
	var st *store.Store
	switch t.level {
	case levelCollection, levelEntry:
		var ok bool
		if st, ok = rq.store(); !ok {
			return
		}
		var err error
		if t.level == levelCollection {
			_, err = st.GetCollection(t.kind, t.account, t.collection)
		} else {
			_, err = st.Stat(t.kind, t.account, t.collection, t.name)
		}
		if err != nil {
			rq.fail(err)
			return
		}
	}
	body, ok := rq.readXML()
	if !ok {
		return
	}
	ins, err := parsePropertyUpdate(body)
	if err != nil {
		http.Error(rq.w, "the body is no DAV:propertyupdate with properties", http.StatusBadRequest)
		return
	}
	patch, verdicts, ok := planUpdate(t.kind, t.level == levelCollection, ins)
	if ok && t.level == levelCollection && !patchEmpty(patch) {
		if _, err := st.UpdateCollection(t.kind, t.account, t.collection, patch); err != nil {
			if !errors.Is(err, store.ErrInvalidMeta) {
				rq.failWrite(err)
				return
			}
			// The store refused what davsync let through: every value
			// set is 409, the rest 424 (nothing changed).
			for i := range verdicts {
				verdicts[i].status = http.StatusFailedDependency
				if w, known := writables[verdicts[i].name]; known && (w.kind == "" || w.kind == t.kind) {
					verdicts[i].status = http.StatusConflict
				}
			}
		} else {
			rq.volumeOpen = true
		}
	}
	ms := newMultistatus(rq.w)
	ms.add(Response{Href: t.href(), Propstats: propstatsOf(verdicts)})
	ms.finish()
}

// mkcolPlan is what a MKCALENDAR or extended MKCOL asks for.
type mkcolPlan struct {
	patch    store.MetaPatch
	verdicts []verdict
	// failed: a property cannot be set (403 with the propstats).
	failed bool
	// component: the supported-calendar-component-set names anything but
	// VEVENT (403 supported-calendar-component).
	component bool
	// calendar: the resourcetype names a calendar (extended MKCOL).
	calendar bool
}

// componentsOK reports whether a supported-calendar-component-set names
// VEVENT and nothing else (TECHNIK 1.4: only events).
func componentsOK(v *node) bool {
	comps := v.all(calName("comp"))
	if len(comps) == 0 || len(comps) != len(v.Children) {
		return false
	}
	for _, c := range comps {
		if !strings.EqualFold(strings.TrimSpace(c.attr("name")), "VEVENT") {
			return false
		}
	}
	return true
}

// parseMkcol reads the body of a MKCALENDAR (cal:mkcalendar) or an
// extended MKCOL (DAV:mkcol, RFC 5689): DAV:set with DAV:prop. Known
// properties are checked as in PROPPATCH; resourcetype and the component
// set are looked at; any other (calendar-timezone, schedule-calendar-
// transp, dead properties) is taken and dropped, so a calendar is never
// refused for what iOS adds. A nil body is no property at all.
func parseMkcol(kind store.Kind, root *node, extended bool) (mkcolPlan, error) {
	var plan mkcolPlan
	if root == nil {
		return plan, nil
	}
	want := calName("mkcalendar")
	if extended {
		want = davName("mkcol")
	}
	if root.Name != want {
		return plan, errXML
	}
	for _, set := range root.all(davName("set")) {
		for _, p := range set.all(davName("prop")) {
			for _, v := range p.Children {
				status := http.StatusOK
				switch v.Name {
				case davName("resourcetype"):
					plan.calendar = v.child(calName("calendar")) != nil && v.child(cardName("addressbook")) == nil
				case calName("supported-calendar-component-set"):
					if !componentsOK(v) {
						plan.component = true
						status = http.StatusForbidden
					}
				default:
					if w, known := writables[v.Name]; known && (w.kind == "" || w.kind == kind) {
						if s := w.apply(&plan.patch, v); s != 0 {
							status = s
						}
					}
				}
				if status != http.StatusOK {
					plan.failed = true
				}
				plan.verdicts = append(plan.verdicts, verdict{name: v.Name, status: status})
			}
		}
	}
	if plan.failed {
		for i := range plan.verdicts {
			if plan.verdicts[i].status == http.StatusOK {
				plan.verdicts[i].status = http.StatusFailedDependency
			}
		}
	}
	return plan, nil
}

func (rq *request) mkcalendar() { rq.makeCalendar(false) }

// mkcol is the extended MKCOL of RFC 5689: a calendar like MKCALENDAR
// (the resourcetype must say calendar, we make no plain collections);
// CardDAV keeps one address book per account (403).
func (rq *request) mkcol() {
	if rq.t.kind == store.Contacts {
		http.Error(rq.w, "one address book per account", http.StatusForbidden)
		return
	}
	rq.makeCalendar(true)
}

// makeCalendar makes a calendar on a new collection path of the home set
// (RFC 4791 5.3.1): 201, or 405 where one exists (the virtual default
// too: the device sees it), 507 beyond MaxCollections, 403 for a
// component set other than VEVENT or a property it cannot set (with the
// propstats, RFC 4791 5.3.1.2 / RFC 5689 3.1). Nothing is made unless all
// of it is fine.
func (rq *request) makeCalendar(extended bool) {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	switch _, err := st.GetCollection(t.kind, t.account, t.collection); {
	case err == nil:
		rq.w.Header().Set("Allow", rq.h.allow(t.kind, t.level))
		http.Error(rq.w, "the collection exists", http.StatusMethodNotAllowed)
		return
	case !errors.Is(err, store.ErrNotFound):
		rq.fail(err)
		return
	}
	body, ok := rq.readXML()
	if !ok {
		return
	}
	plan, err := parseMkcol(t.kind, body, extended)
	switch {
	case err != nil && extended:
		http.Error(rq.w, "the body is no DAV:mkcol", http.StatusUnsupportedMediaType)
		return
	case err != nil:
		http.Error(rq.w, "the body is no CALDAV:mkcalendar", http.StatusBadRequest)
		return
	case extended && !plan.calendar:
		writeXMLError(rq.w, http.StatusForbidden, davName("valid-resourcetype"))
		return
	case plan.component:
		writeXMLError(rq.w, http.StatusForbidden, calName("supported-calendar-component"))
		return
	case plan.failed:
		root := calName("mkcalendar-response")
		if extended {
			root = davName("mkcol-response")
		}
		writeXMLBody(rq.w, http.StatusForbidden, root, propstatElems(propstatsOf(plan.verdicts))...)
		return
	}
	meta := store.Meta{Color: store.DefaultColor}
	if p := plan.patch; p.Name != nil {
		meta.Name = *p.Name
	}
	if p := plan.patch; p.Color != nil {
		meta.Color = *p.Color
	}
	if p := plan.patch; p.Description != nil {
		meta.Description = *p.Description
	}
	if p := plan.patch; p.Order != nil {
		meta.Order = *p.Order
	} else if cols, err := st.Collections(t.kind, t.account); err == nil && len(cols) > 0 {
		// A new calendar comes last (sorted by order).
		meta.Order = cols[len(cols)-1].Order + 1
	}
	if _, err := st.CreateCollection(t.kind, t.account, t.collection, meta); err != nil {
		if errors.Is(err, store.ErrExists) {
			rq.w.Header().Set("Allow", rq.h.allow(t.kind, t.level))
			http.Error(rq.w, "the collection exists", http.StatusMethodNotAllowed)
			return
		}
		rq.failWrite(err)
		return
	}
	rq.volumeOpen = true
	rq.w.Header().Set("Content-Length", "0")
	rq.w.WriteHeader(http.StatusCreated)
}
