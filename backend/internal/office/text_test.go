package office

import (
	"errors"
	"strings"
	"testing"
)

// Plain text files are edited as text, with the same exact counts as Word.
func TestEditPlainText(t *testing.T) {
	out, err := EditText("notes.md", []byte("a b a c"), []TextEdit{{"a", "x", 2}, {"c", "d", 1}})
	if err != nil || string(out) != "x b x d" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := EditText("notes.txt", []byte("a b a"), []TextEdit{{"a", "x", 1}}); err == nil {
		t.Fatal("wrong count accepted")
	}
	// Edits apply one after the other: the second sees the first.
	out, err = EditText("notes.txt", []byte("one"), []TextEdit{{"one", "two", 1}, {"two", "three", 1}})
	if err != nil || string(out) != "three" {
		t.Fatalf("chained: %q %v", out, err)
	}
}

func TestEditTextRefusesBadEdits(t *testing.T) {
	data := []byte("text")
	for name, edits := range map[string][]TextEdit{
		"none":         nil,
		"empty find":   {{"", "x", 1}},
		"zero count":   {{"t", "x", 0}},
		"huge count":   {{"t", "x", 1001}},
		"too many":     make([]TextEdit, 101),
		"too much new": {{"t", strings.Repeat("x", 5<<10), 1000}},
	} {
		if _, err := EditText("a.txt", data, edits); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Growing past the limit by repeated replacement.
	big := []byte(strings.Repeat("a", 3<<20))
	if _, err := EditText("a.txt", big, []TextEdit{{strings.Repeat("a", 3<<20), strings.Repeat("b", 3<<20) + "cc", 1}}); err == nil {
		t.Error("result over 4 MiB accepted")
	}
	if _, err := EditText("a.txt", make([]byte, 50<<20+1), []TextEdit{{"a", "b", 1}}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("input over 50 MiB: %v", err)
	}
}

func TestTextByName(t *testing.T) {
	doc, err := DOCX("# Title\n\nHello world", "Title")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := Text("Letter.DOCX", doc); err != nil || !strings.Contains(text, "Hello world") {
		t.Fatalf("docx: %q %v", text, err)
	}
	book, err := XLSX([]Sheet{{Name: "S", Rows: [][]any{{"Item", "Amount"}, {"Tea", 3}}, Header: true}})
	if err != nil {
		t.Fatal(err)
	}
	if text, err := Text("book.xlsx", book); err != nil || !strings.Contains(text, "Tea") {
		t.Fatalf("xlsx: %q %v", text, err)
	}
	if _, err := Text("image.png", []byte("x")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("png: %v", err)
	}
}

func TestLimitedOutput(t *testing.T) {
	out := &limitedOutput{limit: 5}
	if n, err := out.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if _, err := out.Write([]byte("def")); err == nil {
		t.Fatal("write over the limit accepted")
	}
	if out.String() != "abc" {
		t.Fatalf("kept %q", out.String())
	}
}
