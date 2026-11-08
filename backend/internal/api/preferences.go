package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

// preferenceKinds are the per-account settings the web UI stores. The
// backend keeps them as opaque JSON objects; their shape belongs to the UI.
var preferenceKinds = []string{"appearance", "dashboard", "sound", "docker", "vnc", "files", "bell", "shares", "hints", "desktop", "fileapps", "fileappsdocker"}

// maxBackgroundBytes limits an uploaded background image. It is stored on the stick.
const maxBackgroundBytes = 10 << 20

// Up to 100 individually configured dashboard tiles no longer fit 64 KiB.
// Keep this exception per preference kind; other JSON endpoints stay bounded.
const maxDashboardPreferences = 512 << 10

var backgroundTypes = []string{"image/jpeg", "image/png", "image/webp"}

// preferences reads and writes /config/users/<name>/<kind>.json and keeps
// what it has read in memory, so that loading a page does not wake the stick.
type preferences struct {
	store *config.Store

	mu    sync.Mutex
	cache map[string]json.RawMessage // key: user + "/" + kind
	// locks caches the field lockMinutes of the desktop preferences per
	// account, read on every request of a session (session_lock.go).
	locks map[string]lockTimes
	// lockFailed is when reading them last failed, per account.
	lockFailed map[string]time.Time
}

func newPreferences(store *config.Store) *preferences {
	return &preferences{store: store, cache: map[string]json.RawMessage{}}
}

func preferenceFile(user, kind string) string {
	return "users/" + user + "/" + kind + ".json"
}

// get returns the stored JSON object, or nil if nothing was stored yet.
func (p *preferences) get(user, kind string) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := user + "/" + kind
	if value, ok := p.cache[key]; ok {
		return value, nil
	}
	data, err := p.store.Read(preferenceFile(user, kind))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = nil
	case err != nil:
		return nil, err
	case !isJSONObject(data):
		slog.Warn("ignoring damaged preferences", "user", user, "kind", kind)
		data = nil
	}
	p.cache[key] = data
	return data, nil
}

func (p *preferences) set(user, kind string, value json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.store.Write(preferenceFile(user, kind), append(bytes.TrimSpace(value), '\n')); err != nil {
		return err
	}
	p.cache[user+"/"+kind] = value
	if kind == "desktop" {
		delete(p.locks, user)
		delete(p.lockFailed, user)
	}
	return nil
}

// forget drops what is cached for a deleted account.
func (p *preferences) forget(user string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, kind := range preferenceKinds {
		delete(p.cache, user+"/"+kind)
	}
	delete(p.locks, user)
}

func isJSONObject(data []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(data, &object) == nil && object != nil
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	res := map[string]json.RawMessage{}
	for _, kind := range preferenceKinds {
		value, err := s.prefs.get(user, kind)
		if err != nil {
			slog.Error("read preferences", "user", user, "kind", kind, "err", err)
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config could not be read")
			return
		}
		if value == nil {
			value = json.RawMessage("null")
		}
		res[kind] = value
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !slices.Contains(preferenceKinds, kind) {
		writeError(w, http.StatusNotFound, "not_found", "no such preference")
		return
	}
	var value json.RawMessage
	if kind == "dashboard" && isMultipartImageUpload(r) {
		s.handlePutDashboardImages(w, r)
		return
	}
	if kind == "dashboard" {
		r.Body = http.MaxBytesReader(w, r.Body, maxDashboardPreferences)
		if !readJSONBody(w, r, &value) {
			return
		}
	} else if !readJSON(w, r, &value) {
		return
	}
	if !isJSONObject(value) {
		writeError(w, http.StatusBadRequest, "bad_request", "preferences must be a JSON object")
		return
	}
	if kind == "dashboard" {
		s.saveDashboard(w, r, value, nil)
		return
	}
	if kind == "fileappsdocker" && !validDockerFileApps(value) {
		writeError(w, http.StatusBadRequest, "bad_request", `fileappsdocker maps file endings (lower case, without dot) to "docker:<app>"`)
		return
	}
	if kind == "fileapps" && !validFileApps(value) {
		writeError(w, http.StatusBadRequest, "bad_request", `fileapps maps file endings (lower case, without dot) to "webapp:<id>" or "editor"`)
		return
	}
	user := currentUser(r).Name
	if err := s.prefs.set(user, kind, value); err != nil {
		slog.Error("write preferences", "user", user, "kind", kind, "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func backgroundFile(user string) string {
	return "users/" + user + "/background"
}

func (s *Server) handleGetBackground(w http.ResponseWriter, r *http.Request) {
	data, err := s.opts.Store.Read(backgroundFile(currentUser(r).Name))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no background image")
		return
	}
	contentType := http.DetectContentType(data)
	if !slices.Contains(backgroundTypes, contentType) {
		writeError(w, http.StatusNotFound, "not_found", "no background image")
		return
	}
	// The web UI adds a version to the URL whenever the image changes, so the
	// browser may keep it and the stick is read once per image.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Write(data)
}

func (s *Server) handlePutBackground(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBackgroundBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the image is larger than 10 MB")
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "could not read the image")
		}
		return
	}
	if !slices.Contains(backgroundTypes, http.DetectContentType(data)) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_type", "the image must be JPEG, PNG or WebP")
		return
	}
	user := currentUser(r).Name
	if err := s.opts.Store.Write(backgroundFile(user), data); err != nil {
		slog.Error("write background", "user", user, "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the image could not be saved to /config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteBackground(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	if err := s.opts.Store.Remove(backgroundFile(user)); err != nil {
		slog.Error("remove background", "user", user, "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the image could not be removed from /config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fileAppsExt is a file ending as the default apps store it (PLAN 66).
var fileAppsExt = regexp.MustCompile(`^[a-z0-9]{1,10}$`)

// validDockerFileApps checks the preference kind fileappsdocker (PLAN 72):
// the default Docker app per file ending, {"3mf":"docker:bambu-lab"}. It is
// a kind of its own, so that titand 0.66 keeps reading and writing
// fileapps unchanged after going back.
func validDockerFileApps(value json.RawMessage) bool {
	var m map[string]string
	if json.Unmarshal(value, &m) != nil || len(m) > 200 {
		return false
	}
	for ext, app := range m {
		name, ok := strings.CutPrefix(app, "docker:")
		if !fileAppsExt.MatchString(ext) || !ok || !docker.ValidAppName(name) {
			return false
		}
	}
	return true
}

// validFileApps checks the preference kind fileapps: the default app per
// file ending, {"docx":"webapp:wordcraft","txt":"editor"}.
func validFileApps(value json.RawMessage) bool {
	var m map[string]string
	if json.Unmarshal(value, &m) != nil || len(m) > 200 {
		return false
	}
	for ext, app := range m {
		if !fileAppsExt.MatchString(ext) {
			return false
		}
		if app == "editor" {
			continue
		}
		id, ok := strings.CutPrefix(app, "webapp:")
		if _, known := webapps.Lookup(id); !ok || !known {
			return false
		}
	}
	return true
}
