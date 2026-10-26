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

type xlsxSheet struct {
	Rows []struct {
		Cells []xlsxCell `xml:"c"`
	} `xml:"sheetData>row"`
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

// XLSXText returns every sheet of a workbook as CSV under "## <name>".
func XLSXText(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	var shared []string
	if part, err := readPart(z, "xl/sharedStrings.xml"); err == nil {
		var sst struct {
			Items []struct {
				Text string `xml:"t"`
				Rich []struct {
					Text string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if xml.Unmarshal(part, &sst) == nil {
			for _, item := range sst.Items {
				text := item.Text
				for _, r := range item.Rich {
					text += r.Text
				}
				shared = append(shared, text)
			}
		}
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
	for i, s := range wb.Sheets {
		target := targets[s.RID]
		if target == "" {
			target = fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		}
		part, err := readPart(z, target)
		if err != nil {
			continue
		}
		var sheet xlsxSheet
		if xml.Unmarshal(part, &sheet) != nil {
			continue
		}
		fmt.Fprintf(&out, "## %s\n", s.Name)
		w := csv.NewWriter(&out)
		for _, row := range sheet.Rows {
			var record []string
			sort.SliceStable(row.Cells, func(a, b int) bool { return columnIndex(row.Cells[a].Ref) < columnIndex(row.Cells[b].Ref) })
			for _, c := range row.Cells {
				col := columnIndex(c.Ref)
				for len(record) < col {
					record = append(record, "")
				}
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
				record = append(record, value)
			}
			_ = w.Write(record)
			if out.Len() > MaxText {
				break
			}
		}
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
	}
	return "", ErrUnsupported
}
