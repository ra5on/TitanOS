package davsync

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Bodies of 256 KiB that make one request cost minutes of CPU or gigabytes
// of answer are refused before anything is evaluated (security review
// 1.11, S-3, S-4); what iOS sends still passes.
func TestQueryAndPropLimits(t *testing.T) {
	e := newEnv(t)
	e.put("calendar", "alex", "privat", "a.ics", []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//t//EN\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261110T100000Z\r\nDTEND:20261110T110000Z\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	report := map[string]string{"Depth": "1", "Content-Type": "application/xml"}
	query := func(inner string) string {
		return `<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/></D:prop>` +
			`<C:filter><C:comp-filter name="VCALENDAR">` + inner + `</C:comp-filter></C:filter></C:calendar-query>`
	}
	rng := `<C:time-range start="20261101T000000Z" end="20261201T000000Z"/>`
	cases := []struct {
		name, path, method, body string
		status                   int
	}{
		{"one time-range", "/caldav/alex/calendars/privat/", "REPORT", query(`<C:comp-filter name="VEVENT">` + rng + `</C:comp-filter>`), http.StatusMultiStatus},
		{"two time-ranges", "/caldav/alex/calendars/privat/", "REPORT", query(`<C:comp-filter name="VEVENT">` + strings.Repeat(rng, 2) + `</C:comp-filter>`), http.StatusForbidden},
		{"time-ranges in many comp-filters", "/caldav/alex/calendars/privat/", "REPORT", query(strings.Repeat(`<C:comp-filter name="VEVENT">`+rng+`</C:comp-filter>`, 2000)), http.StatusForbidden},
		{"many UID matches", "/caldav/alex/calendars/privat/", "REPORT", query(`<C:comp-filter name="VEVENT"><C:prop-filter name="UID">` + strings.Repeat(`<C:text-match>a</C:text-match>`, maxFilterTerms+1) + `</C:prop-filter></C:comp-filter>`), http.StatusForbidden},
		{"card query", "/carddav/alex/addressbooks/kontakte/", "REPORT", `<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:getetag/></D:prop><C:filter><C:prop-filter name="FN"><C:text-match>a</C:text-match></C:prop-filter></C:filter></C:addressbook-query>`, http.StatusMultiStatus},
		{"card query with thousands of matches", "/carddav/alex/addressbooks/kontakte/", "REPORT", `<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:getetag/></D:prop><C:filter><C:prop-filter name="EMAIL">` + strings.Repeat(`<C:text-match>a</C:text-match>`, 5000) + `</C:prop-filter></C:filter></C:addressbook-query>`, http.StatusForbidden},
	}
	var props strings.Builder
	for i := range maxPropNames + 1 {
		fmt.Fprintf(&props, `<X:p%d/>`, i)
	}
	propfind := func(inner string) string {
		return `<D:propfind xmlns:D="DAV:" xmlns:X="urn:x"><D:prop>` + inner + `</D:prop></D:propfind>`
	}
	cases = append(cases,
		struct {
			name, path, method, body string
			status                   int
		}{"40 properties", "/caldav/alex/calendars/privat/", "PROPFIND", propfind(strings.Repeat(`<D:getetag/>`, 40)), http.StatusMultiStatus},
		struct {
			name, path, method, body string
			status                   int
		}{"thousands of unknown properties", "/caldav/alex/calendars/privat/", "PROPFIND", propfind(props.String()), http.StatusBadRequest},
		struct {
			name, path, method, body string
			status                   int
		}{"thousands of properties in a multiget", "/caldav/alex/calendars/privat/", "REPORT", `<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:X="urn:x"><D:prop>` + props.String() + `</D:prop><D:href>/caldav/alex/calendars/privat/a.ics</D:href></C:calendar-multiget>`, http.StatusBadRequest},
	)
	for _, c := range cases {
		res, body := e.do(c.method, c.path, "alex", report, strings.NewReader(c.body))
		if res.StatusCode != c.status {
			t.Errorf("%s: %d %s", c.name, res.StatusCode, body)
		}
		if c.status == http.StatusForbidden && !strings.Contains(string(body), "filter") {
			t.Errorf("%s: no precondition: %s", c.name, body)
		}
	}
}
