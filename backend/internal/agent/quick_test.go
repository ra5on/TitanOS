package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestParseQuick(t *testing.T) {
	for _, c := range []struct {
		say, kind, name string
		value           int
		text            string
	}{
		{"Wohnzimmerlicht an", QuickOn, "wohnzimmerlicht", 0, ""},
		{"Schalte das Licht im Wohnzimmer aus.", QuickOff, "das licht im wohnzimmer", 0, ""},
		{"Mach bitte die Stehlampe an", QuickOn, "die stehlampe", 0, ""},
		{"Kannst du die Stehlampe ausschalten?", QuickOff, "die stehlampe", 0, ""},
		{"Stehlampe einschalten", QuickOn, "stehlampe", 0, ""},
		{"Stehlampe auf 40 Prozent", QuickLevel, "stehlampe", 40, ""},
		{"Dimme die Stehlampe auf 15%", QuickLevel, "die stehlampe", 15, ""},
		{"Öffne die Markise", QuickOpen, "die markise", 0, ""},
		{"Fahre den Rollladen im Büro runter", QuickClose, "den rollladen im büro", 0, ""},
		{"Ist der Fernseher an?", QuickState, "der fernseher", 0, ""},
		{"Ist die Markise offen?", QuickState, "die markise", 0, ""},
		{"turn off the kitchen light", QuickOff, "the kitchen light", 0, ""},
		{"Turn the kitchen light on please", QuickOn, "the kitchen light", 0, ""},
		{"set the floor lamp to 30 percent", QuickLevel, "the floor lamp", 30, ""},
		{"is the tv on?", QuickState, "the tv", 0, ""},
		{"Erinnere mich in 10 Minuten an die Pizza im Ofen", QuickRemind, "", 10, "die Pizza im Ofen"},
		{"erinnere mich in 2 Stunden daran, dass", "", "", 0, ""},
		{"Timer 5 Minuten", QuickRemind, "", 5, ""},
		{"Stelle einen Timer auf 1 Stunde", QuickRemind, "", 60, ""},
		{"remind me in 20 minutes to call Anna", QuickRemind, "", 20, "call Anna"},
		{"set a timer for 3 minutes", QuickRemind, "", 3, ""},
		// Not commands: for the model.
		{"Hallo", "", "", 0, ""},
		{"Wie laufen meine Apps?", "", "", 0, ""},
		{"Schalte das Licht aus und fahre die Rollläden runter, danach gute Nacht", "", "", 0, ""},
		{"Stehlampe auf 140 Prozent", "", "", 0, ""},
		{"Schreib mir eine Zusammenfassung. Dann mach das Licht aus", "", "", 0, ""},
	} {
		in := ParseQuick(c.say)
		if c.kind == "" {
			if in != nil {
				t.Errorf("%q: read as %+v, want nothing", c.say, *in)
			}
			continue
		}
		if in == nil {
			t.Errorf("%q: not read", c.say)
			continue
		}
		if in.Kind != c.kind || in.Name != c.name || in.Value != c.value || in.Text != c.text {
			t.Errorf("%q: %+v, want %s %q %d %q", c.say, *in, c.kind, c.name, c.value, c.text)
		}
	}
	if w := QuickWords("das Licht im Wohnzimmer"); len(w) != 2 || w[0] != "licht" || w[1] != "wohnzimmer" {
		t.Errorf("words: %v", w)
	}
}

// A quick step runs without the model and answers; a step the rights
// would ask about is left alone, for the model and the person.
func TestRunnerDirect(t *testing.T) {
	tools := &fakeTools{}
	saved := 0
	r := &Runner{Tools: tools, Save: func(*Chat) error { saved++; return nil }}
	chat := &Chat{ID: NewID(), State: StateIdle, Entries: []Entry{{ID: NewID(), Role: "user", Content: "Stehlampe an"}}}
	done, err := r.Direct(context.Background(), chat, QuickStep{Tool: "look", Args: json.RawMessage(`{}`), Say: func(string) string { return "Stehlampe ist an." }})
	if err != nil || !done {
		t.Fatalf("direct: %v %v", done, err)
	}
	if len(chat.Entries) != 3 || chat.Entries[1].Role != "tool" || !chat.Entries[1].OK || chat.Entries[2].Role != "assistant" || chat.Entries[2].Content != "Stehlampe ist an." {
		t.Fatalf("entries: %+v", chat.Entries)
	}
	if chat.State != StateIdle || len(tools.ran) != 1 || saved == 0 {
		t.Fatalf("state %s ran %v saved %d", chat.State, tools.ran, saved)
	}
	// Needs approval: nothing happens here.
	chat = &Chat{ID: NewID(), State: StateIdle, Entries: []Entry{{ID: NewID(), Role: "user", Content: "VM neu starten"}}}
	done, err = r.Direct(context.Background(), chat, QuickStep{Tool: "change", Args: json.RawMessage(`{}`), Say: func(string) string { return "x" }})
	if err != nil || done || len(chat.Entries) != 1 || chat.Pending != nil || len(tools.ran) != 1 {
		t.Fatalf("asking step: done %v err %v entries %d ran %v", done, err, len(chat.Entries), tools.ran)
	}
	// No words for the result: the step stays, the model goes on.
	done, err = r.Direct(context.Background(), chat, QuickStep{Tool: "look", Args: json.RawMessage(`{}`), Say: func(string) string { return "" }})
	if err != nil || done || len(chat.Entries) != 2 {
		t.Fatalf("no answer: done %v err %v entries %d", done, err, len(chat.Entries))
	}
}
