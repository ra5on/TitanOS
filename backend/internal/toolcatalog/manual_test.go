package toolcatalog

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestManual(t *testing.T) {
	manual := NewManual(fstest.MapFS{
		"README.md":           {Data: []byte("# TitanOS\n\nEin NAS.\n\n## Installation\n\nZip auf den Stick kopieren.\n")},
		"docs/NETWORKING.md":  {Data: []byte("# Netzwerk\n\n## Feste Adresse\n\nUnter Einstellungen › Netzwerk eine feste IP eintragen. Die Änderung gilt zwei Minuten auf Probe.\n\n```\n# kein Titel\n```\n")},
		"docs/design/x.dc.md": {Data: []byte("# Design\n")},
	})
	hits := manual.Search("feste IP Netzwerk", 3)
	if len(hits) == 0 || hits[0].File != "docs/NETWORKING.md" || hits[0].Heading != "Feste Adresse" {
		t.Fatalf("hits: %+v", hits)
	}
	if text, ok := manual.Read("docs/NETWORKING.md", "feste"); !ok || !strings.Contains(text, "Probe") || strings.Contains(text, "## kein Titel") {
		t.Fatalf("read: %q", text)
	}
	if _, ok := manual.Read("../etc/passwd", ""); ok {
		t.Fatal("read outside the manual")
	}
	if len(NewManual(nil).Files()) != 0 {
		t.Fatal("empty manual has files")
	}
}
