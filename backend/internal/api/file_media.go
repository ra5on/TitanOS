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

	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
)

// relPath returns a request path as the file manager cleans it ("" = root).
func relPath(p string) string {
	p = path.Clean("/" + strings.Trim(p, "/"))
	return strings.TrimPrefix(p, "/")
}

// handleFileThumb answers with a small preview of a picture, PDF or video.
// The file is opened like a download (root, rights, os.Root, hidden paths);
// the answer is always an image, never the file itself.
func (s *Server) handleFileThumb(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	root, ok := s.fileRoot(w, r, q.Get("root"))
	if !ok {
		return
	}
	rel := relPath(q.Get("path"))
	if s.opts.Thumbs == nil || root.Kind == "stick" || thumbs.Kind(path.Base(rel)) == "" {
		writeError(w, http.StatusUnsupportedMediaType, "thumb_unsupported", "no thumbnail for this file")
		return
	}
	file, info, err := s.opts.Files.Open(root, rel)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer file.Close()
	box := 320
	if n, err := strconv.Atoi(q.Get("size")); err == nil && n > 0 {
		box = n
	}
	box = thumbs.FitSize(box)
	volumeRel := thumbs.VolumeRel(s.opts.Thumbs.MountRoot, root.Volume, root.Dir, rel)
	keyPath := volumeRel
	if keyPath == "" {
		keyPath = root.ID + "/" + rel
	}
	etag := `"` + thumbs.Key(keyPath, info, box) + `"`
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	volume := root.Volume
	if volumeRel == "" {
		volume = "" // USB drives and anything off a data volume: no cache
	}
	data, contentType, err := s.opts.Thumbs.Get(r.Context(), thumbs.Source{Volume: volume, Rel: keyPath, Name: info.Name(), File: file, Info: info}, box)
	switch {
	case errors.Is(err, thumbs.ErrBusy):
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "thumb_busy", "too many thumbnails at once")
		return
	case err != nil:
		w.Header().Set("Cache-Control", "no-store")
		if !errors.Is(err, thumbs.ErrUnsupported) && !errors.Is(err, context.Canceled) {
			slog.Warn("thumbnail", "root", root.ID, "err", err)
		}
		writeError(w, http.StatusUnsupportedMediaType, "thumb_unsupported", "no thumbnail for this file")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": "thumbnail"}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

type photoEntry struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Taken    time.Time `json:"taken,omitzero"`
	Kind     string    `json:"kind"`
}

// handleFilePhotos lists the pictures and videos of one folder for the photo
// view, with the EXIF date of JPEGs read from the first bytes of each file.
// It is not recursive and stops after 5000 entries or 20 seconds.
func (s *Server) handleFilePhotos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	root, ok := s.fileRoot(w, r, q.Get("root"))
	if !ok {
		return
	}
	if root.Kind == "stick" {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []photoEntry{}, "truncated": false})
		return
	}
	dir := relPath(q.Get("path"))
	entries, err := s.opts.Files.List(root, dir)
	if err != nil {
		writeFileError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list := []photoEntry{}
	truncated := false
	for _, entry := range entries {
		kind := thumbs.Kind(entry.Name)
		if entry.Dir || (kind != "image" && kind != "video") {
			continue
		}
		if len(list) >= 5000 || ctx.Err() != nil {
			truncated = true
			break
		}
		item := photoEntry{Name: entry.Name, Size: entry.Size, Modified: entry.Modified, Kind: kind}
		if ext := strings.ToLower(path.Ext(entry.Name)); ext == ".jpg" || ext == ".jpeg" {
			if f, _, err := s.opts.Files.Open(root, path.Join(dir, entry.Name)); err == nil {
				item.Taken = thumbs.ReadPhoto(f).Taken
				f.Close()
			}
		}
		list = append(list, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list, "truncated": truncated})
}

// contentSearch looks in the full-text index for words inside files under
// the root (and folder) the request names; the root was already checked
// against the account's current rights, and every hit is looked up again
// inside it before it is returned.
func (s *Server) contentSearch(ctx context.Context, root files.Root, opt files.SearchOptions) (files.SearchResult, error) {
	result := files.SearchResult{Entries: []files.FoundEntry{}}
	index := s.opts.FileIndex
	if index == nil || root.Volume == "" || root.Kind == "stick" || root.Kind == "usb" {
		return result, fileindex.ErrUnavailable
	}
	base := thumbs.VolumeRel(index.MountRoot, root.Volume, root.Dir, "")
	if base == "" && root.Kind != "volume" {
		return result, fileindex.ErrUnavailable
	}
	if base == "." {
		base = ""
	}
	sub := relPath(opt.Path)
	prefix := path.Join(base, sub)
	if prefix == "." {
		prefix = ""
	}
	hits, truncated, err := index.Search(ctx, root.Volume, []string{prefix}, opt.Query, 1000)
	if err != nil {
		return result, err
	}
	pinned, err := os.OpenRoot(root.Dir)
	if err != nil {
		return result, err
	}
	defer pinned.Close()
	extensions := map[string]bool{}
	for _, ext := range append(opt.Extensions, opt.Extension) {
		if ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), ".")); ext != "" {
			extensions[ext] = true
		}
	}
	for _, hit := range hits {
		rel := hit.Path
		if base != "" {
			rel = strings.TrimPrefix(rel, base+"/")
		}
		if rel == hit.Path && base != "" {
			continue
		}
		parts := strings.Split(rel, "/")
		if slices.ContainsFunc(parts, func(p string) bool { return strings.HasPrefix(p, ".") }) {
			continue
		}
		info, err := pinned.Lstat(rel)
		if err != nil || !info.Mode().IsRegular() {
			continue // gone or replaced since the last pass
		}
		if len(extensions) > 0 && !extensions[strings.ToLower(strings.TrimPrefix(path.Ext(rel), "."))] ||
			info.Size() < opt.MinSize || opt.MaxSize > 0 && info.Size() > opt.MaxSize ||
			!opt.After.IsZero() && info.ModTime().Before(opt.After) || !opt.Before.IsZero() && info.ModTime().After(opt.Before) {
			continue
		}
		result.Entries = append(result.Entries, files.FoundEntry{Entry: files.Entry{Name: info.Name(), Size: info.Size(), Modified: info.ModTime()}, Path: rel})
	}
	result.Scanned = len(hits)
	result.Truncated = truncated
	return result, nil
}

type indexShare struct {
	Name   string `json:"name"`
	Volume string `json:"volume"`
}

func (s *Server) handleFileIndex(w http.ResponseWriter, r *http.Request) {
	if s.opts.FileIndex == nil {
		writeError(w, http.StatusNotFound, "not_found", "no file index")
		return
	}
	shareList := []indexShare{}
	for _, share := range s.opts.Shares.List() {
		shareList = append(shareList, indexShare{share.Name, share.Volume})
	}
	volumes := s.opts.Storage.Names()
	if volumes == nil {
		volumes = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": s.opts.FileIndex.Status(), "volumes": volumes, "shares": shareList, "mounted": s.opts.Storage.Mounted()})
}

func (s *Server) handleSetFileIndex(w http.ResponseWriter, r *http.Request) {
	var settings fileindex.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if s.opts.FileIndex == nil {
		writeError(w, http.StatusNotFound, "not_found", "no file index")
		return
	}
	if err := s.opts.FileIndex.Set(settings); err != nil {
		if errors.Is(err, fileindex.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not save the settings")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.FileIndex.Status())
}

func (s *Server) handleFileIndexRun(w http.ResponseWriter, r *http.Request) {
	var req struct{ Volume string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Volume)
	if s.opts.FileIndex == nil || s.opts.FileIndex.Start(req.Volume) != nil {
		writeError(w, http.StatusConflict, "index_unavailable", "the full-text index is off for this volume")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleFileIndexRemove(w http.ResponseWriter, r *http.Request) {
	volume := r.PathValue("volume")
	noteTarget(r, volume)
	if s.opts.FileIndex == nil {
		writeError(w, http.StatusNotFound, "not_found", "no file index")
		return
	}
	switch err := s.opts.FileIndex.Remove(volume); {
	case errors.Is(err, fileindex.ErrRunning):
		writeError(w, http.StatusConflict, "index_running", "a pass is running")
	case errors.Is(err, fileindex.ErrInvalid), errors.Is(err, fileindex.ErrUnavailable):
		writeError(w, http.StatusNotFound, "volume_not_found", "volume not mounted")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not remove the index")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleThumbCacheClear(w http.ResponseWriter, r *http.Request) {
	volume := r.PathValue("volume")
	noteTarget(r, volume)
	if s.opts.Thumbs == nil || !slices.Contains(s.opts.Storage.Mounted(), volume) {
		writeError(w, http.StatusNotFound, "volume_not_found", "volume not mounted")
		return
	}
	if err := s.opts.Thumbs.Clear(volume); err != nil {
		writeError(w, http.StatusNotFound, "volume_not_found", "volume not mounted")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
