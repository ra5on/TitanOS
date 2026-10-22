package assistant

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestUnlimitedAgentBeyond500Rounds(t *testing.T) {
	replies := [][]string{}
	for n := 0; n < 505; n++ {
		replies = append(replies, []string{fmt.Sprintf(`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"storage_overview","arguments":{"n":%d}}}]}`, n)})
	}
	replies = append(replies, []string{`{"role":"assistant","content":"all 505 steps completed"}`})
	fake := &fakeOllama{replies: replies}
	m := manager(t, fake.serve(t))
	var calls []string
	chat := newChat(t, "Inspect all objects")
	if err := m.Answer(context.Background(), chat, Turn{Person: Person{Name: "alex", Role: "owner"}, Tools: testTools(&calls), Emit: func(Event) {}}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 505 || chat.Messages[len(chat.Messages)-1].Content != "all 505 steps completed" {
		t.Fatalf("limited agent: %d calls", len(calls))
	}
	if chat.Archived == 0 {
		t.Fatal("long task did not compact")
	}
}

func TestCompactionKeepsMutationDeduplication(t *testing.T) {
	c := newChat(t, "do one change")
	a := &Action{Method: "DELETE", Path: "/api/v1/files", Body: map[string]any{"path": "one.txt"}}
	if repeatedAction(c, a) {
		t.Fatal("first action blocked")
	}
	for i := 0; i < 30; i++ {
		c.Messages = append(c.Messages, Entry{Role: "assistant", Content: strings.Repeat("x", 2000)})
	}
	compact(c, 4096)
	if !repeatedAction(c, a) {
		t.Fatal("compaction forgot mutation")
	}
}

func TestExecutedHistoryCannotBeRegenerated(t *testing.T) {
	c := newChat(t, "remove file")
	c.Messages = append(c.Messages, Entry{Role: "tool", Action: &Action{Key: "file.delete", State: ActionDone}})
	if _, err := LastQuestion(c); err == nil {
		t.Fatal("regenerated side effect")
	}
	if err := CutBefore(c, 0); err == nil {
		t.Fatal("erased side effect")
	}
}

func TestPromptHistoryIsServerOwned(t *testing.T) {
	m := manager(t, "http://127.0.0.1:11434")
	s := m.Settings()
	s.SystemPrompt = "Version one"
	s.PromptHistory = []string{"injected"}
	s.Execution = "direct"
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	s = m.Settings()
	s.SystemPrompt = "Version two"
	s.PromptHistory = []string{"injected"}
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	s = m.Settings()
	if len(s.PromptHistory) == 0 || s.PromptHistory[len(s.PromptHistory)-1] == "injected" {
		t.Fatalf("history: %v", s.PromptHistory)
	}
	preview := m.ContextPreview(Person{Name: "alex", Role: "owner"}, "/storage")
	if !strings.Contains(preview, "Version two") || !strings.Contains(preview, "direct") {
		t.Fatalf("context omitted config: %s", preview)
	}
}
