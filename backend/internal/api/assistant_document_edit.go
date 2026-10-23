package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"
)

type documentEdit struct {
	Root       string            `json:"root"`
	Source     string            `json:"source"`
	Path       string            `json:"path"`
	SourceHash string            `json:"sourceHash,omitempty"`
	Cells      []office.CellEdit `json:"cells,omitempty"`
	Edits      []office.TextEdit `json:"edits"`
}

func (s *Server) renderEdit(u auth.User, q documentEdit) ([]byte, string, error) {
	if s.opts.Assistant.Settings().Access(assistant.AreaFiles) != assistant.AccessWrite {
		return nil, "", assistant.ErrOff
	}
	root, err := s.assistantRoot(u, q.Root)
	if err != nil {
		return nil, "", err
	}
	if hiddenPath(q.Source) || hiddenPath(q.Path) || q.Source == q.Path || strings.EqualFold(q.Source, q.Path) {
		return nil, "", assistant.ErrInvalid
	}
	ext := strings.ToLower(path.Ext(q.Source))
	if ext != strings.ToLower(path.Ext(q.Path)) || ext != ".docx" && ext != ".xlsx" && formats[ext] != "text" && ext != ".md" && ext != ".csv" && ext != ".json" {
		return nil, "", assistant.Errorf("edit supports Word, Excel and text documents; target must be a new file of the same type")
	}
	f, info, err := s.opts.Files.Open(root, q.Source)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	if info.Size() > 50<<20 {
		return nil, "", office.ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, (50<<20)+1))
	if err != nil {
		return nil, "", err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if q.SourceHash != "" && q.SourceHash != hash {
		return nil, "", assistant.Errorf("source changed; read it and propose the edit again")
	}
	if ext != ".docx" && ext != ".xlsx" && (!utf8.Valid(data) || strings.ContainsRune(string(data), 0)) {
		return nil, "", assistant.ErrInvalid
	}
	var result []byte
	if ext == ".xlsx" {
		if len(q.Edits) > 0 {
			return nil, "", assistant.ErrInvalid
		}
		result, err = office.EditCells(data, q.Cells)
	} else {
		if len(q.Cells) > 0 {
			return nil, "", assistant.ErrInvalid
		}
		result, err = office.EditText(q.Source, data, q.Edits)
	}
	if err == nil && (ext == ".docx" || ext == ".xlsx") {
		_, err = office.Text(q.Path, result)
	}
	if err == nil && ext == ".json" && !json.Valid(result) {
		err = assistant.Errorf("edited JSON is invalid")
	}
	return result, hash, err
}
func (s *Server) documentEditTool(u auth.User) assistant.Tool {
	return actionTool("document_edit", "Edit exact text in an existing Word or text document and save a NEW copy. Word replacements must fit one text run; other parts and formatting remain intact. Give exact occurrence counts; no fuzzy replacement. For Excel use cells with exact sheet name, existing cell address, type and value. Formula cells are protected; formulas recalculate when the new copy is opened in a spreadsheet program. No changes to workbook structure.", map[string]any{
		"cells": map[string]any{"type": "array", "maxItems": 500, "items": schema(map[string]any{"sheet": str("Exact sheet name"), "cell": str("Existing cell address, e.g. B2"), "type": enum("Value type", "text", "number", "boolean"), "value": str("New literal value, no formulas")}, "sheet", "cell", "type", "value")},
		"root":  str("Exact accessible root"), "source": str("Existing document path"), "path": str("New output path, never overwrites"), "edits": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": schema(map[string]any{"find": str("Exact old text"), "replace": str("New text"), "count": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}}, "find", "replace", "count")}}, []string{"root", "source", "path"}, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var q documentEdit
		if err := assistant.DecodeArgs(args, &q); err != nil {
			return nil, err
		}
		target, err := s.fileTarget(u, q.Root, q.Path, true)
		if err != nil {
			return nil, err
		}
		q.Path = target
		_, hash, err := s.renderEdit(u, q)
		if err != nil {
			return nil, err
		}
		q.SourceHash = hash
		preview, _ := json.Marshal(map[string]any{"edits": q.Edits, "cells": q.Cells})
		if len(preview) > 2000 {
			preview = []byte(string([]rune(string(preview))[:min(1000, len([]rune(string(preview))))]) + " …")
		}
		return card("file.write", map[string]string{"root": q.Root, "path": q.Path, "format": strings.TrimPrefix(path.Ext(q.Path), "."), "preview": string(preview)}, "POST", "/assistant/files/edit", q), nil
	})
}
func (s *Server) handleDocumentEdit(w http.ResponseWriter, r *http.Request) {
	var q documentEdit
	if !readJSON(w, r, &q) {
		return
	}
	if q.SourceHash == "" {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	u := currentUser(r)
	data, _, err := s.renderEdit(u, q)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	root, err := s.assistantRoot(u, q.Root)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	if err = s.opts.Files.WriteFile(root, q.Path, data, u.UID, false); err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"root": q.Root, "path": q.Path, "recalculationRequired": path.Ext(q.Path) == ".xlsx", "bytes": len(data), "sha256": fmt.Sprintf("%x", sha256.Sum256(data))})
}
