package davsync

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// MaxResults bounds the nresults of a client's limit (sync-collection,
// addressbook-query): a larger limit is taken as MaxResults. Without a
// limit nothing is cut (the list comes from RAM; iOS sends none, and no
// client is known to go on after a 507 it did not ask for).
const MaxResults = 1000

// readLimit reads <limit><nresults>N</nresults></limit> of the namespace
// (DAV: for sync-collection, CardDAV for addressbook-query): 0 without
// one, at most MaxResults; not a positive number is 400.
func (rq *request) readLimit(body *node, name func(string) xml.Name) (int, bool) {
	l := body.child(name("limit"))
	if l == nil {
		return 0, true
	}
	nr := l.child(name("nresults"))
	if nr == nil {
		http.Error(rq.w, "limit needs nresults", http.StatusBadRequest)
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(nr.Text))
	if err != nil || n <= 0 {
		http.Error(rq.w, "nresults must be a positive number", http.StatusBadRequest)
		return 0, false
	}
	return min(n, MaxResults), true
}

// truncated is the response for the request-URI of a cut answer (RFC 6578
// 3.6, RFC 6352 8.6.1): 507 with number-of-matches-within-limits.
func (rq *request) truncated() Response {
	return Response{Href: hrefOut(rq.r.URL.EscapedPath()), Status: http.StatusInsufficientStorage,
		Error: []Elem{elem(davName("number-of-matches-within-limits"))}}
}

// syncCollection answers sync-collection (RFC 6578) on a calendar or the
// address book: an empty token lists every entry, any other the latest
// state of each name changed after it – deleted ones as a bare 404, the
// others with getetag and what else is asked (calendar-data/address-data
// too). sync-level infinite is answered like 1 (a collection holds no
// collections). A token of another epoch, from the future or older than
// the log is 403 valid-sync-token: the device syncs in full. A limit cuts
// the answer with a 507 for the collection and a token that goes on after
// the last change given (pim/store ChangesLimit).
func (rq *request) syncCollection(body *node) {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	tok := body.child(davName("sync-token"))
	if tok == nil {
		http.Error(rq.w, "sync-collection needs a sync-token element", http.StatusBadRequest)
		return
	}
	if lv := body.child(davName("sync-level")); lv != nil {
		switch strings.TrimSpace(lv.Text) {
		case "1", "infinite":
		default:
			http.Error(rq.w, "sync-level must be 1 or infinite", http.StatusBadRequest)
			return
		}
	}
	limit, ok := rq.readLimit(body, davName)
	if !ok {
		return
	}
	pr := parsePropRequest(body)
	cs, err := st.ChangesLimit(t.kind, t.account, t.collection, strings.TrimSpace(tok.Text), limit)
	if errors.Is(err, store.ErrInvalidSyncToken) {
		writeXMLError(rq.w, http.StatusForbidden, davName("valid-sync-token"))
		return
	}
	if err != nil {
		rq.fail(err)
		return
	}
	members := make([]member, 0, len(cs.Changes))
	for _, c := range cs.Changes {
		m := member{href: rq.memberHref(c.Name), name: c.Name, status: http.StatusNotFound}
		if !c.Deleted {
			info, err := st.Stat(t.kind, t.account, t.collection, c.Name)
			switch {
			case err == nil:
				m.info, m.status = info, 0
			case !notFound(err):
				rq.fail(err)
				return
			}
		}
		members = append(members, m)
	}
	ms := newMultistatus(rq.w)
	if !rq.streamMembers(st, ms, members, pr) {
		return
	}
	if cs.Truncated {
		ms.add(rq.truncated())
	}
	ms.finish(textElem(davName("sync-token"), cs.Token))
}
