package davsync

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// reportKey is the root element of a REPORT body.
type reportKey = xml.Name

// report serves one REPORT of a kind ("" = both) on the levels it names.
type report struct {
	kind   store.Kind
	levels levels
	serve  func(rq *request, body *node)
}

// readReports are the reports of collectionReports (supported-report-set
// names exactly these): the multigets, the queries (query.go) and
// sync-collection (sync.go). The Depth header is not looked at: every
// report answers for the members of the collection (RFC 6578 defines
// sync-collection for Depth 0, RFC 4791/6352 the queries for Depth 1;
// clients send either).
func readReports() map[reportKey]report {
	return map[reportKey]report{
		calName("calendar-multiget"):     {kind: store.Calendar, levels: on(levelCollection), serve: (*request).multiget},
		cardName("addressbook-multiget"): {kind: store.Contacts, levels: on(levelCollection), serve: (*request).multiget},
		calName("calendar-query"):        {kind: store.Calendar, levels: on(levelCollection), serve: (*request).calendarQuery},
		cardName("addressbook-query"):    {kind: store.Contacts, levels: on(levelCollection), serve: (*request).addressbookQuery},
		davName("sync-collection"):       {levels: on(levelCollection), serve: (*request).syncCollection},
	}
}

func (rq *request) report() {
	body, ok := rq.readXML()
	if !ok {
		return
	}
	if body == nil {
		http.Error(rq.w, "REPORT needs a body", http.StatusBadRequest)
		return
	}
	rep, ok := rq.h.reports[body.Name]
	if !ok || (rep.kind != "" && rep.kind != rq.t.kind) || !rep.levels.has(rq.t.level) {
		writeXMLError(rq.w, http.StatusForbidden, davName("supported-report"))
		return
	}
	if rq.tooManyProps(body) {
		return
	}
	rep.serve(rq, body)
}

// dataName is calendar-data or address-data: the bytes of an entry in a
// REPORT (never in PROPFIND, RFC 4791 9.6).
func dataName(kind store.Kind) xml.Name {
	if kind == store.Contacts {
		return cardName("address-data")
	}
	return calName("calendar-data")
}

// hrefOut is the href of a response as the client wrote it (its path
// for an absolute URL), so it finds its own.
func hrefOut(raw string) string {
	raw = strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.EscapedPath()
	}
	return raw
}

// member is one entry of a REPORT answer: found (info from the index in
// RAM) or a status of its own (404).
type member struct {
	href   string
	name   string
	info   store.ObjectInfo
	status int
}

// multiget answers calendar-multiget and addressbook-multiget (RFC 4791
// 7.9, RFC 6352 8.7): at most MaxHrefs, each one missing, outside the
// collection or not an entry 404. The index in RAM decides first (so no
// volume is 503 before any byte goes out); then the entries are read and
// streamed one by one.
func (rq *request) multiget(body *node) {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	if _, err := st.GetCollection(t.kind, t.account, t.collection); err != nil {
		rq.fail(err)
		return
	}
	hrefs := body.all(davName("href"))
	switch {
	case len(hrefs) == 0:
		http.Error(rq.w, "a multiget needs hrefs", http.StatusBadRequest)
		return
	case len(hrefs) > MaxHrefs:
		http.Error(rq.w, "at most 1000 hrefs per multiget", http.StatusInsufficientStorage)
		return
	}
	pr := parsePropRequest(body)
	members := make([]member, 0, len(hrefs))
	for _, h := range hrefs {
		m := member{href: hrefOut(h.Text), status: http.StatusNotFound}
		if name, ok := entryOf(t, h.Text); ok {
			info, err := st.Stat(t.kind, t.account, t.collection, name)
			switch {
			case err == nil:
				m.name, m.info, m.status = name, info, 0
			case !notFound(err):
				rq.fail(err)
				return
			}
		}
		members = append(members, m)
	}
	ms := newMultistatus(rq.w)
	if rq.streamMembers(st, ms, members, pr) {
		ms.finish()
	}
}

// wantsData reports whether a prop request asks for calendar-data or
// address-data (never part of allprop, RFC 4791 9.6).
func wantsData(pr propRequest, data xml.Name) bool {
	return pr.mode != modeName && slices.Contains(pr.names, data)
}

// memberHref is the canonical href of an entry of the collection.
func (rq *request) memberHref(name string) string {
	e := rq.t.at(levelEntry)
	e.name = name
	return e.href()
}

// streamMembers writes the responses of a REPORT; false when it answered
// otherwise (nothing streamed yet). Everything up to here
// came from RAM; the bytes of an entry are read from the volume only when
// calendar-data/address-data is asked for, one entry at a time. Before the
// first byte a failed read is answered as such (503); after it the
// connection is aborted: never a 404 for an entry that exists, never a
// 207 that ends early as if complete.
func (rq *request) streamMembers(st *store.Store, ms *multistatus, members []member, pr propRequest) bool {
	t := rq.t
	data := dataName(t.kind)
	read := wantsData(pr, data)
	var rd *store.Reader
	if read {
		// One open of the collection's folder for all entries (1.13; was
		// one per entry), each file read without the collection's lock.
		var err error
		if rd, err = st.Reader(t.kind, t.account, t.collection); err != nil {
			rq.fail(err)
			return false
		}
		defer rd.Close()
	}
	for _, m := range members {
		if rq.r.Context().Err() != nil {
			// The account was forgotten or the client went away.
			if !ms.started {
				rq.unavailable()
				return false
			}
			panic(http.ErrAbortHandler)
		}
		if m.status != 0 {
			ms.add(Response{Href: m.href, Status: m.status})
			continue
		}
		e := t.at(levelEntry)
		e.name = m.name
		info := m.info
		var extra map[xml.Name]extraProp
		if read {
			obj, err := rd.Get(m.name)
			switch {
			case err == nil:
			case notFound(err):
				// Deleted since the index was asked.
				ms.add(Response{Href: m.href, Status: http.StatusNotFound})
				continue
			case !ms.started:
				rq.fail(err)
				return false
			default:
				rq.journal("report aborted", err)
				panic(http.ErrAbortHandler)
			}
			rq.volumeOpen = true
			info = store.ObjectInfo{Name: m.name, Entry: store.Entry{ETag: obj.ETag, Size: int64(len(obj.Data)), MTime: obj.MTime}}
			extra = map[xml.Name]extraProp{data: func() (Elem, bool) { return textElem(data, string(obj.Data)), true }}
		}
		ms.add(Response{Href: m.href, Propstats: rq.propstats(&resource{t: e, obj: &info}, pr, extra)})
	}
	return true
}
