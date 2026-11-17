package davsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// resource is what a response describes: a target with what the store
// knows of it (from RAM).
type resource struct {
	t target
	// cols are the collections of the account (home set).
	cols []store.Collection
	// col is the collection (collection level).
	col *store.Collection
	// obj is the entry (entry level).
	obj *store.ObjectInfo
}

// propDef is a property: where it exists and how its value is made.
// get returns false where the property has no value for the resource
// (404 in the propstat).
type propDef struct {
	name   xml.Name
	levels levels
	// kind limits it to calendars or contacts ("" = both).
	kind store.Kind
	// hidden properties are not part of allprop (RFC 3253, 3744, 6578:
	// supported-report-set, current-user-privilege-set, sync-token).
	hidden bool
	get    func(rq *request, res *resource) (Elem, bool)
}

func (d *propDef) applies(res *resource) bool {
	return d.levels.has(res.t.level) && (d.kind == "" || d.kind == res.t.kind)
}

// propTable holds the properties; steps 7 and 8 add theirs to
// readProps (or with add).
type propTable struct {
	list   []*propDef
	byName map[xml.Name][]*propDef
}

func newPropTable(defs []propDef) *propTable {
	p := &propTable{byName: map[xml.Name][]*propDef{}}
	for _, d := range defs {
		p.add(d)
	}
	return p
}

func (p *propTable) add(d propDef) {
	def := &d
	p.list = append(p.list, def)
	p.byName[d.name] = append(p.byName[d.name], def)
}

func (p *propTable) find(name xml.Name, res *resource) *propDef {
	for _, d := range p.byName[name] {
		if d.applies(res) {
			return d
		}
	}
	return nil
}

// propMode is what a PROPFIND or REPORT asks for.
type propMode int

const (
	modeAll propMode = iota
	modeName
	modeProp
)

// propRequest is the prop, allprop (with include) or propname of a body.
type propRequest struct {
	mode  propMode
	names []xml.Name
}

// parsePropRequest reads the prop request below parent; none is allprop.
func parsePropRequest(parent *node) propRequest {
	if parent == nil {
		return propRequest{mode: modeAll}
	}
	if p := parent.child(davName("prop")); p != nil {
		pr := propRequest{mode: modeProp}
		for _, c := range p.Children {
			pr.names = append(pr.names, c.Name)
		}
		return pr
	}
	if parent.child(davName("propname")) != nil {
		return propRequest{mode: modeName}
	}
	pr := propRequest{mode: modeAll}
	if inc := parent.child(davName("include")); inc != nil {
		for _, c := range inc.Children {
			pr.names = append(pr.names, c.Name)
		}
	}
	return pr
}

// extraProp gives a value only a REPORT knows (calendar-data,
// address-data).
type extraProp func() (Elem, bool)

// propstats evaluates a prop request for a resource: found ones 200,
// unknown or without value 404.
func (rq *request) propstats(res *resource, pr propRequest, extra map[xml.Name]extraProp) []Propstat {
	var found, missing []Elem
	seen := map[xml.Name]bool{}
	ask := func(name xml.Name) {
		if seen[name] {
			return
		}
		seen[name] = true
		if f := extra[name]; f != nil {
			if e, ok := f(); ok {
				found = append(found, e)
				return
			}
		} else if d := rq.h.props.find(name, res); d != nil {
			if e, ok := d.get(rq, res); ok {
				found = append(found, e)
				return
			}
		}
		missing = append(missing, elem(name))
	}
	switch pr.mode {
	case modeName:
		for _, d := range rq.h.props.list {
			if d.applies(res) && !seen[d.name] {
				seen[d.name] = true
				found = append(found, elem(d.name))
			}
		}
		return []Propstat{{Status: http.StatusOK, Props: found}}
	case modeAll:
		for _, d := range rq.h.props.list {
			if d.applies(res) && !d.hidden {
				ask(d.name)
			}
		}
	}
	for _, name := range pr.names {
		ask(name)
	}
	return []Propstat{{Status: http.StatusOK, Props: found}, {Status: http.StatusNotFound, Props: missing}}
}

// The rights of the device on each level, one element per right (iOS
// shows a calendar read-only without write).
var (
	collectionRights   = []string{"read", "write", "write-properties", "write-content", "bind", "unbind", "read-current-user-privilege-set"}
	calendarHomeRights = []string{"read", "bind", "unbind", "read-current-user-privilege-set"}
	// One address book per account (TECHNIK 1.4: MKCOL 403 for CardDAV).
	contactsHomeRights = []string{"read", "read-current-user-privilege-set"}
)

func privilegeSet(rights []string) Elem {
	e := elem(davName("current-user-privilege-set"))
	for _, r := range rights {
		e.Children = append(e.Children, elem(davName("privilege"), elem(davName(r))))
	}
	return e
}

// collectionReports are the reports a collection names in
// supported-report-set (steps 7/8 implement the queries and sync).
func collectionReports(kind store.Kind) []xml.Name {
	if kind == store.Contacts {
		return []xml.Name{cardName("addressbook-multiget"), cardName("addressbook-query"), davName("sync-collection")}
	}
	return []xml.Name{calName("calendar-multiget"), calName("calendar-query"), davName("sync-collection")}
}

func reportSet(names []xml.Name) Elem {
	e := elem(davName("supported-report-set"))
	for _, n := range names {
		e.Children = append(e.Children, elem(davName("supported-report"), elem(davName("report"), elem(n))))
	}
	return e
}

// homeCTag sums the ctags of the collections (changes with any of them,
// from RAM).
func homeCTag(cols []store.Collection) string {
	h := sha256.New()
	for _, c := range cols {
		h.Write([]byte(c.ID + "\x00" + c.CTag + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func contentType(kind store.Kind) string {
	if kind == store.Contacts {
		return "text/vcard; charset=utf-8"
	}
	return "text/calendar; charset=utf-8"
}

func quote(etag string) string { return `"` + etag + `"` }

// readProps are the properties of TECHNIK 1.4 ("Eigenschaften").
func readProps() []propDef {
	value := func(e Elem) func(*request, *resource) (Elem, bool) {
		return func(*request, *resource) (Elem, bool) { return e, true }
	}
	principalOf := func(name xml.Name) func(*request, *resource) (Elem, bool) {
		return func(rq *request, res *resource) (Elem, bool) {
			return elem(name, hrefElem(principalHref(res.t.kind, res.t.account))), true
		}
	}
	colText := func(name xml.Name, f func(c *store.Collection) string) func(*request, *resource) (Elem, bool) {
		return func(rq *request, res *resource) (Elem, bool) { return textElem(name, f(res.col)), true }
	}
	return []propDef{
		{name: davName("resourcetype"), levels: allLevels, get: func(rq *request, res *resource) (Elem, bool) {
			e := elem(davName("resourcetype"))
			switch res.t.level {
			case levelRoot, levelHome:
				e.Children = []Elem{elem(davName("collection"))}
			case levelPrincipal:
				e.Children = []Elem{elem(davName("collection")), elem(davName("principal"))}
			case levelCollection:
				kind := elem(calName("calendar"))
				if res.t.kind == store.Contacts {
					kind = elem(cardName("addressbook"))
				}
				e.Children = []Elem{elem(davName("collection")), kind}
			}
			return e, true
		}},
		{name: davName("displayname"), levels: on(levelPrincipal, levelHome, levelCollection), get: func(rq *request, res *resource) (Elem, bool) {
			name := davName("displayname")
			switch res.t.level {
			case levelPrincipal:
				return textElem(name, res.t.account), true
			case levelHome:
				if res.t.kind == store.Contacts {
					return textElem(name, store.DefaultBookName), true
				}
				return textElem(name, "Kalender"), true
			}
			return textElem(name, res.col.Name), true
		}},
		{name: davName("current-user-principal"), levels: on(levelRoot, levelPrincipal, levelHome, levelCollection), get: principalOf(davName("current-user-principal"))},
		{name: davName("principal-URL"), levels: on(levelPrincipal), get: principalOf(davName("principal-URL"))},
		// Both home sets on both principals (TECHNIK 1.4).
		{name: calName("calendar-home-set"), levels: on(levelPrincipal), get: func(rq *request, res *resource) (Elem, bool) {
			return elem(calName("calendar-home-set"), hrefElem(homeHref(store.Calendar, res.t.account))), true
		}},
		{name: cardName("addressbook-home-set"), levels: on(levelPrincipal), get: func(rq *request, res *resource) (Elem, bool) {
			return elem(cardName("addressbook-home-set"), hrefElem(homeHref(store.Contacts, res.t.account))), true
		}},
		{name: calName("calendar-user-address-set"), levels: on(levelPrincipal), get: value(elem(calName("calendar-user-address-set")))},
		{name: davName("supported-report-set"), levels: on(levelPrincipal, levelHome, levelCollection), hidden: true, get: func(rq *request, res *resource) (Elem, bool) {
			if res.t.level != levelCollection {
				return reportSet(nil), true
			}
			return reportSet(collectionReports(res.t.kind)), true
		}},
		{name: davName("current-user-privilege-set"), levels: on(levelHome, levelCollection), hidden: true, get: func(rq *request, res *resource) (Elem, bool) {
			switch {
			case res.t.level == levelCollection:
				return privilegeSet(collectionRights), true
			case res.t.kind == store.Contacts:
				return privilegeSet(contactsHomeRights), true
			}
			return privilegeSet(calendarHomeRights), true
		}},
		{name: davName("owner"), levels: on(levelCollection), get: principalOf(davName("owner"))},
		{name: xml.Name{Space: nsCS, Local: "getctag"}, levels: on(levelHome, levelCollection), get: func(rq *request, res *resource) (Elem, bool) {
			name := xml.Name{Space: nsCS, Local: "getctag"}
			if res.t.level == levelHome {
				return textElem(name, homeCTag(res.cols)), true
			}
			return textElem(name, res.col.CTag), true
		}},
		{name: davName("sync-token"), levels: on(levelCollection), hidden: true, get: colText(davName("sync-token"), func(c *store.Collection) string { return c.SyncToken })},
		{name: xml.Name{Space: nsApple, Local: "calendar-color"}, levels: on(levelCollection), kind: store.Calendar,
			get: colText(xml.Name{Space: nsApple, Local: "calendar-color"}, func(c *store.Collection) string { return c.Color })},
		{name: xml.Name{Space: nsApple, Local: "calendar-order"}, levels: on(levelCollection), kind: store.Calendar,
			get: colText(xml.Name{Space: nsApple, Local: "calendar-order"}, func(c *store.Collection) string { return strconv.Itoa(c.Order) })},
		{name: calName("calendar-description"), levels: on(levelCollection), kind: store.Calendar,
			get: colText(calName("calendar-description"), func(c *store.Collection) string { return c.Description })},
		{name: cardName("addressbook-description"), levels: on(levelCollection), kind: store.Contacts,
			get: colText(cardName("addressbook-description"), func(c *store.Collection) string { return c.Description })},
		{name: calName("supported-calendar-component-set"), levels: on(levelCollection), kind: store.Calendar,
			get: value(elem(calName("supported-calendar-component-set"), Elem{Name: calName("comp"), Attr: []xml.Attr{{Name: xml.Name{Local: "name"}, Value: "VEVENT"}}}))},
		{name: calName("supported-calendar-data"), levels: on(levelCollection), kind: store.Calendar,
			get: value(elem(calName("supported-calendar-data"), Elem{Name: calName("calendar-data"), Attr: []xml.Attr{
				{Name: xml.Name{Local: "content-type"}, Value: "text/calendar"}, {Name: xml.Name{Local: "version"}, Value: "2.0"}}}))},
		// vCard 3.0: what iOS writes (TECHNIK 1.4).
		{name: cardName("supported-address-data"), levels: on(levelCollection), kind: store.Contacts,
			get: value(elem(cardName("supported-address-data"), Elem{Name: cardName("address-data-type"), Attr: []xml.Attr{
				{Name: xml.Name{Local: "content-type"}, Value: "text/vcard"}, {Name: xml.Name{Local: "version"}, Value: "3.0"}}}))},
		{name: calName("max-resource-size"), levels: on(levelCollection), kind: store.Calendar,
			get: value(textElem(calName("max-resource-size"), strconv.Itoa(store.MaxICS)))},
		{name: cardName("max-resource-size"), levels: on(levelCollection), kind: store.Contacts,
			get: value(textElem(cardName("max-resource-size"), strconv.Itoa(store.MaxVCF)))},
		{name: davName("getcontenttype"), levels: on(levelCollection, levelEntry), get: func(rq *request, res *resource) (Elem, bool) {
			return textElem(davName("getcontenttype"), contentType(res.t.kind)), true
		}},
		{name: davName("getetag"), levels: on(levelEntry), get: func(rq *request, res *resource) (Elem, bool) {
			return textElem(davName("getetag"), quote(res.obj.ETag)), true
		}},
		{name: davName("getcontentlength"), levels: on(levelEntry), get: func(rq *request, res *resource) (Elem, bool) {
			return textElem(davName("getcontentlength"), strconv.FormatInt(res.obj.Size, 10)), true
		}},
		{name: davName("getlastmodified"), levels: on(levelEntry), get: func(rq *request, res *resource) (Elem, bool) {
			if res.obj.MTime.IsZero() {
				return Elem{}, false
			}
			return textElem(davName("getlastmodified"), res.obj.MTime.UTC().Format(http.TimeFormat)), true
		}},
	}
}
