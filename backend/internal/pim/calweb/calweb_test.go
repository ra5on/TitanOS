package calweb

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

var (
	berlin = mustZone("Europe/Berlin")
	now    = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
)

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func str(s string) *string { return &s }

func ics(lines ...string) []byte {
	return []byte(strings.Join(append(append([]string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//t//EN"}, lines...), "END:VCALENDAR", ""), "\r\n"))
}

func has(t *testing.T, data []byte, want ...string) {
	t.Helper()
	unfolded := strings.ReplaceAll(string(data), "\r\n ", "")
	for _, w := range want {
		if !strings.Contains(unfolded, w) {
			t.Errorf("missing %q in\n%s", w, data)
		}
	}
}

func lacks(t *testing.T, data []byte, not ...string) {
	t.Helper()
	for _, w := range not {
		if strings.Contains(string(data), w) {
			t.Errorf("unexpected %q in\n%s", w, data)
		}
	}
}

func TestBuild(t *testing.T) {
	in := Input{Title: str("Elternabend <b>3b</b>, Raum; 12"), Start: "2026-11-17T19:00", End: "2026-11-17T20:30", Timezone: "Europe/Berlin",
		Location: str("Grundschule am Park"), Notes: str("Zeile 1\r\nZeile 2"),
		Repeat: Opt[recur.Simple]{Set: true, Value: &recur.Simple{Freq: "monthly", ByDay: []string{"3TU"}, Count: 9}},
		Alarms: Opt[[]int]{Set: true, Value: &[]int{60, 0, 60}}}
	data, err := Build(in, "u-1", now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	has(t, data, "PRODID:"+ProdID, "BEGIN:VTIMEZONE", "TZID:Europe/Berlin", "X-LIC-LOCATION:Europe/Berlin", "UID:u-1", "DTSTAMP:20261118T100000Z",
		"DTSTART;TZID=Europe/Berlin:20261117T190000", "DTEND;TZID=Europe/Berlin:20261117T203000",
		"RRULE:FREQ=MONTHLY;COUNT=9;BYDAY=3TU", `SUMMARY:Elternabend <b>3b</b>\, Raum\; 12`, `DESCRIPTION:Zeile 1\nZeile 2`,
		"TRIGGER:-PT1H", "TRIGGER:PT0S", "ACTION:DISPLAY")
	if strings.Count(string(data), "BEGIN:VALARM") != 2 {
		t.Fatalf("alarms: %s", data)
	}
	f, err := recur.Read(data, recur.Options{Local: berlin})
	if err != nil || f.Series[0].Master.Rule == nil {
		t.Fatalf("read back: %v", err)
	}
	v, err := Open(data, "", berlin)
	if err != nil || v.Title != *in.Title || v.Start != "2026-11-17T19:00:00+01:00" || v.Timezone != "Europe/Berlin" || v.Rule == nil ||
		v.Rule.Text["de"] == "" || len(v.Alarms) != 2 || *v.Notes != "Zeile 1\nZeile 2" {
		t.Fatalf("view %+v %v", v, err)
	}
	day, err := Build(Input{Title: str("Urlaub"), Start: "2026-12-24", End: "2026-12-27", AllDay: true}, "u-2", now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	has(t, day, "DTSTART;VALUE=DATE:20261224", "DTEND;VALUE=DATE:20261227")
	lacks(t, day, "VTIMEZONE")
	for _, bad := range []Input{
		{Start: ""}, {Start: "2026-11-17T19:00", End: "2026-11-17T18:00"}, {Start: "2026-11-17T19:00", Timezone: "Mars/Olympus"},
		{Start: "2026-11-17", End: "2026-11-17", AllDay: true}, {Start: "2026-11-17T19:00", Title: str("a\x00b")},
		{Start: "2026-11-17T19:00", Alarms: Opt[[]int]{Set: true, Value: &[]int{-1440}}},
		{Start: "2026-11-17T19:00", Alarms: Opt[[]int]{Set: true, Value: &[]int{MaxAlarmMinutes + 1}}},
		{Start: "2026-11-17T19:00", Repeat: Opt[recur.Simple]{Set: true, Value: &recur.Simple{Freq: "hourly"}}},
	} {
		if _, err := Build(bad, "x", now, berlin); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// Windows names resolve; UTC is written with Z.
	win, err := Build(Input{Start: "2026-11-17T19:00", Timezone: "W. Europe Standard Time"}, "w", now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	has(t, win, "DTSTART;TZID=Europe/Berlin:20261117T190000")
	utc, _ := Build(Input{Start: "2026-11-17T19:00", Timezone: "UTC"}, "z", now, berlin)
	has(t, utc, "DTSTART:20261117T190000Z")
}

// 1.13: an all-day event's "am Tag um 9:00" is an alarm after the start
// (TRIGGER:PT9H, −540 minutes), "am Vortag um 9:00" one before it (900);
// both are written as DISPLAY alarms, read back and changed without
// touching an alarm TitanOS cannot show (a day after the start, absolute).
func TestAllDayAlarmAfterStart(t *testing.T) {
	data, err := Build(Input{Title: str("Geburtstag Oma"), Start: "2026-12-03", End: "2026-12-04", AllDay: true,
		Alarms: Opt[[]int]{Set: true, Value: &[]int{900, -540}}}, "a-1", now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	has(t, data, "TRIGGER:PT9H", "TRIGGER:-PT15H", "ACTION:DISPLAY")
	v, err := Open(data, "", berlin)
	if err != nil || len(v.Alarms) != 2 || v.Alarms[0] != -540 || v.Alarms[1] != 900 {
		t.Fatalf("alarms read back %v %v", v.Alarms, err)
	}
	foreign := ics("BEGIN:VEVENT", "UID:a-2", "DTSTAMP:20261118T100000Z", "DTSTART;VALUE=DATE:20261203", "DTEND;VALUE=DATE:20261204",
		"SUMMARY:x", "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:P1DT2H", "END:VALARM",
		"BEGIN:VALARM", "ACTION:AUDIO", "TRIGGER;VALUE=DATE-TIME:20261202T080000Z", "END:VALARM",
		"BEGIN:VALARM", "ACTION:DISPLAY", "X-WR-ALARMUID:keep-me", "TRIGGER:PT9H", "END:VALARM", "END:VEVENT")
	v, err = Open(foreign, "", berlin)
	if err != nil || len(v.Alarms) != 1 || v.Alarms[0] != -540 {
		t.Fatalf("foreign alarms %v %v", v.Alarms, err)
	}
	res, changed, err := Edit(foreign, "", recur.ScopeAll, Input{Alarms: Opt[[]int]{Set: true, Value: &[]int{-540, 2340}}}, EditOptions{Now: now, Local: berlin})
	if err != nil || !changed {
		t.Fatalf("edit: %v %v", changed, err)
	}
	has(t, res.Data, "TRIGGER:P1DT2H", "TRIGGER;VALUE=DATE-TIME:20261202T080000Z", "X-WR-ALARMUID:keep-me", "TRIGGER:-PT39H")
	if strings.Count(string(res.Data), "TRIGGER:PT9H") != 1 {
		t.Fatalf("the kept alarm once: %s", res.Data)
	}
}

// The edit keeps what TitanOS does not show and touches only what
// changed.
func TestEditKeepsTheRest(t *testing.T) {
	data := ics("BEGIN:VTIMEZONE", "TZID:W. Europe Standard Time", "BEGIN:STANDARD", "DTSTART:16010101T030000", "TZOFFSETFROM:+0200", "TZOFFSETTO:+0100",
		"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU", "END:STANDARD", "BEGIN:DAYLIGHT", "DTSTART:16010101T020000", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0200",
		"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", "END:DAYLIGHT", "END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:s", "DTSTAMP:20261101T000000Z", "DTSTART;TZID=W. Europe Standard Time:20261103T090000",
		"DTEND;TZID=W. Europe Standard Time:20261103T100000", "RRULE:FREQ=WEEKLY;COUNT=10", "SUMMARY:Woche", "X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
		"BEGIN:VALARM", "X-WR-ALARMUID:keep-me", "ACTION:AUDIO", "TRIGGER:-PT15M", "END:VALARM",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;RELATED=END:PT0S", "DESCRIPTION:x", "END:VALARM",
		"END:VEVENT")
	v, err := Open(data, "20261110T080000Z", berlin)
	if err != nil || v.Start != "2026-11-10T09:00:00+01:00" || v.Timezone != "Europe/Berlin" || len(v.Alarms) != 1 || v.Alarms[0] != 15 {
		t.Fatalf("open: %+v %v", v, err)
	}
	// The UI sends back what it saw, with a new title and a second alarm.
	in := Input{Title: str("Woche neu"), Start: "2026-11-10T09:00", End: "2026-11-10T10:00", Timezone: v.Timezone,
		Repeat: Opt[recur.Simple]{Set: true, Value: &v.Rule.Simple}, Alarms: Opt[[]int]{Set: true, Value: &[]int{15, 60}}}
	res, changed, err := Edit(data, v.Occurrence, recur.ScopeAll, in, EditOptions{Local: berlin, Now: now})
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	has(t, res.Data, "SUMMARY:Woche neu", "X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC", "X-WR-ALARMUID:keep-me", "TRIGGER;RELATED=END:PT0S",
		"TRIGGER:-PT1H", "DTSTART;TZID=W. Europe Standard Time:20261103T090000", "RRULE:FREQ=WEEKLY;COUNT=10", "TZID:W. Europe Standard Time")
	if strings.Count(string(res.Data), "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("zones: %s", res.Data)
	}
	// Nothing new: nothing to write.
	same := Input{Title: str("Woche"), Start: "2026-11-10T09:00", End: "2026-11-10T10:00", Repeat: Opt[recur.Simple]{Set: true, Value: &v.Rule.Simple}}
	if _, changed, err := Edit(data, v.Occurrence, recur.ScopeAll, same, EditOptions{Local: berlin, Now: now}); err != nil || changed {
		t.Fatalf("unchanged input wrote: %v %v", changed, err)
	}
	// "All" from the second occurrence one hour later moves the whole
	// series by an hour, not to the second week.
	moved := Input{Start: "2026-11-10T10:00", End: "2026-11-10T11:30"}
	res, _, err = Edit(data, v.Occurrence, recur.ScopeAll, moved, EditOptions{Local: berlin, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	has(t, res.Data, "DTSTART;TZID=W. Europe Standard Time:20261103T100000", "DTEND;TZID=W. Europe Standard Time:20261103T113000")
	// "Following" with the rule as seen keeps the shortened COUNT.
	res, _, err = Edit(data, v.Occurrence, recur.ScopeFollowing, Input{Title: str("Neu"), Repeat: Opt[recur.Simple]{Set: true, Value: &v.Rule.Simple}},
		EditOptions{Local: berlin, Now: now, NewUID: "n"})
	if err != nil {
		t.Fatal(err)
	}
	has(t, res.New, "UID:n", "RRULE:FREQ=WEEKLY;COUNT=9", "SUMMARY:Neu", "BEGIN:VTIMEZONE")
	has(t, res.Data, "UNTIL=20261110T075959Z")
	// "This" never gets a rule; a new zone brings its VTIMEZONE.
	res, _, err = Edit(data, v.Occurrence, recur.ScopeThis, Input{Start: "2026-11-10T09:00", End: "2026-11-10T10:00", Timezone: "America/New_York",
		Repeat: Opt[recur.Simple]{Set: true, Value: &recur.Simple{Freq: "daily"}}}, EditOptions{Local: berlin, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	has(t, res.Data, "RECURRENCE-ID;TZID=W. Europe Standard Time:20261110T090000", "DTSTART;TZID=America/New_York:20261110T090000", "TZID:America/New_York")
	if strings.Count(string(res.Data), "RRULE:FREQ=WEEKLY") != 1 || strings.Contains(string(res.Data), "FREQ=DAILY") {
		t.Fatalf("rule on the override: %s", res.Data)
	}
}

func TestSplitICS(t *testing.T) {
	data := []byte("\uFEFFBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//Microsoft Corporation//Outlook 16.0//EN\nMETHOD:PUBLISH\nX-WR-CALNAME:Arbeit\n" +
		"BEGIN:VTIMEZONE\nTZID:W. Europe Standard Time\nBEGIN:STANDARD\nDTSTART:16010101T030000\nTZOFFSETFROM:+0200\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE\n" +
		"BEGIN:VTIMEZONE\nTZID:Unused\nBEGIN:STANDARD\nDTSTART:16010101T030000\nTZOFFSETFROM:+0200\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE\n" +
		"BEGIN:VEVENT\nUID:a\nDTSTART;TZID=\"W. Europe Standard Time\":20261103T090000\nRRULE:FREQ=WEEKLY\nSUMMARY:Lang\n er Titel\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nDTSTART:20261104T090000Z\nSUMMARY:ohne UID\nBEGIN:VALARM\nUID:alarm\nTRIGGER:-PT5M\nACTION:DISPLAY\nEND:VALARM\nEND:VEVENT\n" +
		"BEGIN:VTODO\nUID:todo\nEND:VTODO\n" +
		"BEGIN:VEVENT\nUID:a\nRECURRENCE-ID;TZID=W. Europe Standard Time:20261110T090000\nDTSTART;TZID=W. Europe Standard Time:20261110T100000\nEND:VEVENT\n" +
		"END:VCALENDAR\n")
	sp, err := SplitICS(data, func() string { return "gen" })
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Groups) != 2 || sp.Groups[0].UID != "a" || len(sp.Groups[0].Events) != 2 || sp.Groups[1].UID != "gen" ||
		len(sp.Failed) != 1 || sp.Failed[0] != (Failure{UID: "todo", Reason: "unsupported_component"}) {
		t.Fatalf("split %+v %+v", sp.Groups, sp.Failed)
	}
	a := sp.File(sp.Groups[0])
	has(t, a, "PRODID:-//Microsoft Corporation//Outlook 16.0//EN", "TZID:W. Europe Standard Time", "SUMMARY:Langer Titel", "RECURRENCE-ID")
	lacks(t, a, "METHOD", "X-WR-CALNAME", "Unused", "VTODO", "\uFEFF")
	f, err := recur.Read(a, recur.Options{Local: berlin})
	if err != nil || len(f.Series) != 1 || len(f.Series[0].Overrides) != 1 || f.Series[0].Master.TZGuess {
		t.Fatalf("group a: %v", err)
	}
	if !strings.HasPrefix(string(sp.File(sp.Groups[1])), "BEGIN:VCALENDAR\r\n") || !strings.Contains(string(sp.File(sp.Groups[1])), "BEGIN:VEVENT\r\nUID:gen\r\n") {
		t.Fatalf("group gen: %s", sp.File(sp.Groups[1]))
	}
	for _, bad := range []string{"", "hello", "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:x\n", "BEGIN:VCALENDAR\nBEGIN:VEVENT\nEND:VTODO\nEND:VCALENDAR\n"} {
		if _, err := SplitICS([]byte(bad), NewUID); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// paddedZone is a VTIMEZONE of about n bytes (X-PAD lines), LF only.
func paddedZone(tzid string, n int) string {
	var b strings.Builder
	b.WriteString("BEGIN:VTIMEZONE\nTZID:" + tzid + "\nBEGIN:STANDARD\nDTSTART:16010101T000000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0100\nEND:STANDARD\n")
	for b.Len()+68+16 <= n {
		b.WriteString("X-PAD:" + strings.Repeat("x", 60) + "\n")
	}
	b.WriteString("END:VTIMEZONE\n")
	return b.String()
}

// A VTIMEZONE over 64 KiB is not kept: Size fails the groups that name
// it, before a file is built; a long PRODID is not copied into every
// file; Size is the length of File (S-19).
func TestSplitBounds(t *testing.T) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:" + strings.Repeat("p", 300) + "\nCALSCALE:GREGORIAN\n")
	b.WriteString(paddedZone("Big/Zone", 10*recur.MaxZoneBytes))
	b.WriteString(paddedZone("Big/Zone", 1000)) // the first one counts
	// 64 KiB raw with LF, over it with CRLF.
	b.WriteString(paddedZone("Grown/Zone", recur.MaxZoneBytes-500))
	b.WriteString(paddedZone("Ok/Zone", 4000))
	for i, id := range []string{"Big/Zone", "Grown/Zone", "Ok/Zone", "Ok/Zone"} {
		fmt.Fprintf(&b, "BEGIN:VEVENT\nUID:e%d\nDTSTART;TZID=%s:20261103T090000\nDTEND;TZID=Ok/Zone:20261103T100000\nEND:VEVENT\n", i, id)
	}
	b.WriteString("END:VCALENDAR\n")
	sp, err := SplitICS([]byte(b.String()), NewUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Zones) != 1 || sp.Zones["Ok/Zone"] == "" || sp.ProdID != "" || sp.CalScale != "GREGORIAN" {
		t.Fatalf("zones %d, prodid %q", len(sp.Zones), sp.ProdID)
	}
	for i, g := range sp.Groups {
		n, err := sp.Size(g)
		if i < 2 {
			if !errors.Is(err, ErrZoneTooLarge) {
				t.Errorf("group %s: %d %v", g.UID, n, err)
			}
			continue
		}
		file := sp.File(g)
		if err != nil || n != len(file) {
			t.Errorf("group %s: size %d %v, file %d", g.UID, n, err, len(file))
		}
		has(t, file, "PRODID:"+ProdID, "CALSCALE:GREGORIAN", "TZID:Ok/Zone")
		if _, err := recur.Read(file, recur.Options{Local: berlin, MaxZone: recur.MaxZoneBytes}); err != nil {
			t.Errorf("group %s: %v", g.UID, err)
		}
	}
}

// The zones of an export are bounded: past MaxExportZones they are left
// out (counted), the events still written (S-21).
func TestExportZonesBounded(t *testing.T) {
	var out strings.Builder
	exp := NewExport(&out)
	n := MaxExportZones/(60<<10) + 5
	var files [][]byte
	for i := range n {
		id := fmt.Sprintf("Zone/%d", i)
		files = append(files, []byte(strings.ReplaceAll("BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\n"+paddedZone(id, 60<<10)+
			fmt.Sprintf("BEGIN:VEVENT\nUID:u%d\nDTSTART;TZID=%s:20261103T090000\nEND:VEVENT\nEND:VCALENDAR\n", i, id), "\n", "\r\n")))
	}
	for _, f := range files {
		exp.Zones(f)
		exp.Zones(f) // seen twice, counted once
	}
	if exp.bytes > MaxExportZones || exp.LeftOut == 0 || len(exp.order)+exp.LeftOut != n {
		t.Fatalf("zones %d bytes, kept %d, left out %d of %d", exp.bytes, len(exp.order), exp.LeftOut, n)
	}
	if err := exp.Begin("Test"); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := exp.Events(f); err != nil {
			t.Fatal(err)
		}
	}
	_ = exp.End()
	if got := strings.Count(out.String(), "BEGIN:VTIMEZONE"); got != len(exp.order) || strings.Count(out.String(), "BEGIN:VEVENT") != n {
		t.Fatalf("export with %d zones, %d events", got, strings.Count(out.String(), "BEGIN:VEVENT"))
	}
}

func TestBirthdays(t *testing.T) {
	contacts := []pimstore.ObjectInfo{
		{Name: "anna.vcf", Entry: pimstore.Entry{FN: "Anna", BDay: "1991-11-12", UID: "a", ETag: "e1"}},
		{Name: "ben.vcf", Entry: pimstore.Entry{FN: "Ben", BDay: "--1120"}},
		{Name: "ios.vcf", Entry: pimstore.Entry{FN: "Ida", BDay: "1604-11-21"}},
		{Name: "leap.vcf", Entry: pimstore.Entry{FN: "Lea", BDay: "20000229"}},
		{Name: "group.vcf", Entry: pimstore.Entry{FN: "Gruppe", BDay: "1990-11-13", Group: true}},
		{Name: "bad.vcf", Entry: pimstore.Entry{FN: "X", BDay: "irgendwann"}},
	}
	got := Birthdays(contacts, time.Date(2026, 11, 1, 0, 0, 0, 0, berlin), time.Date(2026, 12, 1, 0, 0, 0, 0, berlin), berlin)
	if len(got) != 3 || got[0].Title != "Anna" || got[0].Start != "2026-11-12" || got[0].End != "2026-11-13" || *got[0].Birthday.Age != 35 ||
		got[1].Birthday.Age != nil || got[1].Start != "2026-11-20" || got[2].Birthday.Year != 0 || got[2].Start != "2026-11-21" || !got[0].ReadOnly {
		t.Fatalf("november: %+v", got)
	}
	feb := Birthdays(contacts, time.Date(2027, 2, 1, 0, 0, 0, 0, berlin), time.Date(2027, 3, 1, 0, 0, 0, 0, berlin), berlin)
	if len(feb) != 1 || feb[0].Start != "2027-02-28" || *feb[0].Birthday.Age != 27 {
		t.Fatalf("29 February in 2027: %+v", feb)
	}
	leap := Birthdays(contacts, time.Date(2028, 2, 1, 0, 0, 0, 0, berlin), time.Date(2028, 3, 1, 0, 0, 0, 0, berlin), berlin)
	if len(leap) != 1 || leap[0].Start != "2028-02-29" {
		t.Fatalf("29 February in 2028: %+v", leap)
	}
	if CountBirthdays(contacts) != 4 {
		t.Fatal("count")
	}
	for raw, want := range map[string][3]int{"1991-11-12T00:00:00Z": {1991, 11, 12}, "--11-12": {0, 11, 12}, "19911112": {1991, 11, 12}} {
		y, m, d, ok := ParseBDay(raw)
		if !ok || [3]int{y, int(m), d} != want {
			t.Errorf("%s: %d %d %d", raw, y, m, d)
		}
	}
}

func TestPreview(t *testing.T) {
	res, err := Preview(PreviewInput{Start: "2026-11-17T19:00", End: "2026-11-17T20:30", Timezone: "Europe/Berlin",
		Repeat: &recur.Simple{Freq: "monthly", ByDay: []string{"3TU"}, Until: "2027-07-13"}}, 5, berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Occurrences) != 5 || res.Occurrences[1].Start != "2026-12-15T19:00:00+01:00" || res.Total == nil || *res.Total != 8 || !res.More ||
		!strings.Contains(res.Text["de"], "dritten Dienstag") || !strings.Contains(res.Text["en"], "third Tuesday") {
		t.Fatalf("preview %+v", res)
	}
	open, err := Preview(PreviewInput{Start: "2026-11-17", AllDay: true, Repeat: &recur.Simple{Freq: "yearly"}}, 5, berlin)
	if err != nil || len(open.Occurrences) != 5 || open.Total != nil || open.Occurrences[4].Start != "2030-11-17" {
		t.Fatalf("open end %+v %v", open, err)
	}
	once, err := Preview(PreviewInput{Start: "2026-11-17T19:00"}, 5, berlin)
	if err != nil || len(once.Occurrences) != 1 || once.Text != nil || *once.Total != 1 {
		t.Fatalf("once %+v %v", once, err)
	}
}

func TestCalendarID(t *testing.T) {
	taken := map[string]bool{"arbeit": true}
	for name, want := range map[string]string{"Arbeit": "arbeit-2", "Fußball & Übung": "fussball-uebung", "  ": "kalender",
		"Geburtstage": "geburtstage-2", "Café Crème": "cafe-creme", "日本": "kalender"} {
		if got := CalendarID(name, func(id string) bool { return taken[id] }); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
	if id := CalendarID(strings.Repeat("a", 300), func(string) bool { return false }); len(id) > 56 || !pimstore.ValidCollection(id) {
		t.Fatalf("long: %q", id)
	}
}

// One event says how many occurrences its series has when the rule ends:
// EXDATEs left out, an override counted once; none without an end.
func TestOpenCountsOccurrences(t *testing.T) {
	series := func(rule string, extra ...string) []byte {
		lines := []string{"BEGIN:VEVENT", "UID:s", "DTSTAMP:20261101T000000Z", "DTSTART;TZID=Europe/Berlin:20261103T090000",
			"DURATION:PT30M", "RRULE:" + rule}
		lines = append(append(lines, extra...), "END:VEVENT")
		return ics(lines...)
	}
	count := func(data []byte, occurrence string) *int {
		t.Helper()
		v, err := Open(data, occurrence, berlin)
		if err != nil {
			t.Fatal(err)
		}
		return v.Occurrences
	}
	if n := count(series("FREQ=WEEKLY;COUNT=12", "EXDATE;TZID=Europe/Berlin:20261110T090000"), ""); n == nil || *n != 11 {
		t.Fatalf("count with exdate: %v", n)
	}
	withOverride := series("FREQ=WEEKLY;UNTIL=20270126T080000Z")
	withOverride = []byte(strings.Replace(string(withOverride), "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:s\r\nDTSTAMP:20261101T000000Z\r\n"+
		"RECURRENCE-ID;TZID=Europe/Berlin:20261117T090000\r\nDTSTART;TZID=Europe/Berlin:20261118T100000\r\nDURATION:PT30M\r\nEND:VEVENT\r\nEND:VCALENDAR", 1))
	if n := count(withOverride, "20261117T080000Z"); n == nil || *n != 13 {
		t.Fatalf("until with override: %v", n)
	}
	if n := count(series("FREQ=WEEKLY"), ""); n != nil {
		t.Fatalf("no end: %d", *n)
	}
	if n := count(series("FREQ=DAILY;COUNT=6000"), ""); n != nil {
		t.Fatalf("above the bound: %d", *n)
	}
	if n := count(ics("BEGIN:VEVENT", "UID:x", "DTSTAMP:20261101T000000Z", "DTSTART:20261110T080000Z", "END:VEVENT"), ""); n != nil {
		t.Fatalf("single: %d", *n)
	}
}
