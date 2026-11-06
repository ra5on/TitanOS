package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Notes of the KI-Agent (PLAN 57, decisions of the owner of 6.11.2026):
// every note is a Markdown file, every folder a real folder below Notizen/
// in the account's own folder of the share KI-Agent, so they show in the
// file manager and over SMB to that account only. The agent reads them and
// writes them with its rights: a new note is "create", changing or
// deleting one is "change" (asks in the standard preset). Pinned notes are
// kept privately in notes.json. All access goes through os.Root: a link
// someone puts in the folder leads nowhere.

const (
	notesDir      = "Notizen"
	noteMaxBytes  = 1 << 20
	noteMaxNotes  = 2000
	noteMaxDepth  = 4
	noteNameLimit = 120
)

type noteInfo struct {
	Path     string    `json:"path"`
	Title    string    `json:"title"`
	Folder   string    `json:"folder"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
	Pinned   bool      `json:"pinned,omitempty"`
	Preview  string    `json:"preview,omitempty"`
}

// cleanNotePath checks a path below Notizen/: folders and a name, no
// "..", no hidden parts, at most noteMaxDepth folders deep. note demands a
// .md file.
func cleanNotePath(p string, note bool) (string, error) {
	p = strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
	if p == "" {
		if note {
			return "", errors.New("name missing")
		}
		return "", nil
	}
	parts := strings.Split(p, "/")
	if len(parts) > noteMaxDepth+1 {
		return "", errors.New("folders too deep")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || utf8.RuneCountInString(part) > noteNameLimit || !utf8.ValidString(part) {
			return "", errors.New("invalid name")
		}
		for _, r := range part {
			if unicode.IsControl(r) || strings.ContainsRune(`<>:"|?*`, r) {
				return "", errors.New("invalid character in name")
			}
		}
	}
	if note && !strings.HasSuffix(strings.ToLower(p), ".md") {
		return "", errors.New("a note ends with .md")
	}
	return strings.Join(parts, "/"), nil
}

// notesRoot opens the account's folder and makes Notizen/ if needed.
func (s *Server) notesRoot(user auth.User) (*os.Root, error) {
	sh, ok := s.agentShare()
	if !ok {
		return nil, errors.New("the share KI-Agent is not there (no data volume?)")
	}
	root, err := s.agentUserRoot(sh, user)
	if err != nil {
		return nil, err
	}
	if err := agentMkdirAll(root, user, s.usersGID(), notesDir); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func noteTitle(p string) string {
	return strings.TrimSuffix(path.Base(p), path.Ext(p))
}

// listNotes walks Notizen/ (hidden entries and links are left out).
func (s *Server) listNotes(user auth.User) ([]string, []noteInfo, error) {
	root, err := s.notesRoot(user)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	meta, _ := s.agentStore().NotesMeta(user.Name)
	folders := []string{}
	notes := []noteInfo{}
	err = fs.WalkDir(root.FS(), notesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, notesDir), "/")
		if rel == "" {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.Count(rel, "/") >= noteMaxDepth {
				return fs.SkipDir
			}
			folders = append(folders, rel)
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(rel), ".md") || len(notes) >= noteMaxNotes {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		n := noteInfo{Path: rel, Title: noteTitle(rel), Folder: path.Dir(rel), Modified: info.ModTime(), Size: info.Size(), Pinned: slices.Contains(meta.Pinned, rel)}
		if n.Folder == "." {
			n.Folder = ""
		}
		if f, err := root.Open(p); err == nil {
			buf := make([]byte, 600)
			k, _ := f.Read(buf)
			f.Close()
			n.Preview = notePreview(string(buf[:k]))
		}
		notes = append(notes, n)
		return nil
	})
	sort.Slice(notes, func(i, j int) bool { return notes[i].Modified.After(notes[j].Modified) })
	sort.Strings(folders)
	return folders, notes, err
}

// notePreview is the start of a note as plain text.
func notePreview(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>-*+ "))
		line = strings.TrimPrefix(strings.TrimPrefix(line, "[ ] "), "[x] ")
		if line != "" {
			out = append(out, line)
		}
	}
	return clipRunes(strings.Join(out, " · "), 140)
}

func (s *Server) readNote(user auth.User, rel string) (string, time.Time, error) {
	root, err := s.notesRoot(user)
	if err != nil {
		return "", time.Time{}, err
	}
	defer root.Close()
	p := path.Join(notesDir, rel)
	st, err := root.Lstat(p)
	if err != nil {
		return "", time.Time{}, err
	}
	if !st.Mode().IsRegular() {
		return "", time.Time{}, errors.New("not a note")
	}
	if st.Size() > noteMaxBytes {
		return "", time.Time{}, errors.New("note too big")
	}
	data, err := root.ReadFile(p)
	return string(data), st.ModTime(), err
}

// writeNote writes a note; with expect set it refuses when the file was
// changed since (another device, SMB).
func (s *Server) writeNote(user auth.User, rel, content string, expect time.Time) (time.Time, error) {
	if len(content) > noteMaxBytes {
		return time.Time{}, errors.New("note too big (1 MB)")
	}
	root, err := s.notesRoot(user)
	if err != nil {
		return time.Time{}, err
	}
	defer root.Close()
	p := path.Join(notesDir, rel)
	if st, err := root.Lstat(p); err == nil {
		if !st.Mode().IsRegular() {
			return time.Time{}, errors.New("not a note")
		}
		if !expect.IsZero() && !st.ModTime().Equal(expect) {
			return time.Time{}, errNoteChanged
		}
	}
	if err := agentWrite(root, user, s.usersGID(), p, []byte(content)); err != nil {
		return time.Time{}, err
	}
	st, err := root.Lstat(p)
	if err != nil {
		return time.Time{}, err
	}
	return st.ModTime(), nil
}

var errNoteChanged = errors.New("the note was changed elsewhere")

func (s *Server) moveNote(user auth.User, from, to string) error {
	root, err := s.notesRoot(user)
	if err != nil {
		return err
	}
	defer root.Close()
	src, dst := path.Join(notesDir, from), path.Join(notesDir, to)
	if _, err := root.Lstat(src); err != nil {
		return err
	}
	if _, err := root.Lstat(dst); err == nil {
		return errors.New("there is already a note or folder with this name")
	}
	if to == from || strings.HasPrefix(to+"/", from+"/") {
		return errors.New("a folder cannot go into itself")
	}
	if dir := path.Dir(dst); dir != notesDir {
		if err := agentMkdirAll(root, user, s.usersGID(), dir); err != nil {
			return err
		}
	}
	if err := root.Rename(src, dst); err != nil {
		return err
	}
	return s.renamePinned(user.Name, from, to)
}

func (s *Server) renamePinned(user, from, to string) error {
	store := s.agentStore()
	meta, err := store.NotesMeta(user)
	if err != nil {
		return nil
	}
	changed := false
	for i, p := range meta.Pinned {
		if p == from || strings.HasPrefix(p, from+"/") {
			meta.Pinned[i] = to + strings.TrimPrefix(p, from)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return store.SaveNotesMeta(user, meta)
}

func (s *Server) deleteNote(user auth.User, rel string) error {
	root, err := s.notesRoot(user)
	if err != nil {
		return err
	}
	defer root.Close()
	p := path.Join(notesDir, rel)
	st, err := root.Lstat(p)
	if err != nil {
		return err
	}
	if st.IsDir() {
		err = root.RemoveAll(p)
	} else {
		err = root.Remove(p)
	}
	if err != nil {
		return err
	}
	store := s.agentStore()
	if meta, err := store.NotesMeta(user.Name); err == nil {
		kept := slices.DeleteFunc(meta.Pinned, func(x string) bool { return x == rel || strings.HasPrefix(x, rel+"/") })
		_ = store.SaveNotesMeta(user.Name, agent.NotesMeta{Pinned: kept})
	}
	return nil
}

func notesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "not_found", "note not found")
	case errors.Is(err, errNoteChanged):
		writeError(w, http.StatusConflict, "note_changed", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "note_invalid", err.Error())
	}
}

func (s *Server) handleAgentNotes(w http.ResponseWriter, r *http.Request) {
	folders, notes, err := s.listNotes(currentUser(r))
	if err != nil {
		notesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders, "notes": notes})
}

func (s *Server) handleAgentNote(w http.ResponseWriter, r *http.Request) {
	rel, err := cleanNotePath(r.URL.Query().Get("path"), true)
	if err != nil {
		notesError(w, err)
		return
	}
	content, modified, err := s.readNote(currentUser(r), rel)
	if err != nil {
		notesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "content": content, "modified": modified})
}

func (s *Server) handleAgentNoteSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string    `json:"path"`
		Content  string    `json:"content"`
		Modified time.Time `json:"modified"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	rel, err := cleanNotePath(req.Path, true)
	if err != nil {
		notesError(w, err)
		return
	}
	modified, err := s.writeNote(currentUser(r), rel, req.Content, req.Modified)
	if err != nil {
		notesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "modified": modified})
}

func (s *Server) handleAgentNoteFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	rel, err := cleanNotePath(req.Path, false)
	if err != nil || rel == "" || strings.Count(rel, "/") >= noteMaxDepth {
		notesError(w, errors.New("invalid folder"))
		return
	}
	user := currentUser(r)
	root, err := s.notesRoot(user)
	if err != nil {
		notesError(w, err)
		return
	}
	defer root.Close()
	if err := agentMkdirAll(root, user, s.usersGID(), path.Join(notesDir, rel)); err != nil {
		notesError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentNoteMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	from, err1 := cleanNotePath(req.From, false)
	to, err2 := cleanNotePath(req.To, false)
	if err1 != nil || err2 != nil || from == "" || to == "" {
		notesError(w, errors.New("invalid path"))
		return
	}
	// A note keeps .md; a folder never gets it.
	if strings.HasSuffix(strings.ToLower(from), ".md") != strings.HasSuffix(strings.ToLower(to), ".md") {
		notesError(w, errors.New("a note stays a note"))
		return
	}
	if err := s.moveNote(currentUser(r), from, to); err != nil {
		notesError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentNoteDelete(w http.ResponseWriter, r *http.Request) {
	rel, err := cleanNotePath(r.URL.Query().Get("path"), false)
	if err != nil || rel == "" {
		notesError(w, errors.New("invalid path"))
		return
	}
	if err := s.deleteNote(currentUser(r), rel); err != nil {
		notesError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentNotePin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string `json:"path"`
		Pinned bool   `json:"pinned"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	rel, err := cleanNotePath(req.Path, true)
	if err != nil {
		notesError(w, err)
		return
	}
	user := currentUser(r)
	store := s.agentStore()
	meta, err := store.NotesMeta(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	meta.Pinned = slices.DeleteFunc(meta.Pinned, func(p string) bool { return p == rel })
	if req.Pinned {
		meta.Pinned = append(meta.Pinned, rel)
	}
	if err := store.SaveNotesMeta(user.Name, meta); err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Tools of the agent for notes. note_write is "create" for a new note and
// "change" for an existing one; note_delete is "change".
func (b *agentToolbox) noteTools() []*agentTool {
	if _, ok := b.s.agentShare(); !ok {
		return nil
	}
	return []*agentTool{
		{name: "notes_list", area: agent.AreaAgent, kind: agent.KindRead, untrusted: true,
			description: "The person's notes (Notizen) with folders, titles and a preview. Use it when the person speaks of a note (\"meine Notiz Einkauf\", \"in den Notizen\").",
			params:      schema(nil), own: b.notesList},
		{name: "note_read", area: agent.AreaAgent, kind: agent.KindRead, untrusted: true,
			description: "Read one note of the person, by its path from notes_list (e.g. \"Haushalt/Einkauf.md\").",
			params:      schema(map[string]any{"path": str("Path below Notizen, ending .md.")}, "path"), own: b.noteRead},
		{name: "note_write", area: agent.AreaAgent, kind: agent.KindCreate,
			description: "Write a note of the person (Markdown). mode new makes a new note (never overwrites), append adds to the end of an existing one, replace writes it anew. Folders are made as needed. Checklists as \"- [ ] Milch\".",
			params: schema(map[string]any{"path": str("Path below Notizen, e.g. \"Haushalt/Einkauf.md\"."), "content": str("The text in Markdown."),
				"mode": enum("new (default), append or replace.", "new", "append", "replace")}, "path", "content"),
			own: b.noteWrite, kindOf: b.noteWriteKind},
		{name: "note_delete", area: agent.AreaAgent, kind: agent.KindChange,
			description: "Delete one note of the person, only when asked to.",
			params:      schema(map[string]any{"path": str("Path below Notizen, ending .md.")}, "path"), own: b.noteDelete},
	}
}

func (b *agentToolbox) noteWriteKind(args json.RawMessage) string {
	var req struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(args, &req)
	if req.Mode == "append" || req.Mode == "replace" {
		return agent.KindChange
	}
	return agent.KindCreate
}

func (b *agentToolbox) notesList(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
	folders, notes, err := b.s.listNotes(b.user)
	if err != nil {
		return agent.Outcome{}, err
	}
	if len(notes) > 200 {
		notes = notes[:200]
	}
	data, _ := json.Marshal(map[string]any{"folders": folders, "notes": notes})
	return agent.Outcome{Text: string(data)}, nil
}

func (b *agentToolbox) noteRead(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &req)
	rel, err := cleanNotePath(req.Path, true)
	if err != nil {
		return agent.Outcome{}, err
	}
	content, _, err := b.s.readNote(b.user, rel)
	if err != nil {
		return agent.Outcome{}, err
	}
	return agent.Outcome{Text: content}, nil
}

func (b *agentToolbox) noteWrite(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct {
		Path, Content, Mode string
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return agent.Outcome{}, errors.New("arguments: path, content, mode")
	}
	if !strings.HasSuffix(strings.ToLower(req.Path), ".md") {
		req.Path += ".md"
	}
	rel, err := cleanNotePath(req.Path, true)
	if err != nil {
		return agent.Outcome{}, err
	}
	old, _, readErr := b.s.readNote(b.user, rel)
	exists := readErr == nil
	content := req.Content
	switch req.Mode {
	case "", "new":
		if exists {
			return agent.Outcome{}, fmt.Errorf("the note %s exists already; use mode append or replace", rel)
		}
	case "append":
		if !exists {
			return agent.Outcome{}, fmt.Errorf("there is no note %s; use mode new", rel)
		}
		content = strings.TrimRight(old, "\n") + "\n" + content
	case "replace":
	default:
		return agent.Outcome{}, errors.New("mode: new, append or replace")
	}
	if _, err := b.s.writeNote(b.user, rel, content, time.Time{}); err != nil {
		return agent.Outcome{}, err
	}
	return agent.Outcome{Text: "Saved note " + rel, Note: "note", NoteText: noteTitle(rel)}, nil
}

func (b *agentToolbox) noteDelete(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &req)
	rel, err := cleanNotePath(req.Path, true)
	if err != nil {
		return agent.Outcome{}, err
	}
	if err := b.s.deleteNote(b.user, rel); err != nil {
		return agent.Outcome{}, err
	}
	return agent.Outcome{Text: "Deleted note " + rel}, nil
}
