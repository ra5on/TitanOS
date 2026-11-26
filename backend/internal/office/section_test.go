package office

import (
	"bytes"
	"testing"
)

func TestParagraphEditRetainsUntouchedParts(t *testing.T) {
	original, err := DOCX("Erster Absatz\n\nZweiter **fetter** Absatz", "Test")
	if err != nil {
		t.Fatal(err)
	}
	replacement := "Inhalt <&> neu"
	before, changed, err := Paragraph(original, ".docx", 0, 2, &replacement)
	if err != nil || before != "Zweiter fetter Absatz" {
		t.Fatalf("selection %q %v", before, err)
	}
	got, _, err := Paragraph(changed, ".docx", 0, 2, nil)
	if err != nil || got != replacement {
		t.Fatalf("changed %q %v", got, err)
	}
	first, _, err := Paragraph(changed, ".docx", 0, 1, nil)
	if err != nil || first != "Erster Absatz" {
		t.Fatal(first, err)
	}
	in, err := openZip(original)
	if err != nil {
		t.Fatal(err)
	}
	out, err := openZip(changed)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range in.File {
		a, e := readPart(in, part.Name)
		if e != nil {
			t.Fatal(e)
		}
		b, e := readPart(out, part.Name)
		if e != nil {
			t.Fatal(e)
		}
		if part.Name != "word/document.xml" && !bytes.Equal(a, b) {
			t.Fatal("changed unrelated part", part.Name)
		}
		if part.Name == "word/document.xml" && bytes.Count(a, []byte("<w:b")) != bytes.Count(b, []byte("<w:b")) {
			t.Fatal("lost run formatting")
		}
	}
}
func TestEditableDeckAndCellScope(t *testing.T) {
	deck, err := OutlineDeck("Erste Folie\nErste Quelle\n\nZweite Folie\nZweite Quelle")
	if err != nil {
		t.Fatal(err)
	}
	updated := "Neue Quelle"
	title, _, e := Paragraph(deck, ".pptx", 2, 1, nil)
	if e != nil || title != "Zweite Folie" {
		t.Fatal(title, e)
	}
	before, copy, e := Paragraph(deck, ".pptx", 1, 2, &updated)
	if e != nil || before != "Erste Quelle" {
		t.Fatal(before, e)
	}
	got, _, e := Paragraph(copy, ".pptx", 2, 2, nil)
	if e != nil || got != "Zweite Quelle" {
		t.Fatal(got, e)
	}
	book, err := XLSX([]Sheet{{Name: "Angebot", Rows: [][]any{{"Position", "Wert"}, {"Preis", 12}, {"Summe", "=B2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	edit := "19"
	before, copy, e = CellRange(book, "Angebot", "B2", &edit, "number")
	if e != nil || before != "12" {
		t.Fatal(before, e)
	}
	got, _, e = CellRange(copy, "Angebot", "B2", nil, "")
	if e != nil || got != "19" {
		t.Fatal(got, e)
	}
	if _, _, e = CellRange(copy, "Angebot", "B3", &edit, "number"); e == nil {
		t.Fatal("formula cell overwritten")
	}
}
