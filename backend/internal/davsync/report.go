package davsync

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// reportKey is the root element of a REPORT body.
type reportKey = xml.Name

// report serves one REPORT of a kind on the levels it names. Step 8 adds
// calendar-query, addressbook-query and sync-collection (named in
// supported-report-set already) to readReports; until then they answer
// 403 DAV:supported-report.
type report struct {
	kind   store.Kind
	levels levels
	serve  func(rq *request, body *node)
}

func readReports() map[reportKey]report {
	return map[reportKey]report{
		calName("calendar-multiget"):     {kind: store.Calendar, levels: on(levelCollection), serve: (*request).multiget},
		cardName("addressbook-multiget"): {kind: store.Contacts, levels: on(levelCollection), serve: (*request).multiget},
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
	if !ok || rep.kind != rq.t.kind || !rep.levels.has(rq.t.level) {
		writeXMLError(rq.w, http.StatusForbidden, davName("supported-report"))
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

// multiget answers calendar-multiget and addressbook-multiget (RFC 4791
// 7.9, RFC 6352 8.7): at most MaxHrefs, each one missing, outside the
// collection or not an entry 404. The index in RAM decides first (so no
// volume is 503 before any byte goes out); then the entries are read and
// streamed one by one. A read that fails after the answer began aborts
// the connection: never a 404 for an entry that exists.
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
	type item struct {
		href  string
		name  string
		found bool
	}
	items := make([]item, 0, len(hrefs))
	for _, h := range hrefs {
		it := item{href: hrefOut(h.Text)}
		if name, ok := entryOf(t, h.Text); ok {
			_, err := st.Stat(t.kind, t.account, t.collection, name)
			switch {
			case err == nil:
				it.name, it.found = name, true
			case !notFound(err):
				rq.fail(err)
				return
			}
		}
		items = append(items, it)
	}
	ms := newMultistatus(rq.w)
	data := dataName(t.kind)
	for _, it := range items {
		if rq.r.Context().Err() != nil {
			// The account was forgotten or the client went away.
			if !ms.started {
				rq.unavailable()
				return
			}
			panic(http.ErrAbortHandler)
		}
		if !it.found {
			ms.add(Response{Href: it.href, Status: http.StatusNotFound})
			continue
		}
		obj, err := st.Get(t.kind, t.account, t.collection, it.name)
		switch {
		case err == nil:
		case notFound(err):
			// Deleted since the index was asked.
			ms.add(Response{Href: it.href, Status: http.StatusNotFound})
			continue
		case !ms.started:
			rq.fail(err)
			return
		default:
			rq.journal("multiget aborted", err)
			panic(http.ErrAbortHandler)
		}
		rq.volumeOpen = true
		e := t.at(levelEntry)
		e.name = it.name
		res := &resource{t: e, obj: &store.ObjectInfo{Name: it.name, Entry: store.Entry{ETag: obj.ETag, Size: int64(len(obj.Data)), MTime: obj.MTime}}}
		extra := map[xml.Name]extraProp{data: func() (Elem, bool) { return textElem(data, string(obj.Data)), true }}
		ms.add(Response{Href: it.href, Propstats: rq.propstats(res, pr, extra)})
	}
	ms.finish()
}
