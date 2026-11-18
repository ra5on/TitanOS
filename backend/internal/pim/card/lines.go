package card

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"
)

// Editing on the content lines (TECHNIK 7.2): a card from a device holds
// what TitanOS does not show (X-APPLE-*, IMPP, groups of the form
// item1.X-ABLabel, parameters of its own). go-vcard's encoder sorts every
// property and parameter, never folds and doubles the backslash of an
// escaped ";" – so a changed card is not written through it. Doc keeps
// each line as it came and writes only the lines that were changed or
// added, unfolded at 75 octets.

// Param is one parameter of a line: the name in upper case, the values
// without quotes (TYPE=WORK,VOICE are two values).
type Param struct {
	Name   string
	Values []string
}

// Line is one content line.
type Line struct {
	// raw is the line as it came (folded lines joined by CRLF, no line
	// end); empty for a new or changed line.
	raw    string
	Group  string
	Name   string // upper case
	Params []Param
	// Value is the value as written (escaped: UnescapeText, Structured).
	Value string
}

// Doc is a card as lines, BEGIN and END included.
type Doc struct {
	Lines []*Line
}

// ErrLines: the bytes are no card of content lines.
var ErrLines = errors.New("not a vcard")

// ParseDoc reads the lines of one card (no checks beyond the form: Parse
// checks the card).
func ParseDoc(data []byte) (*Doc, error) {
	if len(data) > MaxSize {
		return nil, ErrTooLarge
	}
	// Builders: a picture folds into thousands of lines, joining them
	// with += would copy the line again for each of them.
	var logical, raw []*strings.Builder
	for _, phys := range strings.Split(string(data), "\n") {
		phys = strings.TrimSuffix(phys, "\r")
		if (strings.HasPrefix(phys, " ") || strings.HasPrefix(phys, "\t")) && len(logical) > 0 {
			logical[len(logical)-1].WriteString(phys[1:])
			raw[len(raw)-1].WriteString("\r\n" + phys)
			continue
		}
		if strings.TrimSpace(phys) == "" {
			continue
		}
		l, r := &strings.Builder{}, &strings.Builder{}
		l.WriteString(phys)
		r.WriteString(phys)
		logical = append(logical, l)
		raw = append(raw, r)
	}
	d := &Doc{}
	for i, s := range logical {
		l, ok := parseLine(s.String())
		if !ok {
			return nil, ErrLines
		}
		l.raw = raw[i].String()
		d.Lines = append(d.Lines, l)
	}
	if len(d.Lines) < 2 || d.Lines[0].Name != "BEGIN" || d.Lines[len(d.Lines)-1].Name != "END" {
		return nil, ErrLines
	}
	return d, nil
}

func parseLine(s string) (*Line, bool) {
	colon := -1
	quoted := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case ':':
			if !quoted {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon <= 0 {
		return nil, false
	}
	head := splitOutside(s[:colon], ';')
	l := &Line{Value: s[colon+1:]}
	name := head[0]
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		l.Group, name = name[:dot], name[dot+1:]
	}
	if name == "" {
		return nil, false
	}
	l.Name = strings.ToUpper(name)
	for _, p := range head[1:] {
		k, v, ok := strings.Cut(p, "=")
		if strings.TrimSpace(k) == "" {
			return nil, false
		}
		if !ok { // vCard 2.1 form: a bare type
			l.Params = append(l.Params, Param{Name: "TYPE", Values: []string{p}})
			continue
		}
		var values []string
		for _, x := range splitOutside(v, ',') {
			values = append(values, strings.Trim(x, `"`))
		}
		l.Params = append(l.Params, Param{Name: strings.ToUpper(k), Values: values})
	}
	return l, true
}

// splitOutside splits at sep outside double quotes.
func splitOutside(s string, sep byte) []string {
	var out []string
	quoted := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			quoted = !quoted
		case s[i] == sep && !quoted:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// Bytes writes the card: lines as they came, changed ones new (CRLF).
func (d *Doc) Bytes() []byte {
	var b bytes.Buffer
	for _, l := range d.Lines {
		if l.raw != "" {
			b.WriteString(l.raw)
		} else {
			b.WriteString(fold(l.encode()))
		}
		b.WriteString("\r\n")
	}
	return b.Bytes()
}

func (l *Line) encode() string {
	var b strings.Builder
	if l.Group != "" {
		b.WriteString(l.Group + ".")
	}
	b.WriteString(l.Name)
	for _, p := range l.Params {
		b.WriteString(";" + p.Name)
		if len(p.Values) == 0 {
			continue
		}
		b.WriteString("=")
		for i, v := range p.Values {
			if i > 0 {
				b.WriteString(",")
			}
			v = strings.ReplaceAll(v, `"`, "") // never inside a value (RFC 6350 3.3)
			if strings.ContainsAny(v, ":;,") {
				v = `"` + v + `"`
			}
			b.WriteString(v)
		}
	}
	b.WriteString(":" + l.Value)
	return b.String()
}

// fold cuts a line into lines of at most 75 octets (RFC 6350 3.2), never
// inside a UTF-8 sequence.
func fold(s string) string {
	if len(s) <= 75 {
		return s
	}
	var b strings.Builder
	limit := 75
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		b.WriteString(s[:cut])
		b.WriteString("\r\n ")
		s = s[cut:]
		limit = 74 // the space counts
	}
	b.WriteString(s)
	return b.String()
}

// Touch marks a line as changed: it is written anew from its fields.
func (l *Line) Touch() { l.raw = "" }

// Param returns the values of all parameters of the name (upper case).
func (l *Line) Param(name string) []string {
	var out []string
	for _, p := range l.Params {
		if p.Name == name {
			out = append(out, p.Values...)
		}
	}
	return out
}

// HasParam reports whether a parameter of the name is there.
func (l *Line) HasParam(name string) bool {
	for _, p := range l.Params {
		if p.Name == name {
			return true
		}
	}
	return false
}

// DropParam removes the parameters of the name.
func (l *Line) DropParam(name string) {
	var keep []Param
	for _, p := range l.Params {
		if p.Name != name {
			keep = append(keep, p)
		}
	}
	l.Params = keep
	l.Touch()
}

// Version is the VERSION of the card ("3.0", "4.0").
func (d *Doc) Version() string {
	for _, l := range d.Lines {
		if l.Name == "VERSION" {
			return strings.TrimSpace(l.Value)
		}
	}
	return ""
}

// Find returns the lines of a property (upper case), in order.
func (d *Doc) Find(name string) []*Line {
	var out []*Line
	for _, l := range d.Lines {
		if l.Name == name {
			out = append(out, l)
		}
	}
	return out
}

// First is the first line of a property, nil without.
func (d *Doc) First(name string) *Line {
	for _, l := range d.Lines {
		if l.Name == name {
			return l
		}
	}
	return nil
}

// Remove takes lines out. A group (item1.) left with only Apple's
// companions (X-AB*: label, address format) loses them too.
func (d *Doc) Remove(lines ...*Line) {
	gone := map[*Line]bool{}
	groups := map[string]bool{}
	for _, l := range lines {
		gone[l] = true
		if l.Group != "" {
			groups[strings.ToUpper(l.Group)] = true
		}
	}
	for g := range groups {
		used := false
		for _, l := range d.Lines {
			if !gone[l] && strings.EqualFold(l.Group, g) && !strings.HasPrefix(l.Name, "X-AB") {
				used = true
			}
		}
		if !used {
			for _, l := range d.Lines {
				if strings.EqualFold(l.Group, g) {
					gone[l] = true
				}
			}
		}
	}
	keep := d.Lines[:0]
	for _, l := range d.Lines {
		if !gone[l] {
			keep = append(keep, l)
		}
	}
	d.Lines = keep
}

// Companion is the line of a property in the same group (item1.X-ABLabel
// of item1.TEL), nil without a group or line.
func (d *Doc) Companion(l *Line, name string) *Line {
	if l.Group == "" {
		return nil
	}
	for _, o := range d.Lines {
		if o != l && o.Name == name && strings.EqualFold(o.Group, l.Group) {
			return o
		}
	}
	return nil
}

// Add puts a new line behind the last line of the same property, else
// before END.
func (d *Doc) Add(l *Line) {
	l.raw = ""
	at := len(d.Lines) - 1
	for i, o := range d.Lines {
		if o.Name == l.Name && i < len(d.Lines)-1 {
			at = i + 1
		}
	}
	d.Lines = append(d.Lines[:at], append([]*Line{l}, d.Lines[at:]...)...)
}

// Set gives the first line of a property a new value (params kept) and
// removes the other lines of it; without one it adds a line.
func (d *Doc) Set(name, value string, params ...Param) *Line {
	lines := d.Find(name)
	if len(lines) == 0 {
		l := &Line{Name: name, Params: params, Value: value}
		d.Add(l)
		return l
	}
	lines[0].Value = value
	lines[0].Touch()
	d.Remove(lines[1:]...)
	return lines[0]
}

var (
	textEscaper   = strings.NewReplacer(`\`, `\\`, "\r\n", `\n`, "\n", `\n`, ",", `\,`, ";", `\;`)
	textUnescaper = strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\:`, ":")
)

// EscapeText writes a text value (RFC 6350 3.4).
func EscapeText(s string) string { return textEscaper.Replace(s) }

// UnescapeText reads a text value.
func UnescapeText(s string) string { return textUnescaper.Replace(s) }

// Structured splits a value at unescaped ";" and reads each component.
func Structured(v string) []string {
	var out []string
	start := 0
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\':
			i++
		case ';':
			out = append(out, UnescapeText(v[start:i]))
			start = i + 1
		}
	}
	return append(out, UnescapeText(v[start:]))
}

// JoinStructured writes components of a structured value.
func JoinStructured(parts []string) string {
	esc := make([]string, len(parts))
	for i, p := range parts {
		esc[i] = EscapeText(p)
	}
	return strings.Join(esc, ";")
}

// PhotoData returns the picture of the card when it is inline (base64 in
// vCard 3, a data: URI in vCard 4). A picture by reference (VALUE=uri,
// http:) is never fetched: no data.
func (d *Doc) PhotoData() ([]byte, bool) {
	l := d.First("PHOTO")
	if l == nil {
		return nil, false
	}
	v := strings.TrimSpace(l.Value)
	if rest, ok := cutFold(v, "data:"); ok {
		_, payload, ok := strings.Cut(rest, ",")
		if !ok || !strings.Contains(strings.ToLower(rest[:len(rest)-len(payload)]), ";base64") {
			return nil, false
		}
		v = payload
	} else {
		enc := strings.ToLower(strings.Join(l.Param("ENCODING"), ","))
		if enc != "b" && enc != "base64" {
			return nil, false
		}
	}
	data, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, v))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

func cutFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// SetPhoto replaces every PHOTO by a JPEG: PHOTO;ENCODING=b;TYPE=JPEG in
// vCard 3, a data: URI in vCard 4.
func (d *Doc) SetPhoto(jpeg []byte) {
	d.Remove(d.Find("PHOTO")...)
	b64 := base64.StdEncoding.EncodeToString(jpeg)
	if d.Version() == "4.0" {
		d.Add(&Line{Name: "PHOTO", Value: "data:image/jpeg;base64," + b64})
		return
	}
	d.Add(&Line{Name: "PHOTO", Params: []Param{{Name: "ENCODING", Values: []string{"b"}}, {Name: "TYPE", Values: []string{"JPEG"}}}, Value: b64})
}

// ShrinkPhoto rewrites a card whose inline picture is larger than limit
// with the picture shrink makes of it (TECHNIK 5.5, a PUT of a device);
// false when there is nothing to do or the picture cannot be read (the
// card stays as it came).
func ShrinkPhoto(data []byte, limit int, shrink func([]byte) ([]byte, error)) ([]byte, bool) {
	if len(data) <= limit {
		return nil, false // the picture is smaller than the card
	}
	d, err := ParseDoc(data)
	if err != nil {
		return nil, false
	}
	pic, ok := d.PhotoData()
	if !ok || len(pic) <= limit {
		return nil, false
	}
	small, err := shrink(pic)
	if err != nil {
		return nil, false
	}
	d.SetPhoto(small)
	return d.Bytes(), true
}
