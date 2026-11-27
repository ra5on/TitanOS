package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

const officeRegionKind = "office.region"

var errOfficeSelection = errors.New("wähle die Bereiche in derselben gespeicherten Datei erneut aus")

func editorRegion(src pa.Source) (personalOfficeArgs, error) {
	var out personalOfficeArgs
	var allowed []string
	switch personalNativeOffice(src.ID) {
	case "t-word":
		allowed = []string{"paragraph"}
	case "t-exel":
		allowed = []string{"sheet", "range"}
	case "t-pdf":
		allowed = []string{"page"}
	case "t-deck":
		allowed = []string{"slide"}
		if _, ok := src.Ref["paragraph"]; ok {
			allowed = append(allowed, "paragraph")
		}
	default:
		return out, errOfficeSelection
	}
	if len(src.Ref) != len(allowed)+3 {
		return out, errOfficeSelection
	}
	for _, field := range allowed {
		value := src.Ref[field]
		if value == "" {
			return out, errOfficeSelection
		}
		switch field {
		case "sheet":
			if len([]rune(value)) > 128 {
				return out, errOfficeSelection
			}
			out.Sheet = value
		case "range":
			if len(value) > 24 {
				return out, errOfficeSelection
			}
			out.Range = value
		default:
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100000 || strconv.Itoa(n) != value {
				return out, errOfficeSelection
			}
			switch field {
			case "slide":
				out.Slide = n
			case "paragraph":
				out.Paragraph = n
			case "page":
				out.Page = n
			}
		}
	}
	return out, nil
}

func (s *Server) editorOfficeRoot(ctx context.Context, u auth.User, r pa.Request) (files.Root, error) {
	if s.opts.Files == nil || len(r.Sources) == 0 || len(r.Sources) > 16 || (r.Tool != "office.inspect" && !(r.Tool == "office.copy" && len(r.Sources) == 1)) {
		return files.Root{}, errOfficeSelection
	}
	first := r.Sources[0]
	for _, src := range r.Sources {
		if src.Kind != officeRegionKind || src.ID != first.ID || src.Ref["root"] != first.Ref["root"] || src.Ref["mtime"] != first.Ref["mtime"] || src.Ref["version"] != first.Ref["version"] {
			return files.Root{}, errOfficeSelection
		}
		where, err := editorRegion(src)
		if err != nil {
			return files.Root{}, err
		}
		if r.Tool == "office.copy" && where.Slide > 0 && where.Paragraph == 0 {
			return files.Root{}, errOfficeSelection
		}
	}
	first.Kind = "file"
	root, err := s.personalFileRoot(u, first, r.Tool == "office.copy")
	if err != nil {
		return root, err
	}
	info, err := s.opts.Files.Properties(ctx, root, first.ID, false)
	if err != nil || info.Version == "" || info.Version != first.Ref["version"] || strconv.FormatInt(info.Modified.UnixNano(), 10) != first.Ref["mtime"] {
		return root, errOfficeSelection
	}
	return root, nil
}

// A restricted source grants the explicit coordinates in its ref. Job arguments
// may describe a new copy, but can never replace the original coordinates.
func (s *Server) planOfficeSelection(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, []byte, error) {
	var empty pa.Preview
	var edit struct {
		Name        string  `json:"name"`
		Replacement *string `json:"replacement"`
		CellType    string  `json:"cellType"`
	}
	if len(r.Args) > 0 {
		if err := toolcatalog.DecodeArgs(r.Args, &edit); err != nil {
			return empty, nil, err
		}
	}
	root, err := s.editorOfficeRoot(ctx, u, r)
	if err != nil {
		return empty, nil, err
	}
	first := r.Sources[0]
	f, info, err := s.opts.Files.Open(root, first.ID)
	if err != nil {
		return empty, nil, err
	}
	defer f.Close()
	if strconv.FormatInt(info.ModTime().UnixNano(), 10) != first.Ref["mtime"] || info.Size() > maxEditDocument {
		return empty, nil, errOfficeSelection
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEditDocument+1))
	if err != nil {
		return empty, nil, err
	}
	if len(data) > maxEditDocument {
		return empty, nil, office.ErrTooLarge
	}
	p := pa.Preview{Title: fmt.Sprintf("%d Bereiche · %s", len(r.Sources), path.Base(first.ID)), Sources: r.Sources}
	var text strings.Builder
	var result []byte
	for _, src := range r.Sources {
		if err := ctx.Err(); err != nil {
			return empty, nil, err
		}
		a, err := editorRegion(src)
		if err != nil {
			return empty, nil, err
		}
		a.Name, a.Replacement, a.CellType = edit.Name, edit.Replacement, edit.CellType
		normal := r
		src.Kind = "file"
		normal.Sources = []pa.Source{src}
		normal.Args, err = json.Marshal(a)
		if err != nil {
			return empty, nil, err
		}
		part, changed, err := s.planOfficeFile(ctx, u, normal, data)
		if err != nil {
			return empty, nil, err
		}
		if len(r.Sources) == 1 {
			p = part
			p.Sources = r.Sources
			result = changed
			break
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "%s\n%s", part.Title, part.Text)
		if text.Len() > 32768 {
			return empty, nil, errors.New("grenze die Auswahl auf höchstens 32 KiB Text ein")
		}
	}
	if text.Len() > 0 {
		p.Text = text.String()
	}
	if _, err := s.editorOfficeRoot(ctx, u, r); err != nil {
		return empty, nil, err
	}
	return p, result, nil
}
