package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestWantedAreas(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string
	}{
		{"Kannst du eine email an joostpatrick@example.org senden", []string{AreaMail}},
		{"Schreib an anna@example.de, dass ich später komme", []string{AreaMail}},
		{"Hab ich neue Mails?", []string{AreaMail}},
		{"Was steht in meinem Posteingang?", []string{AreaMail}},
		{"Trag mir morgen um 10 einen Termin beim Zahnarzt ein", []string{AreaCalendar}},
		{"Erinnere mich an den Müll", []string{AreaCalendar}},
		{"Welche Telefonnummer hat Anna? Schick ihr dann eine E-Mail", []string{AreaMail, AreaContacts}},
		{"Add a meeting with Bob and look up his phone number", []string{AreaCalendar, AreaContacts}},
		// Not about them: the console, "eventuell", status questions.
		{"Öffne das Terminal", nil},
		{"Läuft eventuell Docker noch?", nil},
		{"Wie ist der Status von Home Assistant?", nil},
		{"Hallo", nil},
	} {
		if got := WantedAreas(c.text); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
}

// wantTools offers more than lazyFrom tools: files, docker and, when open,
// mail and calendar; closed areas say why (Explainer).
type wantTools struct {
	kindTools
	mailOpen bool
}

func (p *wantTools) Specs(chat *Chat) []ToolSpec {
	out := p.kindTools.Specs(chat)
	add := func(name, area string) {
		var s ToolSpec
		s.Type = "function"
		s.Function.Name = name
		s.Area, s.Kind = area, KindRead
		out = append(out, s)
	}
	if p.mailOpen {
		add("mail_send", AreaMail)
	}
	add("calendar_list", AreaCalendar)
	return out
}

func (p *wantTools) Missing() map[string]string {
	if p.mailOpen {
		return nil
	}
	return map[string]string{AreaMail: "Mail is not switched on for this account (Benutzer › Konto › Apps) or has no mailbox yet"}
}

func (p *wantTools) Prepare(ctx context.Context, chat *Chat, name string, args json.RawMessage) (Prepared, error) {
	switch name {
	case "mail_send":
		return Prepared{Tool: name, Args: args, Mode: ModeAllow, Kind: KindChange, Area: AreaMail}, nil
	case "calendar_list":
		return Prepared{Tool: name, Args: args, Mode: ModeAllow, Kind: KindRead, Area: AreaCalendar}, nil
	}
	return p.kindTools.Prepare(ctx, chat, name, args)
}

func TestPreloadOpenArea(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		return 200, []string{answer("Was soll im Betreff stehen?")}
	}}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &wantTools{mailOpen: true}, LazyTools: true}
	chat := newChat("Kannst du eine email an joost@example.org senden")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(toolNames(s.got[0]), ",")
	// Mail comes along in the first round; the calendar and docker do not.
	if !strings.Contains(first, "mail_send") || strings.Contains(first, "calendar_list") || strings.Contains(first, "docker_3") {
		t.Fatalf("first offer %s", first)
	}
	if !slices.Equal(chat.Areas, []string{AreaMail}) {
		t.Fatalf("areas %v", chat.Areas)
	}
	if sys := systemOf(s.got[0]); strings.Contains(sys, "closed for you") {
		t.Fatalf("open area got a note: %s", sys)
	}
	// The next message without the word keeps it ("Betreff Test").
	chat.Entries = append(chat.Entries, Entry{ID: NewID(), Role: "user", Content: "Betreff Test, Text Hallo"})
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if last := strings.Join(toolNames(s.got[len(s.got)-1]), ","); !strings.Contains(last, "mail_send") {
		t.Fatalf("follow-up offer %s", last)
	}
}

func TestPreloadClosedAreaSaysWhy(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		return 200, []string{answer("Mail ist für dein Konto nicht eingeschaltet.")}
	}}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &wantTools{}, LazyTools: true}
	chat := newChat("Kannst du eine email an joost@example.org senden")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(toolNames(s.got[0]), ",")
	// Nothing of mail is offered (the rights close it), but the reason goes along.
	if strings.Contains(first, "mail_") || len(chat.Areas) != 0 {
		t.Fatalf("closed area offered: %s %v", first, chat.Areas)
	}
	sys := systemOf(s.got[0])
	if !strings.Contains(sys, "closed for you now – mail: Mail is not switched on for this account (Benutzer › Konto › Apps)") {
		t.Fatalf("no reason in the system prompt: %s", sys[max(0, len(sys)-400):])
	}
	// A question about something else drops the note.
	chat.Entries = append(chat.Entries, Entry{ID: NewID(), Role: "user", Content: "Und wie geht es Docker?"})
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if sys := systemOf(s.got[len(s.got)-1]); strings.Contains(sys, "closed for you") {
		t.Fatal("the note stayed for another question")
	}
}

// Without LazyTools every tool is offered anyway; the closed note still goes.
func TestPreloadAllTools(t *testing.T) {
	s := &scripted{next: func(n int, body map[string]any) (int, []string) { return 200, []string{answer("ok")} }}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &wantTools{}}
	chat := newChat("Termin morgen und Mail an a@example.org")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if len(chat.Areas) != 0 {
		t.Fatalf("areas without LazyTools: %v", chat.Areas)
	}
	if !strings.Contains(systemOf(s.got[0]), "closed for you now – mail:") {
		t.Fatal("no note")
	}
}

// draftTools: mail_send is a draft ("Nur Vorschläge").
type draftTools struct{ wantTools }

func (d *draftTools) Prepare(ctx context.Context, chat *Chat, name string, args json.RawMessage) (Prepared, error) {
	p, err := d.wantTools.Prepare(ctx, chat, name, args)
	if name == "mail_send" {
		p.Draft = true
	}
	return p, err
}

func (d *draftTools) Execute(ctx context.Context, chat *Chat, p Prepared) (Outcome, error) {
	if p.Draft {
		return Outcome{Text: `{"draft":true}`, Draft: &Draft{App: "mail", Kind: "compose"}}, nil
	}
	return d.wantTools.Execute(ctx, chat, p)
}

// 114-1: the same mail again in one turn makes no second card; another
// mail does.
func TestSameDraftOnce(t *testing.T) {
	mail := `{"to":["a@example.org"],"subject":"Test","text":"Hallo"}`
	s := &scripted{next: func(n int, body map[string]any) (int, []string) {
		switch n {
		case 0:
			return 200, []string{call("mail_send", mail)}
		case 1:
			return 200, []string{call("mail_send", `{"account":"box1","to":["a@example.org"],"subject":"Test","text":"Hallo"}`)}
		case 2:
			return 200, []string{call("mail_send", `{"to":["b@example.org"],"subject":"Test","text":"Hallo"}`)}
		}
		return 200, []string{answer("Zwei Karten.")}
	}}
	r := &Runner{Ollama: s.serve(t), Model: "m", Tools: &draftTools{wantTools{mailOpen: true}}, LazyTools: true}
	chat := newChat("Schick eine Mail an a@example.org")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	var cards, again int
	for _, e := range chat.Entries {
		if e.Role == "tool" && e.Draft != nil {
			cards++
		}
		if e.Role == "tool" && strings.HasPrefix(e.Result, "Already done") {
			again++
		}
	}
	if cards != 2 || again != 1 {
		t.Fatalf("cards %d, refused %d", cards, again)
	}
	// A new message of the person may make the same mail again.
	chat.Entries = append(chat.Entries, Entry{ID: NewID(), Role: "user", Content: "Nochmal bitte"})
	from := len(s.got)
	s.next = func(n int, body map[string]any) (int, []string) {
		if n == from {
			return 200, []string{call("mail_send", mail)}
		}
		return 200, []string{answer("ok")}
	}
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	made := false
	for _, e := range chat.Entries[lastUserIndex(chat)+1:] {
		made = made || (e.Role == "tool" && e.Draft != nil)
	}
	if !made {
		for _, e := range chat.Entries {
			t.Logf("%s %s %s %.80s", e.Role, e.Tool, e.Content, e.Result)
		}
		t.Fatal("a new turn was refused")
	}
}
