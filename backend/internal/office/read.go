package office

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// MaxText is how much text Text returns.
const MaxText = 512 << 10

var ErrUnsupported = errors.New("office: not a Word or Excel file")

func openZip(data []byte) (*zip.Reader, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrUnsupported
	}
	return z, nil
}

func readPart(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			// A part larger than this is no document worth reading here.
			return io.ReadAll(io.LimitReader(r, 64<<20))
		}
	}
	return nil, fmt.Errorf("%w: %s missing", ErrUnsupported, name)
}

// DOCXText returns the text of a Word document, one paragraph per line;
// headings start with #, list items with -, table cells are separated by |.
func DOCXText(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	part, err := readPart(z, "word/document.xml")
	if err != nil {
		return "", err
	}
	d := xml.NewDecoder(bytes.NewReader(part))
	var out, para strings.Builder
	prefix := ""
	inCell := false
	for out.Len() < MaxText {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", ErrUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
				prefix = ""
			case "pStyle":
				for _, a := range t.Attr {
					if a.Name.Local == "val" {
						if n, ok := strings.CutPrefix(a.Value, "Heading"); ok {
							// Word has Heading1 to Heading9; anything else in a
							// file is not trusted to size the prefix.
							if level, err := strconv.Atoi(n); err == nil && level >= 1 && level <= 9 {
								prefix = strings.Repeat("#", level) + " "
							}
						} else if a.Value == "Title" {
							prefix = "# "
						} else if strings.Contains(a.Value, "List") {
							prefix = "- "
						}
					}
				}
			case "numPr":
				if prefix == "" {
					prefix = "- "
				}
			case "tab":
				para.WriteString("\t")
			case "br":
				para.WriteString("\n")
			case "tc":
				inCell = true
			case "tr":
				out.WriteString("|")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				text := para.String()
				if inCell {
					out.WriteString(" " + text + " ")
				} else if strings.TrimSpace(text) != "" || prefix != "" {
					out.WriteString(prefix + text + "\n")
				} else {
					out.WriteString("\n")
				}
			case "tc":
				inCell = false
				out.WriteString("|")
			case "tr":
				out.WriteString("\n")
			}
		case xml.CharData:
			para.Write(t)
		}
	}
	return strings.TrimSpace(out.String()), nil
}

type xlsxRels struct {
	Rels []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

type xlsxWorkbook struct {
	Sheets []struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"sheets>sheet"`
}

type xlsxCell struct {
	Ref    string `xml:"r,attr"`
	Type   string `xml:"t,attr"`
	Value  string `xml:"v"`
	Inline []struct {
		Text string `xml:",chardata"`
	} `xml:"is>t"`
	Rich []struct {
		Text string `xml:",chardata"`
	} `xml:"is>r>t"`
	Formula string `xml:"f"`
}

// maxColumn is the last column a workbook can have (XFD).
const maxColumn = 16383

// columnIndex returns the column of a cell reference such as "C7", from 0.
// References beyond the last column of Excel give -1.
func columnIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
		if n-1 > maxColumn {
			return -1
		}
	}
	return n - 1
}

// maxShared bounds the shared strings kept: a part of millions of empty
// entries must not become millions of strings. A cell naming one beyond
// keeps its number.
const maxShared = 1 << 20

// sharedStrings reads xl/sharedStrings.xml entry by entry.
func sharedStrings(part []byte) []string {
	var shared []string
	d := xml.NewDecoder(bytes.NewReader(part))
	for len(shared) < maxShared {
		token, err := d.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}
		var item struct {
			Text string `xml:"t"`
			Rich []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		}
		if d.DecodeElement(&item, &start) != nil {
			break
		}
		text := item.Text
		for _, r := range item.Rich {
			text += r.Text
		}
		shared = append(shared, text)
	}
	return shared
}

// eachRow gives the rows of a worksheet part to row one at a time, each
// with at most as many cells as a row of Excel has, until row returns
// false. The part is read as a stream: millions of cells never become
// millions of values at once.
func eachRow(part []byte, row func([]xlsxCell) bool) error {
	d := xml.NewDecoder(bytes.NewReader(part))
	inData := false
	for {
		token, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "sheetData" {
				inData = true
			} else if inData && t.Name.Local == "row" {
				cells, err := readRow(d)
				if err != nil {
					return err
				}
				if !row(cells) {
					return nil
				}
			}
		case xml.EndElement:
			if t.Name.Local == "sheetData" {
				inData = false
			}
		}
	}
}

// readRow reads the cells of a row up to its end element.
func readRow(d *xml.Decoder) ([]xlsxCell, error) {
	var cells []xlsxCell
	for {
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local != "c" || len(cells) > maxColumn {
				if err := d.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			var c xlsxCell
			if err := d.DecodeElement(&c, &t); err != nil {
				return nil, err
			}
			cells = append(cells, c)
		case xml.EndElement:
			return cells, nil
		}
	}
}

// cellValue is the text of a cell as the sheet shows it.
func cellValue(c xlsxCell, shared []string) string {
	value := c.Value
	switch c.Type {
	case "s":
		if n, err := strconv.Atoi(c.Value); err == nil && n >= 0 && n < len(shared) {
			value = shared[n]
		}
	case "inlineStr":
		value = ""
		for _, t := range c.Inline {
			value += t.Text
		}
		for _, t := range c.Rich {
			value += t.Text
		}
	case "b":
		value = map[string]string{"1": "TRUE", "0": "FALSE"}[c.Value]
	}
	if value == "" && c.Formula != "" {
		value = "=" + c.Formula
	}
	return value
}

// XLSXText returns every sheet of a workbook as CSV under "## <name>",
// MaxText at most: the budget is checked for every cell, since a small file
// can name one large shared string from many cells.
func XLSXText(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	var shared []string
	if part, err := readPart(z, "xl/sharedStrings.xml"); err == nil {
		shared = sharedStrings(part)
	}
	part, err := readPart(z, "xl/workbook.xml")
	if err != nil {
		return "", err
	}
	var wb xlsxWorkbook
	if err := xml.Unmarshal(part, &wb); err != nil {
		return "", ErrUnsupported
	}
	targets := map[string]string{}
	if part, err := readPart(z, "xl/_rels/workbook.xml.rels"); err == nil {
		var rels xlsxRels
		if xml.Unmarshal(part, &rels) == nil {
			for _, r := range rels.Rels {
				target := strings.TrimPrefix(r.Target, "/")
				if !strings.HasPrefix(target, "xl/") {
					target = path.Join("xl", target)
				}
				targets[r.ID] = target
			}
		}
	}
	var out strings.Builder
	full := false
	read := map[string]bool{}
	for i, s := range wb.Sheets {
		if full {
			break
		}
		target := targets[s.RID]
		if target == "" {
			target = fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		}
		// Every part once, however many sheets name it.
		if read[target] {
			continue
		}
		read[target] = true
		part, err := readPart(z, target)
		if err != nil {
			continue
		}
		fmt.Fprintf(&out, "## %s\n", s.Name)
		w := csv.NewWriter(&out)
		// A sheet that breaks off keeps the rows read before.
		_ = eachRow(part, func(cells []xlsxCell) bool {
			sort.SliceStable(cells, func(a, b int) bool { return columnIndex(cells[a].Ref) < columnIndex(cells[b].Ref) })
			var record []string
			size := out.Len()
			for _, c := range cells {
				col := columnIndex(c.Ref)
				for len(record) < col {
					record = append(record, "")
					size++
				}
				value := cellValue(c, shared)
				if size += len(value) + 1; size > MaxText {
					full = true
					break
				}
				record = append(record, value)
			}
			_ = w.Write(record) // a strings.Builder takes everything
			w.Flush()
			if out.Len() > MaxText {
				full = true
			}
			return !full
		})
		w.Flush()
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String()), nil
}

// Text returns the text of a Word or Excel file by its name.
func Text(name string, data []byte) (string, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".docx":
		return DOCXText(data)
	case ".xlsx":
		return XLSXText(data)
	case ".odt", ".ods", ".odp":
		return ODFText(data)
	case ".pptx":
		return PPTXText(data)
	}
	return "", ErrUnsupported
}
