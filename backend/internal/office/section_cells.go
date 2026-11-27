package office

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CellRange reads exactly the selected rectangle in workbook order. The optional
// replacement uses one tab per column and one newline per row; formulas are never evaluated.
func CellRange(data []byte, sheet, selection string, replacement *string, valueType string) (string, []byte, error) {
	sides := strings.Split(strings.ToUpper(selection), ":")
	if len(sides) > 2 || sheet == "" {
		return "", nil, errors.New("bitte Tabellenblatt und Zellbereich auswählen")
	}
	coordinate := func(ref string) (int, int, bool) {
		if !cellAddress.MatchString(ref) {
			return 0, 0, false
		}
		letters := strings.TrimRight(ref, "0123456789")
		c := columnIndex(ref)
		r, err := strconv.Atoi(ref[len(letters):])
		return c, r, err == nil && c >= 0 && c <= maxColumn && r >= 1 && r <= 1048576
	}
	startCol, startRow, ok1 := coordinate(sides[0])
	endCol, endRow, ok2 := coordinate(sides[len(sides)-1])
	rows, cols := endRow-startRow+1, endCol-startCol+1
	if !ok1 || !ok2 || rows < 1 || cols < 1 || rows*cols > 200 {
		return "", nil, errors.New("wähle einen Bereich mit 1 bis 200 Zellen")
	}
	z, err := openZip(data)
	if err != nil {
		return "", nil, err
	}
	book, err := readPart(z, "xl/workbook.xml")
	if err != nil {
		return "", nil, err
	}
	var wb xlsxWorkbook
	if err = xml.Unmarshal(book, &wb); err != nil {
		return "", nil, err
	}
	id := ""
	for _, s := range wb.Sheets {
		if s.Name == sheet {
			id = s.RID
			break
		}
	}
	if id == "" {
		return "", nil, errors.New("gewähltes Tabellenblatt fehlt")
	}
	part, err := documentPart(z, "xl", "workbook.xml", id, "xl/worksheets")
	if err != nil {
		return "", nil, err
	}
	raw, err := readPart(z, part)
	if err != nil {
		return "", nil, err
	}
	ssraw, _ := readPart(z, "xl/sharedStrings.xml")
	shared := sharedStrings(ssraw)
	values := make([][]string, rows)
	for i := range values {
		values[i] = make([]string, cols)
	}
	total := 0
	if err = eachRow(raw, func(cells []xlsxCell) bool {
		for _, cell := range cells {
			c, r, ok := coordinate(cell.Ref)
			if !ok || c < startCol || c > endCol || r < startRow || r > endRow {
				continue
			}
			value := cellValue(cell, shared)
			total += len(value)
			if total > 32768 {
				return false
			}
			values[r-startRow][c-startCol] = value
		}
		return true
	}); err != nil {
		return "", nil, err
	}
	if total > 32768 {
		return "", nil, ErrTooLarge
	}
	lines := make([]string, rows)
	for i, row := range values {
		lines[i] = strings.Join(row, "\t")
	}
	before := strings.Join(lines, "\n")
	if replacement == nil {
		return before, nil, nil
	}
	if len(*replacement) > 32768 {
		return "", nil, ErrTooLarge
	}
	after := strings.Split(strings.ReplaceAll(*replacement, "\r\n", "\n"), "\n")
	if len(after) != rows {
		return "", nil, errors.New("die Zeilenzahl muss zum gewählten Bereich passen")
	}
	if valueType == "" {
		valueType = "text"
	}
	var edits []CellEdit
	for r, line := range after {
		vals := strings.Split(line, "\t")
		if len(vals) != cols {
			return "", nil, errors.New("die Spaltenzahl muss zum gewählten Bereich passen")
		}
		for c, value := range vals {
			ref := fmt.Sprintf("%s%d", column(startCol+c), startRow+r)
			edits = append(edits, CellEdit{Sheet: sheet, Cell: ref, Type: valueType, Value: value})
		}
	}
	out, err := EditCells(data, edits)
	return before, out, err
}
