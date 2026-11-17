package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 1.10 (V4): a long result keeps its counts and hints in front and ends a
// list at a whole entry, saying how to get the rest.
func TestFitResultKeepsCountsAndWholeEntries(t *testing.T) {
	var entities []map[string]any
	for i := range 100 {
		entities = append(entities, map[string]any{"id": fmt.Sprintf("light.l%03d", i), "name": strings.Repeat("Lampe ", 10), "state": "on"})
	}
	data, _ := json.Marshal(map[string]any{"entities": entities, "total": 250, "nextOffset": 150, "note": "only released ones", "hidden": true, "why": "domain lock not released"})
	// json.Marshal sorts: the list comes first.
	if !strings.HasPrefix(string(data), `{"entities"`) {
		t.Fatalf("marshal order changed: %.40s", data)
	}
	out := fitResult(string(data), 3000, json.RawMessage(`{"domain":"light","offset":50}`))
	if utf8.RuneCountInString(out) > 3000 {
		t.Fatalf("too long: %d", utf8.RuneCountInString(out))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON any more: %v\n%s", err, out)
	}
	for _, k := range []string{"total", "nextOffset", "note", "hidden", "why"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s lost", k)
		}
	}
	if strings.Index(out, `"total"`) > strings.Index(out, `"entities"`) || strings.Index(out, `"why"`) > strings.Index(out, `"entities"`) {
		t.Errorf("counts not before the list: %.200s", out)
	}
	shown := len(got["entities"].([]any))
	if shown == 0 || shown == 100 {
		t.Fatalf("shown %d", shown)
	}
	note, _ := got[cutKey].(string)
	if !strings.Contains(note, fmt.Sprintf("%d of 100", shown)) || !strings.Contains(note, fmt.Sprintf("offset=%d", 50+shown)) {
		t.Errorf("note: %q", note)
	}
	if strings.Index(out, cutKey) > strings.Index(out, `"entities"`) {
		t.Errorf("note after the list")
	}
	// What fits stays whole, only reordered.
	small := `{"entries":[1,2],"total":2}`
	if out := fitResult(small, 3000, nil); out != `{"total":2,"entries":[1,2]}` {
		t.Errorf("small: %s", out)
	}
	// Big numbers stay exact.
	if out := fitResult(`{"size":12345678901234567890}`, 100, nil); !strings.Contains(out, "12345678901234567890") {
		t.Errorf("number changed: %s", out)
	}
}

func TestFitResultCutsTextAtALineEnd(t *testing.T) {
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "2026-11-17 03:%02d kernel: line %d\n", i%60, i)
	}
	out := fitResult(b.String(), 2000, nil)
	if utf8.RuneCountInString(out) > 2000 {
		t.Fatalf("too long: %d", utf8.RuneCountInString(out))
	}
	head, note, ok := strings.Cut(out, "\n[… cut for length: ")
	if !ok || !strings.Contains(note, "characters shown") {
		t.Fatalf("no note: %q", out[len(out)-120:])
	}
	for _, line := range strings.Split(head, "\n") {
		if line != "" && !strings.HasPrefix(line, "2026-11-17 03:") {
			t.Errorf("broken line %q", line)
		}
	}
}

// An older result (before the last question) keeps its counts too, so
// that after "das stimmt nicht" the model still sees what it relied on.
func TestOlderResultKeepsItsHead(t *testing.T) {
	var entities []map[string]any
	for i := range 100 {
		entities = append(entities, map[string]any{"id": fmt.Sprintf("automation.a%03d", i), "attributes": map[string]any{"friendly_name": strings.Repeat("x", 80)}})
	}
	data, _ := json.Marshal(map[string]any{"entities": entities, "total": 100, "released": false, "hidden": true, "why": "automations are not released"})
	now := time.Now()
	var call ToolCall
	call.Function.Name, call.Function.Arguments = "ha_entities", json.RawMessage(`{"domain":"automation"}`)
	chat := &Chat{Entries: []Entry{
		{Role: "user", Content: "wieviele automationen habe ich", At: now},
		{Role: "assistant", Calls: []ToolCall{call}, At: now},
		{Role: "tool", Tool: "ha_entities", Args: json.RawMessage(`{"domain":"automation"}`), Result: string(data), At: now},
		{Role: "assistant", Content: "Keine.", At: now},
		{Role: "user", Content: "das stimmt nicht", At: now},
	}}
	msgs := buildMessages("system", chat, MaxResultText)
	var older string
	for _, m := range msgs {
		if m.Role == "tool" {
			older = m.Content
		}
	}
	if utf8.RuneCountInString(older) > olderResultText || olderResultText <= 1500 {
		t.Fatalf("older result %d characters, limit %d", utf8.RuneCountInString(older), olderResultText)
	}
	for _, want := range []string{`"total":100`, `"hidden":true`, `"why":"automations are not released"`, `"released":false`} {
		if !strings.Contains(older, want) {
			t.Errorf("older result lost %s: %.300s", want, older)
		}
	}
}
