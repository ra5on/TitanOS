package fileindex

import (
	"context"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// excerptRadius is how many characters an excerpt shows on each side of the
// first match.
const excerptRadius = 90

// Excerpt reads the text of a found file again and returns the passage
// around the first word that begins with one of the query's words, for the
// result list (PLAN 72). It never runs text recognition; a scan without a
// text layer has no excerpt. An empty string means none was found.
func (m *Manager) Excerpt(ctx context.Context, f *os.File, name string, size int64, query string) string {
	text, err := m.extract.text(ctx, f, name, size, false)
	if err != nil {
		return ""
	}
	return excerptOf(text, Tokens(query))
}

// excerptOf finds the first word of text that begins with one of terms
// (lower case) and cuts the passage around it on word boundaries, with "…"
// where text was left out. Runs of white space become one space.
func excerptOf(text string, terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	at, end := -1, -1
	start := -1
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && matchesTerm(text[start:i], terms) {
			at, end = start, i
			break
		}
		start = -1
	}
	if at < 0 && start >= 0 && matchesTerm(text[start:], terms) {
		at, end = start, len(text)
	}
	if at < 0 {
		return ""
	}
	from := back(text, at, excerptRadius)
	to := forward(text, end, excerptRadius)
	out := strings.Join(strings.Fields(text[from:to]), " ")
	if from > 0 {
		out = "… " + out
	}
	if to < len(text) {
		out += " …"
	}
	return out
}

func matchesTerm(word string, terms []string) bool {
	word = strings.ToLower(word)
	for _, term := range terms {
		if strings.HasPrefix(word, term) {
			return true
		}
	}
	return false
}

// back goes n runes back from i and then forward to the next word start.
func back(text string, i, n int) int {
	j := i
	for k := 0; k < n && j > 0; k++ {
		_, size := utf8.DecodeLastRuneInString(text[:j])
		j -= size
	}
	if j == 0 {
		return 0
	}
	for j < i {
		r, size := utf8.DecodeRuneInString(text[j:])
		j += size
		if unicode.IsSpace(r) {
			break
		}
	}
	return j
}

// forward goes n runes on from i and then back to the last word end.
func forward(text string, i, n int) int {
	j := i
	for k := 0; k < n && j < len(text); k++ {
		_, size := utf8.DecodeRuneInString(text[j:])
		j += size
	}
	if j >= len(text) {
		return len(text)
	}
	for j > i {
		r, size := utf8.DecodeLastRuneInString(text[:j])
		if unicode.IsSpace(r) {
			break
		}
		j -= size
	}
	return j
}
