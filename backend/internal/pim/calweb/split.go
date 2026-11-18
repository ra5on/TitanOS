package calweb

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/emersion/go-ical"
)

// ErrNotCalendar: the upload is no iCalendar file (400).
var ErrNotCalendar = errors.New("calweb: not an iCalendar file")

// Bounds of a file split for an import.
const (
	// MaxSplitComponents bounds the components of one upload.
	MaxSplitComponents = 100000
	maxDepth           = 8
)

// Group is the events of one UID in an upload, with the VTIMEZONEs they
// name: what goes into one file of the calendar.
type Group struct {
	UID string
	// Events are the VEVENT blocks (master and overrides) as they came,
	// lines ending in CRLF.
	Events []string
	// TZIDs the events name, in order of first use.
	TZIDs []string
}

// Failure is a component that cannot become an entry: its UID (if any)
// and a reason code ("unsupported_component").
type Failure struct {
	UID    string `json:"uid"`
	Reason string `json:"reason"`
}

// Split is an .ics file cut into the events of each UID.
type Split struct {
	// ProdID and CalScale of the (first) VCALENDAR, as written.
	ProdID, CalScale string
	Groups           []*Group
	// Zones are the VTIMEZONE blocks by TZID (the first of each).
	Zones   map[string]string
	Failed  []Failure
	missing int
}

// line is one content line: the unfolded text and the lines as written.
type line struct {
	text string
	raw  []string
}

func lines(s string) []line {
	s = strings.TrimPrefix(s, "\uFEFF")
	var out []line
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		var l string
		if i < 0 {
			l, s = s, ""
		} else {
			l, s = s[:i], s[i+1:]
		}
		l = strings.TrimSuffix(l, "\r")
		if l == "" {
			continue
		}
		if (l[0] == ' ' || l[0] == '\t') && len(out) > 0 {
			last := &out[len(out)-1]
			last.text += l[1:]
			last.raw = append(last.raw, l)
			continue
		}
		out = append(out, line{text: l, raw: []string{l}})
	}
	return out
}

// propName splits a content line into upper-case name, parameters and
// value; quoted parameter values may hold ';' and ':'.
func propName(text string) (name string, params map[string]string, value string) {
	quoted := false
	cut := -1
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '"':
			quoted = !quoted
		case c == ':' && !quoted:
			cut = i
		}
		if cut >= 0 {
			break
		}
	}
	head := text
	if cut >= 0 {
		head, value = text[:cut], text[cut+1:]
	}
	parts := splitUnquoted(head, ';')
	name = strings.ToUpper(strings.TrimSpace(parts[0]))
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		if params == nil {
			params = map[string]string{}
		}
		params[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(v, `"`)
	}
	return name, params, value
}

func splitUnquoted(s string, sep byte) []string {
	var out []string
	quoted, start := false, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case sep:
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func block(ls []line) string {
	var b strings.Builder
	for _, l := range ls {
		for _, r := range l.raw {
			b.WriteString(r)
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

// SplitICS cuts an .ics file (one or more VCALENDARs) into the events of
// each UID, without decoding it as a whole: the lines of each VEVENT stay
// as they came. VTIMEZONEs are kept by TZID; METHOD and the calendar's own
// X- properties go. A VEVENT without UID gets one (newUID); other
// components (VTODO …) are failures. Each group is checked later as the
// file it becomes (pim/store).
func SplitICS(data []byte, newUID func() string) (*Split, error) {
	all := lines(string(data))
	sp := &Split{Zones: map[string]string{}}
	byUID := map[string]*Group{}
	depth, comps, calendars := 0, 0, 0
	for i := 0; i < len(all); i++ {
		name, _, value := propName(all[i].text)
		switch {
		case depth == 0 && name == "BEGIN" && strings.EqualFold(value, "VCALENDAR"):
			depth, calendars = 1, calendars+1
		case depth == 0:
			return nil, ErrNotCalendar
		case depth == 1 && name == "END" && strings.EqualFold(value, "VCALENDAR"):
			depth = 0
		case depth == 1 && name == "BEGIN":
			comp := strings.ToUpper(value)
			end, err := blockEnd(all, i)
			if err != nil {
				return nil, err
			}
			comps++
			if comps > MaxSplitComponents {
				return nil, ErrNotCalendar
			}
			body := all[i : end+1]
			i = end
			switch comp {
			case ical.CompTimezone:
				if id := topValue(body, ical.PropTimezoneID); id != "" {
					if _, ok := sp.Zones[id]; !ok {
						sp.Zones[id] = block(body)
					}
				}
			case ical.CompEvent:
				uid := topValue(body, ical.PropUID)
				if strings.TrimSpace(uid) == "" {
					uid = newUID()
					body = append([]line{body[0], {text: "UID:" + uid, raw: []string{"UID:" + uid}}}, body[1:]...)
					sp.missing++
				}
				g := byUID[uid]
				if g == nil {
					g = &Group{UID: uid}
					byUID[uid] = g
					sp.Groups = append(sp.Groups, g)
				}
				g.Events = append(g.Events, block(body))
				for _, l := range body {
					if _, params, _ := propName(l.text); params["TZID"] != "" && !slices.Contains(g.TZIDs, params["TZID"]) {
						g.TZIDs = append(g.TZIDs, params["TZID"])
					}
				}
			default:
				sp.Failed = append(sp.Failed, Failure{UID: topValue(body, ical.PropUID), Reason: "unsupported_component"})
			}
		case depth == 1 && calendars == 1 && name == ical.PropProductID && sp.ProdID == "":
			sp.ProdID = value
		case depth == 1 && calendars == 1 && name == ical.PropCalendarScale && sp.CalScale == "":
			sp.CalScale = value
		case depth == 1 && name == "END":
			return nil, ErrNotCalendar
		}
	}
	if depth != 0 || calendars == 0 {
		return nil, ErrNotCalendar
	}
	return sp, nil
}

// blockEnd finds the END that closes the BEGIN at all[i].
func blockEnd(all []line, i int) (int, error) {
	depth := 0
	for j := i; j < len(all); j++ {
		name, _, value := propName(all[j].text)
		switch name {
		case "BEGIN":
			depth++
			if depth > maxDepth {
				return 0, ErrNotCalendar
			}
		case "END":
			depth--
			if depth == 0 {
				_, _, open := propName(all[i].text)
				if !strings.EqualFold(value, open) {
					return 0, ErrNotCalendar
				}
				return j, nil
			}
		}
	}
	return 0, ErrNotCalendar
}

// topValue is the value of a property of the component itself (not of a
// VALARM inside it).
func topValue(body []line, prop string) string {
	depth := 0
	for _, l := range body {
		name, _, value := propName(l.text)
		switch {
		case name == "BEGIN":
			depth++
		case name == "END":
			depth--
		case depth == 1 && name == prop:
			return value
		}
	}
	return ""
}

// File is the .ics file of one group: VCALENDAR with the PRODID of the
// upload (or TitanOS's), the VTIMEZONEs it names, the events.
func (sp *Split) File(g *Group) []byte {
	var b bytes.Buffer
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")
	prod := sp.ProdID
	if prod == "" {
		prod = ProdID
	}
	b.WriteString("PRODID:" + prod + "\r\n")
	if sp.CalScale != "" {
		b.WriteString("CALSCALE:" + sp.CalScale + "\r\n")
	}
	for _, id := range g.TZIDs {
		if z, ok := sp.Zones[id]; ok {
			b.WriteString(z)
		}
	}
	for _, e := range g.Events {
		b.WriteString(e)
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.Bytes()
}

// Export writes one VCALENDAR of many entries in two passes over the
// files, so only the zones stay in RAM: Zones for each file, Begin (head
// and each VTIMEZONE once, the first of each TZID), Events for each file
// (every event as it is stored), End. A file that cannot be cut is
// skipped.
type Export struct {
	w     io.Writer
	zones map[string]string
	order []string
	err   error
}

// NewExport starts an export to w.
func NewExport(w io.Writer) *Export {
	return &Export{w: w, zones: map[string]string{}}
}

// Zones notes the VTIMEZONEs of a file (first pass).
func (e *Export) Zones(data []byte) {
	sp, err := SplitICS(data, NewUID)
	if err != nil {
		return
	}
	for _, g := range sp.Groups {
		for _, id := range g.TZIDs {
			if z, ok := sp.Zones[id]; ok {
				if _, seen := e.zones[id]; !seen {
					e.zones[id] = z
					e.order = append(e.order, id)
				}
			}
		}
	}
}

func (e *Export) write(s string) {
	if e.err == nil {
		_, e.err = io.WriteString(e.w, s)
	}
}

// Begin writes the head and the zones.
func (e *Export) Begin(name string) error {
	e.write("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:" + ProdID + "\r\nCALSCALE:GREGORIAN\r\n")
	p := ical.NewProp("X-WR-CALNAME")
	p.SetText(name)
	e.write(fold("X-WR-CALNAME:" + p.Value))
	for _, id := range e.order {
		e.write(e.zones[id])
	}
	return e.err
}

// Events writes the events of a file (second pass).
func (e *Export) Events(data []byte) error {
	sp, err := SplitICS(data, NewUID)
	if err != nil {
		return e.err
	}
	for _, g := range sp.Groups {
		for _, ev := range g.Events {
			e.write(ev)
		}
	}
	return e.err
}

// End closes the calendar.
func (e *Export) End() error {
	e.write("END:VCALENDAR\r\n")
	return e.err
}

// fold writes a content line folded at 75 octets (RFC 5545 3.1), on
// whole runes.
func fold(s string) string {
	var b strings.Builder
	for len(s) > 75 {
		cut := 75
		for cut > 0 && s[cut]&0xc0 == 0x80 {
			cut--
		}
		b.WriteString(s[:cut] + "\r\n ")
		s = s[cut:]
	}
	b.WriteString(s + "\r\n")
	return b.String()
}
