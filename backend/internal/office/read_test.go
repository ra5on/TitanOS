package office

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// pack makes a zip from file names and contents.
func pack(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A document from outside decides nothing about how much is allocated.
func TestDOCXHeadingLevelFromFile(t *testing.T) {
	body := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
	for _, style := range []string{"Heading-1", "Heading999999999", "Heading2"} {
		body += `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + style + `</w:t></w:r></w:p>`
	}
	body += `</w:body></w:document>`
	text, err := DOCXText(pack(t, map[string]string{"word/document.xml": body}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Heading2") || strings.Contains(text, "###") {
		t.Fatalf("text = %q", text)
	}
}

func TestXLSXColumnBeyondExcel(t *testing.T) {
	text, err := XLSXText(pack(t, map[string]string{
		"xl/workbook.xml":          `<workbook><sheets><sheet name="S" r:id="rId1" xmlns:r="r"/></sheets></workbook>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>a</t></is></c><c r="ZZZZZZ1"><v>2</v></c><c r="C1"><v>3</v></c></row></sheetData></worksheet>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > 1000 {
		t.Fatalf("a cell far beyond the last column padded the row to %d bytes", len(text))
	}
	if columnIndex("XFD1") != 16383 || columnIndex("XFE1") != -1 || columnIndex("C7") != 2 {
		t.Fatal("columnIndex")
	}
}

// A small workbook whose cells all name one large shared string gives at
// most MaxText of text, not that string once per cell.
func TestXLSXSharedStringRepeatedStaysWithinMaxText(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	cells := strings.Repeat(`<c t="s"><v>0</v></c>`, 64)
	text, err := XLSXText(pack(t, map[string]string{
		"xl/workbook.xml":          `<workbook><sheets><sheet name="S" r:id="rId1" xmlns:r="r"/></sheets></workbook>`,
		"xl/sharedStrings.xml":     `<sst><si><t>` + big + `</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row>` + cells + `</row></sheetData></worksheet>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > MaxText+1024 {
		t.Fatalf("text of %d bytes", len(text))
	}
}

// A row of millions of empty cells is read cell by cell and never longer
// than a row of Excel can be.
func TestXLSXRowOfMillionsOfCells(t *testing.T) {
	cells := strings.Repeat(`<c/>`, 2_000_000)
	text, err := XLSXText(pack(t, map[string]string{
		"xl/workbook.xml":          `<workbook><sheets><sheet name="S" r:id="rId1" xmlns:r="r"/></sheets></workbook>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row>` + cells + `</row><row><c t="inlineStr"><is><t>after</t></is></c></row></sheetData></worksheet>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > (maxColumn+1)*2+1024 || !strings.Contains(text, "after") {
		t.Fatalf("text of %d bytes, ends %q", len(text), text[max(0, len(text)-40):])
	}
}

// Many sheets that name the same part read it once.
func TestXLSXSamePartForManySheets(t *testing.T) {
	sheets := strings.Repeat(`<sheet name="S" r:id="rId1" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/>`, 50)
	text, err := XLSXText(pack(t, map[string]string{
		"xl/workbook.xml":            `<workbook><sheets>` + sheets + `</sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c t="inlineStr"><is><t>once</t></is></c></row></sheetData></worksheet>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(text, "once"); n != 1 {
		t.Fatalf("part read %d times", n)
	}
}

func TestAmbiguousOfficePart(t *testing.T) {
	for _, alias := range []string{"word/document.xml", "WORD/DOCUMENT.XML", "word/./document.xml"} {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for _, name := range []string{"word/document.xml", alias} {
			if _, err := w.Create(name); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := DOCXText(buf.Bytes()); !errors.Is(err, ErrUnsupported) {
			t.Errorf("ambiguous part %q: %v", alias, err)
		}
	}
}
