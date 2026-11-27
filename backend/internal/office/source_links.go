package office

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// SourceWorkbook adds standard OOXML external hyperlink relationships to column A of
// an editable, single-sheet workbook. All other cells keep their original value types.
func SourceWorkbook(sheet Sheet, sourceURLs []string) ([]byte, error) {
	if !sheet.Header || len(sourceURLs)+1 != len(sheet.Rows) {
		return nil, errors.New("source links do not match the table")
	}
	base, err := XLSX([]Sheet{sheet})
	if err != nil {
		return nil, err
	}
	z, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		return nil, err
	}
	var links, rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	links.WriteString(`<hyperlinks xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	for i, raw := range sourceURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || u.Path != "/api/v1/files/download" {
			return nil, errors.New("invalid original file link")
		}
		fmt.Fprintf(&links, `<hyperlink ref="A%d" r:id="source%d"/>`, i+2, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="source%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, i+1, xmlAttr(raw))
	}
	links.WriteString("</hyperlinks>")
	rels.WriteString("</Relationships>")
	parts := make([][2]string, 0, len(z.File)+1)
	for _, f := range z.File {
		data, err := readPart(z, f.Name)
		if err != nil {
			return nil, err
		}
		content := string(data)
		if f.Name == "xl/worksheets/sheet1.xml" {
			content = strings.Replace(content, "</worksheet>", links.String()+"</worksheet>", 1)
		}
		parts = append(parts, [2]string{f.Name, content})
	}
	parts = append(parts, [2]string{"xl/worksheets/_rels/sheet1.xml.rels", rels.String()})
	return zipFiles(parts)
}
