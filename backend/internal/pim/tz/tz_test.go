package tz

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	l := LoadIANA("Europe/Berlin")
	if l == nil {
		t.Fatal("Europe/Berlin missing from embedded zone data")
	}
	return l
}

func decode(t *testing.T, s string) *ical.Calendar {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(strings.ReplaceAll(s, "\n", "\r\n"))).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

func prop(name, value string, params ...string) *ical.Prop {
	p := ical.NewProp(name)
	p.Value = value
	for i := 0; i+1 < len(params); i += 2 {
		p.Params.Set(params[i], params[i+1])
	}
	return p
}

func TestIANAAndDST(t *testing.T) {
	r := NewResolver(time.UTC, nil)
	cases := []struct {
		value string
		utc   string
	}{
		{"20260328T090000", "2026-03-28T08:00:00Z"}, // CET, day before the change
		{"20260330T090000", "2026-03-30T07:00:00Z"}, // CEST
		{"20261024T090000", "2026-10-24T07:00:00Z"}, // CEST
		{"20261026T090000", "2026-10-26T08:00:00Z"}, // CET
		{"20260329T023000", "2026-03-29T01:30:00Z"}, // in the gap: 03:30 CEST
		{"20261025T023000", "2026-10-25T00:30:00Z"}, // twice: the first (CEST)
	}
	for _, c := range cases {
		v, err := r.Parse(prop("DTSTART", c.value, "TZID", "Europe/Berlin"))
		if err != nil {
			t.Fatal(err)
		}
		if v.Kind != KindZoned || v.Guess || v.TZID != "Europe/Berlin" {
			t.Errorf("%s: kind %v guess %v tzid %q", c.value, v.Kind, v.Guess, v.TZID)
		}
		if got := v.Time.UTC().Format(time.RFC3339); got != c.utc {
			t.Errorf("%s: got %s, want %s", c.value, got, c.utc)
		}
	}
	// The gap rule holds where time.Date itself picks the other offset.
	v, err := r.Parse(prop("DTSTART", "20260308T023000", "TZID", "America/New_York"))
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Time.Format("15:04 MST"); got != "03:30 EDT" {
		t.Errorf("New York gap: %s", got)
	}
}

func TestWindowsAndPrefixedNames(t *testing.T) {
	r := NewResolver(time.UTC, nil)
	for _, tzid := range []string{"W. Europe Standard Time", "/mozilla.org/20050126_1/Europe/Berlin", "/citadel.org/20190914_1/Europe/Berlin"} {
		v, err := r.Parse(prop("DTSTART", "20261110T090000", "TZID", tzid))
		if err != nil {
			t.Fatal(err)
		}
		if v.Guess || v.Time.UTC().Hour() != 8 {
			t.Errorf("%s: %v guess %v", tzid, v.Time, v.Guess)
		}
	}
	if z, ok := WindowsToIANA("Pacific Standard Time"); !ok || z != "America/Los_Angeles" {
		t.Errorf("Pacific Standard Time -> %q", z)
	}
	if len(windowsZones) < 130 {
		t.Errorf("windows table has only %d entries", len(windowsZones))
	}
	for win, iana := range windowsZones {
		if LoadIANA(iana) == nil {
			t.Errorf("%s maps to %s, which the zone data lacks", win, iana)
		}
	}
}

func TestLoadIANARefusesLocalAndPaths(t *testing.T) {
	for _, name := range []string{"Local", "", "/etc/localtime", "../etc/passwd", "Europe/../../etc/passwd"} {
		if LoadIANA(name) != nil {
			t.Errorf("%q resolved", name)
		}
	}
	r := NewResolver(berlin(t), nil)
	if _, guess := r.Location("Local", 2026); !guess {
		t.Error("TZID=Local must not read the system zone")
	}
}

// outlookCal carries an Exchange zone name that is no Windows table entry,
// with its VTIMEZONE (European rules).
const outlookCal = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Microsoft Corporation//Outlook 16.0 MIMEDIR//EN
BEGIN:VTIMEZONE
TZID:Mitteleuropaeische Zeit (eigene)
BEGIN:STANDARD
DTSTART:16011028T030000
RRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=10
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:16010325T020000
RRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=3
TZOFFSETFROM:+0100
TZOFFSETTO:+0200
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VTIMEZONE
TZID:Old US Eastern
BEGIN:STANDARD
DTSTART:20071104T020000
RRULE:FREQ=YEARLY;BYMONTH=11;BYMONTHDAY=1,2,3,4,5,6,7;BYDAY=SU
TZOFFSETFROM:-0400
TZOFFSETTO:-0500
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:20070311T020000
RRULE:FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=8,9,10,11,12,13,14;BYDAY=SU
TZOFFSETFROM:-0500
TZOFFSETTO:-0400
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VTIMEZONE
TZID:Strange Zone
BEGIN:STANDARD
DTSTART:19700101T000000
TZOFFSETFROM:+0117
TZOFFSETTO:+0117
END:STANDARD
END:VTIMEZONE
BEGIN:VTIMEZONE
TZID:Named
X-LIC-LOCATION:Europe/Vienna
BEGIN:STANDARD
DTSTART:19700101T000000
TZOFFSETFROM:+0100
TZOFFSETTO:+0100
END:STANDARD
END:VTIMEZONE
END:VCALENDAR
`

func TestUnknownTZIDWithVTimezone(t *testing.T) {
	cal := decode(t, outlookCal)
	r := NewResolver(time.UTC, cal)

	check := func(tzid, value, wantUTC string, wantGuess bool) {
		t.Helper()
		v, err := r.Parse(prop("DTSTART", value, "TZID", tzid))
		if err != nil {
			t.Fatal(err)
		}
		if got := v.Time.UTC().Format(time.RFC3339); got != wantUTC || v.Guess != wantGuess {
			t.Errorf("%s %s: got %s guess %v, want %s guess %v", tzid, value, got, v.Guess, wantUTC, wantGuess)
		}
	}
	// Matched to an IANA zone with the European rules: DST changes apply.
	check("Mitteleuropaeische Zeit (eigene)", "20260328T090000", "2026-03-28T08:00:00Z", false)
	check("Mitteleuropaeische Zeit (eigene)", "20260330T090000", "2026-03-30T07:00:00Z", false)
	check("Mitteleuropaeische Zeit (eigene)", "20261026T090000", "2026-10-26T08:00:00Z", false)
	// The old US form (BYMONTHDAY range with BYDAY) matches US Eastern.
	check("Old US Eastern", "20260307T090000", "2026-03-07T14:00:00Z", false)
	check("Old US Eastern", "20260309T090000", "2026-03-09T13:00:00Z", false)
	// No zone has +01:17: fixed offset, marked as a guess.
	check("Strange Zone", "20260601T090000", "2026-06-01T07:43:00Z", true)
	// X-LIC-LOCATION wins.
	check("Named", "20260701T090000", "2026-07-01T07:00:00Z", false)
	// Unknown without VTIMEZONE: the NAS zone with guess.
	r2 := NewResolver(berlin(t), nil)
	v, err := r2.Parse(prop("DTSTART", "20260701T090000", "TZID", "Nowhere Standard Time"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Guess || v.Time.UTC().Hour() != 7 {
		t.Errorf("unknown TZID: %v guess %v", v.Time, v.Guess)
	}
}

func TestFloatingDateUTC(t *testing.T) {
	r := NewResolver(berlin(t), nil)
	v, err := r.Parse(prop("DTSTART", "20261110T090000"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != KindFloating || v.Time.Location().String() != "Europe/Berlin" || v.Time.UTC().Hour() != 8 {
		t.Errorf("floating: %v %v", v.Kind, v.Time)
	}
	if v.Format() != "20261110T090000" {
		t.Errorf("floating format %q", v.Format())
	}

	d, err := r.Parse(prop("DTSTART", "20261110", "VALUE", "DATE"))
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsDate() || d.Format() != "20261110" || d.Time.Location() != time.UTC {
		t.Errorf("date: %+v", d)
	}
	if got := d.InZone(berlin(t)).Format(time.RFC3339); got != "2026-11-10T00:00:00+01:00" {
		t.Errorf("date in zone: %s", got)
	}
	// Without VALUE=DATE an 8-digit value is still a date.
	if d2, err := r.Parse(prop("DTEND", "20261111")); err != nil || !d2.IsDate() {
		t.Errorf("bare date: %+v %v", d2, err)
	}

	u, err := r.Parse(prop("DTSTART", "20261110T090000Z", "TZID", "Europe/Berlin"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Kind != KindUTC || u.Time.Hour() != 9 || u.Format() != "20261110T090000Z" {
		t.Errorf("utc: %+v", u)
	}
}

func TestParseListAndProps(t *testing.T) {
	r := NewResolver(time.UTC, nil)
	vs, err := r.ParseList(prop("EXDATE", "20261110T090000,20261117T090000", "TZID", "Europe/Berlin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[1].Time.UTC().Format("2006-01-02T15") != "2026-11-17T08" {
		t.Fatalf("list: %+v", vs)
	}
	ds, err := r.ParseList(prop("EXDATE", "20261110,20261117", "VALUE", "DATE"))
	if err != nil || len(ds) != 2 || !ds[0].IsDate() {
		t.Fatalf("date list: %+v %v", ds, err)
	}
	ps, err := r.ParseList(prop("RDATE", "20261110T090000Z/PT1H30M,20261112T090000Z/20261112T100000Z", "VALUE", "PERIOD"))
	if err != nil || len(ps) != 2 || ps[0].End.Sub(ps[0].Time) != 90*time.Minute || ps[1].End.Sub(ps[1].Time) != time.Hour {
		t.Fatalf("periods: %+v %v", ps, err)
	}
	// Round trip through a property keeps the form.
	p := vs[0].Prop("RECURRENCE-ID")
	if p.Params.Get("TZID") != "Europe/Berlin" || p.Value != "20261110T090000" {
		t.Errorf("prop: %+v", p)
	}
	dp := ds[0].Prop("EXDATE")
	if dp.Params.Get("VALUE") != "DATE" || dp.Value != "20261110" {
		t.Errorf("date prop: %+v", dp)
	}
	for _, bad := range []string{"2026111", "20261310", "20261110T25", "20261110T250000", "2026-11-10", "20261110T090000ZZ", "x"} {
		if _, err := r.Parse(prop("DTSTART", bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	long := strings.TrimSuffix(strings.Repeat("20261110,", MaxListValues+1), ",")
	if _, err := r.ParseList(prop("EXDATE", long, "VALUE", "DATE")); err == nil {
		t.Error("list over the limit accepted")
	}
}

func TestDuration(t *testing.T) {
	cases := map[string]Duration{
		"PT1H30M":  {Exact: 90 * time.Minute},
		"P1D":      {Days: 1},
		"P2W":      {Days: 14},
		"-PT15M":   {Negative: true, Exact: 15 * time.Minute},
		"P1DT12H":  {Days: 1, Exact: 12 * time.Hour},
		"+PT0S":    {},
		"PT45M10S": {Exact: 45*time.Minute + 10*time.Second},
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v", in, got, err)
		}
		back, err := ParseDuration(got.String())
		if err != nil || back != got {
			t.Errorf("%s: round trip %q -> %+v %v", in, got.String(), back, err)
		}
	}
	for _, bad := range []string{"", "P", "PT", "1H", "PT1D", "P1H", "P1D1D", "P99999999999D", "PT1H2X"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// A nominal day across the October change lasts 25 hours.
	start := time.Date(2026, 10, 24, 9, 0, 0, 0, berlin(t))
	if end := (Duration{Days: 1}).AddTo(start); end.Sub(start) != 25*time.Hour || end.Hour() != 9 {
		t.Errorf("P1D across DST: %v", end)
	}
}
