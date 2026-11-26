package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

type personalOfficeArgs struct {
	Paragraph   int     `json:"paragraph"`
	Slide       int     `json:"slide"`
	Page        int     `json:"page"`
	Sheet       string  `json:"sheet"`
	Range       string  `json:"range"`
	Replacement *string `json:"replacement"`
	CellType    string  `json:"cellType"`
	Name        string  `json:"name"`
	Text        string  `json:"text"`
}

type personalDocument struct {
	App  string `json:"app"`
	Root string `json:"root"`
	Path string `json:"path"`
}

type personalOfficeRegion struct {
	Region     string            `json:"region"`
	Before     string            `json:"before"`
	After      string            `json:"after"`
	SourceHash string            `json:"sourceHash,omitempty"`
	Document   *personalDocument `json:"document,omitempty"`
}

func personalWriter(tool string) bool {
	return tool == "office.table" || tool == "office.document" || tool == "office.deck"
}

func personalNativeOffice(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".docx":
		return "t-word"
	case ".xlsx":
		return "t-exel"
	case ".pdf":
		return "t-pdf"
	case ".pptx":
		return "t-deck"
	}
	return ""
}

func (s *Server) personalFileRoot(user auth.User, src pa.Source, write bool) (files.Root, error) {
	rootID := src.Ref["root"]
	if rootID == "" || src.ID == "" || src.ID != path.Clean(src.ID) || (src.Kind != "file" && src.Kind != "folder") {
		return files.Root{}, errors.New("wähle eine Datei oder einen Ordner auf diesem NAS")
	}
	root, err := s.catalogRoot(user, rootID)
	if err != nil || (write && !root.Writable) {
		return files.Root{}, errors.New("kein Zugriff auf den ausgewählten Speicherort")
	}
	if src.Kind != "folder" || src.ID != "." {
		if _, err = s.fileTarget(user, rootID, src.ID, write); err != nil {
			return files.Root{}, errors.New("kein Zugriff auf die Auswahl")
		}
	}

	return root, nil
}

func (s *Server) personalOfficeAccess(_ context.Context, user auth.User, r pa.Request) error {
	if s.opts.Files == nil {
		return errors.New("kein Zugriff auf Dateien")
	}
	if len(r.Sources) != 1 {
		return errors.New("wähle genau eine Quelle")
	}
	src := r.Sources[0]
	write := r.Tool != "office.inspect"
	if personalWriter(r.Tool) {
		if src.Kind != "folder" {
			return errors.New("wähle einen Zielordner für die neue Datei")
		}
	} else if src.Kind != "file" || personalNativeOffice(src.ID) == "" {
		return errors.New("wähle eine DOCX-, XLSX-, PDF- oder PPTX-Datei")
	}
	_, err := s.personalFileRoot(user, src, write)
	return err
}

// planPersonalOffice derives both the preview and the bytes from the explicitly
// selected source. The broker calls it again on approval; no prior binary is trusted.
func (s *Server) planPersonalOffice(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, []byte, error) {
	var a personalOfficeArgs
	var preview pa.Preview
	if len(r.Args) > 0 {
		if err := toolcatalog.DecodeArgs(r.Args, &a); err != nil {
			return preview, nil, err
		}
	}
	if err := s.personalOfficeAccess(ctx, u, r); err != nil {
		return preview, nil, err
	}
	src := r.Sources[0]
	root, err := s.personalFileRoot(u, src, r.Tool != "office.inspect")
	if err != nil {
		return preview, nil, err
	}
	var region personalOfficeRegion
	var out []byte
	var dir, ext string
	if personalWriter(r.Tool) {
		// Only the text of the explicitly configured prior workflow step can
		// enter this worksheet. Source text cannot choose a destination or formula.
		content := a.Text
		if r.PreviousText != "" {
			content = r.PreviousText
		}
		if strings.TrimSpace(content) == "" || len(content) > 32768 {
			return preview, nil, errors.New("für den Entwurf ist ein Text mit höchstens 32 KiB erforderlich")
		}
		if _, err = s.opts.Files.List(root, src.ID); err != nil {
			return preview, nil, errors.New("der gewählte Zielordner ist nicht verfügbar")
		}
		dir = src.ID
		region.After = content
		region.Region = "Entwurf"
		switch r.Tool {
		case "office.document":
			ext = ".docx"
			out, err = office.DOCX(content, strings.TrimSuffix(a.Name, path.Ext(a.Name)))
		case "office.deck":
			ext = ".pptx"
			region.Region = "Gliederung"
			out, err = office.OutlineDeck(content)
		case "office.table":
			ext = ".xlsx"
			rows := make([][]any, 0)
			for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
				var row []any
				for _, cell := range strings.Split(line, "\t") {
					row = append(row, office.LiteralCell(cell))
				}
				rows = append(rows, row)
			}
			out, err = office.XLSX([]office.Sheet{{Name: "Entwurf", Rows: rows}})
		}
		if err != nil {
			return preview, nil, err
		}

	} else {
		data, e := s.readEditable(root, src.ID)
		if e != nil {
			return preview, nil, errors.New("die Quelldatei ist nicht lesbar oder zu groß")
		}
		hash := sha256.Sum256(data)
		region.SourceHash = hex.EncodeToString(hash[:])
		ext = strings.ToLower(path.Ext(src.ID))
		dir = path.Dir(src.ID)
		change := a.Replacement
		if r.Tool == "office.inspect" {
			change = nil
		} else if ext != ".pdf" && change == nil {
			return preview, nil, errors.New("gib die gewünschte Änderung ein")
		}
		switch ext {
		case ".docx", ".pptx":
			if a.Paragraph < 1 {
				return preview, nil, errors.New("wähle eine Absatznummer")
			}
			region.Region = fmt.Sprintf("Absatz %d", a.Paragraph)
			if ext == ".pptx" {
				region.Region = fmt.Sprintf("Folie %d · Absatz %d", a.Slide, a.Paragraph)
			}
			region.Before, out, err = office.Paragraph(data, ext, a.Slide, a.Paragraph, change)
		case ".xlsx":
			region.Region = a.Sheet + " · " + a.Range
			region.Before, out, err = office.CellRange(data, a.Sheet, a.Range, change, a.CellType)
		case ".pdf":
			region.Region = fmt.Sprintf("Seite %d", a.Page)
			var reading office.PDFReading
			reading, err = office.ReadPDF(ctx, data, a.Page, a.Page)
			if err == nil && len(reading.Pages) != 1 {
				err = errors.New("gewählte Seite fehlt")
			}
			if err == nil {
				region.Before = reading.Pages[0].Text
				if r.Tool == "office.copy" {
					out, err = office.SelectPDFPages(ctx, []office.SelectedPDFPage{{Data: data, Number: a.Page}})
					region.After = region.Before
				}
			}
		}
		if err != nil {
			return preview, nil, err
		}
		if change != nil {
			region.After = *change
		}
	}
	preview.Title = region.Region
	if r.Tool != "office.inspect" {
		if a.Name == "" || len(a.Name) > 200 || a.Name != path.Base(a.Name) || strings.ContainsAny(a.Name, "\\\x00\r\n") || strings.HasPrefix(a.Name, ".") || !strings.EqualFold(path.Ext(a.Name), ext) {
			return preview, nil, errors.New("gib einen neuen Dateinamen mit passender Endung an")
		}
		target := path.Join(dir, a.Name)
		if _, err = s.fileTarget(u, root.ID, target, true); err != nil {
			return preview, nil, err
		}
		f, _, openErr := s.opts.Files.Open(root, target)
		if openErr == nil {
			_ = f.Close()
			return preview, nil, errors.New("eine Datei mit diesem Namen ist bereits vorhanden")
		}
		if !errors.Is(openErr, files.ErrNotFound) {
			return preview, nil, errors.New("der Zielname ist nicht verfügbar")
		}
		region.Document = &personalDocument{App: personalNativeOffice(a.Name), Root: root.ID, Path: target}
		preview.Title = "Neue Datei: " + a.Name
		preview.Text = region.After
	} else {
		preview.Text = region.Before
	}
	preview.Data, err = json.Marshal(region)
	preview.Sources = []pa.Source{src}
	return preview, out, err
}

func (s *Server) applyPersonalOffice(ctx context.Context, u auth.User, r pa.Request, approved pa.Preview) (pa.Result, error) {
	var result pa.Result
	p, out, err := s.planPersonalOffice(ctx, u, r)
	if err != nil {
		return result, err
	}
	if string(p.Data) != string(approved.Data) {
		return result, errors.New("die Quelle oder die Vorschau hat sich geändert")
	}
	var selection personalOfficeRegion
	if err = json.Unmarshal(p.Data, &selection); err != nil || selection.Document == nil {
		return result, errors.New("das Ausgabeziel fehlt")
	}
	dst := selection.Document
	root, err := s.personalFileRoot(u, r.Sources[0], true)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = s.opts.Files.WriteFileChecked(root, dst.Path, out, u.UID, false, func() error {
		return pa.Reauthorize(ctx)
	}); err != nil {
		return result, err
	}
	// Verify the published object through the native account-visible file path.
	stored, err := s.readEditable(root, dst.Path)
	if err != nil || sha256.Sum256(stored) != sha256.Sum256(out) {
		return result, errors.New("die neue Datei konnte nicht bestätigt werden")
	}
	result = pa.Result{Title: "Datei erstellt: " + path.Base(dst.Path), Text: p.Text, Data: p.Data, Sources: p.Sources, Link: "/files?" + url.Values{"root": {root.ID}, "path": {path.Dir(dst.Path)}}.Encode()}
	return result, nil
}

func (s *Server) readPersonalFiles(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
	p := pa.Preview{Title: "Ausgewählte Dateien", Sources: r.Sources}
	var output strings.Builder
	for _, src := range r.Sources {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		root, err := s.personalFileRoot(u, src, false)
		if err != nil {
			return p, err
		}
		data, err := s.readEditable(root, src.ID)
		if err != nil {
			return p, errors.New("die gewählte Datei ist nicht lesbar")
		}
		var value string
		switch strings.ToLower(path.Ext(src.ID)) {
		case ".pdf":
			doc, e := office.ReadPDF(ctx, data, 1, 1)
			if e != nil {
				return p, e
			}
			if len(doc.Pages) > 0 {
				value = doc.Pages[0].Text
			}
			value = "Seite 1\n" + value
		case ".txt", ".md", ".csv", ".tsv":
			if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				return p, errors.New("die Auswahl enthält keinen lesbaren Text")
			}
			value = string(data)
		default:
			value, err = office.Text(src.ID, data)
			if err != nil {
				return p, err
			}
		}
		if output.Len() > 0 {
			output.WriteString("\n\n")
		}
		output.WriteString(path.Base(src.ID) + "\n")
		const maxExcerpt = 2048
		rs := []rune(value)
		if len(rs) > maxExcerpt {
			value = string(rs[:maxExcerpt]) + " …"
		}
		output.WriteString(value)
	}
	p.Text = output.String()
	return p, nil
}
