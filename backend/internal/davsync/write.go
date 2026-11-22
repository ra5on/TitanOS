package davsync

import (
	"encoding/xml"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
	"github.com/ra5on/TitanOS/backend/internal/pim/photo"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// writeMethods are the methods of step 7 (TECHNIK 1.4): PUT and DELETE of
// entries, DELETE of collections (strictly by the kind of the path),
// MKCALENDAR and extended MKCOL (RFC 5689) on a new collection path,
// PROPPATCH (proppatch.go).
func writeMethods() map[string]method {
	return map[string]method{
		http.MethodPut:    {levels: on(levelEntry), serve: (*request).put},
		http.MethodDelete: {levels: on(levelCollection, levelEntry), serve: (*request).delete},
		"PROPPATCH":       {levels: on(levelPrincipal, levelHome, levelCollection, levelEntry), serve: (*request).proppatch},
		"MKCALENDAR":      {levels: on(levelCollection), kind: store.Calendar, serve: (*request).mkcalendar},
		"MKCOL":           {levels: on(levelCollection), serve: (*request).mkcol},
	}
}

// writing are the methods that may create, change or delete something:
// a bad name there is 400/403 (nameProblem), not 404.
var writing = map[string]bool{http.MethodPut: true, http.MethodDelete: true, "PROPPATCH": true, "MKCALENDAR": true, "MKCOL": true}

// nameProblem tells a write to a name the store refuses from a path that
// is no path of davsync (0: 404). Only below the home set of the account
// signed in (anything else says nothing about other accounts), for the
// segments after it: 400 for a segment that is no name at all
// (bad escape, "", ".", "..", an encoded "/", NUL or control characters,
// no UTF-8), 403 for a name the rules refuse (leading dot, too long, wrong
// extension, a collection id outside [a-z0-9-] or a UUID). Nothing is
// ever opened for such a name.
func nameProblem(method, escaped, account string) int {
	if !writing[method] || !strings.HasPrefix(escaped, "/") {
		return 0
	}
	parts := strings.Split(escaped[1:], "/")
	if n := len(parts); n > 1 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	if len(parts) < 4 {
		return 0
	}
	var kind store.Kind
	switch parts[0] {
	case CalDAVPrefix[1:]:
		kind = store.Calendar
	case CardDAVPrefix[1:]:
		kind = store.Contacts
	default:
		return 0
	}
	if parts[1] != account || parts[2] != homeSegment(kind) {
		return 0
	}
	refused := false
	for i, p := range parts[3:] {
		s, err := url.PathUnescape(p)
		if err != nil || s == "" || s == "." || s == ".." || !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool {
			return r == '/' || r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0)
		}) {
			return http.StatusBadRequest
		}
		if (i == 0 && !store.ValidCollection(s)) || (i == 1 && !store.ValidName(kind, s)) {
			refused = true
		}
	}
	if refused && len(parts) <= 5 {
		return http.StatusForbidden
	}
	return 0
}

// Names of the preconditions per kind (RFC 4791 5.3.2.1, RFC 6352 6.3.2.1).
func precondition(kind store.Kind, local string) xml.Name {
	if kind == store.Contacts {
		return cardName(local)
	}
	return calName(local)
}

func validData(kind store.Kind) xml.Name {
	if kind == store.Contacts {
		return cardName("valid-address-data")
	}
	return calName("valid-calendar-data")
}

func supportedData(kind store.Kind) xml.Name {
	if kind == store.Contacts {
		return cardName("supported-address-data")
	}
	return calName("supported-calendar-data")
}

// maxEntry is the largest entry of a kind (TECHNIK 5.3).
func maxEntry(kind store.Kind) int64 {
	if kind == store.Contacts {
		return store.MaxVCF
	}
	return store.MaxICS
}

// acceptedType reports whether a PUT body of this Content-Type may be an
// entry: text/calendar resp. text/vcard (also text/x-vcard), UTF-8 if a
// charset is named. Without a Content-Type the data check decides.
func acceptedType(kind store.Kind, header string) bool {
	if strings.TrimSpace(header) == "" {
		return true
	}
	mt, params, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	if cs, ok := params["charset"]; ok && !strings.EqualFold(cs, "utf-8") {
		return false
	}
	if kind == store.Contacts {
		return mt == "text/vcard" || mt == "text/x-vcard"
	}
	return mt == "text/calendar"
}

// tags splits an If-Match or If-None-Match header.
func tags(header string) []string {
	var out []string
	for _, t := range strings.Split(header, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// strongMatch compares an entity tag strongly: quoted, never W/.
func strongMatch(tag, current string) bool {
	return current != "" && tag == quote(current)
}

// pickTag turns an If-Match header into the one tag the store checks
// under its lock: "" without a header, "*" for *, the single tag, or of
// several the one that matches the current tag now (current is asked only
// then; "" = none). No match leaves the first, which fails as it should.
func pickTag(header string, current func() string) string {
	list := tags(header)
	switch {
	case len(list) == 0:
		return ""
	case len(list) == 1:
		return list[0]
	case containsTag(list, "*"):
		return "*"
	}
	cur := current()
	for _, t := range list {
		if strongMatch(t, cur) {
			return t
		}
	}
	return list[0]
}

func containsTag(list []string, tag string) bool {
	for _, t := range list {
		if t == tag {
			return true
		}
	}
	return false
}

// noneMatches evaluates If-None-Match tags (not "*", which the store
// checks itself on PUT) against the current tag: true = 412.
func noneMatches(header, current string) bool {
	for _, t := range tags(header) {
		if t == "*" && current != "" || strongMatch(t, current) {
			return true
		}
	}
	return false
}

// failWrite answers an error of a write. Preconditions go out as a
// DAV:error (RFC 4918 16), "not available" as 503; anything the table
// does not know is rq.fail (404 for not found, else 500 with a line in
// the journal).
func (rq *request) failWrite(err error) {
	var uc *store.UIDConflictError
	switch {
	case unavailable(err):
		rq.unavailable()
	case errors.As(err, &uc):
		// RFC 4791 5.3.2.1: the href of the entry that holds the UID.
		other := rq.t.at(levelEntry)
		other.name = uc.Name
		writeXMLErrorElem(rq.w, http.StatusConflict, elem(precondition(rq.t.kind, "no-uid-conflict"), hrefElem(other.href())))
	default:
		status, cond, text := writeFailure(rq.t.kind, err)
		switch {
		case status == 0:
			rq.fail(err)
		case cond.Local != "":
			writeXMLError(rq.w, status, cond)
		default:
			http.Error(rq.w, text, status)
		}
	}
}

// writeFailure maps an error of a write of the store: the status, the
// precondition of a DAV:error body (none: plain text) – 0 if it is no
// error of the request. ErrUnsupportedComponent comes before
// ErrInvalidData (the store wraps data errors in both).
func writeFailure(kind store.Kind, err error) (int, xml.Name, string) {
	switch {
	case errors.Is(err, store.ErrUnsupportedComponent):
		return http.StatusForbidden, calName("supported-calendar-component"), ""
	case errors.Is(err, store.ErrInvalidData):
		return http.StatusForbidden, validData(kind), ""
	case errors.Is(err, store.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, precondition(kind, "max-resource-size"), ""
	case errors.Is(err, store.ErrPrecondition):
		return http.StatusPreconditionFailed, xml.Name{}, "the entity tag does not match"
	case errors.Is(err, store.ErrCollectionFull):
		return http.StatusInsufficientStorage, xml.Name{}, "the collection is full"
	case errors.Is(err, store.ErrTooManyCollections):
		return http.StatusInsufficientStorage, xml.Name{}, "too many collections"
	case errors.Is(err, store.ErrLastCollection):
		return http.StatusForbidden, xml.Name{}, "the last collection stays"
	case errors.Is(err, store.ErrFixedBook):
		return http.StatusForbidden, xml.Name{}, "one address book per account"
	case errors.Is(err, store.ErrExists):
		return http.StatusConflict, xml.Name{}, "the collection was made meanwhile, try again"
	case errors.Is(err, store.ErrInvalidMeta):
		return http.StatusConflict, xml.Name{}, "a property value is not acceptable"
	}
	return 0, xml.Name{}, ""
}

// put stores an entry with the bytes as they came (RFC 4791 5.3.2, RFC
// 6352 6.3.2): If-None-Match: * only new, If-Match strictly (W/ never
// matches); new 201, overwritten 204, the ETag in the answer as the bytes
// are stored unchanged. A missing collection is 409; the virtual default
// becomes real with its first entry.
//
// A contact whose inline picture is larger than store.MaxPhoto is stored
// with the picture shrunk (TECHNIK 5.5: at most 512 px, a JPEG of at most
// 256 KiB) and answered without ETag (RFC 6352 6.3.2), so that the device
// fetches what is stored. A picture that cannot be read (another format,
// more than photo.DevicePixels) stays as it came, with the ETag.
func (rq *request) put() {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	if !acceptedType(t.kind, rq.r.Header.Get("Content-Type")) {
		writeXMLError(rq.w, http.StatusForbidden, supportedData(t.kind))
		return
	}
	if _, err := st.GetCollection(t.kind, t.account, t.collection); err != nil {
		rq.putFailed(err)
		return
	}
	current := func() string {
		if info, err := st.Stat(t.kind, t.account, t.collection, t.name); err == nil {
			return info.ETag
		}
		return ""
	}
	opt := store.PutOptions{IfMatch: pickTag(rq.r.Header.Get("If-Match"), current)}
	if inm := rq.r.Header.Get("If-None-Match"); inm != "" {
		opt.IfNoneMatch = containsTag(tags(inm), "*")
		if !opt.IfNoneMatch && noneMatches(inm, current()) {
			http.Error(rq.w, "the entity tag matches", http.StatusPreconditionFailed)
			return
		}
	}
	data, ok := rq.readLimited(maxEntry(t.kind), func() {
		writeXMLError(rq.w, http.StatusRequestEntityTooLarge, precondition(t.kind, "max-resource-size"))
	})
	if !ok {
		return
	}
	shrunk := false
	if t.kind == store.Contacts {
		data, shrunk = shrinkPhoto(data)
	}
	res, err := st.Put(t.kind, t.account, t.collection, t.name, data, opt)
	if err != nil {
		rq.putFailed(err)
		return
	}
	rq.volumeOpen = true
	if !shrunk {
		rq.w.Header().Set("ETag", quote(res.ETag))
	}
	if res.Created {
		rq.w.WriteHeader(http.StatusCreated)
		return
	}
	rq.w.WriteHeader(http.StatusNoContent)
}

// shrinkPhoto is the card with its picture shrunk when that is larger
// than store.MaxPhoto; else the bytes as they came.
func shrinkPhoto(data []byte) ([]byte, bool) {
	small, ok := card.ShrinkPhoto(data, store.MaxPhoto, func(pic []byte) ([]byte, error) {
		return photo.Reencode(pic, photo.Contact(photo.DevicePixels))
	})
	if !ok {
		return data, false
	}
	return small, true
}

// putFailed is failWrite with "not found" as 409: the collection of a PUT
// does not exist (RFC 4918 9.7.1), the entry itself never is "not found".
func (rq *request) putFailed(err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(rq.w, "the collection does not exist", http.StatusConflict)
		return
	}
	rq.failWrite(err)
}

// delete removes what the path names, by its kind only: an entry path
// deletes the entry (DeleteObject, never a collection), a collection path
// the collection (DeleteCollection, never an entry). If-Match is the ETag
// of the entry resp. the ctag of the collection. The address book and the
// last own calendar stay (403).
func (rq *request) delete() {
	st, ok := rq.store()
	if !ok {
		return
	}
	t := rq.t
	ifMatch := rq.r.Header.Get("If-Match")
	inm := rq.r.Header.Get("If-None-Match")
	var err error
	if t.level == levelEntry {
		current := func() string {
			if info, err := st.Stat(t.kind, t.account, t.collection, t.name); err == nil {
				return info.ETag
			}
			return ""
		}
		if inm != "" && noneMatches(inm, current()) {
			http.Error(rq.w, "the entity tag matches", http.StatusPreconditionFailed)
			return
		}
		err = st.DeleteObject(t.kind, t.account, t.collection, t.name, pickTag(ifMatch, current))
	} else {
		if t.kind == store.Contacts {
			http.Error(rq.w, "the address book cannot be deleted", http.StatusForbidden)
			return
		}
		current := func() string {
			if c, err := st.GetCollection(t.kind, t.account, t.collection); err == nil {
				return c.CTag
			}
			return ""
		}
		if inm != "" && noneMatches(inm, current()) {
			http.Error(rq.w, "the entity tag matches", http.StatusPreconditionFailed)
			return
		}
		err = st.DeleteCollection(t.kind, t.account, t.collection, pickTag(ifMatch, current))
	}
	if err != nil {
		rq.failWrite(err)
		return
	}
	rq.volumeOpen = true
	rq.w.WriteHeader(http.StatusNoContent)
}

// PrepareEntry is what a PUT does with the bytes before the store sees
// them (the picture of a card shrunk, changed = true then). The sync
// routes of the Android app (api/apppim.go) use it too, so an iPhone and
// the app store the same bytes for the same card.
func PrepareEntry(kind store.Kind, data []byte) (out []byte, changed bool) {
	if kind == store.Contacts {
		return shrinkPhoto(data)
	}
	return data, false
}

// AcceptedType is acceptedType for the sync routes of the Android app.
func AcceptedType(kind store.Kind, header string) bool { return acceptedType(kind, header) }

// MaxEntry is maxEntry for the sync routes of the Android app.
func MaxEntry(kind store.Kind) int64 { return maxEntry(kind) }
