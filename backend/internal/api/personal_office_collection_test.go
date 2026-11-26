package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// SelectPDFPages launches the running executable, exactly as titand in the image.
// Route the same fixed-purpose helper through the test executable for this fixture.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "-selected-pdf-pages" {
		if office.FilterPDFPages(context.Background(), os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func collectionFixture(t *testing.T, tool string) (*testServer, string, func(string) pa.Source) {
	t.Helper()
	ts := personalFixture(t)
	dir := ts.withShare()
	if err := os.Mkdir(filepath.Join(dir, "Ausgabe"), 0700); err != nil {
		t.Fatal(err)
	}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": map[string]string{tool: "auto"}})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]string{tool: "auto"}})
	return ts, dir, func(name string) pa.Source {
		kind := "file"
		if name == "Ausgabe" {
			kind = "folder"
		}
		return pa.Source{Kind: kind, ID: name, Ref: map[string]string{"root": "share/Docs"}}
	}
}
func proposedCollection(t *testing.T, ts *testServer, tool string, sources []pa.Source, args collectionArgs) pa.Job {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: tool, RequestID: "selected-files", Sources: sources, Args: raw})
	workOne(t, ts.server)
	j = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
	if j.Phase != "approval" || j.Preview == nil || j.Token == "" {
		t.Fatal(j)
	}
	return j
}
func approveCollection(t *testing.T, ts *testServer, j pa.Job) pa.Job {
	t.Helper()
	personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	workOne(t, ts.server)
	return personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+j.ID, nil)
}
func TestPersonalOfferComparisonLinksOriginalsAndRechecks(t *testing.T) {
	ts, dir, source := collectionFixture(t, "office.compare")
	first := "A & B.txt"
	second := "Nord.docx"
	a := []byte("Anbieter: =1+1\nGesamtpreis netto: 12,50 EUR\nLieferzeit: 2 Wochen")
	b, err := office.DOCX("Anbieter: Nord\n\nGesamtpreis brutto: 15 EUR", "Zweites Angebot")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, first), a, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, second), b, 0600); err != nil {
		t.Fatal(err)
	}
	args := collectionArgs{Name: "Vergleich.xlsx"}
	ss := []pa.Source{source(first), source("Ausgabe"), source(second)}
	j := proposedCollection(t, ts, "office.compare", ss, args)
	if !strings.Contains(j.Preview.Text, "12,50 EUR") || !strings.Contains(j.Preview.Text, "15 EUR") {
		t.Fatal(j.Preview)
	}
	a = bytes.ReplaceAll(a, []byte("12,50"), []byte("11,70"))
	if err = os.WriteFile(filepath.Join(dir, first), a, 0600); err != nil {
		t.Fatal(err)
	}
	j = approveCollection(t, ts, j)
	if j.Phase != "approval" || !strings.Contains(j.Preview.Text, "11,70 EUR") {
		t.Fatal(j)
	}
	if _, e := os.Stat(filepath.Join(dir, "Ausgabe", args.Name)); !os.IsNotExist(e) {
		t.Fatal("unapproved output exists")
	}
	j = approveCollection(t, ts, j)
	if j.Status != "done" || len(j.Receipts) != 1 {
		t.Fatal(j)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Ausgabe", args.Name))
	if err != nil {
		t.Fatal(err)
	}
	text, err := office.XLSXText(data)
	if err != nil || !strings.Contains(text, "11,70 EUR") || !strings.Contains(text, "Nicht ausgewiesen") {
		t.Fatal(text, err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	part := func(name string) []byte {
		f, e := z.Open(name)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		raw, e := io.ReadAll(f)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	xmlSheet := string(part("xl/worksheets/sheet1.xml"))
	if strings.Contains(xmlSheet, "<f>") || !strings.Contains(xmlSheet, `<hyperlink ref="A2"`) {
		t.Fatal("unsafe cell or missing native link", xmlSheet)
	}
	var rels struct {
		Entries []struct {
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(part("xl/worksheets/_rels/sheet1.xml.rels"), &rels); err != nil || len(rels.Entries) != 2 {
		t.Fatal(rels, err)
	}
	for i, rel := range rels.Entries {
		original := [][]byte{a, b}[i]
		u, err := url.Parse(rel.Target)
		if err != nil || u.Scheme+"://"+u.Host != ts.url {
			t.Fatal(rel, err)
		}
		resp, err := ts.client.Get(u.String())
		if err != nil {
			t.Fatal(err)
		}
		download, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || !bytes.Equal(download, original) {
			t.Fatal("link does not open the original", u, resp.StatusCode, err)
		}
	}
	// The target exists now. A fresh run must leave its workbook unchanged.
	req := j.Request
	req.ChatID = ""
	req.RequestID = "new-comparison"
	again := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	workOne(t, ts.server)
	again = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+again.ID, nil)
	got, _ := os.ReadFile(filepath.Join(dir, "Ausgabe", args.Name))
	if again.Status != "failed" || !bytes.Equal(got, data) {
		t.Fatal(again)
	}
}
func TestPersonalPDFMergeKeepsSelectedPageOrder(t *testing.T) {
	for _, name := range []string{"pdftotext", "pdfunite", "pdfseparate"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " unavailable")
		}
	}
	ts, dir, source := collectionFixture(t, "office.pdf-merge")
	files := []string{"A.pdf", "B.pdf"}
	for i, name := range files {
		data, err := office.PDF("Inhalt "+name, []string{"Quelle Alpha", "Quelle Beta"}[i])
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	j := proposedCollection(t, ts, "office.pdf-merge", []pa.Source{source("A.pdf"), source("B.pdf"), source("Ausgabe")}, collectionArgs{Name: "Seiten.pdf", Pages: []personalPage{{1, 1}, {0, 1}}})
	if !strings.Contains(j.Preview.Text, "1. B.pdf") || j.Preview.Sources[0].Ref["page"] != "1" {
		t.Fatal(j.Preview)
	}
	j = approveCollection(t, ts, j)
	if j.Status != "done" {
		t.Fatal(j)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Ausgabe", "Seiten.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := office.ReadPDF(t.Context(), data, 1, 2)
	if err != nil || len(got.Pages) != 2 || !strings.Contains(got.Pages[0].Text, "B.pdf") || !strings.Contains(got.Pages[1].Text, "A.pdf") {
		t.Fatal(got, err)
	}
}

func TestPersonalOfficeCollectionRechecksNativeShare(t *testing.T) {
	ts, dir, src := collectionFixture(t, "office.compare")
	_, err := ts.users.Create("bea", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	res, _ := ts.do(guest, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	setShare := func(grants map[string]string) {
		res, b := ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"name": "Docs", "volume": "tank", "smb": true, "access": grants})
		if res.StatusCode != 204 {
			t.Fatalf("grant: %d %s", res.StatusCode, b)
		}
	}
	setShare(map[string]string{"alex": "rw", "bea": "rw"})
	for _, n := range []string{"1.txt", "2.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("Gesamtpreis: 12 EUR"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/bea", map[string]any{"rights": map[string]string{"office.compare": "auto"}})
	personalHTTP[pa.State](t, ts, guest, "PUT", "/settings", map[string]any{"rights": map[string]string{"office.compare": "auto"}})
	j := personalHTTP[pa.Job](t, ts, guest, "POST", "/jobs", pa.Request{Tool: "office.compare", RequestID: "native-share", Args: json.RawMessage(`{"name":"Kopie.xlsx"}`), Sources: []pa.Source{src("1.txt"), src("2.txt"), src("Ausgabe")}})
	workOne(t, ts.server)
	j = personalHTTP[pa.Job](t, ts, guest, "GET", "/jobs/"+j.ID, nil)
	if j.Phase != "approval" || j.Token == "" {
		t.Fatal(j)
	}
	setShare(map[string]string{"alex": "rw"})
	after := personalHTTP[pa.Job](t, ts, guest, "GET", "/jobs/"+j.ID, nil)
	if after.Preview != nil || len(after.Request.Sources) != 0 {
		t.Fatal(after)
	}
	res, _ = ts.do(guest, "POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	setShare(map[string]string{"alex": "rw", "bea": "rw"})
	res, _ = ts.do(guest, "POST", personalBase+"/jobs/"+j.ID+"/approve", map[string]string{"token": j.Token})
	if res.StatusCode != 409 {
		t.Fatal(res.StatusCode)
	}
	if _, e := os.Stat(filepath.Join(dir, "Ausgabe", "Kopie.xlsx")); !os.IsNotExist(e) {
		t.Fatal("revoked result was published")
	}
}
