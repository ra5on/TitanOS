package davsync

import (
	"encoding/xml"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func TestTextMatch(t *testing.T) {
	for _, c := range []struct {
		collation, mode, text, value string
		negate, want                 bool
	}{
		{"", "", "jürgen", "Jürgen Groß", false, true},
		{"i;unicode-casemap", "contains", "GROSS", "Jürgen Groß", false, true}, // ß folds to ss
		{"i;unicode-casemap", "equals", "erika muster", "Erika Muster", false, true},
		{"i;unicode-casemap", "starts-with", "muster", "Erika Muster", false, false},
		{"i;unicode-casemap", "ends-with", "muster", "Erika Muster", false, true},
		{"i;ascii-casemap", "", "ERIKA", "erika", false, true},
		{"i;ascii-casemap", "", "JÜRGEN", "jürgen", false, false}, // only ASCII folds
		{"i;octet", "", "Erika", "erika", false, false},
		{"i;octet", "", "rik", "Erika", false, true},
		{"i;octet", "", "rik", "Erika", true, false},
		{"i;octet", "", "x", "Erika", true, true},
	} {
		n := &node{Text: c.text}
		if c.collation != "" {
			n.Attr = append(n.Attr, xmlAttr("collation", c.collation))
		}
		if c.mode != "" {
			n.Attr = append(n.Attr, xmlAttr("match-type", c.mode))
		}
		if c.negate {
			n.Attr = append(n.Attr, xmlAttr("negate-condition", "yes"))
		}
		tm, fe := parseTextMatch(n, "i;unicode-casemap", cardName)
		if fe != nil {
			t.Fatalf("%+v: %+v", c, fe)
		}
		if got := tm.matches(c.value); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
	for _, attrs := range [][2]string{{"collation", "i;basic"}, {"match-type", "fuzzy"}, {"negate-condition", "maybe"}} {
		if _, fe := parseTextMatch(&node{Attr: []xml.Attr{xmlAttr(attrs[0], attrs[1])}}, "i;ascii-casemap", calName); fe == nil {
			t.Errorf("%v accepted", attrs)
		}
	}
}

// A REPORT that reads entries never commits a 207 before the first read
// answered: a failed first read is 500, not a 207; a read that fails
// later aborts the connection (never a 207 that ends as if complete).
func TestReportReadFailure(t *testing.T) {
	e := newEnv(t)
	e.put(store.Calendar, "alex", "privat", "a.ics", fixture(t, "dentist.ics"))
	e.put(store.Calendar, "alex", "privat", "b.ics", fixture(t, "running.ics"))
	query := `<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/><C:calendar-data/></D:prop><C:filter><C:comp-filter name="VCALENDAR"/></C:filter></C:calendar-query>`
	dir := filepath.Join(e.dirs["disk1"], ".titanos/calendar/alex/privat/o")
	lock := func(name string) {
		if err := os.Chmod(filepath.Join(dir, name), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, name), 0o600) })
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	lock("b.ics")
	req, _ := http.NewRequest("REPORT", e.srv.URL+"/caldav/alex/calendars/privat/", strings.NewReader(query))
	req.SetBasicAuth("alex", e.passwords["alex"])
	res, err := e.client().Do(req)
	if err == nil {
		_, err = io.ReadAll(res.Body)
		res.Body.Close()
	}
	if err == nil {
		t.Fatalf("second entry unreadable: the answer ended as if complete (%d)", res.StatusCode)
	}
	lock("a.ics")
	res, _ = e.do("REPORT", "/caldav/alex/calendars/privat/", "alex", nil, strings.NewReader(query))
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("first entry unreadable: %d", res.StatusCode)
	}
	// Only ETags: from RAM, the locked files do not matter.
	res, body := e.do("REPORT", "/caldav/alex/calendars/privat/", "alex", nil,
		strings.NewReader(strings.Replace(query, "<C:calendar-data/>", "", 1)))
	if res.StatusCode != http.StatusMultiStatus || strings.Count(string(body), "<d:response>") != 2 {
		t.Fatalf("etags: %d %s", res.StatusCode, body)
	}
}

// Filters of any shape never panic, and what is parsed is consistent.
func FuzzReportFilters(f *testing.F) {
	for _, seed := range []string{
		`<C:filter xmlns:C="urn:ietf:params:xml:ns:caldav"><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"><C:time-range start="20261101T000000Z"/></C:comp-filter></C:comp-filter></C:filter>`,
		`<C:filter xmlns:C="urn:ietf:params:xml:ns:caldav"><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"><C:prop-filter name="UID"><C:text-match negate-condition="yes" collation="i;octet">x</C:text-match></C:prop-filter></C:comp-filter></C:comp-filter></C:filter>`,
		`<C:filter xmlns:C="urn:ietf:params:xml:ns:caldav"><C:comp-filter name="VCALENDAR"><C:comp-filter name="VTODO"><C:is-not-defined/></C:comp-filter></C:comp-filter></C:filter>`,
		`<C:filter xmlns:C="urn:ietf:params:xml:ns:carddav" test="allof"><C:prop-filter name="TEL"><C:text-match match-type="starts-with">+49</C:text-match></C:prop-filter><C:prop-filter name="FN" test="allof"><C:text-match>a</C:text-match><C:text-match>b</C:text-match></C:prop-filter></C:filter>`,
		`<C:filter xmlns:C="urn:ietf:params:xml:ns:carddav"><C:prop-filter name="EMAIL"><C:is-not-defined/></C:prop-filter></C:filter>`,
		`<C:filter xmlns:C="urn:ietf:params:xml:ns:carddav"/>`, "<", "",
	} {
		f.Add([]byte(seed))
	}
	entries := []store.Entry{
		{FN: "Jürgen Groß", Emails: []string{"j@example.org"}, Phones: []string{"+49301234567"}, UID: "u"},
		{FN: "Familie", Group: true},
		{},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := parseXML(data)
		if err != nil || root == nil {
			return
		}
		if cf, fe := parseCalFilter(root); fe == nil {
			for _, r := range cf.ranges {
				if r[0].IsZero() && r[1].IsZero() || !r[0].IsZero() && !r[1].IsZero() && !r[1].After(r[0]) {
					t.Fatalf("bad range %v passed", r)
				}
			}
			for _, e := range entries {
				cf.uidOK(e.UID)
			}
		} else if fe.status == 0 {
			t.Fatal("refusal without status")
		}
		if cf, fe := parseCardFilter(root); fe == nil {
			for _, e := range entries {
				cf.match(e)
			}
		} else if fe.status == 0 {
			t.Fatal("refusal without status")
		}
	})
}

func xmlAttr(name, value string) xml.Attr { return xml.Attr{Name: xml.Name{Local: name}, Value: value} }
