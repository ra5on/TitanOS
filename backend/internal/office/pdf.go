package office

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// A PDF from Markdown with the standard fonts every reader has (Helvetica,
// Courier) in WinAnsi encoding: German umlauts, ß, €, quotes and dashes
// work; characters outside it become "?". A4, wrapped lines, tables with
// borders, page breaks.

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

func encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			out = append(out, ' ', ' ', ' ', ' ')
		case r >= 32 && r < 127, r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := winAnsi[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

type font struct {
	name string // F1 regular, F2 bold, F3 italic, F4 Courier
	size float64
}

func (f font) width(text string) float64 {
	total := 0.0
	for _, b := range encode(text) {
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
		total += float64(w)
	}
	return total * f.size / 1000
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
		fmt.Fprintf(p.page, "/%s %.1f Tf %s Tj ", s.font.name, s.font.size, pdfString(encode(s.text)))
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
		// A word longer than the line is cut.
		for w.font.width(text) > width && len([]rune(text)) > 1 {
			r := []rune(text)
			cut := len(r) - 1
			for cut > 1 && w.font.width(string(r[:cut])) > width {
				cut--
			}
			add(w.font, string(r[:cut]))
			lines = append(lines, line)
			line, used = nil, 0
			text = string(r[cut:])
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
	// 1 catalog, 2 pages, 3–6 fonts, 7 info, then per page: page, content.
	kids := make([]string, n)
	for i := range n {
		kids[i] = fmt.Sprintf("%d 0 R", 8+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for _, base := range []string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Courier"} {
		obj("<< /Type /Font /Subtype /Type1 /BaseFont /" + base + " /Encoding /WinAnsiEncoding >>")
	}
	obj(fmt.Sprintf("<< /Title %s /Producer (TitanOS) /CreationDate (D:%s) >>", pdfString(encode(title)), time.Now().UTC().Format("20060102150405Z")))
	for i, page := range p.pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R /F4 6 0 R >> >> /Contents %d 0 R >>", pageW, pageH, 9+2*i))
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
