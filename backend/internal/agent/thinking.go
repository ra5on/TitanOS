package agent

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Thinking that arrives inside the text of an answer (1.09, export of
// 17 Nov 2026: a page of English reasoning "The user says …" stood as the
// answer, the real answer glued to its end). The server gives thinking
// apart where it can (Ollama's field thinking, reasoning_content or
// reasoning of OpenAI-compatible servers, thinking blocks of Claude);
// these are the other ways it comes:
//
//   - <think>…</think> (also <thinking>, <reasoning>), or only </think>
//     when the template opened the tag itself;
//   - the Harmony format of gpt-oss as text: <|channel|>analysis<|message|>
//     … <|end|><|start|>assistant<|channel|>final<|message|> …, or with
//     the special tokens taken out: "analysis…assistantfinal…";
//   - unmarked: the model writes its reasoning as the answer and the
//     answer after it, as Ollama joins two messages of gpt-oss when the
//     first is not on the analysis channel. Recognised only when it starts
//     like reasoning ("The user says", "We need to", …) and the answer is
//     glued to it without a space ("…wissen."In Home Assistant …"): then
//     the reasoning ends there, if the reasoning is English and the answer
//     German (the reasoning of these models is English; the agent answers
//     in German unless the person writes otherwise). Without all of these
//     signs nothing is cut, so a real answer is never shortened; reasoning
//     before an English answer stays (a limit).
//
// SplitThinking is used on every answer of the model (Ollama.Chat) and on
// answers read from the store, so chats saved before 1.09 are clean too.

var (
	thinkOpen  = regexp.MustCompile(`^<(think|thinking|reasoning)>`)
	harmonyMsg = regexp.MustCompile(`(?s)(?:<\|start\|>[^<]*)?<\|channel\|>([a-z]+)([^<]*)(?:<\|constrain\|>[^<]*)?<\|message\|>(.*?)(?:<\|end\|>|<\|return\|>|<\|call\|>|$)`)
	// Openers of reasoning (English; gpt-oss, qwen and deepseek reason in
	// English whatever the person writes).
	reasoningStart = regexp.MustCompile(`^(?:The user\b|The person\b|User (?:says|asks|wants)\b|We (?:need|should|have|must|can|might)\b|Let's\b|Let me\b|I need to\b|I should\b|Okay, (?:the|so|we|let)\b|Alright\b|So the user\b|First, (?:I|we)\b|Hmm\b)`)
	// A sentence end directly followed by the start of the next message.
	glue = regexp.MustCompile(`[.!?…]["“”'»)]?(?:(?:[„"“]|\*\*)?[A-ZÄÖÜ]|#|\|)`)
	// Common words of German and of English, to tell the languages apart.
	germanWords  = wordSet("der die das und ist nicht du ich sie es ein eine einen im in mit für auf gibt hast habe keine kein bitte werden wird sind auch noch oder wenn dass")
	englishWords = wordSet("the and is to we user that not it of this they but maybe so if are be should")
)

func wordSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// looks reports whether text has at least n of the words of a language.
func looks(text string, words map[string]bool, n int) bool {
	found := 0
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if words[w] {
			if found++; found >= n {
				return true
			}
		}
	}
	return false
}

// SplitThinking separates thinking from the answer in the text of a
// finished answer. kind names the way it came ("" when nothing was cut).
func SplitThinking(content string) (thinking, answer, kind string) {
	return splitThinking(content, true)
}

// splitThinking does it for a finished answer (done) or for the part
// streamed so far: then unfinished thinking counts as thinking and a
// marker that may be cut in half is held back (it is in neither part).
func splitThinking(content string, done bool) (thinking, answer, kind string) {
	text := strings.TrimLeftFunc(content, unicode.IsSpace)
	if !done && heldBack(text) {
		return "", "", "held"
	}
	// <think>…</think>
	if m := thinkOpen.FindStringSubmatch(text); m != nil {
		closeTag := "</" + m[1] + ">"
		rest := text[len(m[0]):]
		if i := strings.Index(rest, closeTag); i >= 0 {
			return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+len(closeTag):]), "tag"
		}
		if !done {
			return dropPartialTag(rest), "", "tag"
		}
		return "", content, ""
	}
	// Only the closing tag: the template opened it.
	for _, tag := range []string{"</think>", "</thinking>", "</reasoning>"} {
		if i := strings.Index(text, tag); i >= 0 {
			return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+len(tag):]), "tag"
		}
	}
	// Harmony with its tokens.
	if strings.Contains(text, "<|channel|>") || strings.Contains(text, "<|message|>") {
		return splitHarmony(text, done)
	}
	// Harmony without the special tokens.
	if rest, ok := strings.CutPrefix(text, "analysis"); ok && rest != "" && !unicode.IsSpace(firstRune(rest)) && !unicode.IsLower(firstRune(rest)) {
		if i := strings.Index(rest, "assistantfinal"); i >= 0 {
			return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+len("assistantfinal"):]), "harmony"
		}
		if !done {
			return rest, "", "harmony"
		}
		return "", content, ""
	}
	// Unmarked reasoning with the answer glued to its end. While it streams
	// all of it is thinking: an answer that turns out to be one appears
	// when the message is complete (the saved entry replaces the stream).
	if reasoningStart.MatchString(text) {
		if !done {
			return text, "", "unmarked"
		}
		if at := lastGlue(text); at > 0 {
			return strings.TrimSpace(text[:at]), strings.TrimSpace(text[at:]), "unmarked"
		}
	}
	return "", content, ""
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// heldBack reports whether the start of a streamed answer may still turn
// out to be a marker of thinking.
func heldBack(text string) bool {
	if len(text) >= 16 {
		return false
	}
	for _, m := range []string{"<think>", "<thinking>", "<reasoning>", "</think>", "<|channel|>", "<|start|>", "analysis", "The user", "We need", "Let me", "Let's"} {
		if len(text) < len(m) && strings.HasPrefix(m, text) {
			return true
		}
	}
	return false
}

// dropPartialTag takes a "<…" without its ">" off the end of a stream.
func dropPartialTag(s string) string {
	if i := strings.LastIndexByte(s, '<'); i >= 0 && i >= len(s)-16 && !strings.Contains(s[i:], ">") {
		return s[:i]
	}
	return s
}

// lastGlue is where the answer starts in unmarked reasoning: the last
// sentence end followed by a capital without a space, outside code, with
// at least a few words of reasoning before and of answer after it.
func lastGlue(text string) int {
	at := -1
	for _, m := range glue.FindAllStringIndex(text, -1) {
		start := m[1] - 1
		for !utf8.RuneStart(text[start]) {
			start--
		}
		before, after := text[:start], text[start:]
		if strings.Count(before, "`")%2 == 1 || len(strings.Fields(before)) < 8 || len(strings.Fields(after)) < 2 ||
			!looks(before, englishWords, 3) || !looks(after, germanWords, 2) || looks(after, englishWords, 3) {
			continue
		}
		at = start
	}
	return at
}

// splitHarmony reads the messages of the Harmony format: analysis is
// thinking, final the answer; commentary to a tool is dropped (a call that
// the server did not read is never run from the text), commentary to the
// person is thinking when a final answer follows, else the answer.
func splitHarmony(text string, done bool) (thinking, answer, kind string) {
	if !done {
		text = dropPartialToken(text)
	}
	msgs := harmonyMsg.FindAllStringSubmatchIndex(text, -1)
	if len(msgs) == 0 {
		if !done {
			return "", "", "held" // the header of the first message is not complete yet
		}
		return "", text, ""
	}
	var think, final, said []string
	if prefix := strings.TrimSpace(text[:msgs[0][0]]); prefix != "" {
		think = append(think, prefix)
	}
	for _, m := range msgs {
		channel, header, body := text[m[2]:m[3]], text[m[4]:m[5]], strings.TrimSpace(text[m[6]:m[7]])
		switch {
		case body == "":
		case channel == "final":
			final = append(final, body)
		case channel == "commentary" && strings.Contains(header, "to="):
		case channel == "commentary":
			said = append(said, body)
		default:
			think = append(think, body)
		}
	}
	if len(final) == 0 {
		final, said = said, nil
	}
	think = append(think, said...)
	return strings.Join(think, "\n\n"), strings.Join(final, "\n\n"), "harmony"
}

// dropPartialToken takes a "<|…" without its "|>" off the end of a stream.
func dropPartialToken(s string) string {
	if i := strings.LastIndex(s, "<"); i >= 0 && i >= len(s)-14 && !strings.Contains(s[i:], ">") {
		return s[:i]
	}
	return s
}

// separate takes thinking out of the text of a finished message and adds
// it to the thinking the server gave apart.
func separate(msg Message) Message {
	thinking, answer, kind := SplitThinking(msg.Content)
	if kind == "" {
		return msg
	}
	if kind == "unmarked" {
		// Recognised by its look only: say so in the log, with sizes only.
		slog.Info("KI-Agent: reasoning taken out of the answer", "kind", kind, "reasoning", len(thinking), "answer", len(answer))
	}
	msg.Content = answer
	msg.Thinking = joinThinking(msg.Thinking, thinking)
	return msg
}

func joinThinking(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n\n" + b
}

// thinkStream passes the streamed pieces on with the thinking that comes
// inside the text sent as thinking. What it already sent as text stays
// sent; the saved message (separate) is what counts in the end.
type thinkStream struct {
	out             func(Chunk)
	content         strings.Builder
	sentT, sentText string
}

func (s *thinkStream) chunk(c Chunk) {
	if s.out == nil {
		return
	}
	if c.Thinking != "" {
		s.out(Chunk{Thinking: c.Thinking})
	}
	if c.Content == "" {
		return
	}
	s.content.WriteString(c.Content)
	thinking, answer, _ := splitThinking(s.content.String(), false)
	var next Chunk
	if strings.HasPrefix(thinking, s.sentT) {
		next.Thinking = thinking[len(s.sentT):]
		s.sentT = thinking
	}
	if strings.HasPrefix(answer, s.sentText) {
		next.Content = answer[len(s.sentText):]
		s.sentText = answer
	}
	if next.Thinking != "" || next.Content != "" {
		s.out(next)
	}
}

// CleanAnswer is the answer without thinking, for texts of the model that
// go elsewhere (title, push message, export).
func CleanAnswer(content string) string {
	_, answer, _ := SplitThinking(content)
	return answer
}

// cleanEntries takes thinking out of the answers of a chat read from the
// store (saved before 1.09); the next save keeps them so.
func cleanEntries(c *Chat) {
	for i := range c.Entries {
		e := &c.Entries[i]
		if e.Role != "assistant" || e.Content == "" {
			continue
		}
		if thinking, answer, kind := SplitThinking(e.Content); kind != "" {
			e.Content, e.Thinking = answer, joinThinking(e.Thinking, thinking)
		}
	}
}
