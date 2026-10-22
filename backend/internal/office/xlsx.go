package office

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Sheet is one sheet of a workbook. Values are strings, numbers or booleans;
// the first row is set bold as a header when Header is true.
type Sheet struct {
	Name   string  `json:"name"`
	Rows   [][]any `json:"rows"`
	Header bool    `json:"header"`
}

var ErrTooLarge = errors.New("office: too large")

// MaxCells limits a workbook.
const MaxCells = 500000

// SheetFromCSV reads CSV (comma or semicolon) into a sheet; numbers stay numbers.
func SheetFromCSV(name, text string) (Sheet, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if first, _, _ := strings.Cut(text, "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		r.Comma = ';'
	}
	records, err := r.ReadAll()
	if err != nil {
		return Sheet{}, err
	}
	sheet := Sheet{Name: name, Header: true}
	for _, record := range records {
		row := make([]any, len(record))
		for i, v := range record {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) {
				row[i] = n
			} else {
				row[i] = v
			}
		}
		sheet.Rows = append(sheet.Rows, row)
	}
	return sheet, nil
}

func column(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

func sheetName(name string, i int, used map[string]bool) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	if name == "" || used[strings.ToLower(name)] {
		name = fmt.Sprintf("Blatt%d", i+1)
	}
	used[strings.ToLower(name)] = true
	return name
}

// XLSX makes a workbook.
func XLSX(sheets []Sheet) ([]byte, error) {
	if len(sheets) == 0 {
		sheets = []Sheet{{Name: "Blatt1"}}
	}
	cellsTotal := 0
	for _, s := range sheets {
		for _, row := range s.Rows {
			cellsTotal += len(row)
		}
	}
	if cellsTotal > MaxCells || len(sheets) > 50 {
		return nil, ErrTooLarge
	}
	used := map[string]bool{}
	var overrides, workbookSheets, rels strings.Builder
	files := [][2]string{}
	for i, s := range sheets {
		name := sheetName(s.Name, i, used)
		fmt.Fprintf(&overrides, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
		fmt.Fprintf(&workbookSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlAttr(name), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		var data strings.Builder
		for r, row := range s.Rows {
			fmt.Fprintf(&data, `<row r="%d">`, r+1)
			for c, value := range row {
				ref := column(c) + strconv.Itoa(r+1)
				style := ""
				if s.Header && r == 0 {
					style = ` s="1"`
				}
				switch v := value.(type) {
				case nil:
				case float64:
					fmt.Fprintf(&data, `<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(v, 'f', -1, 64))
				case int:
					fmt.Fprintf(&data, `<c r="%s"%s><v>%d</v></c>`, ref, style, v)
				case bool:
					b := 0
					if v {
						b = 1
					}
					fmt.Fprintf(&data, `<c r="%s" t="b"%s><v>%d</v></c>`, ref, style, b)
				default:
					text := fmt.Sprint(v)
					if strings.HasPrefix(text, "=") {
						fmt.Fprintf(&data, `<c r="%s"%s><f>%s</f></c>`, ref, style, xmlText(strings.TrimPrefix(text, "=")))
						continue
					}
					fmt.Fprintf(&data, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlText(text))
				}
			}
			data.WriteString("</row>")
		}
		freeze := ""
		if s.Header && len(s.Rows) > 1 {
			freeze = `<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`
		}
		files = append(files, [2]string{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + freeze + `<sheetData>` + data.String() + `</sheetData></worksheet>`})
	}
	n := len(sheets)
	rels.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, n+1))
	files = append([][2]string{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` + overrides.String() + `</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + workbookSheets.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`},
	}, files...)
	return zipFiles(files)
}

func xmlAttr(s string) string {
	return strings.ReplaceAll(xmlText(s), `"`, "&quot;")
}
