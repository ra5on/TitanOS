package docindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func day(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, berlin)
	if err != nil {
		panic(err)
	}
	return t
}

func letter(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "briefe", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFindDates(t *testing.T) {
	for text, want := range map[string]string{
		"am 03.11.2026":             "2026-11-03",
		"bis 3.11.26 bitte":         "2026-11-03",
		"vom 2026-11-03":            "2026-11-03",
		"am 3. November 2026":       "2026-11-03",
		"on November 3, 2026":       "2026-11-03",
		"on 3 Nov 2026":             "2026-11-03",
		"Ablauf (31.12.)":           "2000-12-31",
		"Betrag 1.284,37 €":         "",
		"Version 1.2.3":             "",
		"am 31.02.2026 nicht":       "",
		"am 15. März 2027 fällig":   "2027-03-15",
		"Kündigung zum 30.6.2027":   "2027-06-30",
		"Rechnung Nr. 12.34.5678 x": "",
	} {
		got := FindDates(text, berlin)
		switch {
		case want == "" && len(got) > 0:
			t.Errorf("%q: found %v", text, got[0].Time)
		case want != "" && (len(got) == 0 || got[0].Time.Format("2006-01-02") != want):
			t.Errorf("%q: %v, want %s", text, got, want)
		}
	}
}

func TestAmounts(t *testing.T) {
	for text, want := range map[string]int64{"1.284,37 €": 128437, "86,00 EUR": 8600, "EUR 12.50": 1250, "1,284.37 USD": 128437, "0,00 €": 0, "Total 15,00": 1500} {
		got := FindAmounts(text)
		if len(got) == 0 || got[0].Cents != want {
			t.Errorf("%q: %v want %d", text, got, want)
		}
	}
	if FormatEuro(128437) != "1.284,37 €" || FormatEuro(8600) != "86,00 €" {
		t.Fatal(FormatEuro(128437))
	}
}

// German examples of the deadline watcher, among them the case of the
// draft: "einen Monat vor Ablauf (31.12.)" is the 30.11.
func TestDeadlinesGerman(t *testing.T) {
	now := day("2026-11-06")
	cases := []struct {
		text, kind, date string
	}{
		{"Kündigung spätestens einen Monat vor Ablauf (31.12.) in Textform.", KindCancel, "2026-11-30"},
		{"Der Vertrag kann mit einer Frist von drei Monaten zum Ende der Laufzeit am 31.03.2027 gekündigt werden.", KindCancel, "2026-12-31"},
		{"Kündigungsfrist: 3 Monate zum Vertragsende (30.06.2027).", KindCancel, "2027-03-30"},
		{"Rechnungsdatum 14.03.2025\nFür dieses Gerät gelten 2 Jahre Gewährleistung ab Kaufdatum.", KindWarranty, "2027-03-14"},
		{"Datum: 01.10.2026\nHerstellergarantie von 3 Jahren", KindWarranty, "2029-10-01"},
		{"Der Betrag wird am 15.11.2026 per SEPA-Lastschrift abgebucht.", KindPayment, "2026-11-15"},
		{"Rechnungsdatum: 03.11.2026\nZahlbar innerhalb von 14 Tagen ohne Abzug.", KindPayment, "2026-11-17"},
		{"Bitte überweisen Sie den Betrag bis zum 28.11.2026.", KindPayment, "2026-11-28"},
		{"Die Unterlagen sind spätestens am 15. Januar 2027 einzureichen.", KindDue, "2027-01-15"},
		{"Your subscription expires on December 1, 2026.", KindCancel, "2026-12-01"},
		{"Payment due: 2026-11-20", KindPayment, "2026-11-20"},
	}
	for _, c := range cases {
		got := Deadlines([]Page{{N: 1, Text: c.text}}, now)
		found := false
		for _, d := range got {
			if d.Kind == c.kind && d.Date.Format("2006-01-02") == c.date {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: %+v, want %s %s", c.text, got, c.kind, c.date)
		}
	}
	// Past deadlines and the date of the document are no deadlines.
	if got := Deadlines([]Page{{N: 1, Text: "Rechnungsdatum: 03.11.2026\nfällig am 01.10.2026"}}, now); len(got) != 0 {
		t.Errorf("past or document date: %+v", got)
	}
	if got := Deadlines([]Page{{N: 1, Text: "Der Betrag wird am 15.11.2026 abgebucht."}}, now); len(got) != 1 || !got[0].Debit {
		t.Errorf("debit: %+v", got)
	}
}

// The test letters (54-D item 5: measure the accuracy of sums and dates).
// The count is logged; every letter must be right.
func TestRecognizeLetters(t *testing.T) {
	now := day("2026-11-06")
	cases := []struct {
		file, sender, typ, kind, date, due string
		cents                              int64
		hint                               Hint
	}{
		{"stadtwerke.txt", "Stadtwerke Musterstadt", TypeEnergy, "Abschlag", "2026-11-03", "2026-11-15", 8600, Hint{}},
		{"telekom.txt", "Telekom", TypeTelecom, "Rechnung", "2026-11-04", "2026-11-18", 4495, Hint{}},
		{"huk.txt", "HUK", TypeInsurance, "Beitragsrechnung", "2026-11-02", "2026-12-01", 61240, Hint{}},
		{"zahnarzt.txt", "Zahnarzt Dr. Weber", TypeHealth, "Rechnung", "2026-10-28", "2026-11-28", 21250, Hint{}},
		{"mediamarkt.txt", "MediaMarkt", TypeShopping, "Rechnung", "2025-03-14", "", 41999, Hint{}},
		{"congstar-mail.txt", "congstar", TypeTelecom, "Rechnung", "2026-11-05", "2026-11-12", 1500, Hint{FromName: "congstar", From: "rechnung@congstar.de", Subject: "Deine Rechnung Oktober 2026"}},
		{"amt.txt", "Finanzamt Musterstadt", TypeTax, "Bescheid", "2026-10-20", "2026-11-23", 28437, Hint{}},
	}
	right, total := 0, 0
	for _, c := range cases {
		info := Recognize(letter(t, c.file), c.hint, now)
		check := func(what string, ok bool, got any) {
			total++
			if ok {
				right++
			} else {
				t.Errorf("%s: %s %v", c.file, what, got)
			}
		}
		check("sender", info.Sender == c.sender, info.Sender)
		check("type", info.Type == c.typ, info.Type)
		check("kind", info.Kind == c.kind, info.Kind)
		check("date", info.Date.Format("2006-01-02") == c.date, info.Date)
		check("amount", info.Cents == c.cents, info.Cents)
		due := ""
		if !info.Due.IsZero() {
			due = info.Due.Format("2006-01-02")
		}
		check("due", due == c.due, due)
		if !info.Sure {
			t.Errorf("%s: not sure %+v", c.file, info)
		}
	}
	t.Logf("test letters: %d of %d values right", right, total)
}

func TestRecognizeUnsure(t *testing.T) {
	info := Recognize("Scan\n\nBitte Unterlagen mitbringen.", Hint{Name: "Scan_0042.pdf"}, day("2026-11-06"))
	if info.Sure {
		t.Fatalf("sure without sender: %+v", info)
	}
}

func TestFileNames(t *testing.T) {
	info := Info{Sender: "Stadtwerke", Kind: "Rechnung", Date: day("2026-11-03")}
	if got := FileName(info, ".PDF"); got != "2026-11-03 Stadtwerke Rechnung.pdf" {
		t.Fatal(got)
	}
	for in, want := range map[string]string{"../../etc": "etc", "a/b": "a b", ".hidden": "hidden", "x\x00y": "xy", "..": ""} {
		if got := SafeName(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestSearchBM25(t *testing.T) {
	var x Index
	x.Put(Doc{Root: "share/KI-Agent", Path: "Dokumente/HUK Beitragsrechnung 2026.pdf", Pages: []Page{{1, "Beitragsrechnung"}, {2, "Kfz-Versicherung. Kündigung spätestens einen Monat vor Ablauf (31.12.)."}}})
	x.Put(Doc{Root: "share/KI-Agent", Path: "Dokumente/Telekom.pdf", Pages: []Page{{1, "Ihre Rechnung Internet und Telefon"}}})
	x.Put(Doc{Root: "share/Docs", Path: "Waschmaschine.pdf", Pages: []Page{{1, "Garantie Waschmaschine zwei Jahre"}}})
	hits := x.Search("Wann läuft die Kfz-Versicherung aus? Kündigung", 5, nil)
	if len(hits) == 0 || hits[0].Path != "Dokumente/HUK Beitragsrechnung 2026.pdf" || hits[0].Page != 2 || !strings.Contains(hits[0].Snippet, "Kündigung") {
		t.Fatalf("hits: %+v", hits)
	}
	// Umlauts folded and stems: "kundigungen" finds "Kündigung".
	if hits := x.Search("kundigungen", 5, nil); len(hits) != 1 {
		t.Fatalf("folded: %+v", hits)
	}
	// keep removes what the account may not see.
	if hits := x.Search("Garantie", 5, func(d Doc) bool { return d.Root != "share/Docs" }); len(hits) != 0 {
		t.Fatalf("kept: %+v", hits)
	}
	// Replacing a document by root and path.
	x.Put(Doc{Root: "share/KI-Agent", Path: "Dokumente/Telekom.pdf", Pages: []Page{{1, "neu"}}})
	if len(x.Docs) != 3 || len(x.Search("Telefon", 5, nil)) != 0 {
		t.Fatal("replace")
	}
	if n := x.Remove(func(d Doc) bool { return d.Root == "share/Docs" }); n != 1 {
		t.Fatal(n)
	}
}
