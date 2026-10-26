package office

import (
	"archive/zip"
	"bytes"
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
