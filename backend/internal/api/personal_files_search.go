package api

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func (s *Server) personalSearchAccess(_ context.Context, u auth.User, r pa.Request) error {
	if len(r.Sources) != 1 || r.Sources[0].Kind != "folder" || s.opts.Files == nil {
		return errors.New("bitte einen freigegebenen Suchordner auswählen")
	}
	_, err := s.personalFileRoot(u, r.Sources[0], false)
	return err
}

// Each hit is resolved in the current account's file-manager root again.
// Returning a search hit does not grant a subsequent tool access to its contents.
func (s *Server) personalFindFiles(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
	var p pa.Preview
	if err := s.personalSearchAccess(ctx, u, r); err != nil {
		return p, err
	}
	q := strings.TrimSpace(r.Prompt)
	if q == "" || len(q) > 256 {
		return p, errors.New("bitte einen Suchbegriff mit höchstens 256 Bytes eingeben")
	}
	folder := r.Sources[0]
	root, err := s.personalFileRoot(u, folder, false)
	if err != nil {
		return p, err
	}
	found, err := s.opts.Files.Search(ctx, root, files.SearchOptions{Path: folder.ID, Query: q})
	if err != nil {
		return p, err
	}
	var names []string
	for _, e := range found.Entries {
		if ctx.Err() != nil {
			return pa.Preview{}, ctx.Err()
		}
		if hiddenPath(e.Path) {
			continue
		}
		if _, err := s.fileTarget(u, root.ID, e.Path, false); err != nil {
			continue
		}
		kind := "file"
		if e.Dir {
			kind = "folder"
		}
		p.Sources = append(p.Sources, pa.Source{Kind: kind, ID: e.Path, Label: path.Base(e.Path), Ref: map[string]string{"root": root.ID}})
		names = append(names, e.Path)
		if len(names) == 32 {
			break
		}
	}
	p.Title = "Dateisuche"
	p.Text = fmt.Sprintf("%d angezeigte Treffer in %s", len(names), folder.Label)
	if len(names) > 0 {
		p.Text += "\n" + strings.Join(names, "\n")
	}
	if found.Truncated || len(found.Entries) > 32 {
		p.Text += "\nWeitere Treffer sind möglich."
	}
	return p, nil
}
