// Package office writes and reads the documents the assistant works with:
// Word (.docx), Excel (.xlsx) and PDF, made from Markdown or tables, and
// the text inside existing Word and Excel files. Only the standard
// library; the formats are kept to what Word, Excel and LibreOffice open.
package office

import (
	"regexp"
	"strings"
)

// Block is a part of a document.
type Block struct {
	Kind  string // heading, paragraph, bullet, number, table, code, rule
	Level int    // heading level 1–6
	Spans []Span // heading, paragraph, bullet, number
	Rows  [][][]Span
	Text  string // code
}

// Span is a piece of text with its look.
type Span struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
}

var (
	headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	bulletRE  = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	numberRE  = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	ruleRE    = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	rowRE     = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	sepRE     = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	inlineRE  = regexp.MustCompile("`[^`]+`|\\*\\*[^*]+\\*\\*|__[^_]+__|\\*[^*\\s][^*]*\\*|_[^_\\s][^_]*_")
)

// Inline splits text into spans by **bold**, *italic* and `code`.
func Inline(text string) []Span {
	var spans []Span
	last := 0
	for _, loc := range inlineRE.FindAllStringIndex(text, -1) {
		if loc[0] > last {
			spans = append(spans, Span{Text: text[last:loc[0]]})
		}
		token := text[loc[0]:loc[1]]
		switch {
		case strings.HasPrefix(token, "`"):
			spans = append(spans, Span{Text: token[1 : len(token)-1], Code: true})
		case strings.HasPrefix(token, "**"), strings.HasPrefix(token, "__"):
			spans = append(spans, Span{Text: token[2 : len(token)-2], Bold: true})
		default:
			spans = append(spans, Span{Text: token[1 : len(token)-1], Italic: true})
		}
		last = loc[1]
	}
	if last < len(text) {
		spans = append(spans, Span{Text: text[last:]})
	}
	return spans
}

func cells(line string) [][]Span {
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	var row [][]Span
	for _, cell := range strings.Split(line, "|") {
		row = append(row, Inline(strings.TrimSpace(cell)))
	}
	return row
}

// Parse reads the Markdown the assistant writes into blocks.
func Parse(markdown string) []Block {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	var blocks []Block
	var paragraph []string
	flush := func() {
		if len(paragraph) > 0 {
			blocks = append(blocks, Block{Kind: "paragraph", Spans: Inline(strings.Join(paragraph, " "))})
			paragraph = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			blocks = append(blocks, Block{Kind: "code", Text: strings.Join(code, "\n")})
		case trimmed == "":
			flush()
		case headingRE.MatchString(line):
			flush()
			m := headingRE.FindStringSubmatch(line)
			blocks = append(blocks, Block{Kind: "heading", Level: len(m[1]), Spans: Inline(m[2])})
		case ruleRE.MatchString(line):
			flush()
			blocks = append(blocks, Block{Kind: "rule"})
		case rowRE.MatchString(line) && i+1 < len(lines) && sepRE.MatchString(lines[i+1]):
			flush()
			table := Block{Kind: "table", Rows: [][][]Span{cells(line)}}
			for i += 2; i < len(lines) && rowRE.MatchString(lines[i]); i++ {
				table.Rows = append(table.Rows, cells(lines[i]))
			}
			i--
			blocks = append(blocks, table)
		case bulletRE.MatchString(line):
			flush()
			blocks = append(blocks, Block{Kind: "bullet", Spans: Inline(bulletRE.FindStringSubmatch(line)[1])})
		case numberRE.MatchString(line):
			flush()
			blocks = append(blocks, Block{Kind: "number", Spans: Inline(numberRE.FindStringSubmatch(line)[1])})
		default:
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return blocks
}
