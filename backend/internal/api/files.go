package api

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
	"github.com/ra5on/TitanOS/backend/internal/usb"
)

var fileErrors = []struct {
	err    error
	status int
	code   string
}{
	{files.ErrNameTooLong, http.StatusBadRequest, "file_name_too_long"},
	// Copy-specific errors wrap generic ones and come first.
	{files.ErrIntoItself, http.StatusBadRequest, "copy_into_itself"},
	{files.ErrSameFolder, http.StatusConflict, "copy_same_folder"},
	{files.ErrSourceProtected, http.StatusForbidden, "copy_source_protected"},
	{files.ErrSourceChanged, http.StatusConflict, "copy_source_changed"},
	{files.ErrRebooted, http.StatusConflict, "copy_rebooted"},
	{files.ErrAccess, http.StatusForbidden, "file_access_denied"},
	{files.ErrTextConflict, http.StatusConflict, "text_conflict"},
	{files.ErrChanged, http.StatusConflict, "file_changed"},
	{files.ErrInvalidJSON, http.StatusBadRequest, "invalid_json"},
	{files.ErrNotText, http.StatusBadRequest, "not_text"},
	{files.ErrProtected, http.StatusConflict, "vm_file_protected"},
	{files.ErrNotFound, http.StatusNotFound, "file_not_found"},
	{files.ErrExists, http.StatusConflict, "file_exists"},
	{files.ErrLinkOutside, http.StatusForbidden, "file_link_outside"},
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

// filesArea is the file manager; an area with narrow dependencies (PLAN 8a,
// see fansArea). shares, storage and usb say where an account may work,
// store holds what the file manager keeps on the stick, fileIndex and thumbs
// are the content search and the previews (nil: none). maintenance reads
// the planned maintenance when it is asked (nil: none; tests start one on a
// running server); admitOperation admits a job unless maintenance runs.
type filesArea struct {
	files          *files.Manager
	shares         *shares.Manager
	storage        *storage.Manager
	usb            *usb.Manager
	store          *config.Store
	fileIndex      *fileindex.Manager
	thumbs         *thumbs.Manager
	maintenance    func() *maintenance.Manager
	admitOperation admitFunc
}

// filesArea is the area of the server; the tools of the catalog and of the
// KI-Agent, WebDAV and "open with" ask it for the roots of an account.
func (s *Server) filesArea() filesArea {
	return filesArea{files: s.opts.Files, shares: s.opts.Shares, storage: s.opts.Storage, usb: s.opts.USB, store: s.opts.Store,
		fileIndex: s.opts.FileIndex, thumbs: s.opts.Thumbs, admitOperation: s.admitOperation,
		maintenance: func() *maintenance.Manager { return s.opts.Maintenance }}
}

func (a filesArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/files/roots", g.user(a.handleFileRoots))
	mux.HandleFunc("GET /api/v1/files/undo", g.user(a.handleFileUndoList))
	mux.HandleFunc("POST /api/v1/files/undo/{id}", g.user(a.handleFileUndo))
	mux.HandleFunc("POST /api/v1/files/rename-preview", g.user(a.handleFileRenamePreview))
	mux.HandleFunc("POST /api/v1/files/rename-batch", g.user(a.handleFileRenameBatch))
	mux.HandleFunc("POST /api/v1/files/archive", g.user(a.handleFileArchiveList))
	mux.HandleFunc("POST /api/v1/files/extract", g.user(a.handleFileExtract))
	mux.HandleFunc("GET /api/v1/files/text/previous", g.user(a.handleFilePrevious))
	mux.HandleFunc("POST /api/v1/files/duplicates", g.user(a.handleFileDuplicates))
	mux.HandleFunc("POST /api/v1/files/search", g.user(a.handleFileSearch))
	mux.HandleFunc("GET /api/v1/files/thumb", g.user(a.handleFileThumb))
	mux.HandleFunc("GET /api/v1/files/photos", g.user(a.handleFilePhotos))
	mux.HandleFunc("GET /api/v1/files/index", g.admin(a.handleFileIndex))
	mux.HandleFunc("PUT /api/v1/files/index", g.admin(a.handleSetFileIndex))
	mux.HandleFunc("POST /api/v1/files/index/run", g.admin(a.handleFileIndexRun))
	mux.HandleFunc("DELETE /api/v1/files/index/{volume}", g.admin(a.handleFileIndexRemove))
	mux.HandleFunc("DELETE /api/v1/files/thumbs/{volume}", g.admin(a.handleThumbCacheClear))
	mux.HandleFunc("POST /api/v1/files/compare", g.user(a.handleFileCompare))
	mux.HandleFunc("GET /api/v1/files/properties", g.user(a.handleFileProperties))
	mux.HandleFunc("GET /api/v1/files/list", g.user(a.handleFileList))
	mux.HandleFunc("GET /api/v1/files/download", g.user(a.handleFileDownload))
	mux.HandleFunc("GET /api/v1/files/zip", g.user(a.handleFileZip))
	mux.HandleFunc("POST /api/v1/files/create", g.user(a.handleFileCreate))
	mux.HandleFunc("POST /api/v1/files/mkdir", g.user(a.handleFileMkdir))
	mux.HandleFunc("POST /api/v1/files/rename", g.user(a.handleFileRename))
	mux.HandleFunc("POST /api/v1/files/delete", g.user(a.handleFileDelete))
	mux.HandleFunc("GET /api/v1/files/trash", g.user(a.handleTrashList))
	mux.HandleFunc("POST /api/v1/files/trash/{action}", g.user(a.handleTrash))
	mux.HandleFunc("GET /api/v1/files/text", g.user(a.handleReadFileText))
	mux.HandleFunc("PUT /api/v1/files/text", g.user(a.handleFileText))
	mux.HandleFunc("PUT /api/v1/files/content", g.user(a.handleFileContent))
	mux.HandleFunc("POST /api/v1/files/inspect", g.user(a.handleFileInspect))
	mux.HandleFunc("POST /api/v1/files/copy", g.user(a.handleFileCopy))
	mux.HandleFunc("POST /api/v1/files/url-download/probe", g.user(a.handleFileDownloadProbe))
	mux.HandleFunc("POST /api/v1/files/url-download", g.user(a.handleFileURLDownload))
	mux.HandleFunc("POST /api/v1/files/jobs/{id}/control", g.user(a.handleFileJobControl))
	mux.HandleFunc("POST /api/v1/files/usage", g.user(a.handleFileUsage))
	mux.HandleFunc("GET /api/v1/files/jobs", g.user(a.handleFileJobs))
	mux.HandleFunc("POST /api/v1/files/jobs/{id}/retry", g.user(a.handleFileJobRetry))
	mux.HandleFunc("POST /api/v1/files/jobs/{id}/cancel", g.user(a.handleFileJobCancel))
}

// registerUploads registers the routes of uploads into the file manager.
func (a filesArea) registerUploads(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/files/uploads", g.user(a.handleUploads))
	mux.HandleFunc("POST /api/v1/files/uploads/{id}/verify", g.user(a.handleUploadVerify))
	mux.HandleFunc("POST /api/v1/files/uploads", g.user(a.handleUploadStart))
	mux.HandleFunc("GET /api/v1/files/uploads/{id}", g.user(a.handleUploadStatus))
	mux.HandleFunc("PATCH /api/v1/files/uploads/{id}", g.user(a.handleUploadChunk))
	mux.HandleFunc("POST /api/v1/files/uploads/{id}/finish", g.user(a.handleUploadFinish))
	mux.HandleFunc("DELETE /api/v1/files/uploads/{id}", g.user(a.handleUploadAbort))
}

// fileRoots returns where an account may work: the shares it has access
// to and, for administrators, the volumes as a whole.
func (a filesArea) fileRoots(user auth.User) []files.Root {
	mounted := a.storage.Mounted()
	var roots []files.Root
	for _, share := range a.shares.List() {
		level, ok := share.Access[user.Name]
		if !ok || !slices.Contains(mounted, share.Volume) {
			continue
		}
		dir := a.shares.Path(share)
		stat := os.Stat
		if share.PerUser {
			// Only the account's own folder (share of the KI-Agent, PLAN 49),
			// never through a link.
			dir, stat = a.shares.UserFolder(share, user.Name), os.Lstat
		}
		if info, err := stat(dir); err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, files.Root{
			ID: "share/" + share.Name, Name: share.Name, Kind: "share", Dir: dir, Volume: share.Volume,
			Writable: level == shares.ReadWrite, Recycle: share.Recycle.Enabled, RecycleDays: share.Recycle.Days,
		})
	}
	if user.Role.CanAdminister() {
		if a.usb != nil {
			for _, drive := range a.usb.Mounted() {
				roots = append(roots, files.Root{ID: "usb/" + drive.Mount, Name: drive.Name, Kind: "usb", Dir: drive.Dir, Writable: !drive.ReadOnly})
			}
		}
		// The stick is only looked at, apart from text files. /config is a
		// link, so that listing the roots does not wake the stick up.
		roots = append(roots, files.Root{ID: "stick", Name: "TITANOS", Kind: "stick", Dir: a.store.Dir(), Editable: true,
			Hide: stickHidden, Show: stickShown})
		for _, volume := range mounted {
			dir := a.storage.MountPoint(volume)
			roots = append(roots, files.Root{ID: "volume/" + volume, Name: volume, Kind: "volume", Dir: dir, Volume: volume, Writable: true, Bins: a.volumeBins(volume, dir)})
		}
	}
	return roots
}

// volumeBins are the folders of shares on a volume whose recycle bin is on,
// relative to the volume: what an administrator deletes or replaces there
// goes to that share's bin.
func (a filesArea) volumeBins(volume, dir string) []string {
	var bins []string
	for _, share := range a.shares.List() {
		if share.Volume != volume || !share.Recycle.Enabled {
			continue
		}
		rel, err := filepath.Rel(dir, a.shares.Path(share))
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			continue
		}
		bins = append(bins, filepath.ToSlash(rel))
	}
	slices.Sort(bins)
	return bins
}

// trashRoot resolves the root of a recycle bin request: a share, or the
// folder of a share inside a volume (bin) for administrators.
func (a filesArea) trashRoot(w http.ResponseWriter, r *http.Request, id, bin string) (files.Root, bool) {
	root, ok := a.fileRoot(w, r, id)
	if !ok || bin == "" {
		return root, ok
	}
	share, ok := root.BinRoot(bin)
	if !ok {
		writeError(w, http.StatusNotFound, "file_not_found", "no such recycle bin")
	}
	return share, ok
}

// stickHidden are folders of the stick that hold password hashes, keys
// and tokens. stickShown below already leaves them out; the list stays as
// a second fence and marks the root as one with secrets (files.Root.Hide).
var stickHidden = []string{"users", "tls", "ssh", "notify", "backup", "ddns", "wireguard", "ipv64", "assistant", "sessions", "cluster", "homeassistant"}

// stickShown is all of the stick that the file manager shows: settings
// without secrets. A folder ("apps/") is shown with everything in it, apart
// from .env files; anything not named here, such as the settings of Home
// Assistant, plugins, the UPS and every key file, does
// not exist as far as the file manager can tell.
var stickShown = []string{
	"apps/", "network/", "shares/", "storage/", "firewall/",
	"docker/settings.json", "docker/sources.json", "docker/auto-update.json", "docker/lan-networks.json", "docker/proxy.json",
	"plugins/settings.json", "plugins/sources.json",
	"system/settings.json", "system/terminal.json", "system/backup.json", "system/energy.json", "system/fans.json",
	"system/power-button.json", "system/power-schedule.json", "system/memory.json", "system/monitoring.json",
	"system/maintenance.json", "system/login.json", "system/intel-gpu.json", "system/update.json", "system/ssh.json",
	"system/webdav.json", "system/health-report.json",
}

// requestRoots are the roots of the account behind a request. The stick is
// offered only to a browser session on this NAS: API tokens, MCP clients,
// the AI agent and other nodes of the cluster never see it. Tokens, MCP
// clients and the agent also never see .titanos, .env files and unfinished
// uploads in any root (machineRoots).
func (a filesArea) requestRoots(r *http.Request) []files.Root {
	roots := a.fileRoots(currentUser(r))
	if webSession(r) && !viaCluster(r) {
		return roots
	}
	roots = slices.DeleteFunc(roots, func(root files.Root) bool { return root.Kind == "stick" })
	if !webSession(r) {
		roots = machineRoots(roots)
	}
	return roots
}

// machineRoots marks roots as handed to a token, an MCP client or the AI
// agent. The tools of the catalog refuse hidden paths themselves; the
// routes open to tokens (files_read_text, files_copy …) get the same fence
// through the root, so that no route can be forgotten.
func machineRoots(roots []files.Root) []files.Root {
	for i := range roots {
		roots[i].Machine = true
	}
	return roots
}

// fileRoot resolves the root a request names, or answers it with an error.
func (a filesArea) fileRoot(w http.ResponseWriter, r *http.Request, id string) (files.Root, bool) {
	for _, root := range a.requestRoots(r) {
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
	// Indexed: the full-text index covers (part of) this root.
	Indexed bool `json:"indexed,omitempty"`
}

func (a filesArea) handleFileRoots(w http.ResponseWriter, r *http.Request) {
	list := []rootResponse{}
	for _, root := range a.requestRoots(r) {
		var total, free uint64
		if root.Kind != "stick" {
			total, free = files.Usage(root)
		}
		indexed := false
		if index := a.fileIndex; index != nil && (root.Kind == "share" || root.Kind == "volume") {
			if rel := thumbs.VolumeRel(index.MountRoot, root.Volume, root.Dir, ""); rel != "" {
				indexed = index.Covers(root.Volume, strings.TrimPrefix(rel, "."))
			}
		}
		list = append(list, rootResponse{Root: root, TotalBytes: total, FreeBytes: free, Indexed: indexed})
	}
	writeJSON(w, http.StatusOK, list)
}

func (a filesArea) handleFileList(w http.ResponseWriter, r *http.Request) {
	root, ok := a.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	entries, err := a.files.List(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a filesArea) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, req.Path))
	root, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := a.files.Mkdir(root, req.Path, currentUser(r).UID); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (a filesArea) handleFileRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, From, To string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, req.From)+" → "+req.To)
	root, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := a.files.Rename(root, req.From, req.To); err != nil {
		writeFileError(w, err)
		return
	}
	undo := a.files.RecordUndo(currentUser(r).Name, "rename", []files.UndoMove{{CurrentRoot: root, OriginalRoot: root, Current: req.To, Original: req.From}})
	writeJSON(w, http.StatusOK, map[string]string{"undo": undo})
}

func (a filesArea) handleFileDelete(w http.ResponseWriter, r *http.Request) {
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
	root, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	if req.Versions != nil {
		if err := a.files.CheckVersions(root, req.Paths, req.Versions); err != nil {
			writeFileError(w, err)
			return
		}
	}
	undo, err := a.files.DeleteWithUndo(root, req.Paths, user.Name, req.Permanent)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("files deleted", "root", root.ID, "paths", req.Paths, "permanent", req.Permanent || undo == "", "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"undo": undo})
}

func (a filesArea) handleTrashList(w http.ResponseWriter, r *http.Request) {
	root, ok := a.trashRoot(w, r, r.URL.Query().Get("root"), r.URL.Query().Get("bin"))
	if !ok {
		return
	}
	user := currentUser(r)
	if r.URL.Query().Get("flat") == "1" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		entries, err := a.files.TrashFlat(ctx, root, user.Name, user.Role.CanAdminister())
		if err != nil {
			writeFileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entries)
		return
	}
	entries, err := a.files.TrashList(root, r.URL.Query().Get("path"), user.Name, user.Role.CanAdminister())
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleTrash restores files from a share's recycle bin or deletes them
// from it for good.
func (a filesArea) handleTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Bin string
		Paths     []string
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlaces(req.Root, req.Paths)...)
	root, ok := a.trashRoot(w, r, req.Root, req.Bin)
	if !ok {
		return
	}
	user := currentUser(r)
	action := r.PathValue("action")
	var err error
	switch action {
	case "preview":
		preview, err := a.files.TrashPreview(root, req.Paths, user.Name, user.Role.CanAdminister())
		if err != nil {
			writeFileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	case "restore":
		err = a.files.TrashRestore(root, req.Paths, user.Name, user.UID, user.Role.CanAdminister())
	case "delete":
		err = a.files.TrashDelete(root, req.Paths, user.Name, user.Role.CanAdminister())
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

func (a filesArea) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	root, ok := a.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	file, info, err := a.files.Open(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer file.Close()
	// A link downloads under its own name, not that of its target.
	name := path.Base(r.URL.Query().Get("path"))
	attachment(w, name)
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.URL.Query().Get("media") == "1" {
		mediaType := map[string]string{".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".m4a": "audio/mp4", ".flac": "audio/flac", ".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg"}[strings.ToLower(path.Ext(name))]
		if mediaType == "" {
			writeError(w, http.StatusBadRequest, "invalid_path", "unsupported media")
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	http.ServeContent(w, r, "", info.ModTime(), file)
}

// handleFileZip streams several files or folders as one zip archive.
func (a filesArea) handleFileZip(w http.ResponseWriter, r *http.Request) {
	root, ok := a.fileRoot(w, r, r.URL.Query().Get("root"))
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
	out := &headerWriter{w: w}
	if err := a.files.Zip(out, root, paths); err != nil {
		if !out.started {
			// Nothing was sent yet (a path that is not there or hidden):
			// a proper answer instead of a broken connection.
			w.Header().Del("Content-Disposition")
			writeFileError(w, err)
			return
		}
		// The answer has started; all that is left is to end it early.
		slog.Warn("zip download", "root", root.ID, "err", err)
		panic(http.ErrAbortHandler)
	}
}

func (a filesArea) handleUploadStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, Dir, Name, Conflict string
		Size                      int64
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, path.Join(req.Dir, req.Name)))
	root, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	status, err := a.files.StartUpload(root, req.Dir, req.Name, req.Size, req.Conflict, user.Name, user.UID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

func (a filesArea) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeUpload(w, r) {
		return
	}
	status, err := a.files.UploadStatus(r.PathValue("id"), currentUser(r).Name)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleUploadChunk appends the request body to an upload. The body goes
// straight to the volume; nothing is kept in RAM or on the stick.
func (a filesArea) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeUpload(w, r) {
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		writeFileError(w, files.ErrInvalid)
		return
	}
	status, err := a.files.WriteChunkContext(r.Context(), r.PathValue("id"), currentUser(r).Name, offset, r.Body)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a filesArea) handleUploadFinish(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeUpload(w, r) {
		return
	}
	user := currentUser(r)
	status, err := a.files.FinishUpload(r.PathValue("id"), user.Name)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("file uploaded", "name", status.Name, "bytes", status.Size, "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, status)
}

func (a filesArea) handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeUpload(w, r) {
		return
	}
	if err := a.files.AbortUpload(r.PathValue("id"), currentUser(r).Name); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a filesArea) handleUploads(w http.ResponseWriter, r *http.Request) {
	root, ok := a.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	uploads, err := a.files.Uploads(root, r.URL.Query().Get("path"), currentUser(r).Name, currentUser(r).UID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, 200, uploads)
}
func (a filesArea) handleUploadVerify(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeUpload(w, r) {
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		writeFileError(w, files.ErrInvalid)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err = a.files.VerifyChunk(r.PathValue("id"), currentUser(r).Name, offset, r.Body); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a filesArea) authorizeUpload(w http.ResponseWriter, r *http.Request) bool {
	previous, err := a.files.UploadRoot(r.PathValue("id"), currentUser(r).Name)
	if err != nil {
		writeFileError(w, err)
		return false
	}
	root, ok := a.fileRoot(w, r, previous.ID)
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

func (a filesArea) handleReadFileText(w http.ResponseWriter, r *http.Request) {
	root, ok := a.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	result, err := a.files.ReadText(root, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleFileText saves an existing text file within the account's writable roots.
func (a filesArea) handleFileText(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path, Content, Version string }
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if !readJSONBody(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, req.Path))
	root, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if req.Version == "" {
		writeFileError(w, files.ErrTextConflict)
		return
	}
	result, err := a.files.SaveTextVersion(root, req.Path, req.Content, req.Version)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("text file saved", "root", root.ID, "path", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, result)
}

// handleFileContent writes a whole file from the request body (PLAN 66:
// a web app saves into the original). ?root=&path=&expect=<mtime-ns |
// RFC 3339 "modified" | none>; Content-Length is required.
func (a filesArea) handleFileContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	noteTarget(r, filePlace(q.Get("root"), q.Get("path")))
	root, ok := a.fileRoot(w, r, q.Get("root"))
	if !ok {
		return
	}
	expect, err := files.ParseExpect(q.Get("expect"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expect must be none, the modification time in nanoseconds or an RFC 3339 time")
		return
	}
	if r.ContentLength < 0 {
		writeError(w, http.StatusLengthRequired, "length_required", "Content-Length is required")
		return
	}
	if r.ContentLength > files.MaxContent {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the file is larger than 2 GiB")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, r.ContentLength)
	user := currentUser(r)
	result, err := a.files.WriteContent(root, q.Get("path"), r.Body, r.ContentLength, user.UID, expect)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("file saved by web app", "root", root.ID, "path", q.Get("path"), "size", result.Size, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, result)
}

// handleFileCopy starts copying or moving files between roots.
func (a filesArea) handleFileCopy(w http.ResponseWriter, r *http.Request) {
	if a.maintenance() != nil {
		leave, ok := a.admitOperation(w)
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
	src, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	dst, ok := a.fileRoot(w, r, req.ToRoot)
	if !ok {
		return
	}
	user := currentUser(r)
	job, err := a.files.StartCopyChoices(src, req.Paths, dst, req.ToDir, req.Move, req.Conflict, user.Name, user.UID, req.Choices)
	if err != nil {
		writeFileError(w, err)
		return
	}
	slog.Info("copy started", "from", src.ID, "to", dst.ID, "move", req.Move, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, job)
}

func (a filesArea) handleFileJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.files.Jobs(currentUser(r).Name))
}

func (a filesArea) handleFileJobCancel(w http.ResponseWriter, r *http.Request) {
	if err := a.files.CancelJob(currentUser(r).Name, r.PathValue("id")); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a filesArea) handleFileInspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root, ToRoot, ToDir string
		Paths               []string
	}
	if !readJSON(w, r, &req) {
		return
	}
	src, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	var dst *files.Root
	if req.ToRoot != "" {
		resolved, ok := a.fileRoot(w, r, req.ToRoot)
		if !ok {
			return
		}
		dst = &resolved
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := a.files.Inspect(ctx, src, req.Paths, dst, req.ToDir)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a filesArea) handleFileJobRetry(w http.ResponseWriter, r *http.Request) {
	if a.maintenance() != nil {
		leave, ok := a.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
	}
	user := currentUser(r)
	for _, job := range a.files.Jobs(user.Name) {
		if job.ID == r.PathValue("id") && job.Operation == "download" {
			// A download has no source root; its target is checked again.
			dst, ok := a.fileRoot(w, r, job.To)
			if !ok {
				return
			}
			result, err := a.files.RetryDownload(user.Name, job.ID, dst)
			if err != nil {
				writeFileError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, result)
			return
		}
		if job.ID == r.PathValue("id") {
			src, ok := a.fileRoot(w, r, job.From)
			if !ok {
				return
			}
			dst, ok := a.fileRoot(w, r, job.To)
			if !ok {
				return
			}
			result, err := a.files.RetryCopy(user.Name, job.ID, src, dst, user.UID)
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

func (a filesArea) handleFileProperties(w http.ResponseWriter, r *http.Request) {
	root, ok := a.fileRoot(w, r, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := a.files.Properties(ctx, root, r.URL.Query().Get("path"), r.URL.Query().Get("checksum") == "sha256")
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a filesArea) handleFileCreate(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, req.Path))
	root, ok := a.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	if err := a.files.CreateText(root, req.Path, currentUser(r).UID); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
