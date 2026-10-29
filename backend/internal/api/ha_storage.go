package api

// Storage for Home Assistant (owner request of 29 Oct 2026): a share of
// this NAS that Home Assistant OS mounts as network storage for its
// backups, its media or as a share for add-ons. TitanOS makes the share
// and an account of its own for it, shows the values to type into Home
// Assistant, can enter them there itself (Supervisor, through the
// connection under Settings › Home Assistant) and watches that backups
// keep arriving. The password of the account is shown once and kept
// nowhere but in the account itself.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

const haStorageFile = "homeassistant/storage.json"

// haStorage is what was set up; older titand ignore the file.
type haStorage struct {
	Share string `json:"share"`
	// Folder is a folder in the share Home Assistant gets instead of the
	// whole share (slash separated, empty: all of it).
	Folder string `json:"folder,omitempty"`
	// Existing: the share was there before; the account was only given
	// access to it.
	Existing bool      `json:"existing,omitempty"`
	ReadOnly bool      `json:"readOnly,omitempty"`
	Account  string    `json:"account"`
	Usage    string    `json:"usage"`
	Mount    string    `json:"mount"`
	Server   string    `json:"server"`
	Created  time.Time `json:"created"`
	// MaxAgeDays: a notice when no backup of Home Assistant arrived on the
	// share for this long; 0 is off. Only for usage backup.
	MaxAgeDays int `json:"maxAgeDays"`
}

// haBackupState is what the last look at the backups found; RAM only.
type haBackupState struct {
	Checked  time.Time `json:"checked"`
	Latest   time.Time `json:"latest,omitzero"`
	Name     string    `json:"name,omitempty"`
	Error    string    `json:"error,omitempty"`
	Overdue  bool      `json:"overdue"`
	notified bool
}

type haStorageState struct {
	mu     sync.Mutex
	loaded bool
	setup  *haStorage
	backup haBackupState
	// check wakes the watcher, e.g. after the setup changed.
	check chan struct{}
}

func (s *Server) haStorage() (*haStorage, error) {
	st := &s.haStore
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.loaded {
		var setup haStorage
		err := s.opts.Store.ReadJSON(haStorageFile, &setup)
		switch {
		case err == nil:
			st.setup = &setup
		case errors.Is(err, fs.ErrNotExist):
		default:
			return nil, err
		}
		st.loaded = true
	}
	if st.setup == nil {
		return nil, nil
	}
	copy := *st.setup
	return &copy, nil
}

func (s *Server) saveHAStorage(setup *haStorage) error {
	st := &s.haStore
	st.mu.Lock()
	defer st.mu.Unlock()
	if setup == nil {
		if err := s.opts.Store.Remove(haStorageFile); err != nil {
			return err
		}
	} else if err := s.opts.Store.WriteJSON(haStorageFile, setup); err != nil {
		return err
	}
	st.setup, st.loaded, st.backup = setup, true, haBackupState{}
	select {
	case st.check <- struct{}{}:
	default:
	}
	return nil
}

func (s *Server) handleHAStorage(w http.ResponseWriter, r *http.Request) {
	setup, err := s.haStorage()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "the settings are not readable from /config")
		return
	}
	s.haStore.mu.Lock()
	backup := s.haStore.backup
	s.haStore.mu.Unlock()
	active := s.opts.HomeAssistant != nil && s.opts.HomeAssistant.Active()
	// The shares to choose from: those on a mounted volume, by name.
	mounted := s.opts.Storage.Mounted()
	if mounted == nil {
		mounted = []string{}
	}
	choices := []map[string]string{}
	for _, share := range s.opts.Shares.List() {
		if slices.Contains(mounted, share.Volume) && share.SMB {
			choices = append(choices, map[string]string{"name": share.Name, "volume": share.Volume})
		}
	}
	res := map[string]any{"setup": setup, "volumes": mounted, "shares": choices, "homeAssistant": active, "server": s.haServerAddress(r)}
	if setup != nil && setup.Usage == "backup" && !backup.Checked.IsZero() {
		res["backup"] = backup
	}
	writeJSON(w, http.StatusOK, res)
}

// handleHAStorageSetup makes the share and its account and, if asked,
// enters the storage in Home Assistant. The answer carries the password,
// once; that is why it needs a person at a web session.
func (s *Server) handleHAStorageSetup(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	var req struct {
		Volume   string `json:"volume"`
		Share    string `json:"share"`
		Account  string `json:"account"`
		Usage    string `json:"usage"`
		QuotaGiB uint64 `json:"quotaGiB"`
		Register bool   `json:"register"`
		// Existing uses a share that is there already; Folder a folder in
		// it; ReadOnly gives the account (and Home Assistant) reading only.
		Existing bool   `json:"existing"`
		Folder   string `json:"folder"`
		ReadOnly bool   `json:"readOnly"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Share)
	if !homeassistant.ValidUsage(req.Usage) {
		writeError(w, http.StatusBadRequest, "ha_storage_usage", "usage must be backup, media or share")
		return
	}
	if setup, err := s.haStorage(); err != nil || setup != nil {
		writeError(w, http.StatusConflict, "ha_storage_exists", "storage for Home Assistant is set up already")
		return
	}
	if req.Register && (s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Active()) {
		writeError(w, http.StatusConflict, "ha_not_connected", "Home Assistant is not connected")
		return
	}
	if req.ReadOnly && req.Usage == "backup" {
		writeError(w, http.StatusBadRequest, "ha_storage_read_only", "backups need to write")
		return
	}
	if _, exists := s.opts.Users.Get(req.Account); exists {
		writeError(w, http.StatusConflict, "ha_storage_account_exists", "an account of this name exists; choose another name")
		return
	}
	var existing shares.Share
	if req.Existing {
		var ok bool
		if existing, ok = s.findShare(req.Share); !ok {
			writeError(w, http.StatusNotFound, "share_not_found", "no such share")
			return
		}
		req.Volume = existing.Volume
	} else if req.Folder != "" {
		writeError(w, http.StatusBadRequest, "ha_storage_folder", "a folder only goes with a share that exists")
		return
	}
	folder, ok := haFolder(req.Folder)
	if !ok {
		writeError(w, http.StatusBadRequest, "ha_storage_folder", "not a folder in the share")
		return
	}
	if folder != "" && !s.haFolderExists(existing, folder) {
		writeError(w, http.StatusBadRequest, "ha_storage_folder", "the folder does not exist in the share")
		return
	}
	access := shares.ReadWrite
	if req.ReadOnly {
		access = shares.ReadOnly
	}
	password := haPassword()
	if _, err := s.opts.Users.Create(req.Account, password, auth.RoleUser); err != nil {
		writeAccountError(w, err)
		return
	}
	var err error
	if req.Existing {
		// Everyone keeps their access; the account joins them.
		changed := existing
		changed.Access = maps.Clone(existing.Access)
		if changed.Access == nil {
			changed.Access = map[string]string{}
		}
		changed.Access[req.Account] = access
		err = s.opts.Shares.Update(existing.Name, changed, s.opts.Storage.Names(), s.userNames())
	} else {
		err = s.opts.Shares.Create(shares.Share{
			Name: req.Share, Volume: req.Volume, SMB: true,
			Access: map[string]string{req.Account: access},
			Quota:  req.QuotaGiB << 30,
		}, s.opts.Storage.Names(), s.userNames())
	}
	if err != nil {
		// Nothing half: the account was only for this share.
		if removeErr := s.opts.Users.Delete(req.Account); removeErr != nil {
			slog.Warn("account for Home Assistant not removed again", "account", req.Account, "err", removeErr)
		}
		writeShareError(w, err)
		return
	}
	s.opts.Shares.Kick()
	s.opts.SSH.Kick()
	setup := &haStorage{
		Share: req.Share, Folder: folder, Existing: req.Existing, ReadOnly: req.ReadOnly,
		Account: req.Account, Usage: req.Usage,
		Mount: homeassistant.MountName(path.Join(req.Share, folder)), Server: s.haServerAddress(r),
		Created: time.Now().UTC().Truncate(time.Second),
	}
	if req.Usage == "backup" {
		setup.MaxAgeDays = 3
	}
	if err := s.saveHAStorage(setup); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	slog.Info("storage for Home Assistant set up", "share", req.Share, "account", req.Account, "usage", req.Usage, "by", currentUser(r).Name, "ip", remoteIP(r))
	res := map[string]any{"setup": setup, "password": password}
	if req.Register {
		res["registered"], res["registerError"] = s.haRegister(r.Context(), setup, password)
	}
	writeJSON(w, http.StatusCreated, res)
}

// handleHAStorageRegister gives the account a new password and enters the
// storage in Home Assistant with it: TitanOS does not keep the old one.
func (s *Server) handleHAStorageRegister(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	setup, err := s.haStorage()
	if err != nil || setup == nil {
		writeError(w, http.StatusNotFound, "ha_storage_missing", "no storage for Home Assistant is set up")
		return
	}
	if s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Active() {
		writeError(w, http.StatusConflict, "ha_not_connected", "Home Assistant is not connected")
		return
	}
	password := haPassword()
	if _, err := s.opts.Users.SetPassword(setup.Account, password); err != nil {
		writeAccountError(w, err)
		return
	}
	s.opts.Shares.Kick()
	if server := s.haServerAddress(r); server != "" && server != setup.Server {
		setup.Server = server
		if err := s.saveHAStorage(setup); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
			return
		}
	}
	registered, registerError := s.haRegister(r.Context(), setup, password)
	slog.Info("storage entered in Home Assistant again", "share", setup.Share, "registered", registered, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"setup": setup, "password": password, "registered": registered, "registerError": registerError})
}

// haRegister enters the storage in Home Assistant. A failure is part of
// the answer, not an error of the setup: the values work by hand as well.
func (s *Server) haRegister(ctx context.Context, setup *haStorage, password string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if setup.Server == "" {
		return false, "no address of the NAS that Home Assistant can reach"
	}
	err := s.opts.HomeAssistant.AddMount(ctx, homeassistant.MountRequest{
		Name: setup.Mount, Usage: setup.Usage, Server: setup.Server, Share: path.Join(setup.Share, setup.Folder),
		Username: setup.Account, Password: password, ReadOnly: setup.ReadOnly,
	})
	if err != nil {
		var ce *homeassistant.CommandError
		switch {
		case errors.Is(err, homeassistant.ErrNoSupervisor):
			return false, "no_supervisor"
		case errors.As(err, &ce) && ce.Message != "":
			return false, ce.Message
		}
		return false, err.Error()
	}
	return true, ""
}

func (s *Server) handleHAStorageChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxAgeDays int `json:"maxAgeDays"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	setup, err := s.haStorage()
	if err != nil || setup == nil {
		writeError(w, http.StatusNotFound, "ha_storage_missing", "no storage for Home Assistant is set up")
		return
	}
	if req.MaxAgeDays < 0 || req.MaxAgeDays > 90 || req.MaxAgeDays > 0 && setup.Usage != "backup" {
		writeError(w, http.StatusBadRequest, "ha_storage_max_age", "maxAgeDays is 0 (off) to 90, and only for backups")
		return
	}
	setup.MaxAgeDays = req.MaxAgeDays
	if err := s.saveHAStorage(setup); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	s.handleHAStorage(w, r)
}

// handleHAStorageForget stops watching; share, account and data stay.
func (s *Server) handleHAStorageForget(w http.ResponseWriter, r *http.Request) {
	if err := s.saveHAStorage(nil); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	s.handleHAStorage(w, r)
}

// handleHAStorageCheck looks at the backups of Home Assistant right away.
func (s *Server) handleHAStorageCheck(w http.ResponseWriter, r *http.Request) {
	setup, err := s.haStorage()
	if err != nil || setup == nil || setup.Usage != "backup" {
		writeError(w, http.StatusNotFound, "ha_storage_missing", "no backup storage for Home Assistant is set up")
		return
	}
	s.checkHABackups(r.Context(), setup, time.Now())
	s.handleHAStorage(w, r)
}

// RunHAStorage looks once a day (and ten minutes after the start) whether
// Home Assistant still puts its backups on the share. It asks Home
// Assistant, not the disk: a sleeping disk stays asleep.
func (s *Server) RunHAStorage(ctx context.Context) {
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.haStore.check:
			timer.Reset(time.Minute)
			continue
		}
		if setup, err := s.haStorage(); err == nil && setup != nil && setup.Usage == "backup" && setup.MaxAgeDays > 0 {
			s.checkHABackups(ctx, setup, time.Now())
		}
		timer.Reset(24 * time.Hour)
	}
}

func (s *Server) checkHABackups(ctx context.Context, setup *haStorage, now time.Time) {
	if s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Active() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	backups, err := s.opts.HomeAssistant.Backups(ctx)
	st := &s.haStore
	st.mu.Lock()
	defer st.mu.Unlock()
	st.backup.Checked, st.backup.Error = now, ""
	if err != nil {
		// Unknown is no alarm: an older Home Assistant has no backup/info.
		st.backup.Error = err.Error()
		return
	}
	latest, found := homeassistant.LatestOn(backups, homeassistant.MountAgent(setup.Mount))
	st.backup.Latest, st.backup.Name = time.Time{}, ""
	since := setup.Created
	if found {
		st.backup.Latest, st.backup.Name, since = latest.Date, latest.Name, latest.Date
	}
	st.backup.Overdue = setup.MaxAgeDays > 0 && now.Sub(since) > time.Duration(setup.MaxAgeDays)*24*time.Hour
	switch {
	case st.backup.Overdue && !st.backup.notified:
		st.backup.notified = true
		if s.opts.Notify != nil {
			s.opts.Notify.Notify("warning", "backup_ha_overdue", setup.Share)
		}
	case !st.backup.Overdue:
		st.backup.notified = false
	}
}

// haServerAddress is the address of the NAS that Home Assistant reaches:
// the one this NAS uses towards it, else the one the browser used.
func (s *Server) haServerAddress(r *http.Request) string {
	if s.opts.HomeAssistant != nil {
		if settings, _ := s.opts.HomeAssistant.Settings(); settings.URL != "" {
			if u, err := url.Parse(settings.URL); err == nil && u.Hostname() != "" {
				port := u.Port()
				if port == "" {
					port = "443"
				}
				if conn, err := net.DialTimeout("udp", net.JoinHostPort(u.Hostname(), port), time.Second); err == nil {
					addr := conn.LocalAddr().(*net.UDPAddr).IP
					conn.Close()
					if !addr.IsLoopback() {
						return addr.String()
					}
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return host
}

// haFolder cleans a folder given relative to the share: no way out of it,
// no hidden or internal folders.
func haFolder(folder string) (string, bool) {
	folder = strings.Trim(strings.ReplaceAll(folder, "\\", "/"), "/")
	if folder == "" {
		return "", true
	}
	if len(folder) > 512 || !fs.ValidPath(folder) {
		return "", false
	}
	for _, part := range strings.Split(folder, "/") {
		if strings.HasPrefix(part, ".") || strings.ContainsAny(part, "\x00\n\r") {
			return "", false
		}
	}
	return folder, true
}

// haFolderExists checks within the share, following no links.
func (s *Server) haFolderExists(share shares.Share, folder string) bool {
	root, err := os.OpenRoot(s.opts.Shares.Path(share))
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Lstat(folder)
	return err == nil && info.IsDir()
}

// haPassword is the password of the account Home Assistant mounts with.
func haPassword() string {
	b := make([]byte, 18)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
