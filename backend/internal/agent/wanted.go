package agent

import (
	"regexp"
	"slices"
	"strings"
)

// 114-1 (owner 19 Nov 2026, chat "E Mail Versenden Nicht Möglich" with
// gpt-oss:20b): "Kannst du eine email an … senden" got "Ich kann leider
// keine E-Mails versenden" without a single tool call. With many tools only
// the core areas are offered and mail sits behind tools_load; a model that
// does not load it answers from what it sees. When the person's message is
// plainly about mail, the calendar, reminders or contacts, the runner loads
// that area before the first round, without the model – like "home" for
// questions from Home Assistant. Loading only picks among the tools the
// toolbox offers this account: it opens nothing the rights close. An area
// that is closed for the account (Mail off, no mailbox, reading switched
// off) gets a note with the reason instead, so that the answer names it
// and where to change it rather than promising or refusing blindly.

// wantedWords are the beginnings of words per area (German and English,
// case-insensitive); a word with a beginning from notWanted does not count
// ("Terminal" is the console, "eventuell" is no event). Phrases with a
// space match anywhere.
var wantedWords = map[string][]string{
	AreaMail: {"mail", "e-mail", "email", "postfach", "posteingang", "newsletter", "betreff", "inbox", "mailbox"},
	AreaCalendar: {"termin", "kalender", "besprechung", "meeting", "geburtstag", "erinnerung", "erinnere", "todo", "to-do",
		"calendar", "appointment", "event", "remind", "birthday"},
	AreaContacts: {"kontakt", "adressbuch", "telefonnummer", "handynummer", "rufnummer", "anschrift",
		"contact", "address book", "phone number"},
}

var notWanted = []string{"terminal", "eventuell", "mailand"}

// addressRe is an e-mail address in the message: that is about mail.
var addressRe = regexp.MustCompile(`[\p{L}0-9._%+-]+@[\p{L}0-9-]+(\.[\p{L}0-9-]+)+`)

var wordRe = regexp.MustCompile(`[\p{L}0-9-]+`)

// WantedAreas are the areas a message of the person is plainly about, in a
// fixed order.
func WantedAreas(text string) []string {
	low := strings.ToLower(text)
	words := wordRe.FindAllString(low, -1)
	about := func(area string) bool {
		for _, w := range wantedWords[area] {
			if strings.Contains(w, " ") {
				if strings.Contains(low, w) {
					return true
				}
				continue
			}
			for _, word := range words {
				if strings.HasPrefix(word, w) && !slices.ContainsFunc(notWanted, func(n string) bool { return strings.HasPrefix(word, n) }) {
					return true
				}
			}
		}
		return false
	}
	var out []string
	for _, area := range []string{AreaMail, AreaCalendar, AreaContacts} {
		if about(area) || (area == AreaMail && addressRe.MatchString(text)) {
			out = append(out, area)
		}
	}
	return out
}

// preload loads the areas the last message of the person is about (with
// LazyTools) and keeps, for this run, a note for those that are closed.
func (r *Runner) preload(chat *Chat) {
	r.closedHint = ""
	i := lastUserIndex(chat)
	if i < 0 || chat.Origin == "homeassistant" {
		return
	}
	wanted := WantedAreas(chat.Entries[i].Content)
	if len(wanted) == 0 {
		return
	}
	have := map[string]bool{}
	for _, s := range r.Tools.Specs(chat) {
		have[s.Area] = true
	}
	var closed []string
	missing := r.missing()
	for _, a := range wanted {
		if have[a] {
			if r.LazyTools && !slices.Contains(chat.Areas, a) {
				chat.Areas = append(chat.Areas, a)
			}
			continue
		}
		if why := missing[a]; why != "" {
			closed = append(closed, a+": "+why)
		}
	}
	if len(closed) > 0 {
		r.closedHint = "[TitanOS] The person's message is about an area that is closed for you now – " + strings.Join(closed, "; ") +
			". Do not promise it and do not only say that you cannot: tell them this reason and where they change it."
	}
}

// system is the system prompt of this run with the note of preload.
func (r *Runner) system() string {
	if r.closedHint == "" {
		return r.System
	}
	return r.System + "\n\n" + r.closedHint
}
