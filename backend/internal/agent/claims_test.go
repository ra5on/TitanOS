package agent

import "testing"

// 1.15 (115-1): an answer that says a draft was sent, moved or deleted.
func TestClaimsDone(t *testing.T) {
	for text, want := range map[string]bool{
		"Die E-Mail von Müller Nico wurde in den Ordner „Deleted Messages“ verschoben.": true,
		"Ich habe die Mail an Ben gesendet.":                                            true,
		"The mail was deleted.":                                                         true,
		"Sobald du bestätigst, wird sie gelöscht.":                                      false,
		"Die Mail ist noch nicht gesendet, die Karte wartet auf dich.":                  false,
		"Ich habe einen Entwurf für Ben geschrieben.":                                   false,
		"Hier ist die Liste deiner Mails.":                                              false,
		"Once you confirm, it will be moved.":                                           false,
	} {
		if got := ClaimsDone(text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}
