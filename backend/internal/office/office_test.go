package office

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const sample = "# Bericht März\n\nDas NAS läuft **gut**, die Platten sind *kühl* und `tank` ist voll – 90 € Rest.\n\n" +
	"## Platten\n\n- sda: 41 °C\n- sdb: 39 °C\n\n1. Backup prüfen\n2. Volume aufräumen\n\n" +
	"| Platte | Temperatur |\n|---|---|\n| sda | 41 |\n| sdb | 39 |\n\n```\nmdadm --detail /dev/md0\n```\n\n---\n\nEnde."

func TestParse(t *testing.T) {
	blocks := Parse(sample)
	var kinds []string
	for _, b := range blocks {
		kinds = append(kinds, b.Kind)
	}
	want := "heading paragraph heading bullet bullet number number table code rule paragraph"
	if strings.Join(kinds, " ") != want {
		t.Fatalf("blocks: %s", strings.Join(kinds, " "))
	}
	if spans := blocks[1].Spans; !spans[1].Bold || !spans[3].Italic || !spans[5].Code {
		t.Fatalf("spans: %+v", spans)
	}
}

func TestDOCXRoundTrip(t *testing.T) {
	data, err := DOCX(sample, "Bericht")
	if err != nil {
		t.Fatal(err)
	}
	text, err := DOCXText(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Bericht März", "## Platten", "- sda: 41 °C", "Das NAS läuft gut", "| Platte | Temperatur |", "mdadm --detail"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if _, err := DOCXText([]byte("not a zip")); err == nil {
		t.Fatal("read garbage")
	}
}

func TestXLSXRoundTrip(t *testing.T) {
	sheet, err := SheetFromCSV("Platten / Temperaturen", "Platte;Temperatur;OK\nsda;41;true\nsdb;39,5;x\n")
	if err != nil {
		t.Fatal(err)
	}
	data, err := XLSX([]Sheet{sheet, {Name: "Summe", Rows: [][]any{{"Summe", "=SUM(Platten - Temperaturen!B2:B3)"}, {true, 3.5, nil, "ä"}}}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := XLSXText(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Platten - Temperaturen", "Platte,Temperatur,OK", "sda,41,true", "## Summe", "TRUE,3.5,,ä"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if column(0) != "A" || column(25) != "Z" || column(26) != "AA" || column(701) != "ZZ" || column(702) != "AAA" {
		t.Fatal("column names")
	}
}

func TestPDF(t *testing.T) {
	long := strings.Repeat("Ein ziemlich langer Absatz, der über mehrere Zeilen umbrechen muss. ", 200) + strings.Repeat("x", 300)
	data, err := PDF(sample+"\n\n"+long, "Bericht (März)")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) || !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	// Every offset in the cross-reference table points at its object.
	xref := bytes.LastIndex(data, []byte("\nxref\n"))
	for i, m := range regexp.MustCompile(`(\d{10}) 00000 n`).FindAllSubmatch(data[xref:], -1) {
		offset, _ := strconv.Atoi(string(m[1]))
		if !bytes.HasPrefix(data[offset:], []byte(strconv.Itoa(i+1)+" 0 obj")) {
			t.Fatalf("object %d not at %d", i+1, offset)
		}
	}
	if pages := bytes.Count(data, []byte("/Type /Page ")); pages < 3 {
		t.Fatalf("%d pages", pages)
	}
	if !bytes.Contains(data, []byte("M\xe4rz")) || !bytes.Contains(data, []byte("\x80")) {
		t.Fatal("umlaut or euro not encoded")
	}
	if tool, err := exec.LookPath("pdftotext"); err == nil {
		file := filepath.Join(t.TempDir(), "a.pdf")
		os.WriteFile(file, data, 0o600)
		out, err := exec.Command(tool, "-enc", "UTF-8", file, "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Bericht März", "90 €", "Platte", "mdadm --detail", "• sda"} {
			if !strings.Contains(string(out), want) {
				t.Fatalf("pdftotext lacks %q:\n%s", want, out[:min(len(out), 600)])
			}
		}
	}
}
