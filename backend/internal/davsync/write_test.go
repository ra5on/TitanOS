package davsync

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// files lists the regular files below a volume folder (relative).
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Writes to names the store refuses never reach the volume: no file
// anywhere, also not outside the collection (security question 8).
func TestBadNamesWriteNothing(t *testing.T) {
	e := newEnv(t)
	ics := fixture(t, "dentist.ics")
	for _, p := range []string{
		"/caldav/alex/calendars/privat/..", "/caldav/alex/calendars/privat/%2e%2e", "/caldav/alex/calendars/privat/a%2Fb.ics",
		"/caldav/alex/calendars/privat/..%2F..%2F..%2Fx.ics", "/caldav/alex/calendars/privat/%2e%2e%2f%2e%2e%2fbea.ics",
		"/caldav/alex/calendars/privat/.x.ics", "/caldav/alex/calendars/privat/%00.ics", "/caldav/alex/calendars/privat/x%0D.ics",
		"/caldav/alex/calendars/privat/" + strings.Repeat("%C3%A4", 99) + ".ics", "/caldav/alex/calendars/%2e%2e/privat/x.ics",
		"/caldav/alex/calendars/privat/x.ics.", "/caldav/alex/calendars/privat/x", "/caldav/alex/calendars/.titanos/x.ics",
	} {
		res, _ := e.do(http.MethodPut, p, "alex", map[string]string{"Content-Type": "text/calendar"}, bytes.NewReader(ics))
		if res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusForbidden {
			t.Errorf("PUT %s: %d", p, res.StatusCode)
		}
	}
	for _, p := range []string{"/caldav/alex/calendars/..", "/caldav/alex/calendars/%2e%2e/", "/caldav/alex/calendars/a%2Fb/", "/caldav/alex/calendars/.x/"} {
		for _, m := range []string{"MKCALENDAR", "MKCOL", "DELETE", "PROPPATCH"} {
			if res, _ := e.do(m, p, "alex", nil, nil); res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s: %d", m, p, res.StatusCode)
			}
		}
	}
	for _, d := range e.dirs {
		if f := files(t, d); len(f) != 0 {
			t.Errorf("written: %v", f)
		}
	}
}

func TestNameProblem(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"PUT", "/caldav/alex/calendars/privat/..", 400},
		{"PUT", "/caldav/alex/calendars/privat/a%2fb.ics", 400},
		{"PUT", "/caldav/alex/calendars/privat/%7f.ics", 400},
		{"PUT", "/caldav/alex/calendars/privat/%ff.ics", 400},
		{"PUT", "/caldav/alex/calendars/../privat/x.ics", 400},
		{"PUT", "/caldav/alex/calendars/privat/.x.ics", 403},
		{"PUT", "/caldav/alex/calendars/privat/x.vcf", 403},
		{"PUT", "/carddav/alex/addressbooks/kontakte/x.ics", 403},
		{"MKCALENDAR", "/caldav/alex/calendars/UPPER/", 403},
		// Not ours to tell: another account, reading, other shapes.
		{"PUT", "/caldav/bea/calendars/privat/..", 0},
		{"PUT", "/caldav/%61lex/calendars/privat/..", 0},
		{"GET", "/caldav/alex/calendars/privat/..", 0},
		{"PROPFIND", "/caldav/alex/calendars/privat/.x.ics", 0},
		{"PUT", "/caldav/alex/calendars/privat/a/b.ics", 0},
		{"PUT", "/caldav/alex/x/privat/..", 0},
		{"PUT", "/caldav/alex/calendars/", 0},
		{"PUT", "/dav/alex/calendars/privat/..", 0},
	} {
		if got := nameProblem(c.method, c.path, "alex"); got != c.want {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, got, c.want)
		}
	}
}

// On another volume without the account's folder (store.ErrElsewhere)
// every write is 503 and nothing appears there; back on the own volume
// the data is as it was.
func TestWritesElsewhereLeaveNothing(t *testing.T) {
	e := newEnv(t)
	e.put(store.Calendar, "alex", "privat", "a.ics", fixture(t, "dentist.ics"))
	e.put(store.Contacts, "alex", "kontakte", "c.vcf", fixture(t, "erika.vcf"))
	for _, v := range []string{"disk2", ""} {
		e.setVolume(v)
		for _, r := range []struct{ method, path, body string }{
			{"PUT", "/caldav/alex/calendars/privat/b.ics", string(fixture(t, "weekly-team.ics"))},
			{"PUT", "/carddav/alex/addressbooks/kontakte/g.vcf", string(fixture(t, "group-family.vcf"))},
			{"DELETE", "/caldav/alex/calendars/privat/a.ics", ""},
			{"MKCALENDAR", "/caldav/alex/calendars/neu/", ""},
			{"MKCOL", "/caldav/alex/calendars/neu/", ""},
			{"PROPPATCH", "/caldav/alex/calendars/privat/", `<A:propertyupdate xmlns:A="DAV:"><A:set><A:prop><A:displayname>X</A:displayname></A:prop></A:set></A:propertyupdate>`},
		} {
			res, _ := e.do(r.method, r.path, "alex", nil, strings.NewReader(r.body))
			if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") != "300" {
				t.Errorf("%q: %s %s: %d", v, r.method, r.path, res.StatusCode)
			}
		}
	}
	if f := files(t, e.dirs["disk2"]); len(f) != 0 {
		t.Errorf("written on disk2: %v", f)
	}
	e.setVolume("disk1")
	objs, err := e.store.Objects(store.Calendar, "alex", "privat")
	if err != nil || len(objs) != 1 || objs[0].Name != "a.ics" {
		t.Errorf("privat: %v %v", objs, err)
	}
	cols, _ := e.store.Collections(store.Calendar, "alex")
	if len(cols) != 1 || cols[0].Name != store.DefaultCalendarName {
		t.Errorf("collections: %+v", cols)
	}
}

// The 51st calendar is 507 (TECHNIK 5.3).
func TestMkcalendarLimit(t *testing.T) {
	e := newEnv(t)
	// The first one makes the virtual "privat" real as well: 49 + privat.
	for i := range store.MaxCollections - 1 {
		if _, err := e.store.CreateCollection(store.Calendar, "alex", fmt.Sprintf("k%d", i), store.Meta{}); err != nil {
			t.Fatal(err)
		}
	}
	if res, _ := e.do("MKCALENDAR", "/caldav/alex/calendars/one-more/", "alex", nil, nil); res.StatusCode != http.StatusInsufficientStorage {
		t.Errorf("51st: %d", res.StatusCode)
	}
	// A new calendar comes after the others.
	e2 := newEnv(t)
	if _, err := e2.store.CreateCollection(store.Calendar, "alex", "a", store.Meta{Order: 7}); err != nil {
		t.Fatal(err)
	}
	if res, _ := e2.do("MKCALENDAR", "/caldav/alex/calendars/b/", "alex", nil, nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("MKCALENDAR: %d", res.StatusCode)
	}
	if c, err := e2.store.GetCollection(store.Calendar, "alex", "b"); err != nil || c.Order != 8 || c.Color != store.DefaultColor || c.Name != "b" {
		t.Errorf("new calendar: %+v %v", c, err)
	}
}

// Every error of a write of the store has its answer; none is 500.
func TestWriteFailure(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		cond   string
	}{
		{fmt.Errorf("%w: VTODO", store.ErrUnsupportedComponent), 403, "supported-calendar-component"},
		{fmt.Errorf("%w: %w", store.ErrInvalidData, errors.New("x")), 403, "valid-calendar-data"},
		{store.ErrTooLarge, 413, "max-resource-size"},
		{store.ErrPrecondition, 412, ""},
		{store.ErrCollectionFull, 507, ""},
		{store.ErrTooManyCollections, 507, ""},
		{store.ErrLastCollection, 403, ""},
		{store.ErrFixedBook, 403, ""},
		{store.ErrExists, 409, ""},
		{store.ErrInvalidMeta, 409, ""},
	} {
		status, cond, _ := writeFailure(store.Calendar, c.err)
		if status != c.status || cond.Local != c.cond {
			t.Errorf("%v: %d %v", c.err, status, cond)
		}
	}
	if s, cond, _ := writeFailure(store.Contacts, store.ErrInvalidData); s != 403 || cond != cardName("valid-address-data") {
		t.Errorf("contacts: %d %v", s, cond)
	}
	if s, _, _ := writeFailure(store.Calendar, errors.New("disk")); s != 0 {
		t.Errorf("other error: %d", s)
	}
}

// A collection full of entries answers 507 (the limit of 20 000 is too
// many for a test; the answer comes from writeFailure).
func TestCollectionFullAnswer(t *testing.T) {
	e := newEnv(t)
	rq := &request{h: e.h, t: target{kind: store.Calendar, level: levelEntry, account: "alex", collection: "privat", name: "a.ics"}}
	for _, c := range []struct {
		err  error
		want int
	}{{store.ErrCollectionFull, 507}, {&store.UIDConflictError{Name: "b c.ics"}, 409}, {store.ErrUnavailable, 503}, {store.ErrNotFound, 404}, {errors.New("io"), 500}} {
		w := &recorder{header: http.Header{}}
		rq.w = &statusWriter{ResponseWriter: w}
		rq.r, _ = http.NewRequest("PUT", "/", nil)
		rq.failWrite(c.err)
		if w.status != c.want {
			t.Errorf("%v: %d", c.err, w.status)
		}
		if c.want == 409 && !strings.Contains(w.body.String(), "<d:href>/caldav/alex/calendars/privat/b%20c.ics</d:href>") {
			t.Errorf("uid conflict: %s", w.body.String())
		}
	}
}

type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	return r.body.Write(b)
}

func TestTags(t *testing.T) {
	cur := func() string { return "abc" }
	for header, want := range map[string]string{
		"": "", "*": "*", `"x"`: `"x"`, `W/"abc"`: `W/"abc"`, `"x", "abc"`: `"abc"`, `"x", *`: "*", `W/"abc", "y"`: `W/"abc"`, `"x", "y"`: `"x"`,
	} {
		if got := pickTag(header, cur); got != want {
			t.Errorf("pickTag(%q) = %q, want %q", header, got, want)
		}
	}
	if !noneMatches(`"abc"`, "abc") || noneMatches(`W/"abc"`, "abc") || noneMatches(`"x"`, "abc") || !noneMatches("*", "abc") || noneMatches("*", "") {
		t.Error("noneMatches")
	}
	for _, c := range []struct {
		kind store.Kind
		ct   string
		ok   bool
	}{
		{store.Calendar, "", true}, {store.Calendar, "text/calendar", true}, {store.Calendar, "text/calendar; charset=UTF-8", true},
		{store.Calendar, "text/calendar; charset=utf-8; component=VEVENT", true}, {store.Calendar, "TEXT/Calendar", true},
		{store.Calendar, "text/calendar; charset=latin1", false}, {store.Calendar, "text/vcard", false}, {store.Calendar, "application/xml", false},
		{store.Calendar, "text/calendar; charset", false},
		{store.Contacts, "text/vcard", true}, {store.Contacts, "text/x-vcard; charset=utf-8", true}, {store.Contacts, "text/calendar", false},
	} {
		if got := acceptedType(c.kind, c.ct); got != c.ok {
			t.Errorf("%s %q: %v", c.kind, c.ct, got)
		}
	}
}

// A PUT, DELETE or collection change counts as a sync with the volume
// open; a refused one does not count.
func TestSyncedAfterWrites(t *testing.T) {
	e := newEnv(t)
	rec := &recordingDevices{Manager: e.devices}
	e.h.Devices = rec
	for _, c := range []struct {
		method, path, body string
		counted            bool
	}{
		{"PUT", "/caldav/alex/calendars/privat/a.ics", string(fixture(t, "dentist.ics")), true},
		{"PUT", "/caldav/alex/calendars/privat/b.ics", "nonsense", false},
		{"MKCALENDAR", "/caldav/alex/calendars/neu/", "", true},
		{"PROPPATCH", "/caldav/alex/calendars/neu/", `<A:propertyupdate xmlns:A="DAV:"><A:set><A:prop><A:displayname>N</A:displayname></A:prop></A:set></A:propertyupdate>`, true},
		{"DELETE", "/caldav/alex/calendars/privat/a.ics", "", true},
		{"DELETE", "/caldav/alex/calendars/privat/a.ics", "", false},
	} {
		rec.mu.Lock()
		rec.calls = nil
		rec.mu.Unlock()
		res, _ := e.do(c.method, c.path, "alex", nil, strings.NewReader(c.body))
		rec.mu.Lock()
		calls := rec.calls
		rec.mu.Unlock()
		if c.counted != (len(calls) == 1) || (c.counted && !calls[0].volumeOpen) {
			t.Errorf("%s %s (%d): %+v", c.method, c.path, res.StatusCode, calls)
		}
	}
}

// A slow PUT body ends at the body timeout (408), nothing written.
func TestPutBodyTimeout(t *testing.T) {
	e := newEnv(t)
	e.h.bodyTimeout = 200 * time.Millisecond
	r, w := io.Pipe()
	defer w.Close()
	go func() { _, _ = w.Write([]byte("BEGIN:VCALENDAR\r\n")) }()
	res, _ := e.do("PUT", "/caldav/alex/calendars/privat/a.ics", "alex", nil, r)
	if res.StatusCode != http.StatusRequestTimeout {
		t.Errorf("stalled: %d", res.StatusCode)
	}
	if f := files(t, e.dirs["disk1"]); len(f) != 0 {
		t.Errorf("written: %v", f)
	}
}

// The request parsing of PROPPATCH, MKCALENDAR and MKCOL never panics, and
// its verdicts are all or nothing.
func FuzzWriteBodies(f *testing.F) {
	for _, seed := range []string{
		`<A:propertyupdate xmlns:A="DAV:"><A:set><A:prop><A:displayname>Arbeit</A:displayname></A:prop></A:set></A:propertyupdate>`,
		`<A:propertyupdate xmlns:A="DAV:" xmlns:D="http://apple.com/ns/ical/"><A:set><A:prop><D:calendar-color>#FF2968FF</D:calendar-color><D:calendar-order>2</D:calendar-order></A:prop></A:set><A:remove><A:prop><D:calendar-color/></A:prop></A:remove></A:propertyupdate>`,
		`<A:propertyupdate xmlns:A="DAV:" xmlns:X="x:"><A:set><A:prop><X:y>1</X:y><A:displayname><A:b/></A:displayname></A:prop></A:set></A:propertyupdate>`,
		`<A:propertyupdate xmlns:A="DAV:"/>`,
		`<B:mkcalendar xmlns:A="DAV:" xmlns:B="urn:ietf:params:xml:ns:caldav"><A:set><A:prop><A:displayname>K</A:displayname><B:supported-calendar-component-set><B:comp name="VEVENT"/></B:supported-calendar-component-set></A:prop></A:set></B:mkcalendar>`,
		`<B:mkcalendar xmlns:A="DAV:" xmlns:B="urn:ietf:params:xml:ns:caldav"><A:set><A:prop><B:supported-calendar-component-set><B:comp name="VTODO"/></B:supported-calendar-component-set></A:prop></A:set></B:mkcalendar>`,
		`<A:mkcol xmlns:A="DAV:" xmlns:B="urn:ietf:params:xml:ns:caldav"><A:set><A:prop><A:resourcetype><A:collection/><B:calendar/></A:resourcetype><B:calendar-description>d</B:calendar-description></A:prop></A:set></A:mkcol>`,
		`<A:mkcol xmlns:A="DAV:"><A:set><A:prop><A:resourcetype><A:collection/></A:resourcetype></A:prop></A:set></A:mkcol>`,
		`<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "b">]><A:propertyupdate xmlns:A="DAV:"/>`,
		"", "<", "\x00",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := parseXML(data)
		if err != nil {
			return
		}
		for _, kind := range []store.Kind{store.Calendar, store.Contacts} {
			if ins, err := parsePropertyUpdate(root); err == nil {
				for _, collection := range []bool{true, false} {
					patch, verdicts, ok := planUpdate(kind, collection, ins)
					if len(verdicts) == 0 {
						t.Fatal("no verdicts")
					}
					for _, v := range verdicts {
						if ok != (v.status == http.StatusOK) {
							t.Fatalf("ok %v with %d", ok, v.status)
						}
					}
					if ok && !patch.Valid() {
						t.Fatal("invalid patch passed")
					}
					if !collection && !patchEmpty(patch) {
						t.Fatal("patch outside a collection")
					}
				}
			}
			for _, extended := range []bool{false, true} {
				plan, err := parseMkcol(kind, root, extended)
				if err != nil {
					continue
				}
				if !plan.failed && !plan.patch.Valid() {
					t.Fatal("invalid meta passed")
				}
				for _, v := range plan.verdicts {
					if plan.failed == (v.status == http.StatusOK) {
						t.Fatalf("failed %v with %d", plan.failed, v.status)
					}
				}
			}
		}
	})
}
