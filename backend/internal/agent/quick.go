package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Quick commands (0.95, owner 15 Nov 2026): a message that says in so many
// words what to switch – "Wohnzimmerlicht an", "Stehlampe auf 40 Prozent",
// "Ist der Fernseher an?", "Erinnere mich in 10 Minuten an die Pizza" –
// needs no model. TitanOS reads it itself and runs the one step through
// the same tools and rights as the model would; whatever it cannot read
// for certain goes to the model as before.

// Quick intents.
const (
	QuickOn     = "on"
	QuickOff    = "off"
	QuickLevel  = "level" // brightness in percent
	QuickOpen   = "open"
	QuickClose  = "close"
	QuickState  = "state"
	QuickRemind = "remind"
	QuickScene  = "scene" // activate a scene
	QuickTemp   = "temp"  // target temperature, Value in tenths of a degree
)

// QuickIntent is what a message asks for, as far as words can tell. Name
// is the device as the person called it; whether such a device exists is
// decided later.
type QuickIntent struct {
	Kind string
	Name string
	// Value: percent for QuickLevel, minutes for QuickRemind.
	Value int
	// Text of a reminder.
	Text string
	// English: the message was English (the answer follows it).
	English bool
}

var (
	quickPolite = regexp.MustCompile(`^(?:(?:hey|hallo|hi|ok|okay)\s+)?(?:bitte\s+|please\s+)?(?:(?:kannst|könntest|würdest)\s+du\s+(?:bitte\s+)?(?:mal\s+)?|(?:can|could|would)\s+you\s+(?:please\s+)?)?`)
	quickTail   = regexp.MustCompile(`(?:\s+bitte|\s+please|\s+mal)?[\s.!?]*$`)

	quickDE = []struct {
		re   *regexp.Regexp
		kind string
	}{
		// Questions first: "Ist der Fernseher an" must not read as "… an".
		{regexp.MustCompile(`^(?:stelle?|setze?|mache?|drehe?|heize)\s+(?:bitte\s+)?(.+?)\s+auf\s+(\d{1,2}(?:\.\d)?)\s*(?:grad|°c|°)$`), QuickTemp},
		{regexp.MustCompile(`^(.+?)\s+auf\s+(\d{1,2}(?:\.\d)?)\s*(?:grad|°c|°)$`), QuickTemp},
		{regexp.MustCompile(`^(?:aktiviere|starte)\s+(?:die\s+)?(?:szene\s+)?(.+)$`), QuickScene},
		{regexp.MustCompile(`^szene\s+(.+?)(?:\s+(?:an|ein|aktivieren|starten))?$`), QuickScene},
		{regexp.MustCompile(`^(.+?)\s+(?:aktivieren|starten)$`), QuickScene},
		{regexp.MustCompile(`^(?:ist|sind|läuft|laufen)\s+(.+?)\s+(?:an|aus|ein|eingeschaltet|ausgeschaltet|offen|auf|zu|geschlossen|geöffnet)$`), QuickState},
		{regexp.MustCompile(`^(?:ist|sind|läuft|laufen)\s+(.+)$`), QuickState},
		{regexp.MustCompile(`^(?:schalte?|mache?|stelle?|drehe?|dimme)\s+(?:bitte\s+)?(.+?)\s+auf\s+(\d{1,3})\s*(?:prozent|%)$`), QuickLevel},
		{regexp.MustCompile(`^(.+?)\s+auf\s+(\d{1,3})\s*(?:prozent|%)$`), QuickLevel},
		{regexp.MustCompile(`^(?:schalte?|mache?|drehe?)\s+(?:bitte\s+)?(.+?)\s+(?:an|ein)$`), QuickOn},
		{regexp.MustCompile(`^(?:schalte?|mache?|drehe?)\s+(?:bitte\s+)?(.+?)\s+aus$`), QuickOff},
		{regexp.MustCompile(`^(.+?)\s+(?:an|ein)(?:schalten|machen)?$`), QuickOn},
		{regexp.MustCompile(`^(.+?)\s+aus(?:schalten|machen)?$`), QuickOff},
		{regexp.MustCompile(`^(?:öffne|oeffne)\s+(.+)$`), QuickOpen},
		{regexp.MustCompile(`^(?:schließe|schliesse)\s+(.+)$`), QuickClose},
		{regexp.MustCompile(`^(?:fahre?|mache?)\s+(.+?)\s+(?:hoch|auf|rauf)$`), QuickOpen},
		{regexp.MustCompile(`^(?:fahre?|mache?)\s+(.+?)\s+(?:runter|zu|herunter)$`), QuickClose},
		{regexp.MustCompile(`^(.+?)\s+(?:hoch|hochfahren|öffnen)$`), QuickOpen},
		{regexp.MustCompile(`^(.+?)\s+(?:runter|runterfahren|schließen)$`), QuickClose},
	}
	quickEN = []struct {
		re   *regexp.Regexp
		kind string
	}{
		{regexp.MustCompile(`^(?:is|are)\s+(.+?)\s+(?:on|off|open|closed|running)$`), QuickState},
		{regexp.MustCompile(`^(?:set|turn|heat)\s+(.+?)\s+to\s+(\d{1,2}(?:\.\d)?)\s*(?:degrees|°c|°)$`), QuickTemp},
		{regexp.MustCompile(`^(.+?)\s+to\s+(\d{1,2}(?:\.\d)?)\s*(?:degrees|°c|°)$`), QuickTemp},
		{regexp.MustCompile(`^(?:activate|start)\s+(?:the\s+)?(?:scene\s+)?(.+)$`), QuickScene},
		{regexp.MustCompile(`^scene\s+(.+)$`), QuickScene},
		{regexp.MustCompile(`^(?:set|dim|turn)\s+(.+?)\s+to\s+(\d{1,3})\s*(?:percent|%)$`), QuickLevel},
		{regexp.MustCompile(`^(.+?)\s+to\s+(\d{1,3})\s*(?:percent|%)$`), QuickLevel},
		{regexp.MustCompile(`^(?:turn|switch)\s+on\s+(.+)$`), QuickOn},
		{regexp.MustCompile(`^(?:turn|switch)\s+off\s+(.+)$`), QuickOff},
		{regexp.MustCompile(`^(?:turn|switch)\s+(.+?)\s+on$`), QuickOn},
		{regexp.MustCompile(`^(?:turn|switch)\s+(.+?)\s+off$`), QuickOff},
		{regexp.MustCompile(`^(.+?)\s+on$`), QuickOn},
		{regexp.MustCompile(`^(.+?)\s+off$`), QuickOff},
		{regexp.MustCompile(`^open\s+(.+)$`), QuickOpen},
		{regexp.MustCompile(`^close\s+(.+)$`), QuickClose},
	}

	remindDE = regexp.MustCompile(`^(?:erinnere?\s+mich|erinner\s+mich)\s+in\s+(\d{1,4})\s*(minuten?|min|stunden?|std|h)\s+(?:an|daran,?\s+dass|daran)\s+(.+)$`)
	timerDE  = regexp.MustCompile(`^(?:(?:stelle?|starte?|setze?)\s+(?:einen\s+|den\s+)?)?(?:timer|wecker)\s+(?:auf\s+|für\s+|von\s+|in\s+)?(\d{1,4})\s*(minuten?|min|stunden?|std|h)$`)
	remindEN = regexp.MustCompile(`^remind\s+me\s+in\s+(\d{1,4})\s*(minutes?|min|hours?|h)\s+(?:to|about|of|that)\s+(.+)$`)
	timerEN  = regexp.MustCompile(`^(?:(?:set|start)\s+(?:a\s+|the\s+)?)?timer\s+(?:for\s+|to\s+|of\s+)?(\d{1,4})\s*(minutes?|min|hours?|h)$`)
)

// ParseQuick reads a message as a quick command, or returns nil. It only
// looks at the words; a device of that name must still be found.
func ParseQuick(message string) *QuickIntent {
	// "21,5 Grad" is a number, not a second clause.
	message = quickDecimal.ReplaceAllString(message, "$1.$2")
	text := strings.ToLower(strings.TrimSpace(message))
	// One short line, no second sentence: anything longer is for the model.
	if text == "" || len([]rune(text)) > 90 || strings.ContainsAny(text, "\n,;:") {
		return nil
	}
	raw := strings.TrimSpace(quickTail.ReplaceAllString(strings.TrimSpace(message), ""))
	text = quickTail.ReplaceAllString(text, "")
	if strings.ContainsAny(quickDecimal2.ReplaceAllString(text, "$1$2"), ".!?") {
		return nil
	}
	cut := len(quickPolite.FindString(text))
	text = text[cut:]
	if cut <= len(strings.ToLower(raw)) {
		// The reminder keeps the person's own spelling.
		if lower := strings.ToLower(raw); len(lower) == len(raw) {
			raw = raw[cut:]
		} else {
			raw = text
		}
	}
	minutes := func(n, unit string) int {
		v, _ := strconv.Atoi(n)
		if strings.HasPrefix(unit, "s") || strings.HasPrefix(unit, "h") {
			v *= 60
		}
		return v
	}
	for _, r := range []struct {
		re      *regexp.Regexp
		english bool
	}{{remindDE, false}, {remindEN, true}} {
		if m := r.re.FindStringSubmatch(text); m != nil {
			if v := minutes(m[1], m[2]); v >= 1 && v <= 7*24*60 {
				return &QuickIntent{Kind: QuickRemind, Value: v, Text: strings.TrimSpace(raw[len(raw)-len(m[3]):]), English: r.english}
			}
			return nil
		}
	}
	for _, r := range []struct {
		re      *regexp.Regexp
		english bool
	}{{timerDE, false}, {timerEN, true}} {
		if m := r.re.FindStringSubmatch(text); m != nil {
			if v := minutes(m[1], m[2]); v >= 1 && v <= 7*24*60 {
				return &QuickIntent{Kind: QuickRemind, Value: v, English: r.english}
			}
			return nil
		}
	}
	for _, set := range []struct {
		list []struct {
			re   *regexp.Regexp
			kind string
		}
		english bool
	}{{quickDE, false}, {quickEN, true}} {
		for _, p := range set.list {
			m := p.re.FindStringSubmatch(text)
			if m == nil {
				continue
			}
			in := &QuickIntent{Kind: p.kind, Name: strings.TrimSpace(m[1]), English: set.english}
			if p.kind == QuickLevel {
				in.Value, _ = strconv.Atoi(m[2])
				if in.Value > 100 {
					return nil
				}
			}
			if p.kind == QuickTemp {
				f, _ := strconv.ParseFloat(m[2], 64)
				in.Value = int(f*10 + 0.5)
				// What a room is heated to; anything else is for the model.
				if in.Value < 50 || in.Value > 300 {
					return nil
				}
			}
			if len(QuickWords(in.Name)) == 0 {
				return nil
			}
			return in
		}
	}
	return nil
}

var (
	quickDecimal  = regexp.MustCompile(`(\d),(\d)`)
	quickDecimal2 = regexp.MustCompile(`(\d)\.(\d)`)
	quickAnd      = regexp.MustCompile(`(?i)\s+(?:und|and)\s+(?:dann\s+|then\s+)?`)
)

// ParseQuickAll reads a message as two or three quick commands joined by
// "und" ("Stehlampe an und Rollladen runter"). Every part must be one;
// reminders are not joined. A polite start counts for the first part only.
func ParseQuickAll(message string) []*QuickIntent {
	parts := quickAnd.Split(strings.TrimSpace(message), -1)
	if len(parts) < 2 || len(parts) > 3 {
		return nil
	}
	var all []*QuickIntent
	for _, part := range parts {
		in := ParseQuick(part)
		if in == nil || in.Kind == QuickRemind || in.Kind == QuickState {
			return nil
		}
		all = append(all, in)
	}
	return all
}

// quickFill are words that say nothing about which device is meant.
var quickFill = map[string]bool{
	"der": true, "die": true, "das": true, "den": true, "dem": true, "des": true, "ein": true, "eine": true, "einen": true,
	"im": true, "in": true, "am": true, "an": true, "auf": true, "vom": true, "von": true, "bei": true, "beim": true, "zum": true, "zur": true,
	"mein": true, "meine": true, "meinen": true, "unser": true, "unsere": true, "alle": true, "allen": true,
	"the": true, "a": true, "at": true, "of": true, "my": true, "our": true, "all": true,
}

var quickSplit = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// QuickWords are the words of a name that count, in lower case.
func QuickWords(name string) []string {
	var words []string
	for _, w := range quickSplit.Split(strings.ToLower(name), -1) {
		if w != "" && !quickFill[w] {
			words = append(words, w)
		}
	}
	return words
}

// QuickStep is the one step of a quick command and what to say after it.
type QuickStep struct {
	Tool string
	Args json.RawMessage
	// Say makes the answer from the result of the step; "" hands the
	// message to the model after all (the step stays in the chat).
	Say func(result string) string
}

// Direct runs the steps of a quick command and answers, without the
// model. It reports false, with the chat untouched, when the rights do not
// let every step run without asking – then the model takes the message and
// the person is asked as usual. A step that fails stays in the chat as a
// failed step (with those that ran before it) and the model goes on from
// there.
func (r *Runner) Direct(ctx context.Context, chat *Chat, steps ...QuickStep) (bool, error) {
	if len(steps) == 0 {
		return false, nil
	}
	ready := make([]Prepared, len(steps))
	for i, q := range steps {
		p, err := r.Tools.Prepare(ctx, chat, q.Tool, q.Args)
		if err != nil || p.Mode != ModeAllow {
			return false, nil
		}
		ready[i] = p
	}
	r.setState(chat, StateRunning, "")
	if r.start.IsZero() {
		r.start = r.now()
	}
	var answers []string
	for i, q := range steps {
		step := Entry{ID: NewID(), At: r.now(), Role: "tool", Tool: q.Tool, Args: q.Args}
		r.emit(Event{Type: "step", Step: &step})
		before := len(chat.Entries)
		done, err := r.execute(ctx, chat, ready[i], step)
		if err != nil || done {
			return done, err
		}
		ran := chat.Entries[before]
		if !ran.OK {
			return false, nil
		}
		answer := q.Say(ran.Result)
		if answer == "" {
			return false, nil
		}
		answers = append(answers, answer)
	}
	if err := r.add(chat, Entry{Role: "assistant", Content: strings.Join(answers, " ")}); err != nil {
		return false, err
	}
	r.setState(chat, StateIdle, "")
	return true, r.save(chat)
}
