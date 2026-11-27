package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func TestPersonalOfficeCopyRightsAndRevision(t *testing.T) {
	ts := personalFixture(t)
	dir := ts.withShare()
	owner, _ := ts.users.Get("alex")
	guest, err := ts.users.Create("gast", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Brief.docx")
	original, err := office.DOCX("Absatz eins\n\nAbsatz zwei", "Brief")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"paragraph":2,"replacement":"Geändert","name":"Entwurf.docx"}`)
	req := pa.Request{Tool: "office.copy", Args: args, Sources: []pa.Source{{Kind: "file", ID: "Brief.docx", Ref: map[string]string{"root": "share/Docs"}}}}
	ctx := pa.WithAuthority(t.Context(), func(context.Context) error { return nil }, nil)
	if e := ts.server.personalOfficeAccess(ctx, guest, req); e == nil {
		t.Fatal("other user can read a private share")
	}
	preview, _, err := ts.server.planPersonalOffice(ctx, owner, req)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := office.DOCX("Absatz eins\n\nNeu", "Brief")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := ts.server.applyPersonalOffice(ctx, owner, req, preview); e == nil {
		t.Fatal("stale approval accepted")
	}
	if err = os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	revoked := pa.WithAuthority(ctx, func(context.Context) error { return errors.New("permission withdrawn") }, nil)
	if _, err := ts.server.applyPersonalOffice(revoked, owner, req, preview); !errors.Is(err, pa.ErrNoEffect) {
		t.Fatalf("revoked publication: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Entwurf.docx")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published after revocation: %v", err)
	}
	result, err := ts.server.applyPersonalOffice(ctx, owner, req, preview)
	if err != nil {
		t.Fatal(err)
	}
	if result.Link == "" || result.Text != "Geändert" {
		t.Fatal(result)
	}
	if _, err = ts.server.applyPersonalOffice(ctx, owner, req, preview); err == nil {
		t.Fatal("overwrote existing output")
	}
	b, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(b, original) {
		t.Fatal("original changed")
	}
}
func TestPersonalSearchAndWorkflowDocument(t *testing.T) {
	ts := personalFixture(t)
	dir := ts.withShare()
	u, _ := ts.users.Get("alex")
	ctx := pa.WithAuthority(t.Context(), func(context.Context) error { return nil }, nil)
	if err := os.WriteFile(filepath.Join(dir, "Beleg.txt"), []byte("Daten"), 0600); err != nil {
		t.Fatal(err)
	}
	folder := pa.Source{Kind: "folder", ID: ".", Label: "Docs", Ref: map[string]string{"root": "share/Docs"}}
	p, err := ts.server.personalFindFiles(ctx, u, pa.Request{Tool: "files.search", Prompt: "Beleg", Sources: []pa.Source{folder}})
	if err != nil || len(p.Sources) != 1 || p.Sources[0].ID != "Beleg.txt" {
		t.Fatalf("search: %+v %v", p, err)
	}
	req := pa.Request{Tool: "office.document", PreviousText: "Liefertermin 12. April", Args: json.RawMessage(`{"name":"Liefertermin.docx"}`), Sources: []pa.Source{folder}}
	preview, _, err := ts.server.planPersonalOffice(ctx, u, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ts.server.applyPersonalOffice(ctx, u, req, preview); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Liefertermin.docx"))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := office.Paragraph(b, ".docx", 0, 1, nil)
	if err != nil || got != req.PreviousText {
		t.Fatal(got, err)
	}
}
