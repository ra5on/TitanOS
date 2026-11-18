package calweb

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
)

// ErrNotCalendar: the upload is no iCalendar file (400).
var ErrNotCalendar = errors.New("calweb: not an iCalendar file")

// ErrZoneTooLarge: a group names a VTIMEZONE over recur.MaxZoneBytes; its
// file is not built (Split.Size, S-19).
var ErrZoneTooLarge = errors.New("calweb: VTIMEZONE too large")

// Bounds of a file split for an import.
const (
	// MaxSplitComponents bounds the components of one upload.
	MaxSplitComponents = 100000
	maxDepth           = 8
	// maxHeadValue bounds PRODID and CALSCALE, copied into every file of
	// an import: a longer one is not taken (TitanOS's PRODID instead).
	maxHeadValue = 255
	// MaxExportZones bounds the bytes of the VTIMEZONEs an export holds
	// (each at most recur.MaxZoneBytes); zones over it are left out, the
	// events keep their TZID (S-21).
	MaxExportZones = 2 << 20
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
	Zones  map[string]string
	Failed []Failure
	// bigZones are the TZIDs whose first VTIMEZONE is over
	// recur.MaxZoneBytes: not kept, the groups naming them fail.
	bigZones map[string]bool
	missing  int
}

// contentLine is one content line of the upload: its bytes with the
// folded lines after it (raw, offsets into the upload), and the unfolded
// text (a copy only when it was folded).
type contentLine struct {
	start, end int // raw bytes, the line break after the last part included
	text       []byte
}

// scanner walks the content lines of an upload without a table of them,
// so an import holds the upload and the blocks cut from it, nothing more.
type scanner struct {
	data []byte
	pos  int
}

func (sc *scanner) physical() (line []byte, next int, ok bool) {
	if sc.pos >= len(sc.data) {
		return nil, sc.pos, false
	}
	i := bytes.IndexByte(sc.data[sc.pos:], '\n')
	end := len(sc.data)
	if i >= 0 {
		end = sc.pos + i + 1
	}
	return bytes.TrimRight(sc.data[sc.pos:end], "\r\n"), end, true
}

// next returns the next content line; empty lines are skipped.
func (sc *scanner) next() (contentLine, bool) {
	for {
		start := sc.pos
		first, end, ok := sc.physical()
		if !ok {
			return contentLine{}, false
		}
		sc.pos = end
		if len(first) == 0 {
			continue
		}
		cl := contentLine{start: start, end: end, text: first}
		folded := false
		for sc.pos < len(sc.data) && (sc.data[sc.pos] == ' ' || sc.data[sc.pos] == '\t') {
			more, end, _ := sc.physical()
			if !folded {
				cl.text = append([]byte(nil), cl.text...)
				folded = true
			}
			cl.text = append(cl.text, more[1:]...)
			sc.pos, cl.end = end, end
		}
		return cl, true
	}
}

// parts splits an unfolded line into name, parameters and value at the
// first ':' outside quotes.
func parts(text []byte) (name, params, value []byte) {
	quoted := false
	cut := len(text)
	for i, c := range text {
		if c == '"' {
			quoted = !quoted
		} else if c == ':' && !quoted {
			cut = i
			break
		}
	}
	head := text[:cut]
	if cut < len(text) {
		value = text[cut+1:]
	}
	if i := bytes.IndexByte(head, ';'); i >= 0 {
		return head[:i], head[i:], value
	}
	return head, nil, value
}

// param finds a parameter (";TZID=…", any case) and returns its value
// without quotes.
func param(params []byte, name string) string {
	key := ";" + name + "="
	for i := 0; i+len(key) <= len(params); i++ {
		if !bytes.EqualFold(params[i:i+len(key)], []byte(key)) {
			continue
		}
		v := params[i+len(key):]
		if len(v) > 0 && v[0] == '"' {
			if j := bytes.IndexByte(v[1:], '"'); j >= 0 {
				return string(v[1 : j+1])
			}
			return string(v[1:])
		}
		if j := bytes.IndexByte(v, ';'); j >= 0 {
			v = v[:j]
		}
		return string(v)
	}
	return ""
}

func is(b []byte, s string) bool { return bytes.EqualFold(bytes.TrimSpace(b), []byte(s)) }

// crlfBlock is the text of a block with CRLF line ends and without empty
// lines; one copy of the bytes.
func crlfBlock(b []byte) string {
	if !bytes.Contains(b, []byte("\n\n")) && !bytes.Contains(b, []byte("\r\n\r\n")) &&
		bytes.Count(b, []byte("\n")) == bytes.Count(b, []byte("\r\n")) && bytes.HasSuffix(b, []byte("\r\n")) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b) + len(b)/40)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		line := b
		if i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}
		out.Write(line)
		out.WriteString("\r\n")
	}
	return out.String()
}

// block is one component at the top of a VCALENDAR while it is read.
type block struct {
	comp       string
	start      int
	depth      int
	uid, tzid  string
	tzids      []string
	hasUIDLine bool
}

// SplitICS cuts an .ics file (one or more VCALENDARs) into the events of
// each UID, without decoding it as a whole: the lines of each VEVENT stay
// as they came (line ends made CRLF). VTIMEZONEs are kept by TZID; METHOD
// and the calendar's own X- properties go. A VEVENT without UID gets one
// (newUID); other components (VTODO …) are failures. Each group is checked
// later as the file it becomes (pim/store).
func SplitICS(data []byte, newUID func() string) (*Split, error) {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		data = data[3:]
	}
	sc := &scanner{data: data}
	sp := &Split{Zones: map[string]string{}}
	byUID := map[string]*Group{}
	depth, comps, calendars := 0, 0, 0
	var cur *block
	for {
		cl, ok := sc.next()
		if !ok {
			break
		}
		name, params, value := parts(cl.text)
		begin, end := is(name, "BEGIN"), is(name, "END")
		switch {
		case depth == 0 && begin && is(value, "VCALENDAR"):
			depth, calendars = 1, calendars+1
		case depth == 0:
			return nil, ErrNotCalendar
		case depth == 1 && end:
			if !is(value, "VCALENDAR") {
				return nil, ErrNotCalendar
			}
			depth = 0
		case depth == 1 && begin:
			comps++
			if comps > MaxSplitComponents {
				return nil, ErrNotCalendar
			}
			cur = &block{comp: strings.ToUpper(string(bytes.TrimSpace(value))), start: cl.start, depth: 1}
			depth = 2
		case depth == 1:
			if calendars == 1 && is(name, ical.PropProductID) && sp.ProdID == "" && len(value) <= maxHeadValue {
				sp.ProdID = string(value)
			}
			if calendars == 1 && is(name, ical.PropCalendarScale) && sp.CalScale == "" && len(value) <= maxHeadValue {
				sp.CalScale = string(value)
			}
		case begin:
			depth++
			cur.depth++
			if cur.depth > maxDepth {
				return nil, ErrNotCalendar
			}
		case end:
			depth--
			cur.depth--
			if cur.depth > 0 {
				continue
			}
			if !is(value, cur.comp) {
				return nil, ErrNotCalendar
			}
			if cur.comp == ical.CompTimezone && cl.end-cur.start > recur.MaxZoneBytes {
				sp.bigZone(cur.tzid) // not copied
			} else {
				sp.finish(cur, crlfBlock(data[cur.start:cl.end]), byUID, newUID)
			}
			cur = nil
		default:
			if cur.depth == 1 && is(name, ical.PropUID) && cur.uid == "" {
				cur.uid, cur.hasUIDLine = string(value), true
			}
			if cur.depth == 1 && cur.comp == ical.CompTimezone && is(name, ical.PropTimezoneID) {
				cur.tzid = string(value)
			}
			if len(params) > 0 {
				if id := param(params, "TZID"); id != "" && !slices.Contains(cur.tzids, id) {
					cur.tzids = append(cur.tzids, id)
				}
			}
		}
	}
	if depth != 0 || calendars == 0 {
		return nil, ErrNotCalendar
	}
	return sp, nil
}

// bigZone notes a VTIMEZONE over recur.MaxZoneBytes, unless its TZID
// has a zone already (the first one counts).
func (sp *Split) bigZone(tzid string) {
	if _, ok := sp.Zones[tzid]; tzid == "" || ok {
		return
	}
	if sp.bigZones == nil {
		sp.bigZones = map[string]bool{}
	}
	sp.bigZones[tzid] = true
}

// finish files a block that has ended.
func (sp *Split) finish(b *block, text string, byUID map[string]*Group, newUID func() string) {
	switch b.comp {
	case ical.CompTimezone:
		if len(text) > recur.MaxZoneBytes { // grown by CRLF line ends
			sp.bigZone(b.tzid)
			return
		}
		if _, ok := sp.Zones[b.tzid]; b.tzid != "" && !ok && !sp.bigZones[b.tzid] {
			sp.Zones[b.tzid] = text
		}
	case ical.CompEvent:
		uid := b.uid
		if strings.TrimSpace(uid) == "" {
			uid = newUID()
			first, rest, _ := strings.Cut(text, "\r\n")
			if b.hasUIDLine {
				// An empty UID line: the new one goes first and wins.
				rest = strings.Replace(rest, "UID:\r\n", "", 1)
			}
			text = first + "\r\nUID:" + uid + "\r\n" + rest
			sp.missing++
		}
		g := byUID[uid]
		if g == nil {
			g = &Group{UID: uid}
			byUID[uid] = g
			sp.Groups = append(sp.Groups, g)
		}
		g.Events = append(g.Events, text)
		for _, id := range b.tzids {
			if !slices.Contains(g.TZIDs, id) {
				g.TZIDs = append(g.TZIDs, id)
			}
		}
	default:
		sp.Failed = append(sp.Failed, Failure{UID: b.uid, Reason: "unsupported_component"})
	}
}

const fileEnd = "END:VCALENDAR\r\n"

// head is what each file of the split begins with: VCALENDAR with the
// PRODID of the upload (or TitanOS's) and its CALSCALE.
func (sp *Split) head() string {
	prod := sp.ProdID
	if prod == "" {
		prod = ProdID
	}
	h := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:" + prod + "\r\n"
	if sp.CalScale != "" {
		h += "CALSCALE:" + sp.CalScale + "\r\n"
	}
	return h
}

// Size is the length of File(g) without building it; ErrZoneTooLarge
// when g names a VTIMEZONE over recur.MaxZoneBytes. An import checks it
// before it builds a file: a zone is copied into every file that names
// it (S-19).
func (sp *Split) Size(g *Group) (int, error) {
	n := len(sp.head()) + len(fileEnd)
	for _, id := range g.TZIDs {
		if sp.bigZones[id] {
			return 0, ErrZoneTooLarge
		}
		n += len(sp.Zones[id])
	}
	for _, e := range g.Events {
		n += len(e)
	}
	return n, nil
}

// File is the .ics file of one group: the head, the VTIMEZONEs it names,
// the events.
func (sp *Split) File(g *Group) []byte {
	var b bytes.Buffer
	b.WriteString(sp.head())
	for _, id := range g.TZIDs {
		if z, ok := sp.Zones[id]; ok {
			b.WriteString(z)
		}
	}
	for _, e := range g.Events {
		b.WriteString(e)
	}
	b.WriteString(fileEnd)
	return b.Bytes()
}

// Export writes one VCALENDAR of many entries in two passes over the
// files, so only the zones stay in RAM: Zones for each file, Begin (head
// and each VTIMEZONE once, the first of each TZID), Events for each file
// (every event as it is stored), End. A file that cannot be cut is
// skipped. The zones are bounded by MaxExportZones bytes.
type Export struct {
	w     io.Writer
	zones map[string]string
	order []string
	bytes int
	// LeftOut counts the zones over MaxExportZones (not written).
	LeftOut int
	err     error
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
			z, ok := sp.Zones[id]
			if _, seen := e.zones[id]; !ok || seen {
				continue
			}
			if e.bytes+len(z) > MaxExportZones {
				e.zones[id] = "" // seen, not written
				e.LeftOut++
				continue
			}
			e.zones[id] = z
			e.order = append(e.order, id)
			e.bytes += len(z)
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
