package card

import "strings"

// Copy returns a copy of the line in another group (item3. for item1.;
// "" for none) that writes the bytes it came with where only the group
// changed (1.13: merging two contacts keeps the other card's lines as
// they were).
func (l *Line) Copy(group string) *Line {
	c := *l
	c.Params = append([]Param(nil), l.Params...)
	if c.raw != "" && group != l.Group {
		if l.Group == "" || !strings.HasPrefix(c.raw, l.Group+".") {
			c.raw = ""
		} else if group == "" {
			c.raw = c.raw[len(l.Group)+1:]
		} else {
			c.raw = group + c.raw[len(l.Group):]
		}
	}
	c.Group = group
	return &c
}

// Insert puts lines before END in the order given, as they are (a copied
// line keeps its bytes; Add would write it anew behind its namesakes).
func (d *Doc) Insert(lines ...*Line) {
	at := len(d.Lines) - 1
	d.Lines = append(d.Lines[:at], append(append([]*Line(nil), lines...), d.Lines[at:]...)...)
}
