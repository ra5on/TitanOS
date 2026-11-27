// Targeted edits retain the original ZIP and touch only text inside the chosen paragraph.
package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	wordNamespace    = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	drawingNamespace = "http://schemas.openxmlformats.org/drawingml/2006/main"
)

type partLinks struct {
	Items []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Mode   string `xml:"TargetMode,attr"`
	} `xml:"Relationship"`
}

func documentPart(z *zip.Reader, directory, document, id, allowedPrefix string) (string, error) {
	raw, err := readPart(z, directory+"/_rels/"+document+".rels")
	if err != nil {
		return "", err
	}
	var links partLinks
	if err = xml.Unmarshal(raw, &links); err != nil {
		return "", err
	}
	for _, item := range links.Items {
		if item.ID != id || item.Mode == "External" {
			continue
		}
		rel := path.Join(directory, item.Target)
		if strings.HasPrefix(item.Target, "/") {
			rel = strings.TrimPrefix(path.Clean(item.Target), "/")
		}
		if strings.HasPrefix(rel, allowedPrefix+"/") && path.Ext(rel) == ".xml" {
			return rel, nil
		}
	}
	return "", errors.New("gewählter Office-Teil fehlt")
}

func slidePart(z *zip.Reader, slide int) (string, error) {
	raw, err := readPart(z, "ppt/presentation.xml")
	if err != nil {
		return "", err
	}
	var doc struct {
		Slides []struct {
			ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if err = xml.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	if slide < 1 || slide > len(doc.Slides) {
		return "", errors.New("gewählte Folie fehlt")
	}
	return documentPart(z, "ppt", "presentation.xml", doc.Slides[slide-1].ID, "ppt/slides")
}

type textSpan struct {
	start, end int64
	text       string
}

// Paragraph reads or replaces one numbered paragraph, counting paragraphs in document
// order (including blank ones). Individual runs, styling and adjacent paragraphs survive.
func Paragraph(data []byte, format string, slide, number int, replacement *string) (string, []byte, error) {
	if number < 1 || number > 100000 || (replacement != nil && (len(*replacement) > 32768 || !utf8.ValidString(*replacement) || strings.ContainsAny(*replacement, "\x00\r\n"))) {
		return "", nil, errors.New("ungültiger Absatz oder Text")
	}
	z, err := openZip(data)
	if err != nil {
		return "", nil, err
	}
	part, ns := "word/document.xml", wordNamespace
	switch format {
	case ".docx":
	case ".pptx":
		part, err = slidePart(z, slide)
		ns = drawingNamespace
	default:
		return "", nil, ErrUnsupported
	}
	if err != nil {
		return "", nil, err
	}
	raw, err := readPart(z, part)
	if err != nil {
		return "", nil, err
	}
	parser := xml.NewDecoder(bytes.NewReader(raw))
	var spans []textSpan
	var before strings.Builder
	active, inText, count := false, false, 0
	for {
		start := parser.InputOffset()
		token, err := parser.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Space != ns {
				continue
			}
			switch t.Name.Local {
			case "p":
				count++
				active = count == number
			case "t":
				inText = true
			}
		case xml.EndElement:
			if t.Name.Space != ns {
				continue
			}
			if t.Name.Local == "t" {
				inText = false
			}
			if t.Name.Local == "p" && active {
				active = false
			}
		case xml.CharData:
			if active && inText {
				if before.Len()+len(t) > 32768 {
					return "", nil, ErrTooLarge
				}
				before.Write(t)
				spans = append(spans, textSpan{start, parser.InputOffset(), string(t)})
			}
		}
		if count >= number && !active {
			break
		}
	}
	if count < number {
		return "", nil, fmt.Errorf("kein Absatz mit Nummer %d", number)
	}
	original := before.String()
	if replacement == nil {
		return original, nil, nil
	}
	if len(spans) == 0 {
		return "", nil, errors.New("dieser Absatz enthält keinen bearbeitbaren Text")
	}
	left := []rune(*replacement)
	var edited bytes.Buffer
	last := int64(0)
	for i, s := range spans {
		n := len(left)
		if i < len(spans)-1 {
			n = min(n, utf8.RuneCountInString(s.text))
		}
		edited.Write(raw[last:s.start])
		if err := xml.EscapeText(&edited, []byte(string(left[:n]))); err != nil {
			return "", nil, err
		}
		last = s.end
		left = left[n:]
	}
	edited.Write(raw[last:])
	out, err := replacePart(z, part, edited.Bytes())
	return original, out, err
}

// SlideText follows the presentation order, reading only the selected slide's
// text. Hidden document metadata, notes and neighbouring slides stay outside it.
func SlideText(data []byte, slide int) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	part, err := slidePart(z, slide)
	if err != nil {
		return "", err
	}
	raw, err := readPart(z, part)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Space == drawingNamespace && element.Name.Local == "t" {
				var run string
				if err := decoder.DecodeElement(&run, &element); err != nil {
					return "", err
				}
				text.WriteString(run)
			}
		case xml.EndElement:
			if element.Name.Space == drawingNamespace && (element.Name.Local == "p" || element.Name.Local == "br") {
				text.WriteByte('\n')
			}
		}
		if text.Len() > MaxText {
			return "", ErrTooLarge
		}
	}
	return strings.TrimSpace(text.String()), nil
}
