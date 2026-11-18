package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

const outlookZone = `BEGIN:VTIMEZONE
TZID:W. Europe Standard Time
BEGIN:STANDARD
DTSTART:16010101T030000
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=10
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:16010101T020000
TZOFFSETFROM:+0100
TZOFFSETTO:+0200
RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=3
END:DAYLIGHT
END:VTIMEZONE
`

func outlookFile(titleA string) []byte {
	return crlf(`
BEGIN:VCALENDAR
PRODID:-//Microsoft Corporation//Outlook 16.0 MIMEDIR//EN
VERSION:2.0
METHOD:PUBLISH
X-WR-CALNAME:Export
` + outlookZone + `BEGIN:VEVENT
UID:a
DTSTAMP:20261101T000000Z
DTSTART;TZID="W. Europe Standard Time":20261110T090000
DTEND;TZID="W. Europe Standard Time":20261110T100000
SUMMARY:` + titleA + `
END:VEVENT
BEGIN:VEVENT
UID:b
DTSTAMP:20261101T000000Z
DTSTART:20261111T080000Z
RRULE:FREQ=DAILY;COUNT=3
SUMMARY:Reihe
END:VEVENT
BEGIN:VTODO
UID:todo
SUMMARY:Aufgabe
END:VTODO
BEGIN:VEVENT
UID:b
DTSTAMP:20261101T000000Z
RECURRENCE-ID:20261112T080000Z
DTSTART:20261112T100000Z
SUMMARY:Reihe verschoben
END:VEVENT
BEGIN:VEVENT
UID:c
DTSTAMP:20261101T000000Z
DTSTART:20261113T080000Z
SUMMARY:Vom iPhone
END:VEVENT
BEGIN:VEVENT
DTSTAMP:20261101T000000Z
DTSTART:20261114T080000Z
SUMMARY:Ohne UID
END:VEVENT
END:VCALENDAR
`)
}

// Import: one file per UID with its VTIMEZONE (a Windows name), doubles by
// UID skipped or replaced in the file they have, other components failed.
func TestCalendarImport(t *testing.T) {
	e := newCalEnv(t)
	// The iPhone made c under its own name.
	e.seed("privat", "C0FFEE-1.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:c\nDTSTAMP:20261101T000000Z\nDTSTART:20261113T080000Z\nSUMMARY:Vom iPhone\nEND:VEVENT\nEND:VCALENDAR\n")
	out := e.importICS("privat", "skip", outlookFile("Besprechung"), http.StatusOK)
	if out.Added != 3 || out.Updated != 0 || out.Skipped != 1 || len(out.Failed) != 1 || out.Failed[0] != (calweb.Failure{UID: "todo", Reason: "unsupported_component"}) {
		t.Fatalf("first import %+v", out)
	}
	raw := e.raw("privat", "a.ics")
	mustHave(t, "a", raw, "TZID:W. Europe Standard Time", `DTSTART;TZID="W. Europe Standard Time":20261110T090000`, "PRODID:-//Microsoft Corporation")
	mustLack(t, "a", raw, "METHOD", "X-WR-CALNAME", "VTODO", "UID:b")
	mustHave(t, "b", e.raw("privat", "b.ics"), "RECURRENCE-ID:20261112T080000Z", "SUMMARY:Reihe verschoben", "RRULE:FREQ=DAILY;COUNT=3")
	list := e.events("from=2026-11-01&to=2026-12-01")
	var a calweb.View
	for _, v := range list.Events {
		if v.UID == "a" {
			a = v
		}
	}
	if a.Start != "2026-11-10T09:00:00+01:00" || a.Timezone != "Europe/Berlin" || a.TZGuess {
		t.Fatalf("windows zone: %+v", a)
	}
	if len(list.Events) != 1+3+1+1 {
		t.Fatalf("events: %d", len(list.Events))
	}

	// Again with a changed a: skip keeps it, replace writes it into a.ics
	// and c into the iPhone's file.
	out = e.importICS("privat", "skip", outlookFile("Besprechung neu"), http.StatusOK)
	if out.Added != 1 || out.Skipped != 3 || out.Updated != 0 {
		t.Fatalf("skip %+v", out)
	}
	mustHave(t, "skipped", e.raw("privat", "a.ics"), "SUMMARY:Besprechung\r")
	cBefore := e.raw("privat", "C0FFEE-1.ics")
	out = e.importICS("privat", "replace", outlookFile("Besprechung neu"), http.StatusOK)
	if out.Added != 1 || out.Updated != 2 || out.Skipped != 1 {
		t.Fatalf("replace %+v", out)
	}
	mustHave(t, "replaced", e.raw("privat", "a.ics"), "SUMMARY:Besprechung neu")
	if c := e.raw("privat", "C0FFEE-1.ics"); c == cBefore || !strings.Contains(c, "PRODID:-//Microsoft") {
		t.Fatalf("c not replaced in its file:\n%s", c)
	}
	if _, err := e.pim.Stat(pimstore.Calendar, "alex", "privat", "c.ics"); err == nil {
		t.Fatal("c doubled")
	}
	for _, bad := range [][]byte{[]byte("kein Kalender"), []byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n")} {
		body, ctype := multipartICS(t, bad)
		req, _ := http.NewRequest("POST", e.ts.url+"/api/v1/calendar/calendars/privat/import", body)
		req.Header.Set("Content-Type", ctype)
		res, _ := e.client.Do(req)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("bad file: %d", res.StatusCode)
		}
		res.Body.Close()
	}
	if res, body := e.ts.request("POST", "/api/v1/calendar/calendars/privat/import?onDuplicate=merge", nil); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "calendar_invalid" {
		t.Fatalf("onDuplicate: %d %s", res.StatusCode, body)
	}
	body, ctype := multipartICS(t, outlookFile("x"))
	req, _ := http.NewRequest("POST", e.ts.url+"/api/v1/calendar/calendars/geburtstage/import", body)
	req.Header.Set("Content-Type", ctype)
	if res, _ := e.client.Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatalf("into the birthdays: %d", res.StatusCode)
	}
}

// 5000 events in one file: under 10 s, RAM bounded (measured and printed;
// t.TempDir is tmpfs on the VM, so fsync costs nothing there).
func TestCalendarImport5000(t *testing.T) {
	e := newCalEnv(t)
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" + strings.ReplaceAll(outlookZone, "\n", "\r\n"))
	start := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	for i := range 5000 {
		at := start.Add(time.Duration(i) * 3 * time.Hour)
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:ev-%d@import.example\r\nDTSTAMP:20261101T000000Z\r\nDTSTART;TZID=W. Europe Standard Time:%s\r\nDURATION:PT1H\r\nSUMMARY:Termin %d\r\nLOCATION:Raum %d\r\n",
			i, at.In(calBerlin).Format("20060102T150405"), i, i%50)
		if i%10 == 0 {
			b.WriteString("RRULE:FREQ=WEEKLY;COUNT=10\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	data := []byte(b.String())
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Gross"}, http.StatusCreated, nil)

	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak uint64
	var mu sync.Mutex
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				runtime.ReadMemStats(&m)
				mu.Lock()
				peak = max(peak, m.HeapAlloc)
				mu.Unlock()
			}
		}
	}()
	began := time.Now()
	out := e.importICS("gross", "skip", data, http.StatusOK)
	took := time.Since(began)
	close(stop)
	<-done
	extra := int64(peak) - int64(base.HeapAlloc)
	t.Logf("import of 5000 events (%d KiB): %v, heap peak +%d MiB over %d MiB before", len(data)>>10, took, extra>>20, base.HeapAlloc>>20)
	if out.Added != 5000 || len(out.Failed) != 0 {
		t.Fatalf("import %+v", out)
	}
	if took > 10*time.Second {
		t.Fatalf("import took %v", took)
	}
	if extra > 40*int64(len(data)) {
		t.Fatalf("import held %d MiB for a %d KiB file", extra>>20, len(data)>>10)
	}
	col, _ := e.pim.GetCollection(pimstore.Calendar, "alex", "gross")
	if col.Count != 5000 {
		t.Fatalf("count %d", col.Count)
	}
	began = time.Now()
	list := e.events("from=2026-03-01&to=2026-04-01&calendars=gross")
	t.Logf("one month of 5000 events: %d occurrences in %v", len(list.Events), time.Since(began))
	if len(list.Events) < 248 {
		t.Fatalf("march: %d", len(list.Events))
	}
}

// Export: one VCALENDAR, each VTIMEZONE once, a download that never runs
// as a page; imported again it gives the same events.
func TestCalendarExport(t *testing.T) {
	e := newCalEnv(t)
	e.importICS("privat", "skip", outlookFile("Besprechung"), http.StatusOK)
	e.seed("privat", "second-zone.ics", "\nBEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\n"+outlookZone+"BEGIN:VEVENT\nUID:z2\nDTSTAMP:20261101T000000Z\nDTSTART;TZID=W. Europe Standard Time:20261120T090000\nSUMMARY:Noch einer\nEND:VEVENT\nEND:VCALENDAR\n")
	res, err := e.client.Get(e.ts.url + "/api/v1/calendar/calendars/privat/export")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	h := res.Header
	if res.StatusCode != http.StatusOK || h.Get("Content-Type") != "text/calendar; charset=utf-8" || h.Get("Content-Disposition") != `attachment; filename="privat.ics"` ||
		h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("export: %d %v", res.StatusCode, h)
	}
	s := string(data)
	if strings.Count(s, "BEGIN:VCALENDAR") != 1 || strings.Count(s, "BEGIN:VTIMEZONE") != 1 || strings.Count(s, "BEGIN:VEVENT") != 6 ||
		!strings.Contains(s, "X-WR-CALNAME:Privat") || !strings.HasSuffix(s, "END:VCALENDAR\r\n") {
		t.Fatalf("export body:\n%s", s)
	}
	e.do("POST", "/api/v1/calendar/calendars", map[string]any{"name": "Kopie"}, http.StatusCreated, nil)
	out := e.importICS("kopie", "skip", data, http.StatusOK)
	if out.Added != 5 || len(out.Failed) != 0 {
		t.Fatalf("re-import %+v", out)
	}
	shape := func(cal string) []string {
		var got []string
		for _, v := range e.events("from=2026-11-01&to=2026-12-01&calendars=" + cal).Events {
			got = append(got, v.UID+"|"+v.Title+"|"+v.Start+"|"+v.End+"|"+v.Timezone)
		}
		slices.Sort(got)
		return got
	}
	if a, b := shape("privat"), shape("kopie"); !slices.Equal(a, b) {
		t.Fatalf("export and import differ:\n%v\n%v", a, b)
	}
	if res, _ := e.client.Get(e.ts.url + "/api/v1/calendar/calendars/geburtstage/export"); res.StatusCode != http.StatusNotFound {
		t.Fatalf("export of the birthdays: %d", res.StatusCode)
	}
}

// Birthdays come from BDAY of the contacts on every query, read-only.
func TestCalendarBirthdays(t *testing.T) {
	e := newCalEnv(t)
	cards := map[string]string{
		"anna.vcf":  "UID:anna\nFN:Anna Beispiel\nN:Beispiel;Anna;;;\nBDAY:1991-11-12",
		"ben.vcf":   "UID:ben\nFN:Ben\nN:;Ben;;;\nBDAY:--1120",
		"ida.vcf":   "UID:ida\nFN:Ida\nN:;Ida;;;\nBDAY;X-APPLE-OMIT-YEAR=1604;VALUE=date:1604-11-21",
		"lea.vcf":   "UID:lea\nFN:Lea\nN:;Lea;;;\nBDAY:20000229",
		"group.vcf": "UID:g\nFN:Familie\nN:Familie;;;;\nBDAY:1990-11-13\nX-ADDRESSBOOKSERVER-KIND:group",
		"none.vcf":  "UID:n\nFN:Ohne\nN:;Ohne;;;",
	}
	for name, body := range cards {
		if _, err := e.pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, name, crlf("\nBEGIN:VCARD\nVERSION:3.0\n"+body+"\nEND:VCARD\n"), pimstore.PutOptions{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var cals []calendarView
	e.do("GET", "/api/v1/calendar/calendars", nil, http.StatusOK, &cals)
	if b := cals[len(cals)-1]; b.ID != "geburtstage" || b.Count != 4 || b.Unavailable {
		t.Fatalf("birthdays calendar %+v", b)
	}
	nov := e.events("from=2026-11-01&to=2026-12-01").Events
	if len(nov) != 3 || nov[0].Kind != "birthday" || nov[0].Birthday.Name != "Anna Beispiel" || *nov[0].Birthday.Age != 35 || nov[0].Start != "2026-11-12" ||
		nov[1].Birthday.Age != nil || nov[1].Start != "2026-11-20" || nov[2].Birthday.Year != 0 || nov[2].Start != "2026-11-21" || !nov[0].ReadOnly {
		t.Fatalf("november %+v", nov)
	}
	if feb := e.events("from=2027-02-01&to=2027-03-01").Events; len(feb) != 1 || feb[0].Start != "2027-02-28" || *feb[0].Birthday.Age != 27 {
		t.Fatalf("29 February in 2027: %+v", feb)
	}
	var one calweb.View
	e.do("GET", "/api/v1/calendar/events/geburtstage/anna.vcf?occurrence=20261112", nil, http.StatusOK, &one)
	if one.Kind != "birthday" || one.Calendar != "geburtstage" || one.Start != "2026-11-12" {
		t.Fatalf("one birthday %+v", one)
	}
	for _, rt := range [][2]string{{"PUT", "/api/v1/calendar/events/geburtstage/anna.vcf"}, {"DELETE", "/api/v1/calendar/events/geburtstage/anna.vcf?scope=all&etag=x"}} {
		if res, body := e.ts.request(rt[0], rt[1], map[string]any{"etag": "x", "event": map[string]any{}}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "calendar_read_only" {
			t.Errorf("%s: %d %s", rt[0], res.StatusCode, body)
		}
	}
	// Nothing about birthdays is stored in the calendars.
	if objs, _ := e.pim.Objects(pimstore.Calendar, "alex", pimstore.DefaultCalendar); len(objs) != 0 {
		t.Fatalf("stored: %v", objs)
	}
}

func TestCalendarPreview(t *testing.T) {
	e := newCalEnv(t)
	var out calweb.PreviewResult
	e.do("POST", "/api/v1/calendar/preview", map[string]any{"start": "2026-11-17T19:00", "end": "2026-11-17T20:30", "timezone": "Europe/Berlin",
		"repeat": map[string]any{"freq": "monthly", "byDay": []string{"3TU"}, "until": "2027-07-13"}}, http.StatusOK, &out)
	if len(out.Occurrences) != 5 || out.Occurrences[2].Start != "2027-01-19T19:00:00+01:00" || out.Total == nil || *out.Total != 8 ||
		out.Text["de"] != "jeden Monat am dritten Dienstag, bis 13.07.2027" || !strings.HasPrefix(out.Text["en"], "every month on the third Tuesday") {
		t.Fatalf("preview %+v", out)
	}
	if res, body := e.ts.request("POST", "/api/v1/calendar/preview", map[string]any{"start": "2026-11-17T19:00", "repeat": map[string]any{"freq": "secondly"}}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "calendar_invalid" {
		t.Fatalf("bad rule: %d %s", res.StatusCode, body)
	}
}

// The journal names account, calendar and a code – never titles or places
// (TECHNIK 8.3 question 26).
func TestCalendarJournalWithoutContent(t *testing.T) {
	var mu sync.Mutex
	var log strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(calLog(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return log.Write(p)
	}), &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)
	e := newCalEnv(t)
	secret := "Geheimtermin Dr. Vertraulich"
	file := strings.Replace(string(outlookFile(secret)), "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:kaputt\r\nSUMMARY:"+secret+"\r\nLOCATION:Hinterhof 7\r\nEND:VEVENT\r\nEND:VCALENDAR", 1)
	out := e.importICS("privat", "skip", []byte(file), http.StatusOK)
	if len(out.Failed) != 2 || out.Failed[1] != (calweb.Failure{UID: "kaputt", Reason: "invalid"}) {
		t.Fatalf("import %+v", out)
	}
	e.do("POST", "/api/v1/calendar/events", map[string]any{"calendar": "privat", "event": map[string]any{"title": secret, "start": "morgen"}}, http.StatusBadRequest, nil)
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(log.String(), "Geheim") || strings.Contains(log.String(), "Hinterhof") || !strings.Contains(log.String(), "calendar=privat") {
		t.Fatalf("journal:\n%s", log.String())
	}
}

type calLog func([]byte) (int, error)

func (f calLog) Write(p []byte) (int, error) { return f(p) }
