package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
)

// WebDAV (docs/WEBDAV.md): the shares of an account under /dav/<share>/,
// on the port of the web UI. Off unless switched on in Settings › Access.
// An account signs in with its name and a WebDAV password TitanOS makes
// (shown once, stored as SHA-256 like API tokens): apps send it with every
// request, so a slow password hash would not do, and the account password
// and second factor stay out of apps.

const (
	davSettingsFile = "system/webdav.json"
	davPrefix       = "/dav"
)

type davSettings struct {
	Enabled bool `json:"enabled"`
}

type davState struct {
	mu       sync.Mutex
	loaded   bool
	settings davSettings
	locks    webdav.LockSystem
}

func davPasswordFile(user string) string { return "users/" + user + "/webdav.json" }

type davPassword struct {
	SHA256  string    `json:"sha256"`
	Created time.Time `json:"created"`
}

func (s *Server) davSettings() davSettings {
	d := &s.dav
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded {
		var settings davSettings
		if err := s.opts.Store.ReadJSON(davSettingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("WebDAV settings not readable", "err", err)
		}
		d.settings, d.loaded, d.locks = settings, true, webdav.NewMemLS()
	}
	return d.settings
}

func (s *Server) handleWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.davSettings())
}

func (s *Server) handleWebDAVSettingsSet(w http.ResponseWriter, r *http.Request) {
	var req davSettings
	if !readJSON(w, r, &req) {
		return
	}
	s.davSettings()
	s.dav.mu.Lock()
	defer s.dav.mu.Unlock()
	if err := s.opts.Store.WriteJSON(davSettingsFile, req); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	s.dav.settings = req
	writeJSON(w, http.StatusOK, req)
}

// handleAccountWebDAV tells whether the account has a WebDAV password.
func (s *Server) handleAccountWebDAV(w http.ResponseWriter, r *http.Request) {
	var p davPassword
	err := s.opts.Store.ReadJSON(davPasswordFile(currentUser(r).Name), &p)
	answer := map[string]any{"enabled": s.davSettings().Enabled, "set": err == nil && p.SHA256 != ""}
	if err == nil && p.SHA256 != "" {
		answer["created"] = p.Created
	}
	writeJSON(w, http.StatusOK, answer)
}

// handleAccountWebDAVNew makes a new WebDAV password and shows it once.
func (s *Server) handleAccountWebDAVNew(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(password))
	if err := s.opts.Store.WriteJSON(davPasswordFile(currentUser(r).Name), davPassword{SHA256: hex.EncodeToString(sum[:]), Created: time.Now().UTC()}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser(r).Name, "password": password})
}

func (s *Server) handleAccountWebDAVDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Store.Remove(davPasswordFile(currentUser(r).Name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// davUser checks the Basic credentials of a request.
func (s *Server) davUser(r *http.Request) (auth.User, bool, time.Duration) {
	name, password, ok := r.BasicAuth()
	ip := remoteIP(r)
	if wait := s.limiter.RetryAfter("dav:"+ip, "dav-user:"+name); wait > 0 {
		return auth.User{}, false, wait
	}
	if !ok || name == "" || len(password) > 256 {
		return auth.User{}, false, 0
	}
	var stored davPassword
	user, known := s.opts.Users.Get(name)
	err := s.opts.Store.ReadJSON(davPasswordFile(name), &stored)
	sum := sha256.Sum256([]byte(password))
	want, _ := hex.DecodeString(stored.SHA256)
	if !known || err != nil || len(want) != len(sum) || subtle.ConstantTimeCompare(sum[:], want) != 1 || user.Disabled {
		s.limiter.Fail("dav:"+ip, "dav-user:"+name)
		slog.Warn("WebDAV sign-in refused", "user", name, "ip", ip)
		return auth.User{}, false, 0
	}
	return user, true, 0
}

// handleWebDAV serves /dav/…: the shares of the account as folders.
func (s *Server) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	if !s.davSettings().Enabled {
		http.NotFound(w, r)
		return
	}
	user, ok, wait := s.davUser(r)
	if !ok {
		if wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, "too many attempts", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="TitanOS", charset="UTF-8"`)
		http.Error(w, "sign in with your WebDAV password", http.StatusUnauthorized)
		return
	}
	var roots []files.Root
	for _, root := range s.fileRoots(user) {
		if root.Kind == "share" {
			roots = append(roots, root)
		}
	}
	handler := &webdav.Handler{
		Prefix:     davPrefix,
		FileSystem: &davShares{roots: roots, open: func(root files.Root) webdav.FileSystem { return s.opts.Files.DAV(root, user.Name, user.UID) }},
		LockSystem: s.dav.locks,
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Info("WebDAV", "user", user.Name, "method", r.Method, "path", r.URL.Path, "err", err)
			}
		},
	}
	handler.ServeHTTP(w, r)
}

// davShares puts the shares of an account side by side: "/" lists them,
// "/<share>/…" is inside one. Nothing can be made at the top.
type davShares struct {
	roots []files.Root
	open  func(files.Root) webdav.FileSystem
}

func (d *davShares) split(name string) (webdav.FileSystem, string, bool) {
	name = strings.TrimPrefix(name, "/")
	share, rest, _ := strings.Cut(name, "/")
	if share == "" {
		return nil, "", false
	}
	i := slices.IndexFunc(d.roots, func(r files.Root) bool { return r.Name == share })
	if i < 0 {
		return nil, "", false
	}
	return d.open(d.roots[i]), "/" + rest, true
}

func (d *davShares) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if fsys, rest, ok := d.split(name); ok && rest != "/" {
		return fsys.Mkdir(ctx, rest, perm)
	}
	return os.ErrPermission
}

func (d *davShares) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if strings.Trim(name, "/") == "" {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
			return nil, os.ErrPermission
		}
		return &davTop{shares: d}, nil
	}
	if fsys, rest, ok := d.split(name); ok {
		return fsys.OpenFile(ctx, rest, flag, perm)
	}
	return nil, os.ErrNotExist
}

func (d *davShares) RemoveAll(ctx context.Context, name string) error {
	if fsys, rest, ok := d.split(name); ok && rest != "/" {
		return fsys.RemoveAll(ctx, rest)
	}
	return os.ErrPermission
}

func (d *davShares) Rename(ctx context.Context, oldName, newName string) error {
	from, oldRest, ok1 := d.split(oldName)
	_, newRest, ok2 := d.split(newName)
	oldShare, _, _ := strings.Cut(strings.TrimPrefix(oldName, "/"), "/")
	newShare, _, _ := strings.Cut(strings.TrimPrefix(newName, "/"), "/")
	if !ok1 || !ok2 || oldShare != newShare {
		// Between shares is a copy, which the client does on its own.
		return os.ErrPermission
	}
	return from.Rename(ctx, oldRest, newRest)
}

func (d *davShares) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if strings.Trim(name, "/") == "" {
		return davDirInfo{name: "/"}, nil
	}
	fsys, rest, ok := d.split(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	if rest == "/" {
		share, _, _ := strings.Cut(strings.TrimPrefix(name, "/"), "/")
		return davDirInfo{name: share}, nil
	}
	return fsys.Stat(ctx, rest)
}

// davTop is the folder of shares.
type davTop struct {
	shares *davShares
	read   bool
}

func (t *davTop) Close() error                   { return nil }
func (t *davTop) Read([]byte) (int, error)       { return 0, os.ErrInvalid }
func (t *davTop) Write([]byte) (int, error)      { return 0, os.ErrPermission }
func (t *davTop) Seek(int64, int) (int64, error) { return 0, nil }
func (t *davTop) Stat() (fs.FileInfo, error)     { return davDirInfo{name: "/"}, nil }
func (t *davTop) Readdir(count int) ([]fs.FileInfo, error) {
	if t.read {
		return nil, nil
	}
	t.read = true
	out := []fs.FileInfo{}
	for _, root := range t.shares.roots {
		out = append(out, davDirInfo{name: root.Name})
	}
	return out, nil
}

type davDirInfo struct{ name string }

func (i davDirInfo) Name() string       { return i.name }
func (i davDirInfo) Size() int64        { return 0 }
func (i davDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (i davDirInfo) ModTime() time.Time { return time.Time{} }
func (i davDirInfo) IsDir() bool        { return true }
func (i davDirInfo) Sys() any           { return nil }
