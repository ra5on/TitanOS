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
	out := fitResult(string(data), 3000, json.RawMessage(`{"domain":"light","offset":50}`), false)
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
	if out := fitResult(small, 3000, nil, false); out != `{"total":2,"entries":[1,2]}` {
		t.Errorf("small: %s", out)
	}
	// Big numbers stay exact.
	if out := fitResult(`{"size":12345678901234567890}`, 100, nil, false); !strings.Contains(out, "12345678901234567890") {
		t.Errorf("number changed: %s", out)
	}
}

func TestFitResultCutsTextAtALineEnd(t *testing.T) {
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "2026-11-17 03:%02d kernel: line %d\n", i%60, i)
	}
	out := fitResult(b.String(), 2000, nil, false)
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
	msgs := buildMessages("system", chat, MaxResultText, nil)
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

// A result without a list, or one that is too long around its list, stays
// valid JSON when cut, and keeps hidden/why whole (security review 1.10,
// M5): a config of an automation is one deep object.
func TestFitResultNeverCutsInsideJSON(t *testing.T) {
	var actions []any
	for i := range 60 {
		actions = append(actions, map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": fmt.Sprintf("light.raum_%d", i)},
			"data": map[string]any{"message": strings.Repeat("Ein langer Text <b> & mehr ", 4)}})
	}
	config := map[string]any{"alias": "Gute Nacht", "triggers": []any{map[string]any{"trigger": "time", "at": "22:00"}}, "actions": actions}
	data, _ := json.Marshal(map[string]any{"entity": "automation.gute_nacht", "id": "1", "config": config, "hidden": true,
		"why": "Devices that are not released are shown as (not released).", "note": "Save it back only whole."})
	for limit := 200; limit <= 6000; limit += 97 {
		out := fitResult(string(data), limit, nil, false)
		if !json.Valid([]byte(out)) {
			t.Fatalf("limit %d: not JSON: %s", limit, out)
		}
		if runes(out) > limit {
			t.Fatalf("limit %d: %d characters", limit, runes(out))
		}
		var got map[string]any
		json.Unmarshal([]byte(out), &got)
		if got["hidden"] != true || got["why"] == nil || got["note"] == nil {
			t.Fatalf("limit %d: hints lost: %s", limit, out)
		}
		if got["truncated"] != true || !strings.Contains(out, CutMark) {
			t.Fatalf("limit %d: cut not marked: %s", limit, out)
		}
	}
	// A list at the top level stays a list.
	list, _ := json.Marshal(actions)
	if out := fitResult(string(list), 900, nil, false); !json.Valid([]byte(out)) || !strings.Contains(out, CutMark) || runes(out) > 900 {
		t.Fatalf("list: %s", out)
	}
	// An own "truncated" of a tool (a search that stopped) is not changed.
	own, _ := json.Marshal(map[string]any{"truncated": false, "text": strings.Repeat("x", 5000)})
	if out := fitResult(string(own), 500, nil, false); !strings.Contains(out, `"truncated":false`) || !json.Valid([]byte(out)) {
		t.Fatalf("own truncated: %s", out)
	}
}

// The note of a cut list names an offset only for a tool that has one
// (security review 1.10, N7).
func TestFitResultOffsetOnlyForPagedTools(t *testing.T) {
	var lines []any
	for i := range 200 {
		lines = append(lines, map[string]any{"when": "2026-11-17T03:00:00Z", "entity": fmt.Sprintf("automation.a_%d", i)})
	}
	data, _ := json.Marshal(map[string]any{"entries": lines})
	if out := fitResult(string(data), 2000, nil, false); strings.Contains(out, "offset") || !strings.Contains(out, "narrow it") {
		t.Fatalf("not paged: %s", out[:300])
	}
	if out := fitResult(string(data), 2000, nil, true); !strings.Contains(out, "offset=") {
		t.Fatalf("paged: %s", out[:300])
	}
}
