package fileindex

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExcerptOf(t *testing.T) {
	text := strings.Repeat("Füllwort ", 40) + "Ihre Kfz-Versicherung verlängert sich,\n\nwenn sie nicht gekündigt wird. " + strings.Repeat("Ende ", 40)
	got := excerptOf(text, Tokens("versicherung kündig"))
	if !strings.HasPrefix(got, "… ") || !strings.HasSuffix(got, " …") || !strings.Contains(got, "Kfz-Versicherung verlängert sich, wenn sie nicht gekündigt wird.") {
		t.Fatalf("%q", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "  ") {
		t.Fatalf("white space kept: %q", got)
	}
	// The first match counts, a word beginning with the term matches.
	if got := excerptOf("Ein kurzer Brief über Kündigungen.", Tokens("kündigung")); got != "Ein kurzer Brief über Kündigungen." {
		t.Fatalf("%q", got)
	}
	// Match as the last word, no match, no terms.
	if got := excerptOf("am Ende steht Kfz", Tokens("kfz")); got != "am Ende steht Kfz" {
		t.Fatalf("%q", got)
	}
	if excerptOf("nichts davon", Tokens("auto")) != "" || excerptOf("text", nil) != "" {
		t.Fatal("excerpt without a match")
	}
}

func TestManagerExcerptReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/brief.txt"
	if err := os.WriteFile(p, []byte("Sehr geehrte Damen,\nhiermit kündige ich die Kfz-Versicherung."), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	m := &Manager{extract: extractor{}}
	got := m.Excerpt(context.Background(), f, "brief.txt", info.Size(), "kfz")
	if got != "Sehr geehrte Damen, hiermit kündige ich die Kfz-Versicherung." {
		t.Fatalf("%q", got)
	}
	// Files the index does not read have no excerpt.
	if m.Excerpt(context.Background(), f, "bild.jpg", info.Size(), "kfz") != "" {
		t.Fatal("excerpt of a picture")
	}
}
