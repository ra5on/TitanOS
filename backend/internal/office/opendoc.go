package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"strings"
)

// collectXMLText collects the character data of an XML part, one line per element
// named in breaks (paragraphs, headings), a space for spaces and tabs the
// formats store as elements. It stops after MaxText bytes.
func collectXMLText(part []byte, out *strings.Builder, breaks map[string]bool, spaces map[string]bool) error {
	d := xml.NewDecoder(bytes.NewReader(part))
	for out.Len() < MaxText {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if spaces[t.Name.Local] {
				out.WriteByte(' ')
			}
		case xml.EndElement:
			if breaks[t.Name.Local] {
				out.WriteByte('\n')
			}
		case xml.CharData:
			out.Write(t)
		}
	}
	return nil
}

// ODFText returns the text of an OpenDocument file (.odt, .ods, .odp): the
// paragraphs and headings of content.xml, one per line (PLAN 72, full-text
// search).
func ODFText(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	part, err := readPart(z, "content.xml")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := collectXMLText(part, &out, map[string]bool{"p": true, "h": true}, map[string]bool{"s": true, "tab": true, "line-break": true}); err != nil {
		return "", err
	}
	return truncateText(out.String()), nil
}

// PPTXText returns the text of a PowerPoint file, slide by slide, one
// paragraph per line.
func PPTXText(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	type slide struct {
		n    int
		file *zip.File
	}
	var slides []slide
	for _, f := range z.File {
		name, ok := strings.CutPrefix(f.Name, "ppt/slides/slide")
		if !ok || !strings.HasSuffix(name, ".xml") || strings.Contains(name, "/") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSuffix(name, ".xml")); err == nil {
			slides = append(slides, slide{n, f})
		}
	}
	if len(slides) == 0 {
		return "", ErrUnsupported
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	var out strings.Builder
	for _, s := range slides {
		if out.Len() >= MaxText {
			break
		}
		part, err := readPart(z, s.file.Name)
		if err != nil {
			return "", err
		}
		if err := collectXMLText(part, &out, map[string]bool{"p": true}, map[string]bool{"tab": true, "br": true}); err != nil {
			return "", err
		}
		out.WriteByte('\n')
	}
	return truncateText(out.String()), nil
}

// truncateText cuts text to MaxText bytes on a rune boundary.
func truncateText(s string) string {
	if len(s) <= MaxText {
		return s
	}
	cut := MaxText
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}
