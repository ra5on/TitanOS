package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// askRead is a read of outside content that needs an approval.
type askRead struct{ fakeTools }

func (a *askRead) Prepare(ctx context.Context, chat *Chat, name string, args json.RawMessage) (Prepared, error) {
	if name == "readask" {
		return Prepared{Tool: name, Args: args, Mode: ModeAsk, Untrusted: true, Summary: "read"}, nil
	}
	return a.fakeTools.Prepare(ctx, chat, name, args)
}

// PLAN 59: an approved read of outside content still marks the chat; the
// mark was lost on the way through the approval, and later web tools ran
// without the stricter ask.
func TestApprovedUntrustedReadTaints(t *testing.T) {
	fake := &fakeOllama{replies: [][]string{{call("readask", `{}`)}, {`{"role":"assistant","content":"Gelesen."}`}}}
	tools := &askRead{}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: tools}
	chat := newChat("Lies die Datei")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if chat.Pending == nil || !chat.Pending.Untrusted || chat.Tainted {
		t.Fatalf("pending %+v tainted %v", chat.Pending, chat.Tainted)
	}
	// Saved and read back (a restart between ask and approval).
	data, _ := json.Marshal(chat)
	var back Chat
	json.Unmarshal(data, &back)
	if err := r.Decide(context.Background(), &back, back.Pending.ID, true); err != nil {
		t.Fatal(err)
	}
	if !back.Tainted {
		t.Fatal("approved read of outside content did not mark the chat")
	}
}

// Keeping the plan does not use up the steps of a small task, but a model
// that only rewrites its plan still ends.
func TestPlanUpdatesAreNotSteps(t *testing.T) {
	plan := call("plan_update", `{"steps":[{"text":"a"},{"text":"b"}]}`)
	var replies [][]string
	for range 4 {
		replies = append(replies, []string{plan})
	}
	replies = append(replies, []string{call("look", `{}`)}, []string{`{"role":"assistant","content":"Fertig."}`})
	fake := &fakeOllama{replies: replies}
	r := &Runner{Ollama: fake.serve(t), Model: "m", Tools: &fakeTools{}, MaxSteps: 2}
	chat := newChat("x")
	if err := r.Run(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	if last := chat.Entries[len(chat.Entries)-1]; last.Content != "Fertig." {
		t.Fatalf("plan updates counted as steps: %+v", last)
	}

	var endless [][]string
	for i := range 40 {
		endless = append(endless, []string{call("plan_update", `{"steps":[{"text":"`+string(rune('a'+i%26))+`"}]}`)})
	}
	fake = &fakeOllama{replies: endless}
	r = &Runner{Ollama: fake.serve(t), Model: "m", Tools: &fakeTools{}, MaxSteps: 3}
	chat = newChat("y")
	r.Run(context.Background(), chat)
	if len(fake.got) > freeBookkeeping+3+2 {
		t.Fatalf("endless plan: %d rounds", len(fake.got))
	}
}

func TestHugeResultsAreClipped(t *testing.T) {
	big := strings.Repeat("ä", 200<<10)
	got := keepResult(big)
	if len(got) > MaxStoredResult+64 || !utf8.ValidString(got) || !strings.Contains(got, "more bytes not kept") {
		t.Fatalf("clipped %d", len(got))
	}
	if keepResult("kurz") != "kurz" {
		t.Fatal("short result changed")
	}
}
