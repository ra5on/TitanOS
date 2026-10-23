package office

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type PDFPage struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}
type PDFReading struct {
	OCR           bool      `json:"ocr,omitempty"`
	Pages         []PDFPage `json:"pages"`
	First         int       `json:"first"`
	LastRequested int       `json:"lastRequested"`
	MorePossible  bool      `json:"morePossible"`
	NeedsOCR      bool      `json:"needsOCR"`
}

// OCRPDF explicitly recognizes at most five pages. Rendering and recognition
// use bounded RAM pipes, with no temporary files in the root overlay.
func OCRPDF(ctx context.Context, data []byte, first, last int) (PDFReading, error) {
	out := PDFReading{OCR: true, First: first, LastRequested: last, Pages: []PDFPage{}, MorePossible: true}
	if first < 1 || last < first || last-first >= 5 || last > 10000 || len(data) > 50<<20 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return out, errors.New("OCR: at most five pages and 50 MiB")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for page := first; page <= last; page++ {
		png := &limitedOutput{limit: 12 << 20}
		render := exec.CommandContext(ctx, "pdftoppm", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-scale-to", "1800", "-singlefile", "-png", "-")
		render.Stdin = bytes.NewReader(data)
		render.Stdout = png
		render.Stderr = &limitedOutput{limit: 4096}
		if err := render.Run(); err != nil {
			return out, fmt.Errorf("OCR render page %d: %w", page, err)
		}
		text := &limitedOutput{limit: MaxText / 5}
		cmd := exec.CommandContext(ctx, "tesseract", "stdin", "stdout", "-l", "deu+eng")
		cmd.Stdin = bytes.NewReader(png.Bytes())
		cmd.Stdout = text
		cmd.Stderr = &limitedOutput{limit: 4096}
		if err := cmd.Run(); err != nil {
			return out, fmt.Errorf("OCR page %d: %w", page, err)
		}
		out.Pages = append(out.Pages, PDFPage{page, strings.TrimSpace(text.String())})
	}
	return out, nil
}

type limitedOutput struct {
	bytes.Buffer
	limit int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("document output limit exceeded")
	}
	return b.Buffer.Write(p)
}

// ReadPDF uses fixed arguments and stdin, never a model-provided command or
// host path. Pages are bounded and source page numbers survive extraction.
func ReadPDF(ctx context.Context, data []byte, first, last int) (PDFReading, error) {
	out := PDFReading{Pages: []PDFPage{}, First: first, LastRequested: last}
	if first < 1 || last < first || last-first >= 20 || last > 10000 || len(data) > 50<<20 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return out, errors.New("PDF: invalid page range or input (20 pages / 50 MiB maximum)")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftotext", "-f", strconv.Itoa(first), "-l", strconv.Itoa(last), "-layout", "-enc", "UTF-8", "-", "-")
	cmd.Stdin = bytes.NewReader(data)
	stdout, stderr := &limitedOutput{limit: MaxText}, &limitedOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return out, fmt.Errorf("PDF extraction failed: %w", err)
	}
	parts := strings.Split(stdout.String(), "\f")
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	out.NeedsOCR = true
	for i, text := range parts {
		if first+i > last {
			break
		}
		text = strings.TrimSpace(text)
		out.Pages = append(out.Pages, PDFPage{first + i, text})
		if text != "" {
			out.NeedsOCR = false
		}
	}
	out.MorePossible = len(out.Pages) == last-first+1
	return out, nil
}
