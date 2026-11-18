package davsync

import (
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// method serves one HTTP method on the levels it names (and of one kind,
// if kind is set); Allow lists it there. The writing methods are in
// write.go (writeMethods).
type method struct {
	levels levels
	kind   store.Kind
	serve  func(rq *request)
}

func (m method) serves(kind store.Kind, l level) bool {
	return m.levels.has(l) && (m.kind == "" || m.kind == family(kind))
}

// readMethods are the methods of step 6.
func readMethods() map[string]method {
	return map[string]method{
		"PROPFIND":      {levels: allLevels, serve: (*request).propfind},
		http.MethodGet:  {levels: on(levelEntry), serve: (*request).get},
		http.MethodHead: {levels: on(levelEntry), serve: (*request).head},
		"REPORT":        {levels: on(levelCollection), serve: (*request).report},
	}
}

// allMethods are the methods of the handler.
func allMethods() map[string]method {
	m := readMethods()
	maps.Copy(m, writeMethods())
	return m
}

// refused are answered alike everywhere (TECHNIK 1.4): iOS moves between
// calendars with DELETE and PUT; no locks, no ACL changes.
var refused = map[string]int{
	"MOVE": http.StatusForbidden, "COPY": http.StatusForbidden,
	"LOCK": http.StatusMethodNotAllowed, "UNLOCK": http.StatusMethodNotAllowed, "ACL": http.StatusMethodNotAllowed,
}

// methodOrder is the order of Allow.
var methodOrder = []string{"OPTIONS", "GET", "HEAD", "PUT", "DELETE", "PROPFIND", "PROPPATCH", "MKCOL", "MKCALENDAR", "REPORT"}

// allow is the Allow header of a level of a kind: OPTIONS and the methods
// of the table that serve it.
func (h *Handler) allow(kind store.Kind, l level) string {
	out := []string{http.MethodOptions}
	var rest []string
	for name, m := range h.methods {
		if m.serves(kind, l) && name != http.MethodOptions {
			rest = append(rest, name)
		}
	}
	slices.SortFunc(rest, func(a, b string) int {
		ia, ib := slices.Index(methodOrder, a), slices.Index(methodOrder, b)
		if ia < 0 || ib < 0 {
			return strings.Compare(a, b)
		}
		return ia - ib
	})
	return strings.Join(append(out, rest...), ", ")
}

func (h *Handler) dispatch(rq *request) {
	if code, ok := refused[rq.r.Method]; ok {
		if code == http.StatusMethodNotAllowed {
			rq.w.Header().Set("Allow", h.allow(rq.t.kind, rq.t.level))
		}
		http.Error(rq.w, http.StatusText(code), code)
		return
	}
	m, ok := h.methods[rq.r.Method]
	if !ok || !m.serves(rq.t.kind, rq.t.level) {
		rq.w.Header().Set("Allow", h.allow(rq.t.kind, rq.t.level))
		http.Error(rq.w, "method not allowed here", http.StatusMethodNotAllowed)
		return
	}
	m.serve(rq)
}

// depth reads the Depth header: 0 or 1. Without one PROPFIND takes 0
// (RFC 4918 says infinity with SHOULD; the smallest answer is safest for
// a client that forgot it); infinity is 403 propfind-finite-depth.
func (rq *request) depth() (int, bool) {
	switch strings.ToLower(strings.TrimSpace(rq.r.Header.Get("Depth"))) {
	case "", "0":
		return 0, true
	case "1":
		return 1, true
	case "infinity":
		writeXMLError(rq.w, http.StatusForbidden, davName("propfind-finite-depth"))
	default:
		http.Error(rq.w, "Depth must be 0 or 1", http.StatusBadRequest)
	}
	return 0, false
}

// propfind answers PROPFIND with Depth 0 or 1 on every level. Root and
// principal need no data (discovery works without the volume); from the
// home set on, everything comes from the store (RAM after the first
// read), 503 without it.
func (rq *request) propfind() {
	depth, ok := rq.depth()
	if !ok {
		return
	}
	body, ok := rq.readXML()
	if !ok {
		return
	}
	if body != nil && body.Name != davName("propfind") {
		http.Error(rq.w, "the body is no DAV:propfind", http.StatusBadRequest)
		return
	}
	if rq.tooManyProps(body) {
		return
	}
	pr := parsePropRequest(body)
	t := rq.t
	var self *resource
	var children []*resource
	var objects []store.ObjectInfo
	switch t.level {
	case levelRoot:
		self = &resource{t: t}
		if depth == 1 {
			children = append(children, &resource{t: t.at(levelPrincipal)})
		}
	case levelPrincipal:
		self = &resource{t: t}
		if depth == 1 {
			home, ok := rq.home()
			if !ok {
				return
			}
			children = append(children, home)
		}
	case levelHome:
		home, ok := rq.home()
		if !ok {
			return
		}
		self = home
		if depth == 1 {
			for i := range home.cols {
				c := t.at(levelCollection)
				c.collection, c.kind = home.cols[i].ID, home.cols[i].Kind
				children = append(children, &resource{t: c, col: &home.cols[i]})
			}
		}
	case levelCollection:
		st, ok := rq.store()
		if !ok {
			return
		}
		col, err := st.GetCollection(t.kind, t.account, t.collection)
		if err != nil {
			rq.fail(err)
			return
		}
		self = &resource{t: t, col: &col}
		if depth == 1 {
			if objects, err = st.Objects(t.kind, t.account, t.collection); err != nil {
				rq.fail(err)
				return
			}
		}
	case levelEntry:
		st, ok := rq.store()
		if !ok {
			return
		}
		info, err := st.Stat(t.kind, t.account, t.collection, t.name)
		if err != nil {
			rq.fail(err)
			return
		}
		self = &resource{t: t, obj: &info}
	}
	ms := newMultistatus(rq.w)
	ms.add(Response{Href: self.t.href(), Propstats: rq.propstats(self, pr, nil)})
	for _, c := range children {
		ms.add(Response{Href: c.t.href(), Propstats: rq.propstats(c, pr, nil)})
	}
	for i := range objects {
		e := t.at(levelEntry)
		e.name = objects[i].Name
		res := &resource{t: e, obj: &objects[i]}
		ms.add(Response{Href: e.href(), Propstats: rq.propstats(res, pr, nil)})
	}
	ms.finish()
}

// home is the home set with its collections. It never has none: the
// store gives the virtual default (ctag 0-0) until there is an own one;
// an empty list would be a store that lost its data, so it is 503.
func (rq *request) home() (*resource, bool) {
	st, ok := rq.store()
	if !ok {
		return nil, false
	}
	cols, err := rq.homeCollections(st) // calendars and reminder lists (tasks.go)
	if err != nil {
		rq.fail(err)
		return nil, false
	}
	if len(cols) == 0 {
		rq.unavailable()
		return nil, false
	}
	return &resource{t: rq.t.at(levelHome), cols: cols}, true
}

// entryHeaders are the headers of GET and HEAD.
func (rq *request) entryHeaders(etag string, info store.ObjectInfo, size int64) {
	h := rq.w.Header()
	h.Set("Content-Type", contentType(rq.t.kind))
	h.Set("ETag", quote(etag))
	if !info.MTime.IsZero() {
		h.Set("Last-Modified", info.MTime.UTC().Format(http.TimeFormat))
	}
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "private, no-cache")
}

// notModified answers If-None-Match with the current ETag (or *).
func (rq *request) notModified(etag string) bool {
	for _, tag := range strings.Split(rq.r.Header.Get("If-None-Match"), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || tag == quote(etag) {
			return true
		}
	}
	return false
}

// get serves an entry: the bytes as stored (they come from the volume).
func (rq *request) get() {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	obj, err := st.Get(t.kind, t.account, t.collection, t.name)
	if err != nil {
		rq.fail(err)
		return
	}
	rq.volumeOpen = true
	info := store.ObjectInfo{Name: obj.Name, Entry: store.Entry{ETag: obj.ETag, MTime: obj.MTime}}
	if rq.notModified(obj.ETag) {
		h := rq.w.Header()
		h.Set("ETag", quote(obj.ETag))
		rq.w.WriteHeader(http.StatusNotModified)
		return
	}
	rq.entryHeaders(obj.ETag, info, int64(len(obj.Data)))
	rq.w.WriteHeader(http.StatusOK)
	_, _ = rq.w.Write(obj.Data)
}

// head answers from the index in RAM (no disk).
func (rq *request) head() {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	info, err := st.Stat(t.kind, t.account, t.collection, t.name)
	if err != nil {
		rq.fail(err)
		return
	}
	rq.entryHeaders(info.ETag, info, info.Size)
	rq.w.WriteHeader(http.StatusOK)
}
