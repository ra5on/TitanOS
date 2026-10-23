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
)

// TextEdit replaces an exact piece of text. Count guards against editing the
// wrong revision or an ambiguous occurrence. DOCX matches must fit one text run.
type TextEdit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Count   int    `json:"count"`
}

func EditText(name string, data []byte, edits []TextEdit) ([]byte, error) {
	if len(data) > 50<<20 || len(edits) == 0 || len(edits) > 100 {
		return nil, ErrTooLarge
	}
	budget := 0
	for _, edit := range edits {
		budget += edit.Count * len(edit.Replace)
		if budget > 4<<20 {
			return nil, ErrTooLarge
		}
		if edit.Find == "" || edit.Count < 1 || edit.Count > 1000 || len(edit.Find)+len(edit.Replace) > MaxText {
			return nil, errors.New("invalid replacement")
		}
	}
	if strings.ToLower(path.Ext(name)) != ".docx" {
		return replaceText(data, edits)
	}
	z, err := openZip(data)
	if err != nil {
		return nil, err
	}
	original, err := readPart(z, "word/document.xml")
	if err != nil {
		return nil, err
	}
	counts := make([]int, len(edits))
	var out bytes.Buffer
	decoder := xml.NewDecoder(bytes.NewReader(original))
	last := int64(0)
	textDepth := 0
	for {
		start := decoder.InputOffset()
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		end := decoder.InputOffset()
		switch token := token.(type) {
		case xml.StartElement:
			if token.Name.Local == "t" && token.Name.Space == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" {
				textDepth++
			}
		case xml.EndElement:
			if token.Name.Local == "t" && textDepth > 0 {
				textDepth--
			}
		case xml.CharData:
			if textDepth > 0 {
				value := string(token)
				changed := false
				for i, edit := range edits {
					n := strings.Count(value, edit.Find)
					if n > 0 {
						counts[i] += n
						value = strings.ReplaceAll(value, edit.Find, edit.Replace)
						changed = true
					}
				}
				if changed {
					out.Write(original[last:start])
					xml.EscapeText(&out, []byte(value))
					last = end
				}
			}
		}
	}
	out.Write(original[last:])
	for i, edit := range edits {
		if counts[i] != edit.Count {
			return nil, fmt.Errorf("replacement %d: expected %d matches, found %d; split Word runs require an explicit new document", i+1, edit.Count, counts[i])
		}
	}
	return replacePart(z, "word/document.xml", out.Bytes())
}
func replaceText(data []byte, edits []TextEdit) ([]byte, error) {
	text := string(data)
	for i, edit := range edits {
		if count := strings.Count(text, edit.Find); count != edit.Count {
			return nil, fmt.Errorf("replacement %d: expected %d matches, found %d", i+1, edit.Count, count)
		}
		text = strings.ReplaceAll(text, edit.Find, edit.Replace)
		if len(text) > 4<<20 {
			return nil, ErrTooLarge
		}
	}
	return []byte(text), nil
}
func replacePart(z *zip.Reader, name string, data []byte) ([]byte, error) {
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	total := int64(0)
	if len(z.File) > 5000 {
		return nil, ErrTooLarge
	}
	for _, f := range z.File {
		total += int64(f.UncompressedSize64)
		if total > 100<<20 {
			return nil, ErrTooLarge
		}
		if f.Name == name {
			w, err := writer.CreateHeader(&f.FileHeader)
			if err != nil {
				return nil, err
			}
			if _, err = w.Write(data); err != nil {
				return nil, err
			}
		} else {
			if err := writer.Copy(f); err != nil {
				return nil, err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
