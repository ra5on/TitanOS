package api

import (
	"context"
	"encoding/base64"
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

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/office"
)

// Working with files for the assistant: writing documents (text, Word,
// Excel, PDF), moving many files at once, copying between roots and
// searching. Writing and moving run through the routes below, which the
// browser calls once the person confirms the card; they work with the
// rights of the account like the file manager.

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
		return assistant.Errorf("format must be text, markdown, csv, json, docx, xlsx or pdf")
	}
	if ext == "" {
		req.Path += want
	} else if formats[ext] != req.Format && !(req.Format == "text" && formats[ext] != "") {
		return assistant.Errorf("the name %s does not fit the format %s; use the extension %s", path.Base(req.Path), req.Format, want)
	}
	if len(req.Content) > maxDocument {
		return assistant.Errorf("the content is too large (at most 4 MB)")
	}
	return nil
}

func (s *Server) fileTools(user auth.User) []assistant.Tool {
	read := func(name, description string, parameters map[string]any, call func(context.Context, json.RawMessage) (any, error)) assistant.Tool {
		return reading(name, description, parameters, true, call)
	}
	moves := map[string]any{"type": "array", "maxItems": 500, "description": "The moves, each from and to a path inside the root.",
		"items": map[string]any{"type": "object", "properties": map[string]any{"from": str("Current path."), "to": str("New path; missing folders are created.")}, "required": []string{"from", "to"}}}
	sheets := map[string]any{"type": "array", "description": "For xlsx: the sheets, each with name, rows (arrays of values; numbers stay numbers, text starting with = is a formula) and header (first row bold).",
		"items": map[string]any{"type": "object", "properties": map[string]any{
			"name": str("Name of the sheet."), "header": map[string]any{"type": "boolean"},
			"rows": map[string]any{"type": "array", "items": map[string]any{"type": "array"}},
		}}}
	return []assistant.Tool{
		read("files_search", "Search for files and folders by name in a root of the file manager, also in subfolders.",
			schema(map[string]any{"root": str("Root ID such as share/Media."), "query": str("Part of the name."), "path": str("Folder to search in, empty for the whole root."), "extension": str("File extension, comma-separated extensions, or images for all common photo/image formats."), "offset": map[string]any{"type": "integer", "minimum": 0, "description": "Next result offset from previous response."}}, "root"),
			s.assistantFilesSearch(user)),
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

func (s *Server) taskTool() assistant.Tool {
	return actionTool("start_task", "Propose a task with several changes as a plan the person approves once. After approval, the actions of this task run without asking again, except destructive ones. Use it before sorting or renaming many files, writing several documents, setting up an app or cleaning up.",
		map[string]any{"title": str("What the task does, in one line."), "steps": map[string]any{"type": "array", "minItems": 1, "maxItems": 30, "items": map[string]any{"type": "string"}, "description": "The steps, each in one short line."}},
		[]string{"title", "steps"},
		func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			var req struct {
				Title string
				Steps []string
			}
			if err := assistant.DecodeArgs(args, &req); err != nil {
				return nil, err
			}
			req.Title = strings.TrimSpace(req.Title)
			if req.Title == "" || len(req.Steps) == 0 || len(req.Steps) > 30 {
				return nil, assistant.Errorf("a task needs a title and 1 to 30 steps")
			}
			for i, step := range req.Steps {
				if r := []rune(strings.TrimSpace(step)); len(r) > 300 {
					req.Steps[i] = string(r[:300]) + " …"
				}
			}
			a := card("task", map[string]string{"title": req.Title}, "", "", nil)
			a.Path, a.Steps = "", req.Steps
			return a, nil
		})
}

func (s *Server) planWrite(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req writeRequest
		if err := assistant.DecodeArgs(args, &req); err != nil {
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
			return nil, assistant.Errorf("%v", err)
		}
		root, _ := s.assistantRoot(user, req.Root)
		exists := false
		if f, _, err := s.opts.Files.Open(root, p); err == nil {
			f.Close()
			exists = true
		}
		if exists && !req.Replace {
			return nil, assistant.Errorf("%s exists; set replace to true to replace it (the old version is kept as .prev), or choose another name", p)
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
			return withLevel(card("file.replace", params, "POST", "/assistant/files/write", req), assistant.LevelDanger, "", ""), nil
		}
		return card("file.write", params, "POST", "/assistant/files/write", req), nil
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
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req moveRequest
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		if len(req.Moves) == 0 || len(req.Moves) > 500 {
			return nil, assistant.Errorf("give 1 to 500 moves")
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
			"POST", "/assistant/files/move", req), nil
	}
}

func (s *Server) planCopy(user auth.User) plan {
	return func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
		var req struct {
			Root, ToRoot, ToDir string
			Paths               []string
			Move                bool
		}
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		if len(req.Paths) == 0 || len(req.Paths) > 500 {
			return nil, assistant.Errorf("give 1 to 500 paths")
		}
		for i, p := range req.Paths {
			clean, err := s.fileTarget(user, req.Root, p, req.Move)
			if err != nil {
				return nil, err
			}
			req.Paths[i] = clean
		}
		dst, err := s.assistantRoot(user, req.ToRoot)
		if err != nil {
			return nil, err
		}
		if !dst.Writable {
			return nil, assistant.Errorf("this account may not write in %s", dst.Name)
		}
		dir := strings.TrimPrefix(path.Clean("/"+req.ToDir), "/")
		if hiddenPath(dir) {
			return nil, assistant.Errorf("invalid folder %q", req.ToDir)
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

func (s *Server) assistantFilesSearch(user auth.User) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var req struct {
			Root, Query, Path, Extension string
			Offset                       int
		}
		if err := assistant.DecodeArgs(args, &req); err != nil {
			return nil, err
		}
		root, err := s.assistantRoot(user, req.Root)
		if err != nil {
			return nil, err
		}
		if hiddenPath(req.Path) {
			return nil, assistant.Errorf("this folder is not shown to the assistant")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if req.Offset < 0 {
			return nil, assistant.ErrInvalid
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
			return nil, assistant.Errorf("%v", err)
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

// handleAssistantWrite writes a file the assistant made, once the person
// confirmed it.
func (s *Server) handleAssistantWrite(w http.ResponseWriter, r *http.Request) {
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
	slog.Info("file written by the assistant", "root", root.ID, "path", req.Path, "format", req.Format, "bytes", len(data), "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, map[string]any{"root": root.ID, "path": req.Path, "bytes": len(data)})
}

// handleAssistantMove moves many files within one root, creating missing
// folders. It stops at the first that fails and says how far it got.
func (s *Server) handleAssistantMove(w http.ResponseWriter, r *http.Request) {
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
	slog.Info("files moved by the assistant", "root", root.ID, "count", done, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"moved": done})
}

// nasAttachment reads a file of the file manager as an attachment: an
// image as is, Word and Excel as text, any other as text if it is one.
func (s *Server) nasAttachment(user auth.User, a assistant.Attachment) (assistant.Attachment, error) {
	root, err := s.assistantRoot(user, a.Root)
	if err != nil || hiddenPath(a.Path) {
		return a, errors.New("no such file")
	}
	name := path.Base(a.Path)
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".docx", ".xlsx":
		text, err := s.readDocument(root, a.Path)
		if err != nil {
			return a, err
		}
		if len(text) > assistant.MaxTextAttachment {
			text = text[:assistant.MaxTextAttachment]
		}
		return assistant.Attachment{Name: name, Kind: "text", Text: strings.ToValidUTF8(text, "")}, nil
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		f, info, err := s.opts.Files.Open(root, a.Path)
		if err != nil {
			return a, err
		}
		defer f.Close()
		if info.Size() > assistant.MaxImageAttachment {
			return a, errors.New("the image is larger than 4 MB")
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return a, err
		}
		return assistant.Attachment{Name: name, Kind: "image", Image: base64.StdEncoding.EncodeToString(data)}, nil
	}
	text, err := s.opts.Files.ReadText(root, a.Path)
	if err != nil {
		return a, fmt.Errorf("only text, Word, Excel and image files can be attached: %w", err)
	}
	if len(text.Content) > assistant.MaxTextAttachment {
		return a, errors.New("the file is larger than 256 KiB")
	}
	return assistant.Attachment{Name: name, Kind: "text", Text: text.Content}, nil
}
