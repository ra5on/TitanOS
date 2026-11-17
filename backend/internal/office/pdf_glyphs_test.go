package office

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 1.09 (export of a chat of 17 Nov 2026: "Home?Assistant",
// "Entwickler?Tools???Entitäten", "• ? ha_entities"): no character of a
// chat becomes "?" any more. Spaces and hyphens of every kind become their
// plain form, ✓/✗ and arrows are drawn with the standard fonts Symbol and
// ZapfDingbats and come out of the PDF as themselves.
func TestPDFHasNoQuestionMarksForUnicode(t *testing.T) {
	md := "# Alles zu \u2013 Prüfung\n\n" +
		"Sie haben 0\u202fAutomation\u2011Entitäten in Home\u202fAssistant.\n\n" +
		"Menü \u201eEntwickler\u2011Tools\u202f›\u202fEntitäten\u201c, Rechte › Home Assistant.\n\n" +
		"- ✓ Home Assistant gelesen\n- ✗ Dateien gesucht\n- ! Fehler\n\n" +
		"Temperatur ≥ 21 °C → Heizung aus, ≤ 18 °C ⇒ an; ✅ fertig, ❌ nicht.\n\n" +
		"Zeichen: ă ș ① ﬁ \u200bunsichtbar\ufe0f, 中\n\n" +
		"| Element | Status |\n|---|---|\n| Fenster\u2011 und Türkontakte | geschlossen (State\u202f=\u202foff) |\n"
	data, err := PDF(md, "Chat: Fenster\u202f›\u202fTüren ✓")
	if err != nil {
		t.Fatal(err)
	}
	// The only "?" in the whole file would be one we wrote: there is none
	// in the text, so there must be none in the content streams.
	for _, stream := range bytes.Split(data, []byte("stream\n"))[1:] {
		body, _, _ := bytes.Cut(stream, []byte("endstream"))
		if bytes.Contains(body, []byte("?")) && !bytes.Contains(body, []byte("begincmap")) {
			t.Fatalf("a ? in the text: %q", body[:min(len(body), 400)])
		}
	}
	if !bytes.Contains(data, []byte("/BaseFont /ZapfDingbats /ToUnicode")) || !bytes.Contains(data, []byte("/Title <FEFF")) {
		t.Fatal("fallback fonts or title missing")
	}
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed; the content streams are checked above")
	}
	file := filepath.Join(t.TempDir(), "chat.pdf")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tool, "-enc", "UTF-8", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(out)), " ")
	for _, want := range []string{
		"Alles zu – Prüfung",
		"0 Automation-Entitäten in Home Assistant",
		"„Entwickler-Tools › Entitäten“",
		"Rechte › Home Assistant",
		"✓ Home Assistant gelesen", "✗ Dateien gesucht", "! Fehler",
		"≥ 21 °C → Heizung aus, ≤ 18 °C ⇒ an; ✔ fertig, ✘ nicht",
		"a s 1 fi unsichtbar, ❏",
		"Fenster- und Türkontakte", "(State = off)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("pdftotext lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "?") {
		t.Errorf("a ? in the text:\n%s", text)
	}
	info, err := exec.Command("pdfinfo", file).Output()
	if err == nil && !strings.Contains(strings.Join(strings.Fields(string(info)), " "), "Title: Chat: Fenster › Türen ✓") {
		t.Errorf("title: %s", info)
	}
}
