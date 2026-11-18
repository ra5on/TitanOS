package holidays

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func TestEaster(t *testing.T) {
	for year, want := range map[int]string{
		2000: "2000-04-23", 2019: "2019-04-21", 2024: "2024-03-31", 2025: "2025-04-20",
		2026: "2026-04-05", 2027: "2027-03-28", 2038: "2038-04-25", 2285: "2285-03-22",
	} {
		if got := Easter(year).Format("2006-01-02"); got != want {
			t.Errorf("Easter %d = %s, want %s", year, got, want)
		}
	}
}

func dates(list []Holiday) map[string]string {
	out := map[string]string{}
	for _, h := range list {
		out[h.Key] = h.Date.Format("2006-01-02")
	}
	return out
}

func TestHolidaysPerState(t *testing.T) {
	counts := map[string]int{"": 9, "by": 12, "bw": 12, "bb": 12, "be": 10, "mv": 11, "th": 11, "sn": 11, "sl": 12,
		"nw": 11, "rp": 11, "he": 10, "st": 11, "hb": 10, "hh": 10, "ni": 10, "sh": 10}
	for state, n := range counts {
		if got := len(For(2026, state)); got != n {
			t.Errorf("%q 2026: %d holidays, want %d: %v", state, got, n, dates(For(2026, state)))
		}
	}
	by := dates(For(2026, "by"))
	for key, want := range map[string]string{
		"goodFriday": "2026-04-03", "easterMonday": "2026-04-06", "ascension": "2026-05-14", "whitMonday": "2026-05-25",
		"corpusChristi": "2026-06-04", "epiphany": "2026-01-06", "allSaints": "2026-11-01", "unity": "2026-10-03",
	} {
		if by[key] != want {
			t.Errorf("Bayern %s = %s, want %s", key, by[key], want)
		}
	}
	if _, ok := by["assumption"]; ok {
		t.Error("Mariä Himmelfahrt holds only in parts of Bayern")
	}
	for year, want := range map[int]string{2023: "2023-11-22", 2024: "2024-11-20", 2025: "2025-11-19", 2026: "2026-11-18"} {
		if got := dates(For(year, "sn"))["repentance"]; got != want {
			t.Errorf("Buß- und Bettag %d = %s, want %s", year, got, want)
		}
	}
	// Days with a first year.
	if _, ok := dates(For(2022, "mv"))["womensDayMV"]; ok {
		t.Error("Frauentag in MV before 2023")
	}
	if _, ok := dates(For(2017, "hh"))["reformationNorth"]; ok {
		t.Error("Reformationstag in Hamburg as a state holiday before 2018")
	}
	// In order of the year.
	list := For(2026, "bb")
	for i := 1; i < len(list); i++ {
		if list[i].Date.Before(list[i-1].Date) {
			t.Fatalf("not in order: %v", list)
		}
	}
}

func TestParseRegion(t *testing.T) {
	for in, want := range map[string]string{"de": "", "de-by": "by", "de-th": "th"} {
		if got, ok := ParseRegion(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "by", "de-", "de-xx", "de-BY", "at", "de-by/../x", "de-by.ics"} {
		if _, ok := ParseRegion(in); ok {
			t.Errorf("%q valid", in)
		}
	}
}

func TestICS(t *testing.T) {
	data := ICS("by", "de", 2025, 2028)
	for _, l := range strings.Split(strings.TrimSuffix(string(data), "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("line of %d octets: %q", len(l), l)
		}
	}
	if bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("bare LF")
	}
	cal, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	events := cal.Events()
	if len(events) != 4*12 {
		t.Fatalf("%d events", len(events))
	}
	uids := map[string]bool{}
	for _, e := range events {
		uid, _ := e.Props.Text(ical.PropUID)
		uids[uid] = true
	}
	if len(uids) != len(events) {
		t.Fatal("UIDs not distinct")
	}
	if name, _ := cal.Props.Text("X-WR-CALNAME"); name != "Feiertage (Bayern)" {
		t.Fatalf("name %q", name)
	}
	en := string(ICS("", "en", 2026, 2026))
	if !strings.Contains(en, "SUMMARY:Good Friday") || !strings.Contains(en, "X-WR-CALNAME:Public holidays in Germany") || strings.Contains(en, "Allerheiligen") {
		t.Fatalf("english: %s", en)
	}
	if first, last := Years(time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)); first != 2025 || last != 2028 {
		t.Fatalf("years %d–%d", first, last)
	}
}

func TestFold(t *testing.T) {
	long := "SUMMARY:" + strings.Repeat("ä", 60)
	folded := fold(long)
	for _, l := range strings.Split(strings.TrimSuffix(folded, "\r\n"), "\r\n") {
		if len(l) > 75 || !strings.HasPrefix(l, "SUMMARY") && !strings.HasPrefix(l, " ") {
			t.Fatalf("line %q", l)
		}
	}
	if strings.ReplaceAll(strings.TrimSuffix(folded, "\r\n"), "\r\n ", "") != long {
		t.Fatal("unfolded differs")
	}
}
