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

// CutMark stands wherever the fitter shortened a value of a JSON result
// (a string, the rest of a list or of an object). Tools that save what the
// model gives back (ha_automation_save) refuse anything holding it: a cut
// config saved back would lose its end (security review 1.10, M5).
const CutMark = "[cut for length]"

// fitResult returns text in at most limit characters. args are the
// arguments of the call (for the offset of the next page), may be nil;
// paged says the tool takes an offset for its next page.
//
// JSON stays JSON (security review 1.10, M5): a result without a list, or
// one whose other fields are too long, is shortened value by value into
// a valid object with "truncated", and its counts and hints ("hidden",
// "why", "note" …) come first and stay whole.
func fitResult(text string, limit int, args json.RawMessage, paged bool) string {
	if limit <= 0 {
		return text
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		var fields map[string]json.RawMessage
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if dec.Decode(&fields) == nil && !dec.More() {
			_, hasNext := fields["nextOffset"]
			if out, ok := fitObject(fields, limit, args, paged || hasNext || hasArg(args, "offset")); ok {
				return out
			}
			return shrinkObject(fields, limit)
		}
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	if strings.HasPrefix(trimmed, "[") {
		var v any
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if dec.Decode(&v) == nil && !dec.More() {
			if out, ok := shrinkValue(v, limit); ok {
				return out
			}
		}
	}
	return cutText(text, limit)
}

// encode writes v as compact JSON without escaping <, > and &.
func encode(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return "null"
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// tailRoom is what the note at the end of a shortened list or object needs.
const tailRoom = 48

// shrinkValue writes v as JSON in at most limit characters, shortening
// strings and leaving out the end of lists and objects, each marked with
// CutMark. ok is false when not even a stand-in fits.
func shrinkValue(v any, limit int) (string, bool) {
	whole := encode(v)
	if runes(whole) <= limit {
		return whole, true
	}
	switch x := v.(type) {
	case string:
		r := []rune(x)
		lo, hi := 0, len(r)
		fits := func(n int) bool { return runes(encode(string(r[:n])+" "+CutMark)) <= limit }
		if !fits(0) {
			return "", false
		}
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if fits(mid) {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		return encode(string(r[:lo]) + " " + CutMark), true
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		used, n := 2, 0
		for _, item := range x {
			room := limit - used - tailRoom - 1
			enc := encode(item)
			if runes(enc) > room {
				var ok bool
				if enc, ok = shrinkValue(item, room); !ok {
					break
				}
			}
			if n > 0 {
				b.WriteByte(',')
				used++
			}
			b.WriteString(enc)
			used += runes(enc)
			n++
			if strings.Contains(enc, CutMark) {
				break
			}
		}
		if n < len(x) {
			if n > 0 {
				b.WriteByte(',')
			}
			b.WriteString(encode(fmt.Sprintf("%d more %s", len(x)-n, CutMark)))
		}
		b.WriteByte(']')
		out := b.String()
		return out, runes(out) <= limit
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		fields := make(map[string]json.RawMessage, len(x))
		for _, k := range keys {
			fields[k] = json.RawMessage(encode(x[k]))
		}
		out := writeFields(fields, keys, limit)
		return out, runes(out) <= limit
	}
	return "", false
}

// writeFields writes the fields in this order into at most limit
// characters: what fits whole, the first that does not shortened, the
// rest left out and counted under "…".
func writeFields(fields map[string]json.RawMessage, keys []string, limit int) string {
	var b strings.Builder
	b.WriteByte('{')
	used, n, left := 2, 0, 0
	for _, k := range keys {
		name := encode(k) + ":"
		room := limit - used - tailRoom - runes(name) - 1
		enc := compactRaw(fields[k])
		if runes(enc) > room {
			var v any
			dec := json.NewDecoder(bytes.NewReader(fields[k]))
			dec.UseNumber()
			ok := false
			if dec.Decode(&v) == nil {
				enc, ok = shrinkValue(v, room)
			}
			if !ok {
				left++
				continue
			}
		}
		if n > 0 {
			b.WriteByte(',')
			used++
		}
		b.WriteString(name)
		b.WriteString(enc)
		used += runes(name) + runes(enc)
		n++
	}
	if left > 0 {
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(encode("…") + ":" + encode(fmt.Sprintf("%d more fields %s", left, CutMark)))
	}
	b.WriteByte('}')
	return b.String()
}

func compactRaw(raw json.RawMessage) string {
	var c bytes.Buffer
	if json.Compact(&c, raw) == nil {
		return c.String()
	}
	return string(raw)
}

// shrinkObject is a result object that does not fit even with its longest
// list cut: its head first and whole where it can be, then "truncated"
// and a note, then the other fields as far as they go.
func shrinkObject(fields map[string]json.RawMessage, limit int) string {
	keys := orderedKeys(fields)
	all := make(map[string]json.RawMessage, len(fields)+2)
	for k, v := range fields {
		all[k] = v
	}
	all[cutKey] = json.RawMessage(encode("This result was too long: values marked " + CutMark + " are shortened or left out. Ask for less (a filter, one item) to see them whole; never save a shortened value back."))
	extra := []string{cutKey}
	if _, ok := fields["truncated"]; !ok {
		all["truncated"] = json.RawMessage("true")
		extra = []string{"truncated", cutKey}
	}
	at := 0
	for at < len(keys) && slices.Contains(headKeys, keys[at]) {
		at++
	}
	keys = slices.Insert(keys, at, extra...)
	return writeFields(all, keys, limit)
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
func fitObject(fields map[string]json.RawMessage, limit int, args json.RawMessage, paged bool) (string, bool) {
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
		switch {
		case n < len(items) && paged:
			note += fmt.Sprintf("; call again with offset=%d for the next ones, or narrow it with a filter", start+n)
		case n < len(items):
			// No paging in this tool (security review 1.10, N7).
			note += "; narrow it with a filter or a shorter time window to see the others"
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

// hasArg reports whether the call gave this argument.
func hasArg(args json.RawMessage, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return false
	}
	_, ok := fields[key]
	return ok
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
