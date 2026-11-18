// Package pimtest makes realistic calendars and address books for the
// measurements of 1.13 (docs/validation/performance-113.md): the size an
// owner has after some years with an iPhone. Only tests import it.
package pimtest

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// Item is one entry: the name of its file and its bytes.
type Item struct {
	Name string
	Data []byte
}

// fold folds a content line at 75 octets (RFC 5545 3.1, RFC 6350 3.2), as
// iOS writes it.
func fold(line string) string {
	if len(line) <= 75 {
		return line + "\r\n"
	}
	var b strings.Builder
	for len(line) > 75 {
		b.WriteString(line[:75])
		b.WriteString("\r\n ")
		line = line[75:]
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}

var (
	photoOnce sync.Once
	photo     []byte
)

// Photo is a JPEG of 320×320 px of about 25 KB, as iOS keeps a contact's
// picture in the card.
func Photo() []byte {
	photoOnce.Do(func() {
		r := rand.New(rand.NewPCG(1, 2))
		img := image.NewRGBA(image.Rect(0, 0, 320, 320))
		for y := range 320 {
			for x := range 320 {
				n := uint8(r.IntN(48))
				img.Set(x, y, color.RGBA{uint8(x*255/320)/2 + n, uint8(y*255/320)/2 + n, 120 + n, 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			panic(err)
		}
		photo = buf.Bytes()
	})
	return photo
}

// Contacts are n vCards 3.0 as an iPhone writes them: name, company, two
// e-mail addresses, two numbers, an address; nine in ten with a birthday
// (one in ten of those without a year, iOS 1604), every second with a
// picture (Photo).
func Contacts(n int) []Item {
	pic := base64.StdEncoding.EncodeToString(Photo())
	out := make([]Item, 0, n)
	for i := range n {
		var b strings.Builder
		b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\n")
		fmt.Fprintf(&b, "N:Nachname%04d;Vorname%04d;;;\r\n", i, i)
		fmt.Fprintf(&b, "FN:Vorname%04d Nachname%04d\r\n", i, i)
		fmt.Fprintf(&b, "ORG:Firma %d;\r\n", i%97)
		fmt.Fprintf(&b, "item1.EMAIL;type=INTERNET;type=pref:vorname%04d@example.org\r\n", i)
		fmt.Fprintf(&b, "EMAIL;type=INTERNET;type=WORK:n%04d@firma%d.example\r\n", i, i%97)
		fmt.Fprintf(&b, "TEL;type=CELL;type=VOICE;type=pref:+49 170 %07d\r\n", 1000000+i)
		fmt.Fprintf(&b, "TEL;type=WORK;type=VOICE:089 %06d\r\n", 200000+i)
		fmt.Fprintf(&b, "item2.ADR;type=HOME;type=pref:;;Hauptstraße %d;München;;80331;Deutschland\r\n", i%300+1)
		if i%10 != 9 {
			month, day := i%12+1, i%28+1
			if i%10 == 3 {
				fmt.Fprintf(&b, "BDAY;X-APPLE-OMIT-YEAR=1604:1604-%02d-%02d\r\n", month, day)
			} else {
				fmt.Fprintf(&b, "BDAY;VALUE=date:%d-%02d-%02d\r\n", 1950+i%55, month, day)
			}
		}
		if i%2 == 0 {
			b.WriteString(fold("PHOTO;ENCODING=b;TYPE=JPEG:" + pic))
		}
		fmt.Fprintf(&b, "UID:perf-contact-%04d\r\nREV:2026-11-01T10:00:00Z\r\nEND:VCARD\r\n", i)
		out = append(out, Item{Name: fmt.Sprintf("perf-contact-%04d.vcf", i), Data: []byte(b.String())})
	}
	return out
}

// Calendar names the calendars of Calendars.
func Calendar(i int) string { return fmt.Sprintf("kal%d", i+1) }

// Base is the middle of the events: 2026-11-18 (the day of the
// measurement); singles lie a year before and after it.
var Base = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)

// Calendars are the events of `calendars` calendars, `events` in all,
// `series` of them recurring (weekly, daily with COUNT, monthly, yearly,
// some with EXDATE and an override): one file per UID, as an iPhone
// keeps them, in Europe/Berlin. One event in a hundred has a title
// longer than the index keeps (clipped). The result maps a calendar to
// its entries.
func Calendars(calendars, events, series int) map[string][]Item {
	out := map[string][]Item{}
	r := rand.New(rand.NewPCG(7, 11))
	for i := range events {
		cal := Calendar(i % calendars)
		uid := fmt.Sprintf("perf-event-%05d", i)
		var b strings.Builder
		b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\nCALSCALE:GREGORIAN\r\n")
		day := Base.AddDate(0, 0, r.IntN(730)-365)
		hour := 7 + r.IntN(12)
		start := time.Date(day.Year(), day.Month(), day.Day(), hour, 15*r.IntN(4), 0, 0, time.UTC)
		end := start.Add(time.Duration(30+30*r.IntN(4)) * time.Minute)
		title := fmt.Sprintf("Termin %d mit Besprechung", i)
		if i%100 == 42 {
			title = strings.Repeat("Sehr langer Titel einer Besprechung ", 10) + fmt.Sprint(i)
		}
		event := func(extra string) {
			b.WriteString("BEGIN:VEVENT\r\n")
			fmt.Fprintf(&b, "UID:%s\r\nDTSTAMP:20261101T100000Z\r\nCREATED:20260101T100000Z\r\n", uid)
			b.WriteString(extra)
			b.WriteString(fold("SUMMARY:" + title))
			fmt.Fprintf(&b, "LOCATION:Raum %d\r\nSEQUENCE:0\r\nTRANSP:OPAQUE\r\n", i%40)
			if i%5 == 0 {
				b.WriteString("BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Erinnerung\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n")
			}
			b.WriteString("END:VEVENT\r\n")
		}
		local := func(t time.Time) string { return t.Format("20060102T150405") }
		times := fmt.Sprintf("DTSTART;TZID=Europe/Berlin:%s\r\nDTEND;TZID=Europe/Berlin:%s\r\n", local(start), local(end))
		if i < series {
			// A series starts within the year before Base.
			dur := end.Sub(start)
			start = start.AddDate(-1, 0, 0)
			if start.After(Base) {
				start = start.AddDate(0, -6, 0)
			}
			end = start.Add(dur)
			times = fmt.Sprintf("DTSTART;TZID=Europe/Berlin:%s\r\nDTEND;TZID=Europe/Berlin:%s\r\n", local(start), local(end))
			var rule string
			switch i % 4 {
			case 0:
				rule = "RRULE:FREQ=WEEKLY;BYDAY=" + []string{"MO", "TU", "WE", "TH", "FR"}[i%5] + "\r\n"
			case 1:
				rule = "RRULE:FREQ=DAILY;COUNT=400\r\n"
			case 2:
				rule = "RRULE:FREQ=MONTHLY;BYDAY=2TU\r\n"
			default:
				rule = "RRULE:FREQ=YEARLY\r\n"
			}
			if i%8 == 0 {
				rule += "EXDATE;TZID=Europe/Berlin:" + local(start.AddDate(0, 0, 14)) + "\r\n"
			}
			event(times + rule)
			if i%8 == 0 {
				// An override of the third week.
				moved := start.AddDate(0, 0, 21)
				event(fmt.Sprintf("RECURRENCE-ID;TZID=Europe/Berlin:%s\r\nDTSTART;TZID=Europe/Berlin:%s\r\nDTEND;TZID=Europe/Berlin:%s\r\n",
					local(moved), local(moved.Add(time.Hour)), local(moved.Add(2*time.Hour))))
			}
		} else {
			event(times)
		}
		b.WriteString("END:VCALENDAR\r\n")
		out[cal] = append(out[cal], Item{Name: uid + ".ics", Data: []byte(b.String())})
	}
	return out
}
