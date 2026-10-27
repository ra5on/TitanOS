package fileindex

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minTerm = 2
	maxTerm = 48
	// maxTerms bounds the distinct words kept per file; the rest of a huge
	// document is still searchable through its first words.
	maxTerms = 20000
)

// Tokens splits text into lower-case words of letters and digits, each once.
// German umlauts stay as they are; "ß" is kept, "ẞ" folds to it.
func Tokens(text string) []string {
	seen := map[string]bool{}
	var out []string
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		word := strings.ToLower(text[start:end])
		start = -1
		if n := utf8.RuneCountInString(word); n < minTerm || n > maxTerm || seen[word] || len(out) >= maxTerms {
			return
		}
		seen[word] = true
		out = append(out, word)
	}
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(text))
	return out
}
