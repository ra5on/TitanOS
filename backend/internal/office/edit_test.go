package office

import (
	"bytes"
	"testing"
)

func TestEditWordKeepsPartsAndRequiresExactCounts(t *testing.T) {
	original, err := DOCX("# Invoice\n\nTotal 120 EUR", "Invoice")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := EditText("x.docx", original, []TextEdit{{"120", "135 & tax", 1}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := DOCXText(changed)
	if err != nil || !bytes.Contains([]byte(text), []byte("135 & tax")) {
		t.Fatal(text, err)
	}
	z, _ := openZip(original)
	zz, _ := openZip(changed)
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			a, _ := readPart(z, f.Name)
			b, _ := readPart(zz, f.Name)
			if !bytes.Equal(a, b) {
				t.Fatal("changed unrelated part", f.Name)
			}
		}
	}
	if _, err := EditText("x.docx", original, []TextEdit{{"120", "135", 2}}); err == nil {
		t.Fatal("ambiguous count accepted")
	}
}

func TestWorkbookEditPreservesFormulaAndRejectsMissingCell(t *testing.T) {
	original, err := XLSX([]Sheet{{Name: "Budget", Rows: [][]any{{"Item", "Amount"}, {"X", 12}, {"Total", "=SUM(B2:B2)"}}, Header: true}})
	if err != nil {
		t.Fatal(err)
	}
	edited, err := EditCells(original, []CellEdit{{Sheet: "Budget", Cell: "B2", Value: "25.5", Type: "number"}})
	if err != nil {
		t.Fatal(err)
	}
	z, _ := openZip(edited)
	sheet, _ := readPart(z, "xl/worksheets/sheet1.xml")
	if !bytes.Contains(sheet, []byte("25.5")) || !bytes.Contains(sheet, []byte("SUM(B2:B2)")) {
		t.Fatal(string(sheet))
	}
	workbook, _ := readPart(z, "xl/workbook.xml")
	if !bytes.Contains(workbook, []byte(`fullCalcOnLoad="1"`)) {
		t.Fatal("missing recalculation")
	}
	for _, cell := range []string{"B3", "C99"} {
		if _, err := EditCells(original, []CellEdit{{Sheet: "Budget", Cell: cell, Value: "5", Type: "number"}}); err == nil {
			t.Fatal("accepted formula or missing cell", cell)
		}
	}
}
