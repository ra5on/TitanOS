package office

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type CellEdit struct {
	Sheet string `json:"sheet"`
	Cell  string `json:"cell"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

var cellAddress = regexp.MustCompile(`^[A-Z]{1,3}[1-9][0-9]{0,6}$`)

// EditCells preserves unrelated workbook parts and existing cell styles. Only
// existing cells are writable; formulas and absent coordinates are rejected.
func EditCells(data []byte, edits []CellEdit) ([]byte, error) {
	if len(data) > 50<<20 || len(edits) == 0 || len(edits) > 500 {
		return nil, ErrTooLarge
	}
	z, err := openZip(data)
	if err != nil {
		return nil, err
	}
	workbook, err := readPart(z, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = xml.Unmarshal(workbook, &book); err != nil {
		return nil, err
	}
	relData, err := readPart(z, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err = xml.Unmarshal(relData, &rels); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	for _, sheet := range book.Sheets {
		for _, rel := range rels.Items {
			if rel.ID == sheet.ID && rel.Mode != "External" {
				target := path.Clean(path.Join("xl", rel.Target))
				if strings.HasPrefix(rel.Target, "/") {
					target = strings.TrimPrefix(rel.Target, "/")
				}
				if strings.HasPrefix(target, "xl/worksheets/") {
					targets[sheet.Name] = target
				}
			}
		}
	}
	grouped := map[string]map[string]CellEdit{}
	total := 0
	for _, e := range edits {
		target := targets[e.Sheet]
		if target == "" || !cellAddress.MatchString(e.Cell) || len(e.Value) > 32767 {
			return nil, fmt.Errorf("invalid sheet or cell")
		}
		total += len(e.Value)
		if total > MaxText {
			return nil, ErrTooLarge
		}
		if e.Type != "text" && e.Type != "number" && e.Type != "boolean" {
			return nil, fmt.Errorf("cell type must be text, number or boolean")
		}
		if grouped[target] == nil {
			grouped[target] = map[string]CellEdit{}
		}
		if _, ok := grouped[target][e.Cell]; ok {
			return nil, fmt.Errorf("duplicate cell")
		}
		grouped[target][e.Cell] = e
	}
	for target, wanted := range grouped {
		part, err := readPart(z, target)
		if err != nil {
			return nil, err
		}
		decoder := xml.NewDecoder(bytes.NewReader(part))
		var out bytes.Buffer
		last := int64(0)
		found := 0
		for {
			start := decoder.InputOffset()
			token, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			open, ok := token.(xml.StartElement)
			if !ok || open.Name.Local != "c" {
				continue
			}
			address, style := "", ""
			for _, attr := range open.Attr {
				if attr.Name.Local == "r" {
					address = attr.Value
				}
				if attr.Name.Local == "s" {
					style = attr.Value
				}
			}
			edit, ok := wanted[address]
			if !ok {
				continue
			}
			var cell struct {
				Formula *string `xml:"f"`
			}
			if e = decoder.DecodeElement(&cell, &open); e != nil {
				return nil, e
			}
			if cell.Formula != nil {
				return nil, fmt.Errorf("%s contains a formula; edit a value cell instead", address)
			}
			out.Write(part[last:start])
			fmt.Fprintf(&out, `<c xmlns="%s" r="%s"`, xmlAttr(open.Name.Space), address)
			if style != "" {
				fmt.Fprintf(&out, ` s="%s"`, xmlAttr(style))
			}
			switch edit.Type {
			case "text":
				fmt.Fprintf(&out, ` t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, xmlAttr(edit.Value))
			case "number":
				if !numberLiteral.MatchString(edit.Value) {
					return nil, fmt.Errorf("invalid number")
				}
				if _, e := strconv.ParseFloat(edit.Value, 64); e != nil {
					return nil, e
				}
				fmt.Fprintf(&out, `><v>%s</v></c>`, edit.Value)
			case "boolean":
				if edit.Value != "true" && edit.Value != "false" {
					return nil, fmt.Errorf("invalid boolean")
				}
				v := "0"
				if edit.Value == "true" {
					v = "1"
				}
				fmt.Fprintf(&out, ` t="b"><v>%s</v></c>`, v)
			}
			last = decoder.InputOffset()
			found++
		}
		if found != len(wanted) {
			return nil, fmt.Errorf("one or more cells are absent; no cells were added")
		}
		out.Write(part[last:])
		data, err = replacePart(z, target, out.Bytes())
		if err != nil {
			return nil, err
		}
		z, err = openZip(data)
		if err != nil {
			return nil, err
		}
	}
	// Consumers must recalculate formulas when opening the new copy.
	rootDecoder := xml.NewDecoder(bytes.NewReader(workbook))
	namespace := "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	for {
		token, e := rootDecoder.Token()
		if e != nil {
			break
		}
		if start, ok := token.(xml.StartElement); ok {
			namespace = start.Name.Space
			break
		}
	}
	calcTag := []byte(`<calcPr xmlns="` + xmlAttr(namespace) + `" fullCalcOnLoad="1" forceFullCalc="1"/>`)
	calc := regexp.MustCompile(`<(?:(?:[A-Za-z_][\w.-]*):)?calcPr\b[^>]*(?:/>|>\s*</(?:(?:[A-Za-z_][\w.-]*):)?calcPr>)`)
	if calc.Match(workbook) {
		workbook = calc.ReplaceAll(workbook, calcTag)
	} else {
		at := bytes.LastIndex(workbook, []byte("</"))
		if at < 0 {
			return nil, ErrUnsupported
		}
		workbook = append(append(append([]byte{}, workbook[:at]...), calcTag...), workbook[at:]...)
	}
	return replacePart(z, "xl/workbook.xml", workbook)
}

var numberLiteral = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
