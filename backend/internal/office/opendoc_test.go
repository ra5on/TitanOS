package office

import (
	"strings"
	"testing"
)

func TestODFAndPPTXText(t *testing.T) {
	odt, err := zipFiles([][2]string{
		{"mimetype", "application/vnd.oasis.opendocument.text"},
		{"content.xml", `<?xml version="1.0"?><office:document-content xmlns:office="urn:o" xmlns:text="urn:t"><office:body><office:text><text:h>Kfz-Versicherung</text:h><text:p>Kündigung bis<text:s/>30.11.</text:p></office:text></office:body></office:document-content>`},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Text("Brief.odt", odt)
	if err != nil || got != "Kfz-Versicherung\nKündigung bis 30.11.\n" {
		t.Fatalf("odt %q %v", got, err)
	}
	pptx, err := zipFiles([][2]string{
		{"ppt/slides/slide10.xml", `<p:sld xmlns:p="urn:p" xmlns:a="urn:a"><a:p><a:r><a:t>Zehn</a:t></a:r></a:p></p:sld>`},
		{"ppt/slides/slide2.xml", `<p:sld xmlns:p="urn:p" xmlns:a="urn:a"><a:p><a:r><a:t>Zwei</a:t></a:r><a:r><a:t> Folie</a:t></a:r></a:p></p:sld>`},
		{"ppt/slides/_rels/slide2.xml.rels", `<Relationships/>`},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = Text("Vortrag.pptx", pptx)
	if err != nil || !strings.Contains(got, "Zwei Folie") || strings.Index(got, "Zwei") > strings.Index(got, "Zehn") {
		t.Fatalf("pptx %q %v", got, err)
	}
	if _, err := Text("leer.pptx", odt); err == nil {
		t.Fatal("a pptx without slides is accepted")
	}
	if _, err := Text("kaputt.odt", []byte("no zip")); err == nil {
		t.Fatal("broken odt accepted")
	}
}
