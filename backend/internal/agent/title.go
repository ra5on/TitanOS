package agent

import (
	"regexp"
	"strings"
)

// FileLine is a line naming an attached file or a file of the NAS.
var FileLine = regexp.MustCompile(`(?m)^\[(Anhang|Datei): [^\]\n]*\]\s*$`)

// QuestionTitleMax bounds a question taken as the title (a pasted log, a
// letter).
const QuestionTitleMax = 400

// QuestionTitle is the title of a chat from its first question.
func QuestionTitle(text string) string {
	// The names of attached files are no title; the question after them is.
	if rest := strings.TrimSpace(FileLine.ReplaceAllString(text, "")); rest != "" {
		text = rest
	}
	// 1.07 (owner 17 Nov 2026): the whole question until the model has
	// written a short title (api/agent_title.go) – never cut off with "…".
	// Only a pasted text of many pages is kept to its first words, at a word.
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > QuestionTitleMax {
		cut := string(r[:QuestionTitleMax])
		if i := strings.LastIndex(cut, " "); i > QuestionTitleMax/2 {
			cut = cut[:i]
		}
		return cut
	}
	return text
}

// cutTitle is how titles ended before 1.07: the question cut after about
// 60 characters with " …".
const cutTitle = " …"

// LegacyTitle reports whether the chat still has a title of before 1.07
// that was cut off with " …" (and the person did not name it so).
func (c *Chat) LegacyTitle() bool {
	return !c.TitleOwn && strings.HasSuffix(c.Title, cutTitle)
}

// ShownTitle is the title to show, export and send: a cut title of before
// 1.07 is shown as the whole first question (1.09: the export of 17 Nov
// 2026 was headed "… Schlösser und …"). The stored title stays, so that
// the model can still be asked once for a short one.
func (c *Chat) ShownTitle() string {
	if !c.LegacyTitle() {
		return c.Title
	}
	for _, e := range c.Entries {
		if e.Role == "user" {
			if t := QuestionTitle(e.Content); t != "" {
				return t
			}
			break
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(c.Title, cutTitle))
}
