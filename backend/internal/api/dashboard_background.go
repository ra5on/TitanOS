package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

const maxDashboardImages = 5

var backgroundID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func dashboardImageFile(user, id string) string { return "users/" + user + "/backgrounds/" + id }

// Images are immutable and addressed by their content. The atomically replaced
// dashboard preference is the commit point: failed saves never replace the
// previous selection or the legacy appearance image used by older app versions.
func dashboardImageIDs(value []byte) ([]string, error) {
	var prefs struct {
		Visual struct {
			Images []struct {
				ID string `json:"id"`
			} `json:"images"`
			Background struct {
				Source string `json:"source"`
				ID     string `json:"id"`
			} `json:"background"`
		} `json:"visual"`
	}
	if err := json.Unmarshal(value, &prefs); err != nil {
		return nil, err
	}
	if len(prefs.Visual.Images) > maxDashboardImages {
		return nil, errors.New("too many images")
	}
	ids := []string{}
	for _, image := range prefs.Visual.Images {
		if !backgroundID.MatchString(image.ID) || slices.Contains(ids, image.ID) {
			return nil, errors.New("invalid image ID")
		}
		ids = append(ids, image.ID)
	}
	if prefs.Visual.Background.Source == "custom" && !slices.Contains(ids, prefs.Visual.Background.ID) {
		return nil, errors.New("selected image is not in the library")
	}
	return ids, nil
}

func (s *Server) handlePutDashboardImages(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDashboardImages*maxBackgroundBytes+maxDashboardPreferences+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, 400, "bad_request", "invalid image upload")
		return
	}
	var value []byte
	images := map[string][]byte{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, 400, "bad_request", "invalid image upload")
			return
		}
		switch part.FormName() {
		case "preferences":
			if value != nil {
				writeError(w, 400, "bad_request", "duplicate preferences")
				return
			}
			value, err = io.ReadAll(io.LimitReader(part, maxDashboardPreferences+1))
			if err != nil || len(value) > maxDashboardPreferences {
				writeError(w, 413, "too_large", "dashboard preferences are too large")
				return
			}
		case "image":
			id := part.FileName()
			if !backgroundID.MatchString(id) || images[id] != nil || len(images) >= maxDashboardImages {
				writeError(w, 400, "bad_request", "invalid image ID")
				return
			}
			data, err := io.ReadAll(io.LimitReader(part, maxBackgroundBytes+1))
			if err != nil || len(data) > maxBackgroundBytes {
				writeError(w, 413, "too_large", "the image is larger than 10 MB")
				return
			}
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != id {
				writeError(w, 400, "bad_request", "image digest mismatch")
				return
			}
			if !slices.Contains(backgroundTypes, http.DetectContentType(data)) {
				writeError(w, 415, "unsupported_type", "the image must be JPEG, PNG or WebP")
				return
			}
			images[id] = data
		default:
			writeError(w, 400, "bad_request", "unknown upload field")
			return
		}
	}
	if !isJSONObject(value) {
		writeError(w, 400, "bad_request", "preferences must be a JSON object")
		return
	}
	s.saveDashboard(w, r, value, images)
}

func (s *Server) saveDashboard(w http.ResponseWriter, r *http.Request, value json.RawMessage, images map[string][]byte) {
	ids, err := dashboardImageIDs(value)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid background library")
		return
	}
	for id := range images {
		if !slices.Contains(ids, id) {
			writeError(w, 400, "bad_request", "unreferenced image")
			return
		}
	}
	user := currentUser(r).Name
	p := s.prefs
	p.mu.Lock()
	defer p.mu.Unlock()
	key := user + "/dashboard"
	old, known := p.cache[key]
	if !known {
		old, err = p.store.Read(preferenceFile(user, "dashboard"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			writeError(w, 503, "config_unavailable", "could not read preferences")
			return
		}
	}
	oldIDs, _ := dashboardImageIDs(old)
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, id := range created {
				_ = p.store.Remove(dashboardImageFile(user, id))
			}
		}
	}()
	for _, id := range ids {
		existing, readErr := p.store.Read(dashboardImageFile(user, id))
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			writeError(w, 500, "config_write_failed", "could not read image")
			return
		}
		if data, ok := images[id]; ok {
			if readErr == nil && bytes.Equal(existing, data) {
				continue
			}
			if readErr == nil {
				writeError(w, 409, "bad_request", "stored image is damaged")
				return
			}
			if err = p.store.Write(dashboardImageFile(user, id), data); err != nil {
				writeError(w, 500, "config_write_failed", "could not save image")
				return
			}
			created = append(created, id)
		} else if readErr != nil {
			writeError(w, 400, "bad_request", "background image is missing")
			return
		}
	}
	if err = p.store.Write(preferenceFile(user, "dashboard"), append(bytes.TrimSpace(value), '\n')); err != nil {
		writeError(w, 500, "config_write_failed", "could not save preferences")
		return
	}
	p.cache[key] = bytes.Clone(value)
	committed = true
	// Only during an explicit save; keep the current and previous library.
	entries, _ := os.ReadDir(filepath.Join(p.store.Dir(), "users", user, "backgrounds"))
	for _, entry := range entries {
		if !entry.IsDir() && backgroundID.MatchString(entry.Name()) && !slices.Contains(ids, entry.Name()) && !slices.Contains(oldIDs, entry.Name()) {
			_ = p.store.Remove(dashboardImageFile(user, entry.Name()))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetDashboardImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !backgroundID.MatchString(id) {
		writeError(w, 404, "not_found", "no background image")
		return
	}
	data, err := s.opts.Store.Read(dashboardImageFile(currentUser(r).Name, id))
	if err != nil || !slices.Contains(backgroundTypes, http.DetectContentType(data)) {
		writeError(w, 404, "not_found", "no background image")
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}

func isMultipartImageUpload(r *http.Request) bool {
	kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return kind == "multipart/form-data"
}
