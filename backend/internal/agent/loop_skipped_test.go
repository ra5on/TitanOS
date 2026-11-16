package agent

import "testing"

// 0.99: the calls of a round that were not made because an earlier one
// waits come after that step's result, in the order the model called them.
func TestTakeSkippedKeepsOrder(t *testing.T) {
	chat := &Chat{Entries: []Entry{
		{Role: "user", Content: "X und Y neu starten"},
		{Role: "assistant"},
		{Role: "tool", Tool: "app_action", Result: skippedWaiting},
		{Role: "tool", Tool: "app_action", Result: skippedWaiting},
	}}
	skipped := chat.TakeSkipped()
	if len(skipped) != 2 || len(chat.Entries) != 2 {
		t.Fatalf("taken %d, left %d", len(skipped), len(chat.Entries))
	}
	chat.Entries = append(chat.Entries, Entry{Role: "tool", Tool: "app_action", Result: "restarted"})
	chat.Entries = append(chat.Entries, skipped...)
	if chat.Entries[2].Result != "restarted" || chat.Entries[3].Result != skippedWaiting {
		t.Fatalf("order: %q then %q", chat.Entries[2].Result, chat.Entries[3].Result)
	}
	// Nothing to take: the chat stays as it is.
	if again := (&Chat{Entries: []Entry{{Role: "tool", Tool: "x", Result: "ok"}}}).TakeSkipped(); len(again) != 0 {
		t.Fatalf("took %d from a chat without skipped calls", len(again))
	}
}
