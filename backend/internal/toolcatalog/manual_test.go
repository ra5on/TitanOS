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

// 1.10 (V14): the release notes come along under releases/, apart from the
// manual: docs_search does not find them, Release reads them.
func TestManualReleaseNotes(t *testing.T) {
	manual := NewManual(fstest.MapFS{
		"docs/AGENT.md":        {Data: []byte("# Agent\n\n## Neu\n\nText.\n")},
		"releases/v1.9.0.md":   {Data: []byte("# TitanOS 1.09\n\nNeu: Automationen.\n")},
		"releases/v1.10.0.md":  {Data: []byte("# TitanOS 1.10\n\nNeu: agent_access.\n")},
		"releases/v0.100.0.md": {Data: []byte("# TitanOS 0.100\n")},
		"releases/notes.txt":   {Data: []byte("x")},
	})
	for _, f := range manual.Files() {
		if strings.HasPrefix(f, "releases/") {
			t.Fatalf("release notes in the manual: %v", manual.Files())
		}
	}
	if hits := manual.Search("Automationen", 3); len(hits) != 0 {
		t.Fatalf("search finds release notes: %+v", hits)
	}
	if text, ok := manual.Release("v1.10.0"); !ok || !strings.Contains(text, "agent_access") {
		t.Fatalf("release: %q %v", text, ok)
	}
	if _, ok := manual.Release("../docs/AGENT"); ok {
		t.Fatal("release outside its folder")
	}
	if got := strings.Join(manual.Releases(), " "); got != "1.10.0 1.9.0 0.100.0" {
		t.Fatalf("releases: %s", got)
	}
}
