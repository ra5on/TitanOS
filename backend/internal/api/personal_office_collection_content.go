package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// Compare only explicitly labelled statements. Missing or ambiguous amounts stay visible
// for review. Source text, including strings beginning with '=', is always a literal cell.
var offerLabels = regexp.MustCompile(`(?i)^\s*(anbieter|lieferant|supplier|vendor|gesamtpreis(?:\s+(?:brutto|netto))?|gesamtbetrag|gesamtsumme|endbetrag|angebotssumme|grand total|total price|lieferzeit|liefertermin|delivery time|gültig bis|gueltig bis|valid until|zahlungsbedingungen|zahlungsziel|payment terms)\s*(?:[:=;,]|\t|\s{2,})\s*(\S.*)$`)
var offerHeaders = []string{"Original", "Anbieter", "Gesamtpreis", "Lieferzeit", "Gültig bis", "Zahlungsbedingungen"}

func offerColumn(label string) int {
	switch strings.ToLower(label) {
	case "anbieter", "lieferant", "supplier", "vendor":
		return 1
	case "lieferzeit", "liefertermin", "delivery time":
		return 3
	case "gültig bis", "gueltig bis", "valid until":
		return 4
	case "zahlungsbedingungen", "zahlungsziel", "payment terms":
		return 5
	default:
		return 2
	}
}
func pdfPageSource(src pa.Source, page int) pa.Source {
	src.Ref = maps.Clone(src.Ref)
	src.Ref["page"] = fmt.Sprint(page)
	src.Ref["region"] = fmt.Sprintf("Seite %d", page)
	return src
}
func readCollectionPage(ctx context.Context, f collectionFile, number int) (string, error) {
	doc, err := office.ReadPDF(ctx, f.data, number, number)
	if err != nil {
		return "", err
	}
	if len(doc.Pages) != 1 || doc.Pages[0].Number != number {
		return "", errors.New("selected PDF page unavailable")
	}
	return doc.Pages[0].Text, nil
}
func mergeSelectedPDFs(ctx context.Context, input collectionInput, details *collectionPreview, build bool) (pa.Preview, []byte, error) {
	var p pa.Preview
	var pages []office.SelectedPDFPage
	var lines []string
	total := 0
	for i, entry := range input.args.Pages {
		f := input.files[entry.Source]
		total += len(f.data)
		if total > 48<<20 {
			return p, nil, office.ErrTooLarge
		}
		text, err := readCollectionPage(ctx, f, entry.Page)
		if err != nil {
			return p, nil, err
		}
		source := pdfPageSource(f.source, entry.Page)
		excerpt := personalLimitText(strings.Join(strings.Fields(text), " "), 120)
		details.Evidence = append(details.Evidence, collectionEvidence{File: f.source.ID, SHA256: f.hash, Page: entry.Page, Text: excerpt})
		lines = append(lines, fmt.Sprintf("%d. %s · Seite %d", i+1, path.Base(f.source.ID), entry.Page))
		p.Sources = append(p.Sources, source)
		if build {
			pages = append(pages, office.SelectedPDFPage{Data: f.data, Number: entry.Page})
		}
	}
	p.Text = strings.Join(lines, "\n")
	if !build {
		return p, nil, nil
	}
	out, err := office.SelectPDFPages(ctx, pages)
	return p, out, err
}
func offerText(f collectionFile) (string, error) {
	switch strings.ToLower(path.Ext(f.source.ID)) {
	case ".txt", ".md", ".csv", ".tsv":
		if !utf8.Valid(f.data) || strings.IndexByte(string(f.data), 0) >= 0 {
			return "", errPersonalInvalid
		}
		return string(f.data), nil
	default:
		return office.Text(f.source.ID, f.data)
	}
}
func compareOffers(ctx context.Context, input collectionInput, origin string, details *collectionPreview, build bool) (pa.Preview, []byte, error) {
	var p pa.Preview
	base, err := url.Parse(origin)
	if err != nil || base.Hostname() == "" || base.Scheme != "https" && base.Scheme != "http" || base.User != nil {
		return p, nil, errors.New("original file URL unavailable")
	}
	header := make([]any, len(offerHeaders))
	for i, h := range offerHeaders {
		header[i] = h
	}
	sheet := office.Sheet{Name: "Angebotsvergleich", Header: true, Rows: [][]any{header}}
	var links, summary []string
	for index, f := range input.files {
		readable := false
		var regions []struct {
			page int
			text string
		}
		if strings.EqualFold(path.Ext(f.source.ID), ".pdf") {
			for _, pg := range input.args.Pages {
				if pg.Source != index {
					continue
				}
				text, err := readCollectionPage(ctx, f, pg.Page)
				if err != nil {
					return p, nil, err
				}
				regions = append(regions, struct {
					page int
					text string
				}{pg.Page, text})
				p.Sources = append(p.Sources, pdfPageSource(f.source, pg.Page))
			}
		} else {
			text, err := offerText(f)
			if err != nil {
				return p, nil, err
			}
			regions = append(regions, struct {
				page int
				text string
			}{0, text})
			p.Sources = append(p.Sources, f.source)
		}
		values := make([][]string, len(header))
		for _, region := range regions {
			var evidence []string
			readable = readable || strings.TrimSpace(region.text) != ""
			for line := range strings.SplitSeq(region.text, "\n") {
				m := offerLabels.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				col := offerColumn(m[1])
				if len(values[col]) >= 6 {
					continue
				}
				quotation := personalLimitText(strings.TrimSpace(m[0]), 300)
				evidence = append(evidence, quotation)
				value := personalLimitText(strings.TrimSpace(m[2]), 220)
				if col == 2 {
					value = strings.TrimSpace(m[1]) + ": " + value
				} // retain net/gross and currency as written
				values[col] = append(values[col], value)
			}
			details.Evidence = append(details.Evidence, collectionEvidence{File: f.source.ID, SHA256: f.hash, Page: region.page, Text: strings.Join(evidence, "\n")})
		}
		row := make([]any, len(header))
		row[0] = office.LiteralCell(path.Base(f.source.ID))
		var columns []string
		for col := 1; col < len(header); col++ {
			value := strings.Join(values[col], " · ")
			if value == "" {
				value = "Nicht ausgewiesen"
				if !readable {
					value = "Kein lesbarer Quelltext"
				}
			}
			row[col] = office.LiteralCell(value)
			columns = append(columns, offerHeaders[col]+": "+value)
		}
		summary = append(summary, path.Base(f.source.ID)+"\n"+strings.Join(columns, "\n"))
		link := *base
		link.Path = "/api/v1/files/download"
		link.RawQuery = url.Values{"root": {f.root.ID}, "path": {f.source.ID}}.Encode()
		if len(regions) > 0 && regions[0].page > 0 {
			link.Fragment = fmt.Sprintf("page=%d", regions[0].page)
		}
		links = append(links, link.String())
		sheet.Rows = append(sheet.Rows, row)
	}
	p.Text = strings.Join(summary, "\n\n")
	if !build {
		return p, nil, nil
	}
	out, err := office.SourceWorkbook(sheet, links)
	return p, out, err
}
