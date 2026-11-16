package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/diskwake"
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
	// passwords are the WebDAV passwords as read from the stick, once per
	// account: a client sends its password with every request, and /config
	// is not read for what a client repeats. Changes go through titand
	// (new, delete, forgetWebDAV), which keep this in step.
	passwords map[string]davKnown
}

// davKnown is the WebDAV password of an account as read from the stick.
// account and passwordHash name the account it was read for: a new
// account of the same name (made within the same second, too: its
// password hash has a salt of its own) reads its own. sum is nil when
// there is none.
type davKnown struct {
	account      time.Time
	passwordHash string
	sum          []byte
}

func davPasswordFile(user string) string { return "users/" + user + "/webdav.json" }

type davPassword struct {
	SHA256  string    `json:"sha256"`
	Created time.Time `json:"created"`
}

// davArea is WebDAV: its switch, the WebDAV passwords of the accounts and
// the server under /dav; an area with narrow dependencies (PLAN 8a, see
// fansArea). store holds switch and passwords, state what was read and the
// locks (state of the server), users the accounts, files the folders.
// limiter is the limit against guessed passwords (state of the server).
// webSession and reconfirm guard a new password; fileRoots says where an
// account may work.
type davArea struct {
	store      *config.Store
	users      *auth.Users
	files      *files.Manager
	state      *davState
	limiter    limiterFunc
	webSession webSessionFunc
	reconfirm  reconfirmFunc
	fileRoots  func(user auth.User) []files.Root
}

// davArea is the area of the server; removing an account forgets its
// WebDAV password through it.
func (s *Server) davArea() davArea {
	return davArea{store: s.opts.Store, users: s.opts.Users, files: s.opts.Files, state: &s.dav, limiter: s.rateLimiter,
		webSession: s.requireWebSession, reconfirm: s.authArea().reconfirm, fileRoots: func(user auth.User) []files.Root { return s.filesArea().fileRoots(user) }}
}

func (a davArea) register(mux *routeMux, admin, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/dav/", a.handleWebDAV)
	mux.HandleFunc("/dav", a.handleWebDAV)
	mux.HandleFunc("GET /api/v1/system/webdav", admin(a.handleWebDAVSettings))
	mux.HandleFunc("PUT /api/v1/system/webdav", admin(a.handleWebDAVSettingsSet))
	mux.HandleFunc("GET /api/v1/account/webdav", user(a.handleAccountWebDAV))
	mux.HandleFunc("POST /api/v1/account/webdav", user(a.handleAccountWebDAVNew))
	mux.HandleFunc("DELETE /api/v1/account/webdav", user(a.handleAccountWebDAVDelete))
}

func (a davArea) davSettings() davSettings {
	d := a.state
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded {
		var settings davSettings
		if err := a.store.ReadJSON(davSettingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("WebDAV settings not readable", "err", err)
		}
		d.settings, d.loaded, d.locks = settings, true, webdav.NewMemLS()
	}
	return d.settings
}

func (a davArea) handleWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.davSettings())
}

func (a davArea) handleWebDAVSettingsSet(w http.ResponseWriter, r *http.Request) {
	var req davSettings
	if !readJSON(w, r, &req) {
		return
	}
	a.davSettings()
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if err := a.store.WriteJSON(davSettingsFile, req); err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.state.settings = req
	writeJSON(w, http.StatusOK, req)
}

// handleAccountWebDAV tells whether the account has a WebDAV password.
func (a davArea) handleAccountWebDAV(w http.ResponseWriter, r *http.Request) {
	var p davPassword
	err := a.store.ReadJSON(davPasswordFile(currentUser(r).Name), &p)
	answer := map[string]any{"enabled": a.davSettings().Enabled, "set": err == nil && p.SHA256 != ""}
	if err == nil && p.SHA256 != "" {
		answer["created"] = p.Created
	}
	writeJSON(w, http.StatusOK, answer)
}

// handleAccountWebDAVNew makes a new WebDAV password and shows it once.
func (a davArea) handleAccountWebDAVNew(w http.ResponseWriter, r *http.Request) {
	if !a.webSession(w, r) {
		return
	}
	// Password and Code confirm it like an API token: the WebDAV password
	// is another way to the shares of the account.
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !a.reconfirm(w, currentUser(r), req.Password, req.Code) {
		return
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(password))
	err := a.store.WriteJSON(davPasswordFile(currentUser(r).Name), davPassword{SHA256: hex.EncodeToString(sum[:]), Created: time.Now().UTC()})
	// Also after a failed write: what the stick holds now is read again.
	a.davForget(currentUser(r).Name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser(r).Name, "password": password})
}

// forgetWebDAV removes the WebDAV password of an account whose password
// changed or whose factors were reset: whoever made it with a session that
// has just ended would keep the way to the shares otherwise.
func (a davArea) forgetWebDAV(name string) {
	defer a.davForget(name)
	if err := a.store.Remove(davPasswordFile(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Error("remove the WebDAV password", "user", name, "err", err)
	}
}

func (a davArea) handleAccountWebDAVDelete(w http.ResponseWriter, r *http.Request) {
	defer a.davForget(currentUser(r).Name)
	if err := a.store.Remove(davPasswordFile(currentUser(r).Name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// davUser checks the Basic credentials of a request.
func (a davArea) davUser(r *http.Request) (auth.User, bool, time.Duration) {
	name, password, ok := r.BasicAuth()
	ip := remoteIP(r)
	if wait := a.limiter().RetryAfter("dav:"+ip, "dav-user:"+name); wait > 0 {
		return auth.User{}, false, wait
	}
	if !ok || name == "" || len(password) > 256 {
		return auth.User{}, false, 0
	}
	user, known := a.users.Get(name)
	var want []byte
	if known {
		want = a.davPasswordOf(user)
	}
	sum := sha256.Sum256([]byte(password))
	if !known || len(want) != len(sum) || subtle.ConstantTimeCompare(sum[:], want) != 1 || user.Disabled {
		a.limiter().Fail("dav:"+ip, "dav-user:"+name)
		slog.Warn("WebDAV sign-in refused", "user", name, "ip", ip)
		return auth.User{}, false, 0
	}
	return user, true, 0
}

// davPasswordOf returns the hash of the WebDAV password of an account,
// nil for none; the stick is read only the first time.
func (a davArea) davPasswordOf(user auth.User) []byte {
	d := a.state
	d.mu.Lock()
	defer d.mu.Unlock()
	if known, ok := d.passwords[user.Name]; ok && known.account.Equal(user.Created) && known.passwordHash == user.PasswordHash {
		return known.sum
	}
	var stored davPassword
	data, err := a.store.Read(davPasswordFile(user.Name))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		// Not readable now (stick away): no sign-in, and asked again next time.
		slog.Warn("WebDAV password not readable", "user", user.Name, "err", err)
		return nil
	case json.Unmarshal(data, &stored) != nil:
		// Damaged: no password, until a new one is made.
		slog.Warn("WebDAV password damaged", "user", user.Name)
	}
	sum, _ := hex.DecodeString(stored.SHA256)
	if len(sum) == 0 {
		sum = nil
	}
	if d.passwords == nil {
		d.passwords = map[string]davKnown{}
	}
	d.passwords[user.Name] = davKnown{account: user.Created, passwordHash: user.PasswordHash, sum: sum}
	return sum
}

// davForget drops what is known of the WebDAV password of an account, so
// that the next request reads the stick.
func (a davArea) davForget(name string) {
	a.state.mu.Lock()
	delete(a.state.passwords, name)
	a.state.mu.Unlock()
}

// handleWebDAV serves /dav/…: the shares of the account as folders.
func (a davArea) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	if !a.davSettings().Enabled {
		http.NotFound(w, r)
		return
	}
	user, ok, wait := a.davUser(r)
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
	for _, root := range a.fileRoots(user) {
		if root.Kind == "share" {
			roots = append(roots, root)
		}
	}
	handler := &webdav.Handler{
		Prefix: davPrefix,
		FileSystem: &davShares{roots: roots, open: func(root files.Root) webdav.FileSystem {
			diskwake.Note(root.Volume, diskwake.WebDAV, user.Name)
			return a.files.DAV(root, user.Name, user.UID)
		}},
		LockSystem: a.state.locks,
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Info("WebDAV", "user", user.Name, "method", r.Method, "path", r.URL.Path, "err", err)
			}
		},
	}
	if r.Method == http.MethodPut {
		// A file that arrives shorter than announced is not put in place.
		r = r.WithContext(files.WithDAVLength(r.Context(), r.ContentLength))
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
