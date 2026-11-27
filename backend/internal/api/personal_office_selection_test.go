package api

import (
	"context"
	"encoding/json"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPersonalNativeOfficeSelectionIsBoundToRevisionAndRegions(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	owner, _ := ts.users.Get("alex")
	dir := ts.withShare()
	guest, err := ts.users.Create("gast", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	ctx := pa.WithAuthority(t.Context(), func(context.Context) error { return nil }, nil)
	makeSource := func(name string, data []byte, fields map[string]string) pa.Source {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		root, err := s.catalogRoot(owner, "share/Docs")
		if err != nil {
			t.Fatal(err)
		}
		info, err := s.opts.Files.Properties(ctx, root, name, false)
		if err != nil {
			t.Fatal(err)
		}
		fields["root"] = root.ID
		fields["mtime"] = strconv.FormatInt(info.Modified.UnixNano(), 10)
		fields["version"] = info.Version
		return pa.Source{Kind: officeRegionKind, ID: name, Ref: fields}
	}
	b, err := office.DOCX("Eins\n\nUngenannter Absatz\n\nDrei", "")
	if err != nil {
		t.Fatal(err)
	}
	first := makeSource("Auswahl.docx", b, map[string]string{"paragraph": "3"})
	last := first
	last.Ref = map[string]string{}
	for k, v := range first.Ref {
		last.Ref[k] = v
	}
	last.Ref["paragraph"] = "1"
	req := pa.Request{Tool: "office.inspect", Sources: []pa.Source{first, last}, RequestID: "native-region"}
	personalHTTP[map[string]any](t, ts, ts.client, "PUT", "/limits/alex", map[string]any{"rights": map[string]pa.Level{"office.inspect": pa.Auto}})
	personalHTTP[pa.State](t, ts, ts.client, "PUT", "/settings", map[string]any{"rights": map[string]pa.Level{"office.inspect": pa.Auto}})
	res, body := ts.request("POST", personalBase+"/contexts", map[string]any{"sources": req.Sources, "tool": req.Tool})
	var handoff map[string]string
	if res.StatusCode != 201 || json.Unmarshal(body, &handoff) != nil {
		t.Fatalf("handoff: %d %s", res.StatusCode, body)
	}
	got := personalHTTP[personalContext](t, ts, ts.client, "POST", "/contexts/"+handoff["id"]+"/consume", map[string]any{})
	req.Sources = got.Sources
	job := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", req)
	workOne(t, s)
	job = personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+job.ID, nil)
	if job.Status != "done" || job.Result == nil || job.Result.Text != "Absatz 3\nDrei\n\nAbsatz 1\nEins" || len(job.Result.Sources) != 2 {
		t.Fatalf("genuine job: %+v", job)
	}
	if e := s.personalOfficeAccess(ctx, guest, req); e == nil {
		t.Fatal("foreign share accepted")
	}
	if _, e := s.personal.tools["files.read"].Prepare(ctx, owner, req); e == nil {
		// The read tool's native Check must deny this source before preparation.
		t.Fatal("bound editor selection accepted as complete file")
	}
	req.Args = json.RawMessage(`{"paragraph":2}`)
	if _, _, e := s.planPersonalOffice(ctx, owner, req); e == nil {
		t.Fatal("request replaced editor coordinates")
	}
	req.Args = nil
	copyReq := pa.Request{Tool: "office.copy", Sources: []pa.Source{last}, Args: json.RawMessage(`{"name":"Kopie.docx","replacement":"Geändert"}`)}
	preview, _, err := s.planPersonalOffice(ctx, owner, copyReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.applyPersonalOffice(ctx, owner, copyReq, preview); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "Kopie.docx"))
	v, _, err := office.Paragraph(out, ".docx", 0, 1, nil)
	if err != nil || v != "Geändert" {
		t.Fatal(v, err)
	}
	original := filepath.Join(dir, first.ID)
	info, _ := os.Stat(original)
	altered, _ := office.DOCX("Andere Datei", "")
	if err = os.WriteFile(original, altered, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(original, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, _, e := s.planPersonalOffice(ctx, owner, req); e == nil {
		t.Fatal("stale version with preserved mtime accepted")
	}
	redacted := personalHTTP[pa.Job](t, ts, ts.client, "GET", "/jobs/"+job.ID, nil)
	if redacted.Result != nil {
		t.Fatal("stale result remained visible")
	}
	deck, _ := office.OutlineDeck("Erste\nNicht ausgewählt\n\nZweite\nGewählter Text")
	slide := makeSource("Präsentation.pptx", deck, map[string]string{"slide": "2"})
	req.Sources = []pa.Source{slide}
	preview, _, err = s.planPersonalOffice(ctx, owner, req)
	if err != nil || !strings.Contains(preview.Text, "Gewählter Text") || strings.Contains(preview.Text, "Nicht ausgewählt") {
		t.Fatal(preview.Text, err)
	}
	sheetData, _ := office.XLSX([]office.Sheet{{Name: "Planung", Rows: [][]any{{"Vertraulich", "Links", "Rechts"}}}})
	sheet := makeSource("Budget.xlsx", sheetData, map[string]string{"sheet": "Planung", "range": "B1:C1"})
	req.Sources = []pa.Source{sheet}
	preview, _, err = s.planPersonalOffice(ctx, owner, req)
	if err != nil || !strings.Contains(preview.Text, "Links") || strings.Contains(preview.Text, "Vertraulich") {
		t.Fatal(preview.Text, err)
	}
	req.Sources = []pa.Source{slide, sheet}
	if _, _, e := s.planPersonalOffice(ctx, owner, req); e == nil {
		t.Fatal("mixed source files accepted")
	}
}

func TestPersonalNativePDFRegionsReadInEditorOrder(t *testing.T) {
	for _, cmd := range []string{"pdfunite", "pdfseparate", "pdftotext", "pdfinfo"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Skip(cmd)
		}
	}
	ctx := t.Context()
	ts := personalFixture(t)
	s := ts.server
	owner, _ := ts.users.Get("alex")
	dir := ts.withShare()
	var pages []office.SelectedPDFPage
	for _, label := range []string{"Erster Marker", "Privater Mittelteil", "Letzter Marker"} {
		raw, err := office.PDF(label, label)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, office.SelectedPDFPage{Data: raw, Number: 1})
	}
	data, err := office.SelectPDFPages(ctx, pages)
	if err != nil {
		t.Fatal(err)
	}
	name := "Test.pdf"
	if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := s.catalogRoot(owner, "share/Docs")
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.opts.Files.Properties(ctx, root, name, false)
	if err != nil {
		t.Fatal(err)
	}
	req := pa.Request{Tool: "office.inspect"}
	for _, page := range []string{"3", "1"} {
		req.Sources = append(req.Sources, pa.Source{Kind: officeRegionKind, ID: name, Ref: map[string]string{"root": root.ID, "version": info.Version, "mtime": strconv.FormatInt(info.Modified.UnixNano(), 10), "page": page}})
	}
	p, _, err := s.planPersonalOffice(ctx, owner, req)
	if err != nil || strings.Contains(p.Text, "Privater") || strings.Index(p.Text, "Letzter Marker") < 0 || strings.Index(p.Text, "Erster Marker") < strings.Index(p.Text, "Letzter Marker") {
		t.Fatal(p.Text, err)
	}
}
