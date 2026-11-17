package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Push messages of T-Mail stay small enough for every push service
// (security review of 1.05): title 120, text 240 characters.
func TestMailPushTextsShort(t *testing.T) {
	long := strings.Repeat("ä", 5000)
	if got := runesAtMost(long, 120); utf8.RuneCountInString(got) != 120 || !utf8.ValidString(got) {
		t.Fatalf("%d runes", utf8.RuneCountInString(got))
	}
	if runesAtMost("kurz", 240) != "kurz" {
		t.Fatal("short text changed")
	}
}
