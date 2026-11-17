package office

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"
)

// A PDF from Markdown with the standard fonts every reader has, so nothing
// is embedded: Helvetica and Courier in WinAnsi encoding for the text
// (German umlauts, ß, €, quotes, dashes, ›), Symbol for arrows and signs
// of mathematics (→ ≤ ≥ ≠ ≈), ZapfDingbats for ✓ ✗ ★ ● ■. Both of the
// latter carry a ToUnicode map, so the text can be copied and searched.
// Since 1.09 no character becomes "?" (the export of a chat of 17 Nov 2026
// was full of them): narrow and other spaces become a space, every kind
// of hyphen and dash a hyphen, letters with accents outside WinAnsi their
// base letter, invisible characters nothing; what no standard font has
// (other scripts, most emoji) is drawn as an empty box ❏. A4, wrapped
// lines, tables with borders, page breaks.

const (
	pageW, pageH = 595.0, 842.0
	marginX      = 56.0
	marginTop    = 60.0
	marginBottom = 60.0
)

// Widths of Helvetica for the characters 32–126, in 1/1000 em.
var helvetica = []int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var winAnsi = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A,
	'‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// Fonts besides the four of the text (F1–F4).
const (
	fontSymbol   = "F5"
	fontDingbats = "F6"
)

// glyph is one character as drawn: a code in the font of the text (font
// ""), or in Symbol or ZapfDingbats with its width in 1/1000 em.
type glyph struct {
	font  string
	code  byte
	width int
}

// symbolGlyphs are characters of the Symbol font: code, width (Adobe AFM).
var symbolGlyphs = map[rune]glyph{
	'←': {fontSymbol, 0xAC, 987}, '↑': {fontSymbol, 0xAD, 603}, '→': {fontSymbol, 0xAE, 987}, '↓': {fontSymbol, 0xAF, 603},
	'↔': {fontSymbol, 0xAB, 1042}, '⇐': {fontSymbol, 0xDC, 987}, '⇒': {fontSymbol, 0xDE, 987}, '⇔': {fontSymbol, 0xDB, 1042},
	'≤': {fontSymbol, 0xA3, 549}, '≥': {fontSymbol, 0xB3, 549}, '≠': {fontSymbol, 0xB9, 549}, '≈': {fontSymbol, 0xBB, 549},
	'∞': {fontSymbol, 0xA5, 713}, '−': {fontSymbol, 0x2D, 549}, '√': {fontSymbol, 0xD6, 549}, '∑': {fontSymbol, 0xE5, 713},
	'Δ': {fontSymbol, 0x44, 612}, 'Ω': {fontSymbol, 0x57, 768}, 'α': {fontSymbol, 0x61, 631}, 'β': {fontSymbol, 0x62, 549},
	'π': {fontSymbol, 0x70, 549},
}

// dingbatGlyphs are characters of ZapfDingbats; a few emoji are drawn as
// the sign they stand for (✅ as ✔, ❌ as ✘).
var dingbatGlyphs = map[rune]glyph{
	'✓': {fontDingbats, 0x33, 755}, '✔': {fontDingbats, 0x34, 846}, '✗': {fontDingbats, 0x37, 571}, '✘': {fontDingbats, 0x38, 677},
	'★': {fontDingbats, 0x48, 816}, '●': {fontDingbats, 0x6C, 791}, '■': {fontDingbats, 0x6E, 761}, '❏': {fontDingbats, 0x6F, 762},
	'▲': {fontDingbats, 0x73, 892}, '▼': {fontDingbats, 0x74, 892}, '◆': {fontDingbats, 0x75, 788},
	'✅': {fontDingbats, 0x34, 846}, '☑': {fontDingbats, 0x34, 846},
	'❌': {fontDingbats, 0x38, 677}, '❎': {fontDingbats, 0x38, 677}, '✖': {fontDingbats, 0x38, 677},
}

// glyphAliases are drawn with the glyph of another character.
var glyphAliases = map[rune]bool{'✅': true, '☑': true, '❌': true, '❎': true, '✖': true}

// unknownGlyph stands for a character no standard font has.
var unknownGlyph = dingbatGlyphs['❏']

// plainText are characters written as other WinAnsi text.
var plainText = map[rune]string{
	'′': "'", '″': "\"", '⚠': "!", '❗': "!", '❕': "!", '❓': "?", '❔': "?", '⁄': "/", '∕': "/", '∶': ":",
}

// invisible are characters that are drawn as nothing: joiners, marks of
// direction, variation selectors and skin tones of emoji, the byte order mark.
func invisible(r rune) bool {
	switch {
	case r < 32, r == 0x7F, r >= 0x80 && r < 0xA0, r == 0x200B, r == 0x200C, r == 0x200D, r == 0x2060, r == 0xFEFF,
		r >= 0x200E && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069,
		r >= 0xFE00 && r <= 0xFE0F, r >= 0x1F3FB && r <= 0x1F3FF, r >= 0xE0100 && r <= 0xE01EF:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

// glyphsOf appends how r is drawn.
func glyphsOf(r rune, out []glyph) []glyph {
	switch {
	case r == '\t':
		return append(out, glyph{code: ' '}, glyph{code: ' '}, glyph{code: ' '}, glyph{code: ' '})
	case r >= 32 && r < 127, r >= 0xA0 && r <= 0xFF:
		return append(out, glyph{code: byte(r)})
	}
	if b, ok := winAnsi[r]; ok {
		return append(out, glyph{code: b})
	}
	if g, ok := symbolGlyphs[r]; ok {
		return append(out, g)
	}
	if g, ok := dingbatGlyphs[r]; ok {
		return append(out, g)
	}
	if text, ok := plainText[r]; ok {
		for _, c := range text {
			out = append(out, glyph{code: byte(c)})
		}
		return out
	}
	switch {
	case invisible(r):
		return out
	case unicode.Is(unicode.Zs, r):
		// Narrow no-break space (U+202F), thin space, ideographic space …
		return append(out, glyph{code: ' '})
	case unicode.Is(unicode.Pd, r):
		// Non-breaking hyphen (U+2011), hyphen, figure dash …
		return append(out, glyph{code: '-'})
	}
	// Letters with marks outside WinAnsi (ă, ș, ł has none), ligatures,
	// full-width letters, circled numbers: their compatibility form.
	if d := norm.NFKD.String(string(r)); d != string(r) {
		before := len(out)
		for _, c := range d {
			if c == r {
				break
			}
			out = glyphsOf(c, out)
		}
		if len(out) > before && !slices.Contains(out[before:], unknownGlyph) {
			return out
		}
		out = out[:before]
	}
	return append(out, unknownGlyph)
}

// glyphRun is a piece of text in one font.
type glyphRun struct {
	font  string // "" the font of the text
	codes []byte
	width int
}

// glyphRuns splits text into pieces by font.
func glyphRuns(text string) []glyphRun {
	var out []glyphRun
	var gs []glyph
	for _, r := range text {
		gs = glyphsOf(r, gs[:0])
		for _, g := range gs {
			if n := len(out); n > 0 && out[n-1].font == g.font {
				out[n-1].codes = append(out[n-1].codes, g.code)
				out[n-1].width += g.width
				continue
			}
			out = append(out, glyphRun{font: g.font, codes: []byte{g.code}, width: g.width})
		}
	}
	return out
}

type font struct {
	name string // F1 regular, F2 bold, F3 italic, F4 Courier
	size float64
}

func (f font) width(text string) float64 {
	return f.scale(f.units(text))
}

// scale turns units of the font (thousandths of its size) into points.
func (f font) scale(units int) float64 {
	return float64(units) * f.size / 1000
}

// units is the width of text in thousandths of the font size.
func (f font) units(text string) int {
	total := 0
	for _, run := range glyphRuns(text) {
		if run.font != "" {
			total += run.width
			continue
		}
		for _, b := range run.codes {
			w := 556
			switch {
			case f.name == "F4":
				w = 600
			case b >= 32 && b < 127:
				w = helvetica[b-32]
			}
			if f.name == "F2" {
				w = w * 105 / 100
			}
			total += w
		}
	}
	return total
}

type segment struct {
	font font
	text string
}

type pdfWriter struct {
	pages    []*bytes.Buffer
	page     *bytes.Buffer
	y        float64
	numbered int
}

func (p *pdfWriter) newPage() {
	p.page = &bytes.Buffer{}
	p.pages = append(p.pages, p.page)
	p.y = pageH - marginTop
}

func (p *pdfWriter) need(height float64) {
	if p.page == nil || p.y-height < marginBottom {
		p.newPage()
	}
}

func pdfString(b []byte) string {
	var s strings.Builder
	s.WriteByte('(')
	for _, c := range b {
		if c == '(' || c == ')' || c == '\\' {
			s.WriteByte('\\')
		}
		s.WriteByte(c)
	}
	s.WriteByte(')')
	return s.String()
}

func (p *pdfWriter) text(x, y float64, segs []segment) {
	fmt.Fprintf(p.page, "BT %.2f %.2f Td ", x, y)
	for _, s := range segs {
		for _, run := range glyphRuns(s.text) {
			name := s.font.name
			if run.font != "" {
				name = run.font
			}
			fmt.Fprintf(p.page, "/%s %.1f Tf %s Tj ", name, s.font.size, pdfString(run.codes))
		}
	}
	p.page.WriteString("ET\n")
}

// wrap lays spans out into lines no wider than width.
func wrap(spans []Span, size float64, bold bool, width float64) [][]segment {
	type word struct {
		font  font
		text  string
		space bool // a space comes before it
	}
	var words []word
	space := false
	for _, span := range spans {
		f := font{"F1", size}
		switch {
		case span.Code:
			f.name = "F4"
		case span.Bold || bold:
			f.name = "F2"
		case span.Italic:
			f.name = "F3"
		}
		for i, piece := range strings.Split(span.Text, " ") {
			if i > 0 {
				space = true
			}
			if piece == "" {
				continue
			}
			words = append(words, word{f, piece, space})
			space = false
		}
	}
	var lines [][]segment
	var line []segment
	used := 0.0
	add := func(f font, text string) {
		if len(line) > 0 && line[len(line)-1].font == f {
			line[len(line)-1].text += text
		} else {
			line = append(line, segment{f, text})
		}
		used += f.width(text)
	}
	for _, w := range words {
		text := w.text
		if w.space && used > 0 {
			text = " " + text
		}
		if used > 0 && used+w.font.width(text) > width {
			lines = append(lines, line)
			line, used = nil, 0
			text = w.text
		}
		// A word longer than the line is cut: as much as fits, at least one
		// character, the rest on the next lines. In one pass over the word;
		// trying ever shorter pieces took hours for a word of 100 000
		// characters (a minified line, base64).
		if w.font.width(text) > width {
			r := []rune(text)
			units := make([]int, len(r))
			total := 0
			for i, c := range r {
				units[i] = w.font.units(string(c))
				total += units[i]
			}
			start := 0
			for w.font.scale(total) > width && len(r)-start > 1 {
				cut, taken := start, 0
				for cut < len(r)-1 && w.font.scale(taken+units[cut]) <= width {
					taken += units[cut]
					cut++
				}
				if cut == start {
					taken += units[cut]
					cut++
				}
				add(w.font, string(r[start:cut]))
				lines = append(lines, line)
				line, used = nil, 0
				total -= taken
				start = cut
			}
			text = string(r[start:])
		}
		add(w.font, text)
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines
}

func (p *pdfWriter) paragraph(spans []Span, size float64, bold bool, indent, before, after float64, marker string) {
	lines := wrap(spans, size, bold, pageW-2*marginX-indent)
	if len(lines) == 0 {
		lines = [][]segment{{}}
	}
	lead := size * 1.35
	p.need(before + lead)
	p.y -= before
	for i, line := range lines {
		p.need(lead)
		p.y -= lead
		if i == 0 && marker != "" {
			p.text(marginX+indent-14, p.y, []segment{{font{"F1", size}, marker}})
		}
		p.text(marginX+indent, p.y, line)
	}
	p.y -= after
}

func (p *pdfWriter) table(rows [][][]Span) {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return
	}
	size, pad := 9.5, 4.0
	colW := (pageW - 2*marginX) / float64(columns)
	lead := size * 1.3
	p.y -= 4
	for r, row := range rows {
		cellLines := make([][][]segment, columns)
		height := 0.0
		for c := range columns {
			var spans []Span
			if c < len(row) {
				spans = row[c]
			}
			cellLines[c] = wrap(spans, size, r == 0, colW-2*pad)
			height = max(height, float64(len(cellLines[c]))*lead+2*pad)
		}
		height = max(height, lead+2*pad)
		p.need(height)
		top := p.y
		if r == 0 {
			fmt.Fprintf(p.page, "0.92 g %.2f %.2f %.2f %.2f re f 0 g\n", marginX, top-height, colW*float64(columns), height)
		}
		for c := range columns {
			x := marginX + float64(c)*colW
			fmt.Fprintf(p.page, "0.65 G 0.5 w %.2f %.2f %.2f %.2f re S 0 G\n", x, top-height, colW, height)
			y := top - pad
			for _, line := range cellLines[c] {
				y -= lead
				p.text(x+pad, y+2, line)
			}
		}
		p.y = top - height
	}
	p.y -= 10
}

// PDF makes a document from Markdown.
func PDF(markdown, title string) ([]byte, error) {
	p := &pdfWriter{}
	p.newPage()
	previous := ""
	for _, block := range Parse(markdown) {
		switch block.Kind {
		case "heading":
			size := map[int]float64{1: 20, 2: 16, 3: 13}[block.Level]
			if size == 0 {
				size = 11.5
			}
			p.paragraph(block.Spans, size, true, 0, size*0.6, size*0.3, "")
		case "paragraph":
			p.paragraph(block.Spans, 11, false, 0, 0, 7, "")
		case "bullet":
			p.paragraph(block.Spans, 11, false, 18, 0, 2, "•")
		case "number":
			if previous != "number" {
				p.numbered = 0
			}
			p.numbered++
			p.paragraph(block.Spans, 11, false, 18, 0, 2, fmt.Sprintf("%d.", p.numbered))
		case "code":
			for _, line := range strings.Split(block.Text, "\n") {
				p.paragraph([]Span{{Text: line, Code: true}}, 9, false, 8, 0, 0, "")
			}
			p.y -= 7
		case "rule":
			p.need(14)
			p.y -= 7
			fmt.Fprintf(p.page, "0.65 G 0.5 w %.2f %.2f m %.2f %.2f l S 0 G\n", marginX, p.y, pageW-marginX, p.y)
			p.y -= 7
		case "table":
			p.table(block.Rows)
		}
		if previous == "bullet" && block.Kind != "bullet" || previous == "number" && block.Kind != "number" {
			p.y -= 5
		}
		previous = block.Kind
	}

	var out bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	n := len(p.pages)
	// 1 catalog, 2 pages, 3–6 fonts of the text, 7 info, 8 Symbol,
	// 9 ZapfDingbats, 10–11 their ToUnicode maps, then per page: page,
	// content.
	const firstPage = 12
	kids := make([]string, n)
	for i := range n {
		kids[i] = fmt.Sprintf("%d 0 R", firstPage+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for _, base := range []string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Courier"} {
		obj("<< /Type /Font /Subtype /Type1 /BaseFont /" + base + " /Encoding /WinAnsiEncoding >>")
	}
	obj(fmt.Sprintf("<< /Title %s /Producer (TitanOS) /CreationDate (D:%s) >>", pdfText(title), time.Now().UTC().Format("20060102150405Z")))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Symbol /ToUnicode 10 0 R >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /ZapfDingbats /ToUnicode 11 0 R >>")
	for _, f := range []string{fontSymbol, fontDingbats} {
		cmap := toUnicode(f)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(cmap), cmap))
	}
	for i, page := range p.pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R /F4 6 0 R /F5 8 0 R /F6 9 0 R >> >> /Contents %d 0 R >>", pageW, pageH, firstPage+1+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", page.Len(), page.String()))
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes(), nil
}

// pdfText is a text string of the document information in UTF-16 (the
// title of a chat may have any character).
func pdfText(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}
	b.WriteString(">")
	return b.String()
}

// toUnicode is the ToUnicode CMap of Symbol or ZapfDingbats for the codes
// used here: what a reader copies or searches for.
func toUnicode(font string) string {
	codes := map[byte]rune{}
	for _, table := range []map[rune]glyph{symbolGlyphs, dingbatGlyphs} {
		for r, g := range table {
			if g.font != font {
				continue
			}
			// Several characters drawn alike (✅ as ✔): the sign itself.
			if _, alias := glyphAliases[r]; !alias {
				codes[g.code] = r
			}
		}
	}
	keys := slices.Sorted(maps.Keys(codes))
	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	fmt.Fprintf(&b, "/CMapName /TitanOS-%s def\n/CMapType 2 def\n1 begincodespacerange\n<00> <FF>\nendcodespacerange\n", font)
	fmt.Fprintf(&b, "%d beginbfchar\n", len(keys))
	for _, c := range keys {
		fmt.Fprintf(&b, "<%02X> <%04X>\n", c, codes[c])
	}
	b.WriteString("endbfchar\nendcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.String()
}
