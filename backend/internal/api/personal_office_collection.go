package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

type personalPage struct {
	Source int `json:"source"`
	Page   int `json:"page"`
}
type collectionArgs struct {
	Name  string         `json:"name"`
	Pages []personalPage `json:"pages"`
}
type collectionFile struct {
	source pa.Source
	root   files.Root
	data   []byte
	hash   string
}
type collectionInput struct {
	folder pa.Source
	root   files.Root
	files  []collectionFile
	args   collectionArgs
}
type collectionEvidence struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Page   int    `json:"page,omitempty"`
	Text   string `json:"text,omitempty"`
}
type collectionPreview struct {
	Document personalDocument     `json:"document"`
	Evidence []collectionEvidence `json:"evidence"`
}

func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		var tools []personalTool
		for _, entry := range []struct{ id, title string }{{"office.compare", "Angebote als Tabelle vergleichen"}, {"office.pdf-merge", "Ausgewählte PDF-Seiten zusammenführen"}} {
			tools = append(tools, personalTool{ID: entry.id, Title: entry.title, App: "files", SourceKinds: []string{"file", "folder"}, Default: pa.Ask, Sensitive: true,
				Check: func(_ context.Context, u auth.User, r pa.Request) error {
					_, err := s.collectionInput(u, r)
					return err
				},
				Prepare: func(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
					p, _, _, err := s.planCollection(ctx, u, r, false)
					return p, err
				},
				Apply: s.applyCollection,
			})
		}
		return tools
	})
}
func (s *Server) collectionInput(u auth.User, r pa.Request) (collectionInput, error) {
	var input collectionInput
	if s.opts.Files == nil {
		return input, errPersonalDenied
	}
	if err := toolcatalog.DecodeArgs(r.Args, &input.args); err != nil {
		return input, err
	}
	ext := ".xlsx"
	if r.Tool == "office.pdf-merge" {
		ext = ".pdf"
	}
	name := input.args.Name
	if name == "" || len(name) > 200 || name != path.Base(name) || strings.Contains(name, "\\") || strings.HasPrefix(name, ".") || strings.ContainsFunc(name, unicode.IsControl) || !strings.EqualFold(path.Ext(name), ext) {
		return input, errPersonalInvalid
	}
	seen := map[[2]string]bool{}
	for _, src := range r.Sources {
		key := [2]string{src.Ref["root"], src.ID}
		if seen[key] {
			return input, errPersonalInvalid
		}
		seen[key] = true
		root, err := s.personalFileRoot(u, src, src.Kind == "folder")
		if err != nil {
			return input, err
		}
		switch src.Kind {
		case "folder":
			if input.folder.ID != "" {
				return input, errPersonalInvalid
			}
			input.folder, input.root = src, root
		case "file":
			srcExt := strings.ToLower(path.Ext(src.ID))
			if r.Tool == "office.pdf-merge" && srcExt != ".pdf" || !slices.Contains([]string{".pdf", ".docx", ".xlsx", ".txt", ".md", ".csv", ".tsv"}, srcExt) {
				return input, errPersonalInvalid
			}
			input.files = append(input.files, collectionFile{source: src, root: root})
		default:
			return input, errPersonalInvalid
		}
	}
	if input.folder.ID == "" || len(input.files) < 2 || len(input.files) > 8 || len(input.args.Pages) > 20 {
		return input, errPersonalInvalid
	}
	selected := map[int]bool{}
	seenPage := map[personalPage]bool{}
	for _, p := range input.args.Pages {
		if p.Source < 0 || p.Source >= len(input.files) || p.Page < 1 || p.Page > 10000 || seenPage[p] || !strings.EqualFold(path.Ext(input.files[p.Source].source.ID), ".pdf") {
			return input, errPersonalInvalid
		}
		selected[p.Source] = true
		seenPage[p] = true
	}
	for i, f := range input.files {
		if strings.EqualFold(path.Ext(f.source.ID), ".pdf") && !selected[i] {
			return input, errPersonalInvalid
		}
	}
	return input, nil
}
func (s *Server) collectionBytes(root files.Root, path string, allowance int) ([]byte, error) {
	f, st, err := s.opts.Files.Open(root, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st.Size() > int64(allowance) {
		return nil, office.ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(allowance)+1))
	if len(data) > allowance {
		return nil, office.ErrTooLarge
	}
	return data, err
}

func (s *Server) planCollection(ctx context.Context, u auth.User, r pa.Request, build bool) (pa.Preview, []byte, collectionInput, error) {
	var p pa.Preview
	input, err := s.collectionInput(u, r)
	if err != nil {
		return p, nil, input, err
	}
	dst := path.Join(input.folder.ID, input.args.Name)
	if _, err := s.fileTarget(u, input.root.ID, dst, true); err != nil {
		return p, nil, input, err
	}
	if _, err := s.opts.Files.List(input.root, input.folder.ID); err != nil {
		return p, nil, input, err
	}
	f, _, err := s.opts.Files.Open(input.root, dst)
	if err == nil {
		_ = f.Close()
		return p, nil, input, files.ErrExists
	}
	if !errors.Is(err, files.ErrNotFound) {
		return p, nil, input, err
	}
	total := 0
	for i, item := range input.files {
		if err := ctx.Err(); err != nil {
			return p, nil, input, err
		}
		data, err := s.collectionBytes(item.root, item.source.ID, (32<<20)-total)
		if err != nil {
			return p, nil, input, err
		}
		total += len(data)
		if total > 32<<20 {
			return p, nil, input, office.ErrTooLarge
		}
		input.files[i].data = data
		sum := sha256.Sum256(data)
		input.files[i].hash = hex.EncodeToString(sum[:])
	}
	details := collectionPreview{Document: personalDocument{App: personalNativeOffice(dst), Root: input.root.ID, Path: dst}}
	var out []byte
	if r.Tool == "office.compare" {
		p, out, err = compareOffers(ctx, input, r.Origin, &details, build)
	} else {
		p, out, err = mergeSelectedPDFs(ctx, input, &details, build)
	}
	if err != nil {
		return p, nil, input, err
	}
	p.Title = "Neue Datei: " + input.args.Name
	p.Data, err = json.Marshal(details)
	p.Sources = append(p.Sources, input.folder)
	return p, out, input, err
}
func (s *Server) applyCollection(ctx context.Context, u auth.User, r pa.Request, approved pa.Preview) (pa.Result, error) {
	var result pa.Result
	p, out, input, err := s.planCollection(ctx, u, r, true)
	if err != nil || personalDigest(p) != personalDigest(approved) {
		return result, pa.ErrNoEffect
	}
	var details collectionPreview
	if err := json.Unmarshal(p.Data, &details); err != nil {
		return result, pa.ErrNoEffect
	}
	// Building a large PDF may take time. Reconfirm all grants and file identities
	// immediately before publication, through the native account-visible paths.
	for _, item := range input.files {
		root, err := s.personalFileRoot(u, item.source, false)
		if err != nil {
			return result, pa.ErrNoEffect
		}
		current, err := s.collectionBytes(root, item.source.ID, len(item.data))
		if err != nil || sha256.Sum256(current) != sha256.Sum256(item.data) {
			return result, pa.ErrNoEffect
		}
	}
	root, err := s.personalFileRoot(u, input.folder, true)
	if err != nil || ctx.Err() != nil {
		return result, pa.ErrNoEffect
	}
	if err = s.opts.Files.WriteFileChecked(root, details.Document.Path, out, u.UID, false, s.personalOfficePublish(ctx)); err != nil {
		if errors.Is(err, files.ErrExists) {
			return result, pa.ErrNoEffect
		}
		return result, err
	}
	actual, err := s.readEditable(root, details.Document.Path)
	if err != nil || sha256.Sum256(actual) != sha256.Sum256(out) {
		return result, errors.New("unverified output file")
	}
	return pa.Result{Title: "Datei erstellt: " + input.args.Name, Text: p.Text, Sources: p.Sources, Data: p.Data, Link: "/files?" + url.Values{"root": {root.ID}, "path": {input.folder.ID}}.Encode()}, nil
}
