package office

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestPDFReadingPageNumbers(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("poppler not installed")
	}
	data, err := PDF("# Evidence\n\nA documented test result.", "Report")
	if err != nil {
		t.Fatal(err)
	}
	r, err := ReadPDF(context.Background(), data, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pages) == 0 || r.Pages[0].Number != 1 || !strings.Contains(r.Pages[0].Text, "documented test") || r.NeedsOCR {
		t.Fatalf("%+v", r)
	}
	if _, err := ReadPDF(context.Background(), data, 1, 21); err == nil {
		t.Fatal("unbounded page request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadPDF(ctx, data, 1, 1); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestOCRUsesBoundedPagePipes(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract unavailable")
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("poppler unavailable")
	}
	data, err := PDF("# TITANOS REPORT\n\nInvoice number 12345", "Report")
	if err != nil {
		t.Fatal(err)
	}
	result, err := OCRPDF(context.Background(), data, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 1 || !result.OCR || !strings.Contains(result.Pages[0].Text, "12345") {
		t.Fatalf("unexpected OCR: %+v", result)
	}
	if _, err = OCRPDF(context.Background(), data, 1, 6); err == nil {
		t.Fatal("unbounded OCR accepted")
	}
}
