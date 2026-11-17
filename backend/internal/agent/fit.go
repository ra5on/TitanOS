package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Fitting a tool result into what the model may see (1.10, V4). A hard
// cut after N characters broke the JSON and, because json.Marshal sorts
// the keys, took exactly the fields that say how much there is: "total",
// "nextOffset", "note" stand behind a long "entities". So:
//
//   - the counts and hints of a result come first, the lists last;
//   - a list that does not fit ends at its last whole entry, and a field
//     before it says how many are shown and how to get the rest;
//   - a result that is no JSON object is cut at a line end, with the same
//     kind of hint.
//
// This is how the model sees a result; the chat keeps it as it was.

// headKeys are fields that say what a result holds; they lead, in this
// order. Everything else follows sorted, lists at the end.
var headKeys = []string{"total", "count", "shown", "cut", "truncated", "nextOffset", "next", "offset", "scanned",
	"released", "hidden", "why", "note", "answer", "error", "byDomain"}

// cutKey is the field the fitter adds when it shortens a list.
const cutKey = "cutForLength"

// fitResult returns text in at most limit characters. args are the
// arguments of the call (for the offset of the next page), may be nil.
func fitResult(text string, limit int, args json.RawMessage) string {
	if limit <= 0 {
		return text
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		var fields map[string]json.RawMessage
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if dec.Decode(&fields) == nil && !dec.More() {
			if out, ok := fitObject(fields, limit, args); ok {
				return out
			}
		}
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return cutText(text, limit)
}

// cutText cuts plain text at the last line end that fits.
func cutText(text string, limit int) string {
	total := utf8.RuneCountInString(text)
	note := fmt.Sprintf("\n[… cut for length: %d of %d characters shown; ask for less (a filter, fewer lines) to see the rest / gekürzt]", 0, total)
	keep := max(0, limit-utf8.RuneCountInString(note)-8)
	r := []rune(text)
	if keep > len(r) {
		keep = len(r)
	}
	head := string(r[:keep])
	if i := strings.LastIndexByte(head, '\n'); i > len(head)/2 {
		head = head[:i]
	}
	shown := utf8.RuneCountInString(head)
	return head + fmt.Sprintf("\n[… cut for length: %d of %d characters shown; ask for less (a filter, fewer lines) to see the rest / gekürzt]", shown, total)
}

// orderedKeys puts the head keys first, then the others with short values,
// then those with long values (lists, big objects).
func orderedKeys(fields map[string]json.RawMessage) []string {
	var head, small, big []string
	for _, k := range headKeys {
		if _, ok := fields[k]; ok {
			head = append(head, k)
		}
	}
	for k, v := range fields {
		if slices.Contains(headKeys, k) {
			continue
		}
		if len(v) > 200 || bytes.HasPrefix(bytes.TrimSpace(v), []byte("[")) {
			big = append(big, k)
		} else {
			small = append(small, k)
		}
	}
	slices.Sort(small)
	slices.SortFunc(big, func(a, b string) int { return len(fields[a]) - len(fields[b]) })
	return append(append(head, small...), big...)
}

func writeObject(fields map[string]json.RawMessage, keys []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(k)
		b.Write(name)
		b.WriteByte(':')
		var c bytes.Buffer
		if json.Compact(&c, fields[k]) == nil {
			b.Write(c.Bytes())
		} else {
			b.Write(fields[k])
		}
	}
	b.WriteByte('}')
	return b.String()
}

// fitObject writes a JSON object with its head first and cuts its longest
// list at a whole entry when it does not fit. ok is false when nothing
// sensible fits (the caller cuts the text then).
func fitObject(fields map[string]json.RawMessage, limit int, args json.RawMessage) (string, bool) {
	keys := orderedKeys(fields)
	out := writeObject(fields, keys)
	if utf8.RuneCountInString(out) <= limit {
		return out, true
	}
	// The longest list is cut; the other fields stay.
	list := ""
	for _, k := range keys {
		v := bytes.TrimSpace(fields[k])
		if bytes.HasPrefix(v, []byte("[")) && (list == "" || len(v) > len(fields[list])) {
			list = k
		}
	}
	if list == "" {
		return "", false
	}
	var items []json.RawMessage
	if json.Unmarshal(fields[list], &items) != nil {
		return "", false
	}
	start := argInt(args, "offset")
	rest := map[string]json.RawMessage{}
	for k, v := range fields {
		if k != list {
			rest[k] = v
		}
	}
	build := func(n int) string {
		note := fmt.Sprintf("%s: %d of %d shown here (cut for length)", list, n, len(items))
		if n < len(items) {
			note += fmt.Sprintf("; call again with offset=%d for the next ones, or narrow it with a filter", start+n)
		}
		f := map[string]json.RawMessage{}
		for k, v := range rest {
			f[k] = v
		}
		f[list], _ = json.Marshal(items[:n])
		f[cutKey], _ = json.Marshal(note)
		ks := orderedKeys(rest)
		// The note stands right after the head, before any list.
		at := 0
		for at < len(ks) && slices.Contains(headKeys, ks[at]) {
			at++
		}
		ks = slices.Insert(ks, at, cutKey)
		return writeObject(f, append(ks, list))
	}
	// Largest n that fits.
	lo, hi := 0, len(items)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if utf8.RuneCountInString(build(mid)) <= limit {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	out = build(lo)
	if utf8.RuneCountInString(out) > limit {
		// Even the fields around the list are too long.
		return "", false
	}
	return out, true
}

// argInt is a number argument of a call, 0 when it is missing.
func argInt(args json.RawMessage, key string) int {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return 0
	}
	var n int
	_ = json.Unmarshal(fields[key], &n)
	return max(0, n)
}
