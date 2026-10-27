package fileindex

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/office"
)

const (
	maxPlainFile  = 8 << 20
	maxPlainText  = 1 << 20
	maxOfficeFile = 32 << 20
	maxPDFFile    = 50 << 20 // the limit of office.ReadPDF
	maxPDFPages   = 200
	maxIndexText  = 2 << 20
)

var errSkip = errors.New("fileindex: not indexed")

var plainTypes = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".log": true, ".json": true, ".xml": true,
	".html": true, ".htm": true, ".yaml": true, ".yml": true, ".ini": true, ".conf": true, ".cfg": true, ".tex": true,
	".srt": true, ".vtt": true, ".rst": true, ".org": true,
}

// Indexable reports whether the text of a file of this name and size is read.
func Indexable(name string, size int64) bool {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case plainTypes[ext]:
		return size <= maxPlainFile
	case ext == ".docx" || ext == ".xlsx":
		return size <= maxOfficeFile
	case ext == ".pdf":
		return size <= maxPDFFile
	}
	return false
}

// extractor reads the text of files. The functions are replaceable in tests;
// PDF and OCR use the fixed, bounded helpers of the office package.
type extractor struct {
	pdf func(ctx context.Context, data []byte, first, last int) (office.PDFReading, error)
	ocr func(ctx context.Context, data []byte, first, last int) (office.PDFReading, error)
}

func (e extractor) text(ctx context.Context, f *os.File, name string, size int64, ocr bool) (string, error) {
	ext := strings.ToLower(path.Ext(name))
	if !Indexable(name, size) {
		return "", errSkip
	}
	if plainTypes[ext] {
		data, err := io.ReadAll(io.LimitReader(f, maxPlainText))
		if err != nil {
			return "", err
		}
		if !utf8.Valid(data) {
			// Latin-1 and friends: keep what is readable instead of nothing.
			data = []byte(strings.ToValidUTF8(string(data), " "))
		}
		return string(data), nil
	}
	data, err := io.ReadAll(io.LimitReader(f, size+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) != size {
		return "", errors.New("fileindex: file changed while reading")
	}
	if ext != ".pdf" {
		return office.Text(name, data)
	}
	var out strings.Builder
	empty, pages := true, 0
	for first := 1; first <= maxPDFPages && out.Len() < maxIndexText; first += 20 {
		reading, err := e.pdf(ctx, data, first, first+19)
		if err != nil {
			if first == 1 {
				return "", err
			}
			break // past the last page
		}
		pages += len(reading.Pages)
		for _, page := range reading.Pages {
			if page.Text != "" {
				empty = false
			}
			out.WriteString(page.Text)
			out.WriteByte('\n')
		}
		if !reading.MorePossible {
			break
		}
	}
	if empty && pages > 0 && ocr && e.ocr != nil {
		// A scan without a text layer: the first five pages, slowly.
		reading, err := e.ocr(ctx, data, 1, min(pages, 5))
		if err == nil {
			for _, page := range reading.Pages {
				out.WriteString(page.Text)
				out.WriteByte('\n')
			}
		}
	}
	return out.String(), nil
}
