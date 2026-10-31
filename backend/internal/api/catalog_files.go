package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Working with files over MCP: writing documents (text, Word, Excel, PDF),
// moving many files at once, copying between roots and searching. Writing
// and moving run through the routes below, with the rights of the account
// like the file manager.

// formats are what file_write makes, by file extension.
var formats = map[string]string{
	".txt": "text", ".md": "markdown", ".markdown": "markdown", ".csv": "csv", ".json": "json",
	".docx": "docx", ".xlsx": "xlsx", ".pdf": "pdf",
	".yaml": "text", ".yml": "text", ".conf": "text", ".ini": "text", ".toml": "text", ".xml": "text",
	".html": "text", ".css": "text", ".js": "text", ".sh": "text", ".py": "text", ".log": "text", ".tsv": "text",
}

var formatExtension = map[string]string{"text": ".txt", "markdown": ".md", "csv": ".csv", "json": ".json", "docx": ".docx", "xlsx": ".xlsx", "pdf": ".pdf"}

// maxDocument is the largest content the model may hand over for a file.
const maxDocument = 4 << 20

type writeRequest struct {
	Root    string         `json:"root"`
	Path    string         `json:"path"`
	Format  string         `json:"format"`
	Content string         `json:"content"`
	Sheets  []office.Sheet `json:"sheets,omitempty"`
	Replace bool           `json:"replace"`
}

// render turns what the model wrote into the bytes of the file.
func (req writeRequest) render() ([]byte, error) {
	title := strings.TrimSuffix(path.Base(req.Path), path.Ext(req.Path))
	switch req.Format {
	case "docx":
		return office.DOCX(req.Content, title)
	case "pdf":
		return office.PDF(req.Content, title)
	case "xlsx":
		sheets := req.Sheets
		if len(sheets) == 0 {
			sheet, err := office.SheetFromCSV(title, req.Content)
			if err != nil {
				return nil, fmt.Errorf("the content is no CSV: %w", err)
			}
			sheets = []office.Sheet{sheet}
		}
		return office.XLSX(sheets)
	case "json":
		if !json.Valid([]byte(req.Content)) {
			return nil, errors.New("the content is no valid JSON")
		}
	}
	if !utf8.ValidString(req.Content) || strings.ContainsRune(req.Content, 0) {
		return nil, errors.New("the content is no text")
	}
	return []byte(req.Content), nil
}

// normalize checks a write request and fills in the format from the name.
func normalizeWrite(req *writeRequest) error {
	ext := strings.ToLower(path.Ext(req.Path))
	if req.Format == "" {
		req.Format = formats[ext]
	}
	want, known := formatExtension[req.Format]
	if !known {
		return toolcatalog.Errorf("format must be text, markdown, csv, json, docx, xlsx or pdf")
	}
	if ext == "" {
		req.Path += want
	} else if formats[ext] != req.Format && !(req.Format == "text" && formats[ext] != "") {
		return toolcatalog.Errorf("the name %s does not fit the format %s; use the extension %s", path.Base(req.Path), req.Format, want)
	}
	if len(req.Content) > maxDocument {
		return toolcatalog.Errorf("the content is too large (at most 4 MB)")
	}
	return nil
}

func (s *Server) fileTools(user auth.User) []toolcatalog.Tool {
	read := func(name, description string, parameters map[string]any, call func(context.Context, json.RawMessage) (any, error)) toolcatalog.Tool {
		return reading(name, description, parameters, true, call)
	}
	moves := map[string]any{"type": "array", "maxItems": 500, "description": "The moves, each from and to a path inside the root.",
		"items": map[string]any{"type": "object", "properties": map[string]any{"from": str("Current path."), "to": str("New path; missing folders are created.")}, "required": []string{"from", "to"}}}
	sheets := map[string]any{"type": "array", "description": "For xlsx: the sheets, each with name, rows (arrays of values; numbers stay numbers, text starting with = is a formula) and header (first row bold).",
		"items": map[string]any{"type": "object", "properties": map[string]any{
			"name": str("Name of the sheet."), "header": map[string]any{"type": "boolean"},
			"rows": map[string]any{"type": "array", "items": map[string]any{"type": "array"}},
		}}}
	return []toolcatalog.Tool{
		read("document_read", "Read a PDF page range with source page numbers. Up to 20 pages and 50 MiB per call. Empty/scanned pages need OCR; missing text is not proof of missing content.", schema(map[string]any{"root": str("Exact file root"), "path": str("PDF path"), "ocr": map[string]any{"type": "boolean", "description": "Explicit OCR for scans, at most five pages; requires OCR setting"}, "first": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000}, "last": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000}}, "root", "path", "first", "last"), func(ctx context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Root, Path  string
				OCR         bool
				First, Last int
			}
			if err := toolcatalog.DecodeArgs(args, &q); err != nil {
				return nil, err
			}
			root, err := s.catalogRoot(user, q.Root)
			if err != nil {
				return nil, err
			}
			if hiddenPath(q.Path) {
				return nil, toolcatalog.ErrInvalid
			}
			return s.readPDF(ctx, root, q.Path, q.First, q.Last, q.OCR)
		}),
		read("files_search", "Search files recursively inside one exact root returned by files_list. For photos use extension=images and omit query; query matches file names literally, not file types. Repeat for other accessible roots as needed.",
			schema(map[string]any{"root": str("Root ID such as share/Media."), "query": str("Part of the name."), "path": str("Folder to search in, empty for the whole root."), "extension": str("File extension, comma-separated extensions, or images for all common photo/image formats."), "offset": map[string]any{"type": "integer", "minimum": 0, "description": "Next result offset from previous response."}}, "root"),
			s.catalogFilesSearch(user)),
		actionTool("file_write", "Propose to write a file: plain text, Markdown, CSV, JSON, or a Word (.docx), Excel (.xlsx) or PDF document. For docx and pdf, write the content as Markdown (headings, lists, tables, bold, italic). For xlsx, give sheets or CSV content.",
			map[string]any{"root": str("Root ID such as share/Documents."), "path": str("Path of the file inside the root; missing folders are created."),
				"format":  enum("Format; taken from the extension if left out.", "text", "markdown", "csv", "json", "docx", "xlsx", "pdf"),
				"content": str("The content (Markdown for docx and pdf)."), "sheets": sheets,
				"replace": map[string]any{"type": "boolean", "description": "Replace an existing file; its old version is kept as .prev."}},
			[]string{"root", "path"}, s.planWrite(user)),
		actionTool("files_move", "Propose to move or rename many files and folders within one root at once, e.g. to sort them into folders. Never replaces anything.",
			map[string]any{"root": str("Root ID."), "moves": moves}, []string{"root", "moves"}, s.planMoves(user)),
		actionTool("file_copy", "Propose to copy or move files and folders into a folder of another (or the same) root. Runs in the background; existing names get a number.",
			map[string]any{"root": str("Root ID of the files."), "paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Paths inside the root."},
				"toRoot": str("Root ID of the target."), "toDir": str("Folder in the target root."), "move": map[string]any{"type": "boolean", "description": "Move instead of copy."}},
			[]string{"root", "paths", "toRoot", "toDir"}, s.planCopy(user)),
	}
}

func (s *Server) planWrite(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
		var req writeRequest
		if err := toolcatalog.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		if err := normalizeWrite(&req); err != nil {
			return nil, err
		}
		p, err := s.fileTarget(user, req.Root, req.Path, true)
		if err != nil {
			return nil, err
		}
		req.Path = p
		if _, err := req.render(); err != nil {
			return nil, toolcatalog.Errorf("%v", err)
		}
		root, _ := s.catalogRoot(user, req.Root)
		exists := false
		if f, _, err := s.opts.Files.Open(root, p); err == nil {
			f.Close()
			exists = true
		}
		if exists && !req.Replace {
			return nil, toolcatalog.Errorf("%s exists; set replace to true to replace it (the old version is kept as .prev), or choose another name", p)
		}
		preview := req.Content
		if req.Format == "xlsx" && len(req.Sheets) > 0 {
			data, _ := json.Marshal(req.Sheets)
			preview = string(data)
		}
		if r := []rune(preview); len(r) > 600 {
			preview = string(r[:600]) + " …"
		}
		params := map[string]string{"root": req.Root, "path": p, "format": req.Format, "preview": preview}
		if exists {
			return withLevel(card("file.replace", params, "POST", "/files/write-document", req), toolcatalog.LevelDanger, "", ""), nil
		}
		return card("file.write", params, "POST", "/files/write-document", req), nil
	}
}

type moveRequest struct {
	Root  string `json:"root"`
	Moves []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"moves"`
}

func (s *Server) planMoves(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
		var req moveRequest
		if err := toolcatalog.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		if len(req.Moves) == 0 || len(req.Moves) > 500 {
			return nil, toolcatalog.Errorf("give 1 to 500 moves")
		}
		var lines []string
		for i := range req.Moves {
			from, err := s.fileTarget(user, req.Root, req.Moves[i].From, true)
			if err != nil {
				return nil, err
			}
			to, err := s.fileTarget(user, req.Root, req.Moves[i].To, true)
			if err != nil {
				return nil, err
			}
			req.Moves[i].From, req.Moves[i].To = from, to
			if len(lines) < 12 {
				lines = append(lines, from+" → "+to)
			}
		}
		if len(req.Moves) > len(lines) {
			lines = append(lines, fmt.Sprintf("… (%d more)", len(req.Moves)-len(lines)))
		}
		return card("files.move", map[string]string{"root": req.Root, "count": fmt.Sprint(len(req.Moves)), "preview": strings.Join(lines, "\n")},
			"POST", "/files/move-many", req), nil
	}
}

func (s *Server) planCopy(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
		var req struct {
			Root, ToRoot, ToDir string
			Paths               []string
			Move                bool
		}
		if err := toolcatalog.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		if len(req.Paths) == 0 || len(req.Paths) > 500 {
			return nil, toolcatalog.Errorf("give 1 to 500 paths")
		}
		for i, p := range req.Paths {
			clean, err := s.fileTarget(user, req.Root, p, req.Move)
			if err != nil {
				return nil, err
			}
			req.Paths[i] = clean
		}
		dst, err := s.catalogRoot(user, req.ToRoot)
		if err != nil {
			return nil, err
		}
		if !dst.Writable {
			return nil, toolcatalog.Errorf("this account may not write in %s", dst.Name)
		}
		dir := strings.TrimPrefix(path.Clean("/"+req.ToDir), "/")
		if hiddenPath(dir) {
			return nil, toolcatalog.Errorf("invalid folder %q", req.ToDir)
		}
		key := "files.copy"
		if req.Move {
			key = "files.moveTo"
		}
		params := map[string]string{"root": req.Root, "count": fmt.Sprint(len(req.Paths)), "toRoot": req.ToRoot, "toDir": dir, "preview": strings.Join(req.Paths[:min(len(req.Paths), 12)], "\n")}
		body := map[string]any{"root": req.Root, "toRoot": req.ToRoot, "toDir": dir, "paths": req.Paths, "move": req.Move, "conflict": files.ConflictRename}
		return card(key, params, "POST", "/files/copy", body), nil
	}
}

func (s *Server) catalogFilesSearch(user auth.User) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var req struct {
			Root, Query, Path, Extension string
			Offset                       int
		}
		if err := toolcatalog.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		root, err := s.catalogRoot(user, req.Root)
		if err != nil {
			return nil, err
		}
		if hiddenPath(req.Path) {
			return nil, toolcatalog.Errorf("this folder is not shown over MCP")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if req.Offset < 0 {
			return nil, toolcatalog.ErrInvalid
		}
		extension := strings.ToLower(strings.TrimSpace(req.Extension))
		if extension == "images" || extension == "photos" {
			extension = "jpg,jpeg,png,gif,webp,heic,heif,avif,tif,tiff,bmp,dng,cr2,cr3,nef,arw,orf,rw2,raf,pef"
		}
		opt := files.SearchOptions{Path: req.Path, Query: req.Query}
		if extension != "" {
			opt.Extensions = strings.Split(extension, ",")
		}
		result, err := s.opts.Files.Search(ctx, root, opt)
		if err != nil {
			return nil, catalogFileError(err)
		}
		found := []files.FoundEntry{}
		for _, e := range result.Entries {
			if !hiddenPath(e.Path) {
				found = append(found, e)
			}
		}
		start := min(req.Offset, len(found))
		end := min(start+100, len(found))
		out := map[string]any{"root": root.ID, "entries": found[start:end], "total": len(found), "scanned": result.Scanned, "truncated": result.Truncated, "observedAt": time.Now()}
		if end < len(found) {
			out["nextOffset"] = end
		}
		if result.Truncated {
			out["next"] = "Search narrower subfolders; this scan is incomplete. Do not infer absence of other photos/files."
		}
		return out, nil
	}
}

// readDocument reads a Word or Excel file as text.
func (s *Server) readDocument(root files.Root, p string) (string, error) {
	f, info, err := s.opts.Files.Open(root, p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if info.Size() > 50<<20 {
		return "", errors.New("the document is too large")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return office.Text(p, data)
}

// handleWriteDocument writes a file that an MCP client made.
func (s *Server) handleWriteDocument(w http.ResponseWriter, r *http.Request) {
	var req writeRequest
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxDocument)
	if !readJSONBody(w, r, &req) {
		return
	}
	if err := normalizeWrite(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_document", err.Error())
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if root.Kind == "stick" || hiddenPath(req.Path) {
		writeFileError(w, files.ErrProtected)
		return
	}
	data, err := req.render()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_document", err.Error())
		return
	}
	user := currentUser(r)
	if err := s.opts.Files.WriteFile(root, req.Path, data, user.UID, req.Replace); err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("file written over MCP", "root", root.ID, "path", req.Path, "format", req.Format, "bytes", len(data), "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, map[string]any{"root": root.ID, "path": req.Path, "bytes": len(data), "sha256": fmt.Sprintf("%x", sha256.Sum256(data))})
}

// handleMoveMany moves many files within one root, creating missing
// folders. It stops at the first that fails and says how far it got.
func (s *Server) handleMoveMany(w http.ResponseWriter, r *http.Request) {
	var req moveRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Moves) == 0 || len(req.Moves) > 500 {
		writeError(w, http.StatusBadRequest, "bad_request", "give 1 to 500 moves")
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	done := 0
	for _, m := range req.Moves {
		if hiddenPath(m.From) || hiddenPath(m.To) {
			writeFileError(w, files.ErrProtected)
			return
		}
		// Missing folders of the target are made one by one.
		dir := path.Dir(strings.TrimPrefix(path.Clean("/"+m.To), "/"))
		parts := strings.Split(dir, "/")
		for i := range parts {
			if dir == "." {
				break
			}
			if err := s.opts.Files.Mkdir(root, strings.Join(parts[:i+1], "/"), user.UID); err != nil && !errors.Is(err, files.ErrExists) {
				writeError(w, http.StatusConflict, "move_failed", fmt.Sprintf("%d of %d moved; %s: %v", done, len(req.Moves), m.To, err))
				return
			}
		}
		if err := s.opts.Files.Rename(root, m.From, m.To); err != nil {
			writeError(w, http.StatusConflict, "move_failed", fmt.Sprintf("%d of %d moved; %s → %s: %v", done, len(req.Moves), m.From, m.To, err))
			return
		}
		done++
	}
	slog.Info("files moved in one batch", "root", root.ID, "count", done, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"moved": done})
}
