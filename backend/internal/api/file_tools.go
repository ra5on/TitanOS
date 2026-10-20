package api

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"net/http"
	"time"
)

func (s *Server) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root string
		files.SearchOptions
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.opts.Files.Search(ctx, root, req.SearchOptions)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleFileCompare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Path, ToRoot, ToDir string
		Checksum, ShowHidden      bool
	}
	if !readJSON(w, r, &req) {
		return
	}
	left, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	right, ok := s.fileRoot(w, r, req.ToRoot)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.opts.Files.Compare(ctx, left, req.Path, right, req.ToDir, req.Checksum, req.ShowHidden)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFileUndoList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Files.UndoList(currentUser(r).Name))
}
func (s *Server) handleFileUndo(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var item *files.UndoInfo
	for _, record := range s.opts.Files.UndoList(user.Name) {
		if record.ID == r.PathValue("id") {
			item = &record
			break
		}
	}
	if item == nil {
		writeFileError(w, files.ErrNoJob)
		return
	}
	roots := map[string]files.Root{}
	for _, id := range item.Roots {
		root, ok := s.fileRoot(w, r, id)
		if !ok {
			return
		}
		roots[id] = root
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := s.opts.Files.Undo(ctx, user.Name, item.ID, roots, user.UID); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFileRenamePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root string
		files.RenameOptions
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	result, err := s.opts.Files.RenamePreview(root, req.RenameOptions)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleFileRenameBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root    string
		Changes []files.RenameChange
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.opts.Files.RenameBatch(ctx, root, req.Changes, currentUser(r).Name)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFileArchiveList(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	result, err := s.opts.Files.ArchiveList(root, req.Path)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleFileExtract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Path, Version, ToRoot, ToDir, Name string
		Paths                                    []string
	}
	if !readJSON(w, r, &req) {
		return
	}
	src, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	dst, ok := s.fileRoot(w, r, req.ToRoot)
	if !ok {
		return
	}
	user := currentUser(r)
	result, err := s.opts.Files.StartExtract(src, req.Path, req.Version, req.Paths, dst, req.ToDir, req.Name, user.Name, user.UID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleFileJobControl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paused   bool
		Priority int
		Rate     int64
	}
	if !readJSON(w, r, &req) {
		return
	}
	// Resolve current access again before resuming a transfer.
	user := currentUser(r)
	var job *files.Job
	for _, item := range s.opts.Files.Jobs(user.Name) {
		if item.ID == r.PathValue("id") {
			job = &item
			break
		}
	}
	if job == nil {
		writeFileError(w, files.ErrNoJob)
		return
	}
	if !req.Paused {
		src, ok := s.fileRoot(w, r, job.From)
		if !ok {
			return
		}
		dst, ok := s.fileRoot(w, r, job.To)
		if !ok {
			return
		}
		if !dst.Writable || job.Move && !src.Writable {
			writeFileError(w, files.ErrReadOnly)
			return
		}
	}
	if err := s.opts.Files.ControlJob(user.Name, job.ID, req.Paused, req.Priority, req.Rate); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFileUsage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Path string
		ShowHidden bool
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.opts.Files.DirectoryUsage(ctx, root, req.Path, req.ShowHidden)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFilePrevious(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	result, err := s.opts.Files.ReadPreviousText(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleFileDuplicates(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Path string
		ShowHidden bool
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.opts.Files.Duplicates(ctx, root, req.Path, req.ShowHidden)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
