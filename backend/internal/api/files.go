package api

import (
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

var fileErrors = []struct {
	err    error
	status int
	code   string
}{
	{files.ErrProtected, http.StatusConflict, "vm_file_protected"},
	{files.ErrNotFound, http.StatusNotFound, "file_not_found"},
	{files.ErrExists, http.StatusConflict, "file_exists"},
	{files.ErrInvalid, http.StatusBadRequest, "invalid_path"},
	{files.ErrReadOnly, http.StatusForbidden, "read_only_share"},
	{files.ErrNoSpace, http.StatusInsufficientStorage, "no_space"},
	{files.ErrMismatch, http.StatusConflict, "upload_mismatch"},
	{files.ErrIncomplete, http.StatusConflict, "upload_incomplete"},
	{files.ErrConfirm, http.StatusBadRequest, "not_confirmed"},
	{files.ErrVolumeChanged, http.StatusConflict, "upload_volume_unavailable"},
	{system.ErrMemoryPressure, http.StatusServiceUnavailable, "upload_memory_pressure"},
	{files.ErrNoUpload, http.StatusNotFound, "upload_not_found"},
}

func writeFileError(w http.ResponseWriter, err error) {
	for _, known := range fileErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.err.Error())
			return
		}
	}
	slog.Error("file operation failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "the file operation failed")
}

// fileRoots returns where an account may work: the shares it has access
// to and, for administrators, the volumes as a whole.
func (s *Server) fileRoots(user auth.User) []files.Root {
	mounted := s.opts.Storage.Mounted()
	var roots []files.Root
	for _, share := range s.opts.Shares.List() {
		level, ok := share.Access[user.Name]
		if !ok || !slices.Contains(mounted, share.Volume) {
			continue
		}
		dir := s.opts.Shares.Path(share)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, files.Root{
			ID: "share/" + share.Name, Name: share.Name, Kind: "share", Dir: dir, Volume: share.Volume,
			Writable: level == shares.ReadWrite, Recycle: share.Recycle.Enabled,
		})
	}
	if user.Role.CanAdminister() {
		for _, volume := range mounted {
			roots = append(roots, files.Root{ID: "volume/" + volume, Name: volume, Kind: "volume", Dir: s.opts.Storage.MountPoint(volume), Volume: volume, Writable: true})
		}
	}
	return roots
}

// fileRoot resolves the root a request names, or answers it with an error.
func (s *Server) fileRoot(w http.ResponseWriter, r *http.Request, id string) (files.Root, bool) {
	for _, root := range s.fileRoots(currentUser(r)) {
		if root.ID == id {
			return root, true
		}
	}
	// The same answer for "does not exist" and "not yours".
	writeError(w, http.StatusNotFound, "file_not_found", "no such share")
	return files.Root{}, false
}

type rootResponse struct {
	files.Root
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

func (s *Server) handleFileRoots(w http.ResponseWriter, r *http.Request) {
	list := []rootResponse{}
	for _, root := range s.fileRoots(currentUser(r)) {
		total, free := files.Usage(root)
		list = append(list, rootResponse{Root: root, TotalBytes: total, FreeBytes: free})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	entries, err := s.opts.Files.List(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := s.opts.Files.Mkdir(root, req.Path, currentUser(r).UID); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, From, To string }
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := s.opts.Files.Rename(root, req.From, req.To); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root      string
		Paths     []string
		Permanent bool
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	if err := s.opts.Files.Delete(root, req.Paths, user.Name, req.Permanent); err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("files deleted", "root", root.ID, "paths", req.Paths, "permanent", req.Permanent || !root.Recycle, "user", user.Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// attachment makes the browser save the answer under the given name. Files
// in a share are never shown as pages of this site, whatever they contain.
func attachment(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	file, info, err := s.opts.Files.Open(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer file.Close()
	attachment(w, info.Name())
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", info.ModTime(), file)
}

// handleFileZip streams several files or folders as one zip archive.
func (s *Server) handleFileZip(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	paths := r.URL.Query()["path"]
	if len(paths) == 0 {
		writeFileError(w, files.ErrInvalid)
		return
	}
	name := root.Name
	if len(paths) == 1 {
		name = path.Base(paths[0])
	}
	attachment(w, name+".zip")
	w.Header().Set("Content-Type", "application/zip")
	if err := s.opts.Files.Zip(w, root, paths); err != nil {
		// The answer has started; all that is left is to end it early.
		slog.Warn("zip download", "root", root.ID, "err", err)
	}
}

func (s *Server) handleUploadStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Dir, Name, Conflict string
		Size                      int64
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	status, err := s.opts.Files.StartUpload(root, req.Dir, req.Name, req.Size, req.Conflict, user.Name, user.UID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

func (s *Server) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeUpload(w, r) {
		return
	}
	status, err := s.opts.Files.UploadStatus(r.PathValue("id"), currentUser(r).Name)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleUploadChunk appends the request body to an upload. The body goes
// straight to the volume; nothing is kept in RAM or on the stick.
func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeUpload(w, r) {
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		writeFileError(w, files.ErrInvalid)
		return
	}
	status, err := s.opts.Files.WriteChunkContext(r.Context(), r.PathValue("id"), currentUser(r).Name, offset, r.Body)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleUploadFinish(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeUpload(w, r) {
		return
	}
	user := currentUser(r)
	status, err := s.opts.Files.FinishUpload(r.PathValue("id"), user.Name)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("file uploaded", "name", status.Name, "bytes", status.Size, "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeUpload(w, r) {
		return
	}
	if err := s.opts.Files.AbortUpload(r.PathValue("id"), currentUser(r).Name); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	uploads, err := s.opts.Files.Uploads(root, r.URL.Query().Get("path"), currentUser(r).Name, currentUser(r).UID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, 200, uploads)
}
func (s *Server) handleUploadVerify(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeUpload(w, r) {
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		writeFileError(w, files.ErrInvalid)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err = s.opts.Files.VerifyChunk(r.PathValue("id"), currentUser(r).Name, offset, r.Body); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) authorizeUpload(w http.ResponseWriter, r *http.Request) bool {
	previous, err := s.opts.Files.UploadRoot(r.PathValue("id"), currentUser(r).Name)
	if err != nil {
		writeFileError(w, err)
		return false
	}
	root, ok := s.fileRoot(w, r, previous.ID)
	if !ok {
		return false
	}
	if !root.Writable {
		writeFileError(w, files.ErrReadOnly)
		return false
	}
	if root.Dir != previous.Dir || root.Volume != previous.Volume {
		writeFileError(w, files.ErrVolumeChanged)
		return false
	}
	return true
}
