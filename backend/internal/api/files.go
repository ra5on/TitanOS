package api

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

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
	{files.ErrNameTooLong, http.StatusBadRequest, "file_name_too_long"},
	{files.ErrTextConflict, http.StatusConflict, "text_conflict"},
	{files.ErrInvalidJSON, http.StatusBadRequest, "invalid_json"},
	{files.ErrNotText, http.StatusBadRequest, "not_text"},
	{files.ErrProtected, http.StatusConflict, "vm_file_protected"},
	{files.ErrNotFound, http.StatusNotFound, "file_not_found"},
	{files.ErrExists, http.StatusConflict, "file_exists"},
	{files.ErrInvalid, http.StatusBadRequest, "invalid_path"},
	{files.ErrScanLimit, http.StatusUnprocessableEntity, "file_scan_limit"},
	{context.DeadlineExceeded, http.StatusRequestTimeout, "file_scan_timeout"},
	{files.ErrReadOnly, http.StatusForbidden, "read_only_share"},
	{files.ErrNoSpace, http.StatusInsufficientStorage, "no_space"},
	{files.ErrMismatch, http.StatusConflict, "upload_mismatch"},
	{files.ErrIncomplete, http.StatusConflict, "upload_incomplete"},
	{files.ErrConfirm, http.StatusBadRequest, "not_confirmed"},
	{files.ErrVolumeChanged, http.StatusConflict, "upload_volume_unavailable"},
	{system.ErrMemoryPressure, http.StatusServiceUnavailable, "upload_memory_pressure"},
	{files.ErrNoUpload, http.StatusNotFound, "upload_not_found"},
	{files.ErrJobBusy, http.StatusConflict, "file_job_busy"},
	{files.ErrNoJob, http.StatusNotFound, "job_not_found"},
	{files.ErrDownloadLimit, http.StatusTooManyRequests, "download_limit"},
	{files.ErrBlocked, http.StatusForbidden, "download_blocked"},
	{files.ErrHashMismatch, http.StatusConflict, "download_hash_mismatch"},
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
			Writable: level == shares.ReadWrite, Recycle: share.Recycle.Enabled, RecycleDays: share.Recycle.Days,
		})
	}
	if user.Role.CanAdminister() {
		if s.opts.USB != nil {
			for _, drive := range s.opts.USB.Mounted() {
				roots = append(roots, files.Root{ID: "usb/" + drive.Mount, Name: drive.Name, Kind: "usb", Dir: drive.Dir, Writable: !drive.ReadOnly})
			}
		}
		// The stick is only looked at, apart from text files. Its secrets
		// stay hidden; /config is a link, so that listing the roots does not
		// wake the stick up.
		roots = append(roots, files.Root{ID: "stick", Name: "TITANOS", Kind: "stick", Dir: s.opts.Store.Dir(), Editable: true,
			Hide: []string{"users", "tls", "ssh", "notify", "backup"}})
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
		var total, free uint64
		if root.Kind != "stick" {
			total, free = files.Usage(root)
		}
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
	noteTarget(r, filePlace(req.Root, req.Path))
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
	noteTarget(r, filePlace(req.Root, req.From)+" → "+req.To)
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := s.opts.Files.Rename(root, req.From, req.To); err != nil {
		writeFileError(w, err)
		return
	}
	undo := s.opts.Files.RecordUndo(currentUser(r).Name, "rename", []files.UndoMove{{CurrentRoot: root, OriginalRoot: root, Current: req.To, Original: req.From}})
	writeJSON(w, http.StatusOK, map[string]string{"undo": undo})
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root      string
		Paths     []string
		Permanent bool
		Versions  map[string]string
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlaces(req.Root, req.Paths)...)
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	if req.Versions != nil {
		if err := s.opts.Files.CheckVersions(root, req.Paths, req.Versions); err != nil {
			writeFileError(w, err)
			return
		}
	}
	undo, err := s.opts.Files.DeleteWithUndo(root, req.Paths, user.Name, req.Permanent)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("files deleted", "root", root.ID, "paths", req.Paths, "permanent", req.Permanent || !root.Recycle, "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"undo": undo})
}

func (s *Server) handleTrashList(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	user := currentUser(r)
	if r.URL.Query().Get("flat") == "1" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		entries, err := s.opts.Files.TrashFlat(ctx, root, user.Name, user.Role.CanAdminister())
		if err != nil {
			writeFileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entries)
		return
	}
	entries, err := s.opts.Files.TrashList(root, r.URL.Query().Get("path"), user.Name, user.Role.CanAdminister())
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleTrash restores files from a share's recycle bin or deletes them
// from it for good.
func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root  string
		Paths []string
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlaces(req.Root, req.Paths)...)
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	action := r.PathValue("action")
	var err error
	switch action {
	case "preview":
		preview, err := s.opts.Files.TrashPreview(root, req.Paths, user.Name, user.Role.CanAdminister())
		if err != nil {
			writeFileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	case "restore":
		err = s.opts.Files.TrashRestore(root, req.Paths, user.Name, user.UID, user.Role.CanAdminister())
	case "delete":
		err = s.opts.Files.TrashDelete(root, req.Paths, user.Name, user.Role.CanAdminister())
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such action")
		return
	}
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("recycle bin "+action, "root", root.ID, "paths", req.Paths, "user", user.Name, "ip", remoteIP(r))
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
	if r.URL.Query().Get("media") == "1" {
		mediaType := map[string]string{".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".m4a": "audio/mp4", ".flac": "audio/flac", ".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg"}[strings.ToLower(path.Ext(info.Name()))]
		if mediaType == "" {
			writeError(w, http.StatusBadRequest, "invalid_path", "unsupported media")
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": info.Name()}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
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
	name += ".zip"
	if chosen := r.URL.Query().Get("name"); chosen != "" {
		if path.Base(chosen) != chosen || strings.ContainsAny(chosen, "\\\x00\r\n") || len(chosen) <= 4 || len(chosen) > 200 || !strings.HasSuffix(strings.ToLower(chosen), ".zip") {
			writeFileError(w, files.ErrInvalid)
			return
		}
		name = chosen
	}
	attachment(w, name)
	w.Header().Set("Content-Type", "application/zip")
	if err := s.opts.Files.Zip(w, root, paths); err != nil {
		// The answer has started; all that is left is to end it early.
		slog.Warn("zip download", "root", root.ID, "err", err)
		panic(http.ErrAbortHandler)
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
	noteTarget(r, filePlace(req.Root, path.Join(req.Dir, req.Name)))
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

func (s *Server) handleReadFileText(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	result, err := s.opts.Files.ReadText(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleFileText saves an existing text file within the account's writable roots.
func (s *Server) handleFileText(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path, Content, Version string }
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if !readJSONBody(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, req.Path))
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if req.Version == "" {
		writeFileError(w, files.ErrTextConflict)
		return
	}
	result, err := s.opts.Files.SaveTextVersion(root, req.Path, req.Content, req.Version)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("text file saved", "root", root.ID, "path", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, result)
}

// handleFileCopy starts copying or moving files between roots.
func (s *Server) handleFileCopy(w http.ResponseWriter, r *http.Request) {
	if s.opts.Maintenance != nil {
		leave, ok := s.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
	}
	var req struct {
		Choices                       map[string]files.CopyChoice
		Root, ToRoot, ToDir, Conflict string
		Paths                         []string
		Move                          bool
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, append(filePlaces(req.Root, req.Paths), "→ "+filePlace(req.ToRoot, req.ToDir))...)
	src, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	dst, ok := s.fileRoot(w, r, req.ToRoot)
	if !ok {
		return
	}
	user := currentUser(r)
	job, err := s.opts.Files.StartCopyChoices(src, req.Paths, dst, req.ToDir, req.Move, req.Conflict, user.Name, user.UID, req.Choices)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("copy started", "from", src.ID, "to", dst.ID, "move", req.Move, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleFileJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Files.Jobs(currentUser(r).Name))
}

func (s *Server) handleFileJobCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Files.CancelJob(currentUser(r).Name, r.PathValue("id")); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFileInspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, ToRoot, ToDir string
		Paths               []string
	}
	if !readJSON(w, r, &req) {
		return
	}
	src, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	var dst *files.Root
	if req.ToRoot != "" {
		resolved, ok := s.fileRoot(w, r, req.ToRoot)
		if !ok {
			return
		}
		dst = &resolved
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.opts.Files.Inspect(ctx, src, req.Paths, dst, req.ToDir)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFileJobRetry(w http.ResponseWriter, r *http.Request) {
	if s.opts.Maintenance != nil {
		leave, ok := s.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
	}
	user := currentUser(r)
	for _, job := range s.opts.Files.Jobs(user.Name) {
		if job.ID == r.PathValue("id") && job.Operation == "download" {
			// A download has no source root; its target is checked again.
			dst, ok := s.fileRoot(w, r, job.To)
			if !ok {
				return
			}
			result, err := s.opts.Files.RetryDownload(user.Name, job.ID, dst)
			if err != nil {
				writeFileError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, result)
			return
		}
		if job.ID == r.PathValue("id") {
			src, ok := s.fileRoot(w, r, job.From)
			if !ok {
				return
			}
			dst, ok := s.fileRoot(w, r, job.To)
			if !ok {
				return
			}
			result, err := s.opts.Files.RetryCopy(user.Name, job.ID, src, dst, user.UID)
			if err != nil {
				writeFileError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, result)
			return
		}
	}
	writeFileError(w, files.ErrNoJob)
}

func (s *Server) handleFileProperties(w http.ResponseWriter, r *http.Request) {
	root, ok := s.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.opts.Files.Properties(ctx, root, r.URL.Query().Get("path"), r.URL.Query().Get("checksum") == "sha256")
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFileCreate(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, req.Path))
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := s.opts.Files.CreateText(root, req.Path, currentUser(r).UID); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
