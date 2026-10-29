package api

// Storage for Home Assistant (owner request of 29 Oct 2026): shares of
// this NAS (or folders in them) that Home Assistant OS mounts as network
// storage for its backups, its media or as shares for add-ons. One account
// of its own serves all entries. TitanOS makes the share or gives the
// account access to one that exists, can enter each entry in Home Assistant
// itself (Supervisor, through the connection under Settings › Home
// Assistant) and watches that backups keep arriving. The password of the
// account is shown once and kept nowhere but in the account itself.

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
	// Account mounts every entry; it was made for Home Assistant.
	Account string    `json:"account"`
	Server  string    `json:"server"`
	Entries []haEntry `json:"entries"`
}

// haEntry is one share (or folder in it) Home Assistant mounts.
type haEntry struct {
	Share string `json:"share"`
	// Folder is a folder in the share Home Assistant gets instead of the
	// whole share (slash separated, empty: all of it).
	Folder string `json:"folder,omitempty"`
	// Existing: the share was there before; the account was only given
	// access to it.
	Existing bool      `json:"existing,omitempty"`
	ReadOnly bool      `json:"readOnly,omitempty"`
	Usage    string    `json:"usage"`
	Mount    string    `json:"mount"`
	Created  time.Time `json:"created"`
	// MaxAgeDays: a notice when no backup of Home Assistant arrived on the
	// share for this long; 0 is off. Only for usage backup.
	MaxAgeDays int `json:"maxAgeDays"`
}

// place is the share or share/folder of an entry.
func (e haEntry) place() string { return path.Join(e.Share, e.Folder) }

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
	// backups by mount name.
	backups map[string]*haBackupState
	// check wakes the watcher, e.g. after the setup changed.
	check chan struct{}
}

// haResult says what Home Assistant made of one entry.
type haResult struct {
	Mount string `json:"mount"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
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
	copy.Entries = slices.Clone(st.setup.Entries)
	return &copy, nil
}

func (s *Server) saveHAStorage(setup *haStorage) error {
	st := &s.haStore
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := s.opts.Store.WriteJSON(haStorageFile, setup); err != nil {
		return err
	}
	st.setup, st.loaded = setup, true
	// Backup states of entries that are gone go with them.
	for mount := range st.backups {
		if !slices.ContainsFunc(setup.Entries, func(e haEntry) bool { return e.Mount == mount }) {
			delete(st.backups, mount)
		}
	}
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
	type entryView struct {
		haEntry
		Backup *haBackupState `json:"backup,omitempty"`
	}
	entries := []entryView{}
	account, server := "", s.haServerAddress(r)
	if setup != nil {
		account = setup.Account
		if setup.Server != "" {
			server = setup.Server
		}
		s.haStore.mu.Lock()
		for _, e := range setup.Entries {
			view := entryView{haEntry: e}
			if b := s.haStore.backups[e.Mount]; b != nil && e.Usage == "backup" {
				copy := *b
				view.Backup = &copy
			}
			entries = append(entries, view)
		}
		s.haStore.mu.Unlock()
	}
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
	writeJSON(w, http.StatusOK, map[string]any{"account": account, "server": server, "entries": entries,
		"volumes": mounted, "shares": choices, "homeAssistant": active})
}

// handleHAStorageAdd adds an entry: a new share or one that exists, a
// folder in it, the use. The first entry makes the account. Entered in
// Home Assistant directly, the account gets a new password, which is the
// answer's (once) and goes to every entry there; that is why it needs a
// person at a web session.
func (s *Server) handleHAStorageAdd(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	var req struct {
		// Account names the account of the first entry only.
		Account  string `json:"account"`
		Volume   string `json:"volume"`
		Share    string `json:"share"`
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
	noteTarget(r, path.Join(req.Share, req.Folder))
	if !homeassistant.ValidUsage(req.Usage) {
		writeError(w, http.StatusBadRequest, "ha_storage_usage", "usage must be backup, media or share")
		return
	}
	if req.ReadOnly && req.Usage == "backup" {
		writeError(w, http.StatusBadRequest, "ha_storage_read_only", "backups need to write")
		return
	}
	setup, err := s.haStorage()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "the settings are not readable from /config")
		return
	}
	if req.Register && (s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Active()) {
		writeError(w, http.StatusConflict, "ha_not_connected", "Home Assistant is not connected")
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
	if !ok || folder != "" && !s.haFolderExists(existing, folder) {
		writeError(w, http.StatusBadRequest, "ha_storage_folder", "the folder does not exist in the share")
		return
	}
	entry := haEntry{
		Share: req.Share, Folder: folder, Existing: req.Existing, ReadOnly: req.ReadOnly, Usage: req.Usage,
		Mount: homeassistant.MountName(path.Join(req.Share, folder)), Created: time.Now().UTC().Truncate(time.Second),
	}
	if req.Usage == "backup" {
		entry.MaxAgeDays = 3
	}
	newAccount := setup == nil
	if newAccount {
		if _, exists := s.opts.Users.Get(req.Account); exists {
			writeError(w, http.StatusConflict, "ha_storage_account_exists", "an account of this name exists; choose another name")
			return
		}
		setup = &haStorage{Account: req.Account}
	} else if slices.ContainsFunc(setup.Entries, func(e haEntry) bool { return e.Mount == entry.Mount }) {
		writeError(w, http.StatusConflict, "ha_storage_entry_exists", "Home Assistant has this share or folder already")
		return
	}
	setup.Server = s.haServerAddress(r)

	// A new password whenever there is something to tell Home Assistant,
	// and always for a new account.
	password := ""
	if newAccount || req.Register {
		password = haPassword()
	}
	if newAccount {
		if _, err := s.opts.Users.Create(setup.Account, password, auth.RoleUser); err != nil {
			writeAccountError(w, err)
			return
		}
	}
	undo := func() {
		if !newAccount {
			return
		}
		// Nothing half: the account was only for this.
		if err := s.opts.Users.Delete(setup.Account); err != nil {
			slog.Warn("account for Home Assistant not removed again", "account", setup.Account, "err", err)
		}
	}
	access := shares.ReadWrite
	if req.ReadOnly {
		access = shares.ReadOnly
	}
	if req.Existing {
		err = s.haGrant(existing, setup.Account, access)
	} else {
		err = s.opts.Shares.Create(shares.Share{
			Name: req.Share, Volume: req.Volume, SMB: true,
			Access: map[string]string{setup.Account: access},
			Quota:  req.QuotaGiB << 30,
		}, s.opts.Storage.Names(), s.userNames())
	}
	if err != nil {
		undo()
		writeShareError(w, err)
		return
	}
	if !newAccount && password != "" {
		if _, err := s.opts.Users.SetPassword(setup.Account, password); err != nil {
			writeAccountError(w, err)
			return
		}
	}
	s.opts.Shares.Kick()
	s.opts.SSH.Kick()
	setup.Entries = append(setup.Entries, entry)
	if err := s.saveHAStorage(setup); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	slog.Info("storage for Home Assistant added", "share", entry.place(), "account", setup.Account, "usage", entry.Usage, "by", currentUser(r).Name, "ip", remoteIP(r))
	res := map[string]any{"setup": setup, "entry": entry}
	if password != "" {
		res["password"] = password
	}
	if req.Register {
		// The new password goes to every entry, or the others stop working.
		res["results"] = s.haRegisterAll(r.Context(), setup, password)
	}
	writeJSON(w, http.StatusCreated, res)
}

// haGrant gives the account access to a share that exists; everyone else
// keeps theirs. Read and write wins over read only.
func (s *Server) haGrant(share shares.Share, account, access string) error {
	if share.Access[account] == shares.ReadWrite || share.Access[account] == access {
		return nil
	}
	changed := share
	changed.Access = maps.Clone(share.Access)
	if changed.Access == nil {
		changed.Access = map[string]string{}
	}
	changed.Access[account] = access
	return s.opts.Shares.Update(share.Name, changed, s.opts.Storage.Names(), s.userNames())
}

// handleHAStorageRegister gives the account a new password and enters
// every entry in Home Assistant with it: TitanOS does not keep the old one.
func (s *Server) handleHAStorageRegister(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	setup, err := s.haStorage()
	if err != nil || setup == nil || len(setup.Entries) == 0 {
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
	results := s.haRegisterAll(r.Context(), setup, password)
	slog.Info("storage entered in Home Assistant again", "entries", len(setup.Entries), "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"setup": setup, "password": password, "results": results})
}

// haRegisterAll enters every entry in Home Assistant with this password:
// new ones are added, those there already get the new password. A failure
// is part of the answer, not an error: the values work by hand as well.
func (s *Server) haRegisterAll(ctx context.Context, setup *haStorage, password string) []haResult {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	results := make([]haResult, 0, len(setup.Entries))
	fail := func(reason string) []haResult {
		for _, e := range setup.Entries {
			results = append(results, haResult{Mount: e.Mount, Error: reason})
		}
		return results
	}
	if setup.Server == "" {
		return fail("no address of the NAS that Home Assistant can reach")
	}
	mounts, err := s.opts.HomeAssistant.Mounts(ctx)
	if err != nil {
		return fail(haReason(err))
	}
	for _, e := range setup.Entries {
		req := homeassistant.MountRequest{
			Name: e.Mount, Usage: e.Usage, Server: setup.Server, Share: e.place(),
			Username: setup.Account, Password: password, ReadOnly: e.ReadOnly,
		}
		if slices.ContainsFunc(mounts, func(m homeassistant.Mount) bool { return m.Name == e.Mount }) {
			err = s.opts.HomeAssistant.UpdateMount(ctx, req)
		} else {
			err = s.opts.HomeAssistant.AddMount(ctx, req)
		}
		result := haResult{Mount: e.Mount, OK: err == nil}
		if err != nil {
			result.Error = haReason(err)
		}
		results = append(results, result)
	}
	return results
}

func haReason(err error) string {
	var ce *homeassistant.CommandError
	switch {
	case errors.Is(err, homeassistant.ErrNoSupervisor):
		return "no_supervisor"
	case errors.As(err, &ce) && ce.Message != "":
		return ce.Message
	}
	return err.Error()
}

func (s *Server) haEntry(w http.ResponseWriter, r *http.Request) (*haStorage, int, bool) {
	setup, err := s.haStorage()
	if err == nil && setup != nil {
		mount := r.PathValue("mount")
		if i := slices.IndexFunc(setup.Entries, func(e haEntry) bool { return e.Mount == mount }); i >= 0 {
			return setup, i, true
		}
	}
	writeError(w, http.StatusNotFound, "ha_storage_missing", "no such storage for Home Assistant")
	return nil, 0, false
}

func (s *Server) handleHAStorageChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxAgeDays int `json:"maxAgeDays"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	setup, i, ok := s.haEntry(w, r)
	if !ok {
		return
	}
	if req.MaxAgeDays < 0 || req.MaxAgeDays > 90 || req.MaxAgeDays > 0 && setup.Entries[i].Usage != "backup" {
		writeError(w, http.StatusBadRequest, "ha_storage_max_age", "maxAgeDays is 0 (off) to 90, and only for backups")
		return
	}
	setup.Entries[i].MaxAgeDays = req.MaxAgeDays
	if err := s.saveHAStorage(setup); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	s.handleHAStorage(w, r)
}

// handleHAStorageRemove takes an entry out: of Home Assistant, and the
// account's access to its share unless another entry needs it. Share and
// data stay. Nobody left with access, the person removing it gets it.
func (s *Server) handleHAStorageRemove(w http.ResponseWriter, r *http.Request) {
	setup, i, ok := s.haEntry(w, r)
	if !ok {
		return
	}
	entry := setup.Entries[i]
	noteTarget(r, entry.place())
	setup.Entries = slices.Delete(setup.Entries, i, i+1)
	if !slices.ContainsFunc(setup.Entries, func(e haEntry) bool { return e.Share == entry.Share }) {
		if share, found := s.findShare(entry.Share); found && share.Access[setup.Account] != "" {
			changed := share
			changed.Access = maps.Clone(share.Access)
			delete(changed.Access, setup.Account)
			if len(changed.Access) == 0 && !changed.Guest {
				changed.Access[currentUser(r).Name] = shares.ReadWrite
			}
			if err := s.opts.Shares.Update(share.Name, changed, s.opts.Storage.Names(), s.userNames()); err != nil {
				writeShareError(w, err)
				return
			}
			s.opts.Shares.Kick()
		}
	}
	if err := s.saveHAStorage(setup); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	removed := ""
	if s.opts.HomeAssistant != nil && s.opts.HomeAssistant.Active() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
		if err := s.opts.HomeAssistant.RemoveMount(ctx, entry.Mount); err != nil {
			removed = haReason(err)
		}
		cancel()
	}
	slog.Info("storage for Home Assistant removed", "share", entry.place(), "haError", removed, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"removedError": removed})
}

// handleHAStorageCheck looks at the backups of Home Assistant right away.
func (s *Server) handleHAStorageCheck(w http.ResponseWriter, r *http.Request) {
	setup, err := s.haStorage()
	if err != nil || setup == nil {
		writeError(w, http.StatusNotFound, "ha_storage_missing", "no storage for Home Assistant is set up")
		return
	}
	s.checkHABackups(r.Context(), setup, time.Now(), true)
	s.handleHAStorage(w, r)
}

// RunHAStorage looks once a day (and ten minutes after the start) whether
// Home Assistant still puts its backups on the shares. It asks Home
// Assistant, not the disks: a sleeping disk stays asleep.
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
		if setup, err := s.haStorage(); err == nil && setup != nil {
			s.checkHABackups(ctx, setup, time.Now(), false)
		}
		timer.Reset(24 * time.Hour)
	}
}

// checkHABackups asks once for all backup entries; all says to look also
// at those whose watching is off (a person asked).
func (s *Server) checkHABackups(ctx context.Context, setup *haStorage, now time.Time, all bool) {
	var watched []haEntry
	for _, e := range setup.Entries {
		if e.Usage == "backup" && (all || e.MaxAgeDays > 0) {
			watched = append(watched, e)
		}
	}
	if len(watched) == 0 || s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Active() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	backups, err := s.opts.HomeAssistant.Backups(ctx)
	st := &s.haStore
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.backups == nil {
		st.backups = map[string]*haBackupState{}
	}
	for _, e := range watched {
		b := st.backups[e.Mount]
		if b == nil {
			b = &haBackupState{}
			st.backups[e.Mount] = b
		}
		b.Checked, b.Error = now, ""
		if err != nil {
			// Unknown is no alarm: an older Home Assistant has no backup/info.
			b.Error = err.Error()
			continue
		}
		latest, found := homeassistant.LatestOn(backups, homeassistant.MountAgent(e.Mount))
		b.Latest, b.Name = time.Time{}, ""
		since := e.Created
		if found {
			b.Latest, b.Name, since = latest.Date, latest.Name, latest.Date
		}
		b.Overdue = e.MaxAgeDays > 0 && now.Sub(since) > time.Duration(e.MaxAgeDays)*24*time.Hour
		switch {
		case b.Overdue && !b.notified:
			b.notified = true
			if s.opts.Notify != nil {
				s.opts.Notify.Notify("warning", "backup_ha_overdue", e.place())
			}
		case !b.Overdue:
			b.notified = false
		}
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
