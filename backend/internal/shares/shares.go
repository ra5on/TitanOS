// Package shares manages the shared folders and makes the system match
// them: Linux and Samba accounts, smb.conf, the NFS exports and the
// services themselves.
//
// The system is rebuilt from the image on every boot, so nothing in /etc
// survives. Everything here is derived from /config and written again
// whenever accounts, shares or the mounted volumes change.
package shares

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"

	"golang.org/x/sys/unix"
)

const (
	configFile = "shares/shares.json"
	// UsersGID is the group "users" of Debian. All accounts are in it, and
	// everything in a share belongs to it.
	UsersGID = 100
	// recycleDir is where Samba moves deleted files, per user.
	recycleDir = ".recycle"
)

// Access levels of an account on a share.
const (
	ReadOnly  = "ro"
	ReadWrite = "rw"
)

// Share is a shared folder as stored in /config/shares/shares.json.
type Share struct {
	Name   string `json:"name"`
	Volume string `json:"volume"`
	// SMB offers the share to Windows, macOS and Linux file managers.
	SMB bool `json:"smb"`
	NFS NFS  `json:"nfs"`
	// Access maps account names to "ro" or "rw". Others have no access.
	Access map[string]string `json:"access"`
	// Guest lets anyone on the network read the share without a password.
	Guest bool `json:"guest"`
	// Browseable shows the share when a client lists the NAS.
	Browseable  bool        `json:"browseable"`
	Recycle     Recycle     `json:"recycle"`
	TimeMachine TimeMachine `json:"timeMachine"`
	// Quota is the most the share may hold in bytes; 0 for no limit. It is
	// enforced only on volumes with share quotas (ext4, XFS).
	Quota uint64 `json:"quota,omitempty"`
	// QuotaID is the project of the share's files, kept once given.
	QuotaID uint32 `json:"quotaID,omitempty"`
	// PreviousVersions offers the Btrfs snapshots of the volume as
	// "Previous Versions" in Windows (Samba shadow_copy2).
	PreviousVersions bool `json:"previousVersions,omitempty"`
	// Guard watches renames over SMB for ransomware and makes the share
	// read-only when it sees it (guard.go).
	Guard bool `json:"guard,omitempty"`
	// GuardLocked is set by titand, never by the client: the share is
	// read-only since then, until an administrator lifts it.
	GuardLocked *GuardLock `json:"guardLocked,omitempty"`
	// PerUser gives every account with access a folder of its own and
	// shows it only that folder, over SMB (path …/%u, the Unix user of the session) and in the file
	// manager. Set by titand for the share of the KI-Agent (PLAN 49), never
	// by the client; such a share has no NFS and no guests.
	PerUser bool `json:"perUser,omitempty"`
}

// NFS exports the share to the listed hosts or networks. NFS knows no
// accounts: permissions apply to every user on an allowed client.
type NFS struct {
	Enabled bool     `json:"enabled"`
	Clients []string `json:"clients"`
	// Mode is empty for legacy settings, or off/ro/rw. A protected export
	// is persisted with Enabled=false so old applications cannot export it rw.
	Mode string `json:"mode,omitempty"`
}

// Recycle keeps files deleted over SMB for a number of days.
type Recycle struct {
	Enabled bool `json:"enabled"`
	Days    int  `json:"days"`
}

// TimeMachine makes the share a backup target for macOS.
type TimeMachine struct {
	Enabled bool `json:"enabled"`
	// SizeGB limits what Time Machine may use; 0 is no limit.
	SizeGB int `json:"sizeGB"`
}

var (
	ErrNotLoaded     = errors.New("shares: configuration could not be read from /config")
	ErrInvalidName   = errors.New("shares: invalid share name")
	ErrExists        = errors.New("shares: a share with this name exists")
	ErrNotFound      = errors.New("shares: no such share")
	ErrUnknownVolume = errors.New("shares: no such volume")
	ErrUnknownUser   = errors.New("shares: no such account")
	ErrInvalidAccess = errors.New("shares: access must be ro or rw")
	ErrInvalidClient = errors.New("shares: invalid NFS client")
	ErrInvalidOption = errors.New("shares: option out of range")
	ErrVolumeChange  = errors.New("shares: a share cannot move to another volume")
)

var (
	// A host name, an address, a network (192.168.1.0/24) or a pattern (*.lan).
	clientRE = regexp.MustCompile(`^[A-Za-z0-9*][A-Za-z0-9.:/*_-]{0,99}$`)
	// Names Samba gives a meaning of their own.
	reservedNames = []string{"global", "homes", "printers", "print", "ipc"}
)

// maxNameLength is in characters; longer names get cut off in the
// network view of Windows and in Finder.
const maxNameLength = 32

// ValidName reports whether a share may be called name. Letters of any
// language, digits, spaces, "-", "_" and "." are allowed, so "Fotos 2024"
// works. Left out are characters that Samba, Windows or macOS treat
// specially in a share name or path ("%" is a Samba variable, "[ ]" end the
// section in smb.conf, "/ \ : * ? \" < > |" are not allowed by Windows).
// The name is also the folder on the volume, so it must not start with "."
// (hidden, and the place of .titanos and .recycle), and Windows drops a
// blank or "." at the end.
func ValidName(name string) bool {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameLength {
		return false
	}
	if strings.TrimSpace(name) != name || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "  ") {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" -_.", r) {
			return false
		}
	}
	return !slices.ContainsFunc(reservedNames, func(reserved string) bool { return strings.EqualFold(reserved, name) })
}

// sameName compares like Samba and the SMB clients do: "Fotos" and "fotos"
// are the same share.
func sameName(a, b string) bool {
	return strings.EqualFold(a, b)
}

// Manager knows the shares and applies them to the system.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// Disabled leaves the system alone: no accounts, no config files, no
	// services. Set in development mode.
	Disabled bool
	// Asleep tells whether the disks of a volume sleep (from RAM, without
	// touching them). The hourly emptying of recycle bins waits for such a
	// volume until it is awake again instead of waking it. Nil: never.
	Asleep func(volume string) bool
	// recycleSwept is when the recycle bins of a volume were last looked
	// through, recycleLooked when Run last asked (only Run uses both).
	recycleSwept  map[string]time.Time
	recycleLooked time.Time

	mountRoot string
	mounts    func() map[string]bool
	gid       int // group that owns the shares
	sambaConf string
	exports   string
	nfsRoot   string // NFS shares are bind-mounted here
	runDir    string

	mu      sync.Mutex
	shares  []Share
	loaded  bool
	applied [sha256.Size]byte
	kick    chan struct{}

	quota       quotaState
	quotaKernel quotaKernel

	// gen counts saved changes; appliedGen is the newest one Apply has
	// brought to the system. Deleting a share's data waits for it.
	gen, appliedGen uint64
	// lastApplied: when Apply last ended without error (LastApplied).
	lastApplied time.Time
	purge       purgeState

	guard guardState
	// Shadow returns the folder of @GMT snapshots of a volume, "" if it has
	// none (not Btrfs). Set by titand from the storage manager.
	Shadow func(volume string) string
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, mountRoot: "/mnt", gid: UsersGID, mounts: mountPoints,
		sambaConf: "/etc/samba/smb.conf", exports: "/etc/exports", nfsRoot: "/srv/nfs", runDir: "/run/titanos",
		kick: make(chan struct{}, 1),
	}
}

// shadowDir is Shadow for a share.
func (m *Manager) shadowDir(share Share) string {
	if m.Shadow == nil {
		return ""
	}
	return m.Shadow(share.Volume)
}

// Path returns where the files of a share live.
func (m *Manager) Path(share Share) string {
	return filepath.Join(m.mountRoot, share.Volume, share.Name)
}

// Load reads the shares from /config.
func (m *Manager) Load() error {
	var shares []Share
	err := m.store.ReadJSON(configFile, &shares)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shares = shares
	for i := range m.shares {
		if !slices.Contains([]string{"", "off", ReadOnly, ReadWrite}, m.shares[i].NFS.Mode) {
			m.loaded = false
			return ErrInvalidOption
		}
		if m.shares[i].NFS.Mode == ReadOnly {
			m.shares[i].NFS.Enabled = true
		}
	}
	m.loaded = true
	return nil
}

// List returns the shares sorted by name.
func (m *Manager) List() []Share {
	m.mu.Lock()
	defer m.mu.Unlock()
	shares := slices.Clone(m.shares)
	sort.Slice(shares, func(i, j int) bool { return shares[i].Name < shares[j].Name })
	return shares
}

func (m *Manager) save(shares []Share) error {
	if shares == nil {
		shares = []Share{}
	}
	persisted := slices.Clone(shares)
	for i := range persisted {
		if persisted[i].NFS.Mode == ReadOnly {
			persisted[i].NFS.Enabled = false
		}
	}
	if err := m.store.WriteJSON(configFile, persisted); err != nil {
		return err
	}
	m.shares = shares
	m.gen++
	select {
	case m.kick <- struct{}{}:
	default:
	}
	return nil
}

// normalize checks a share and fills in defaults.
func normalize(share *Share, volumes, users []string) error {
	switch share.NFS.Mode {
	case "": // Existing clients retain their original enabled flag.
	case "off":
		share.NFS.Enabled = false
	case ReadOnly, ReadWrite:
		share.NFS.Enabled = true
	default:
		return ErrInvalidOption
	}
	if !ValidName(share.Name) {
		return ErrInvalidName
	}
	if !slices.Contains(volumes, share.Volume) {
		return ErrUnknownVolume
	}
	if share.Access == nil {
		share.Access = map[string]string{}
	}
	for user, level := range share.Access {
		if !slices.Contains(users, user) {
			return fmt.Errorf("%w: %s", ErrUnknownUser, user)
		}
		if level != ReadOnly && level != ReadWrite {
			return ErrInvalidAccess
		}
	}
	clients := []string{}
	for _, client := range share.NFS.Clients {
		client = strings.TrimSpace(client)
		if client == "" {
			continue
		}
		if !clientRE.MatchString(client) {
			return fmt.Errorf("%w: %s", ErrInvalidClient, client)
		}
		clients = append(clients, client)
	}
	share.NFS.Clients = clients
	if share.NFS.Enabled && len(clients) == 0 {
		return fmt.Errorf("%w: NFS needs at least one host or network", ErrInvalidClient)
	}
	if share.PerUser {
		// Every account its own folder: NFS knows no accounts, guests none.
		share.NFS.Enabled, share.Guest = false, false
	}
	if share.Recycle.Days == 0 {
		share.Recycle.Days = 30
	}
	if share.Recycle.Days < 1 || share.Recycle.Days > 3650 || share.TimeMachine.SizeGB < 0 {
		return ErrInvalidOption
	}
	if share.Quota != 0 && (share.Quota < MinQuota || share.Quota > MaxQuota) {
		return ErrInvalidOption
	}
	return nil
}

// Create adds a share. volumes and users are the names that exist.
func (m *Manager) Create(share Share, volumes, users []string) error {
	if err := normalize(&share, volumes, users); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if slices.ContainsFunc(m.shares, func(s Share) bool { return sameName(s.Name, share.Name) }) {
		return ErrExists
	}
	share.QuotaID = 0
	if err := m.assignQuotaID(&share); err != nil {
		return err
	}
	return m.save(append(slices.Clone(m.shares), share))
}

// Update changes the settings of a share. Name and volume stay.
func (m *Manager) Update(name string, share Share, volumes, users []string) error {
	share.Name = name
	if err := normalize(&share, volumes, users); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	if m.shares[i].Volume != share.Volume {
		return ErrVolumeChange
	}
	share.QuotaID = m.shares[i].QuotaID
	share.GuardLocked = m.shares[i].GuardLocked
	share.PerUser = m.shares[i].PerUser
	if share.PerUser {
		share.NFS.Enabled, share.Guest = false, false
	}
	if err := m.assignQuotaID(&share); err != nil {
		return err
	}
	shares := slices.Clone(m.shares)
	shares[i] = share
	return m.save(shares)
}

// Relocate points a share at another volume, after its files were copied
// there (the KI-Agent's move, PLAN 58). The files on the old volume stay
// for the caller to remove; the quota id is global and stays.
func (m *Manager) Relocate(name, volume string, volumes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	if !slices.Contains(volumes, volume) {
		return fmt.Errorf("%w: %s", ErrUnknownVolume, volume)
	}
	share := m.shares[i]
	if share.Volume == volume {
		return nil
	}
	share.Volume = volume
	shares := slices.Clone(m.shares)
	shares[i] = share
	return m.save(shares)
}

// GID is the group that owns the files of the shares ("users").
func (m *Manager) GID() int { return m.gid }

// UserFolder is the folder of an account in a share with PerUser.
func (m *Manager) UserFolder(share Share, user string) string {
	return filepath.Join(m.Path(share), user)
}

// userFolders makes the folder of every account with access in a share
// with PerUser, owned by the account and closed to everyone else (0700):
// even where a share is offered without %u (an older version after a
// way back), other accounts cannot open it over SMB.
func (m *Manager) userFolders(share Share, path string, users []auth.User) error {
	for _, u := range users {
		if _, ok := share.Access[u.Name]; !ok || strings.ContainsAny(u.Name, "/\\") || u.Name == "" {
			continue
		}
		dir := filepath.Join(path, u.Name)
		if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
			return fmt.Errorf("share %s: %s is no folder", share.Name, u.Name)
		}
		if err := os.Chown(dir, u.UID, m.gid); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a share. The files stay where they are.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	gone := m.shares[i]
	if err := m.save(slices.Delete(slices.Clone(m.shares), i, i+1)); err != nil {
		return err
	}
	if gone.QuotaID != 0 {
		m.quota.mu.Lock()
		m.quota.released = append(m.quota.released, releasedQuota{gone.Volume, gone.QuotaID})
		m.quota.mu.Unlock()
	}
	return nil
}

// ForgetUser takes a deleted account out of all shares.
func (m *Manager) ForgetUser(user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	shares := slices.Clone(m.shares)
	changed := false
	for i := range shares {
		if _, ok := shares[i].Access[user]; ok {
			access := map[string]string{}
			for name, level := range shares[i].Access {
				if name != user {
					access[name] = level
				}
			}
			shares[i].Access = access
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return m.save(shares)
}

// Kick asks for the system to be brought up to date now, e.g. after an
// account changed.
func (m *Manager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// LetGo asks for the shares of a volume to be taken down now and ends the
// open SMB connections to them, before the volume is locked. The caller
// has already stopped reporting the volume as mounted.
func (m *Manager) LetGo(ctx context.Context, volume string) {
	m.Kick()
	if m.Disabled {
		return
	}
	for _, share := range m.List() {
		if share.Volume != volume {
			continue
		}
		if _, err := m.runner.Run(ctx, "", "smbcontrol", "smbd", "close-share", share.Name); err != nil {
			slog.Debug("close share before locking", "share", share.Name, "err", err)
		}
	}
}

// mountPoints returns what is mounted right now.
func mountPoints() map[string]bool {
	data, _ := os.ReadFile("/proc/self/mounts")
	return parseMounts(string(data))
}

// parseMounts reads the mount points from /proc/self/mounts, which writes
// a blank in a path as \040.
func parseMounts(data string) map[string]bool {
	mounts := map[string]bool{}
	for line := range strings.SplitSeq(data, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			mounts[unescapeOctal(fields[1])] = true
		}
	}
	return mounts
}

func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// bindShares mounts the folder of every NFS share at <nfsRoot>/<name>.
func (m *Manager) bindShares(ctx context.Context, shares []Share) error {
	mounts := mountPoints()
	for _, share := range shares {
		if !share.NFS.Enabled {
			continue
		}
		target := filepath.Join(m.nfsRoot, share.Name)
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		if mounts[target] {
			continue
		}
		if _, err := m.runner.Run(ctx, "", "mount", "--bind", m.Path(share), target); err != nil {
			return err
		}
	}
	return nil
}

// unbindShares removes the mounts of shares that are no longer offered over NFS.
func (m *Manager) unbindShares(ctx context.Context, shares []Share) {
	entries, _ := os.ReadDir(m.nfsRoot)
	mounts := mountPoints()
	for _, entry := range entries {
		if slices.ContainsFunc(shares, func(s Share) bool { return s.NFS.Enabled && s.Name == entry.Name() }) {
			continue
		}
		target := filepath.Join(m.nfsRoot, entry.Name())
		if mounts[target] {
			if _, err := m.runner.Run(ctx, "", "umount", target); err != nil {
				slog.Debug("unmount NFS share", "path", target, "err", err)
				continue
			}
		}
		os.Remove(target)
	}
}

// sweepRecycle deletes files that have been in a share's recycle bin for
// longer than its retention time. It only ever looks inside ".recycle".
func (m *Manager) sweepRecycle(mounted []string, now time.Time) {
	m.sweepRecycleOn(mounted, nil, now)
}

// sweepRecycleDue looks through the recycle bins of every mounted volume
// once an hour, the first time an hour after the volume was first seen.
// A volume whose disks sleep is left alone until they are awake again; it
// is then looked through at once, so a bin on a volume that is awake only
// now and then still gets emptied. Asked at most once a minute.
func (m *Manager) sweepRecycleDue(mounted []string, now time.Time) {
	if m.Disabled || now.Sub(m.recycleLooked) < time.Minute && !now.Before(m.recycleLooked) {
		return
	}
	m.recycleLooked = now
	if m.recycleSwept == nil {
		m.recycleSwept = map[string]time.Time{}
	}
	due := map[string]bool{}
	for _, volume := range mounted {
		last, seen := m.recycleSwept[volume]
		switch {
		case !seen:
			m.recycleSwept[volume] = now
		case now.Sub(last) < time.Hour:
		case m.Asleep != nil && m.Asleep(volume):
		default:
			due[volume] = true
			m.recycleSwept[volume] = now
		}
	}
	if len(due) > 0 {
		m.sweepRecycleOn(mounted, due, now)
	}
}

// sweepRecycleOn is sweepRecycle for the volumes in only (nil: all).
func (m *Manager) sweepRecycleOn(mounted []string, only map[string]bool, now time.Time) {
	if m.Disabled {
		return
	}
	for _, share := range m.List() {
		if !share.Recycle.Enabled || !slices.Contains(mounted, share.Volume) || only != nil && !only[share.Volume] {
			continue
		}
		bin := filepath.Join(m.Path(share), recycleDir)
		cutoff := now.AddDate(0, 0, -share.Recycle.Days)
		removed := 0
		var dirs []string
		// When a folder was deleted: the file manager notes it on the
		// folder and leaves the dates of the files inside alone
		// (files.rememberTrash). A file counts from the newest of its own
		// date, its own note and the notes of the folders above it, so
		// old photos in a folder deleted yesterday stay their full time.
		// WalkDir visits a folder before what is in it.
		since := map[string]time.Time{}
		_ = filepath.WalkDir(bin, func(path string, entry fs.DirEntry, err error) error { // the callback collects what it can; unreadable entries are skipped
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				m.repairBinFolder(path, entry)
			}
			if path == bin {
				return nil
			}
			deleted := since[filepath.Dir(path)]
			if noted := trashNote(path); noted.After(deleted) {
				deleted = noted
			}
			if entry.IsDir() {
				since[path] = deleted
				dirs = append(dirs, path)
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			if info.ModTime().After(deleted) {
				deleted = info.ModTime()
			}
			if deleted.Before(cutoff) {
				if os.Remove(path) == nil {
					removed++
				}
			}
			return nil
		})
		// Deepest first; a directory that still holds something stays.
		slices.Reverse(dirs)
		for _, dir := range dirs {
			os.Remove(dir)
		}
		if removed > 0 {
			slog.Info("recycle bin emptied", "share", share.Name, "files", removed)
		}
	}
}

// repairBinFolder makes a folder of the recycle bin writable for the group
// of the shares. Versions before 0.75 let the file manager make them for
// root only; Samba, working as the account, could then not move a deleted
// file there and deleted it for good.
func (m *Manager) repairBinFolder(path string, entry fs.DirEntry) {
	info, err := entry.Info()
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o020 != 0 {
		return
	}
	_ = os.Lchown(path, -1, m.gid)          // best effort, the next sweep tries again
	_ = os.Chmod(path, 0o770|os.ModeSetgid) // see above
}

// trashAttribute is where the file manager notes when and from where an
// entry went to the recycle bin (files.trashAttribute).
const trashAttribute = "user.titanos.trash"

// trashNote is the time of deletion noted on an entry of the recycle bin,
// zero if there is none. Links are not followed.
func trashNote(path string) time.Time {
	data := make([]byte, 8192)
	n, err := unix.Lgetxattr(path, trashAttribute, data)
	if err != nil {
		return time.Time{}
	}
	var note struct {
		Deleted time.Time `json:"deleted"`
	}
	if json.Unmarshal(data[:n], &note) != nil {
		return time.Time{}
	}
	return note.Deleted
}

// UseMountRoot puts the volumes below root instead of /mnt. For tests of
// other packages.
func (m *Manager) UseMountRoot(root string) { m.mountRoot = root }
