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
}

// NFS exports the share to the listed hosts or networks. NFS knows no
// accounts: whoever sits on one of those hosts can read and write.
type NFS struct {
	Enabled bool     `json:"enabled"`
	Clients []string `json:"clients"`
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

	mountRoot string
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
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, mountRoot: "/mnt", gid: UsersGID,
		sambaConf: "/etc/samba/smb.conf", exports: "/etc/exports", nfsRoot: "/srv/nfs", runDir: "/run/titanos",
		kick: make(chan struct{}, 1),
	}
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
	if err := m.store.WriteJSON(configFile, shares); err != nil {
		return err
	}
	m.shares = shares
	select {
	case m.kick <- struct{}{}:
	default:
	}
	return nil
}

// normalize checks a share and fills in defaults.
func normalize(share *Share, volumes, users []string) error {
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
	if share.Recycle.Days == 0 {
		share.Recycle.Days = 30
	}
	if share.Recycle.Days < 1 || share.Recycle.Days > 3650 || share.TimeMachine.SizeGB < 0 {
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
	shares := slices.Clone(m.shares)
	shares[i] = share
	return m.save(shares)
}

// Delete removes a share. The files stay where they are.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	return m.save(slices.Delete(slices.Clone(m.shares), i, i+1))
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

// State is what the system is derived from.
type State struct {
	Users []auth.User
	// Mounted are the volumes that are mounted. Shares on other volumes are
	// not offered: their folder would be an empty one in RAM.
	Mounted []string
}

// Run keeps the system in line with the configuration until ctx is done.
// state is asked for the accounts and mounted volumes each time.
func (m *Manager) Run(ctx context.Context, state func() State) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	sweep := time.NewTicker(time.Hour)
	defer sweep.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			if err := m.Load(); err == nil {
				slog.Info("shares read from /config")
			}
		} else if err := m.Apply(ctx, state()); err != nil {
			slog.Error("apply shares", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
		case <-sweep.C:
			m.sweepRecycle(state().Mounted, time.Now())
		}
	}
}

// Apply makes accounts, configuration files and services match the shares.
// It does nothing if nothing changed since the last time.
func (m *Manager) Apply(ctx context.Context, state State) error {
	if m.Disabled {
		return nil
	}
	shares := m.List()
	var offered []Share
	for _, share := range shares {
		if slices.Contains(state.Mounted, share.Volume) {
			offered = append(offered, share)
		}
	}
	users := slices.Clone(state.Users)
	sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })

	smbConf := renderSMBConf(offered, m.Path)
	exports := renderExports(offered, m.nfsRoot)
	passwords := renderSMBPasswd(users)
	signature := sha256.Sum256([]byte(smbConf + "\x00" + exports + "\x00" + passwords))
	m.mu.Lock()
	unchanged := signature == m.applied
	m.mu.Unlock()
	if unchanged {
		// A share taken off NFS may still have been busy when its mount
		// was to be removed; try again until it is gone.
		m.unbindShares(ctx, offered)
		return nil
	}

	if err := m.syncAccounts(ctx, users, passwords); err != nil {
		return err
	}
	for _, share := range offered {
		path := m.Path(share)
		if err := os.MkdirAll(path, 0o770); err != nil {
			return err
		}
		// Group "users" with the setgid bit: everything created inside
		// belongs to the group, whoever wrote it and over which protocol.
		if err := os.Chown(path, -1, m.gid); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o770|os.ModeSetgid); err != nil {
			return err
		}
	}
	if err := writeFile(m.sambaConf, smbConf); err != nil {
		return err
	}
	if err := writeFile(m.exports, exports); err != nil {
		return err
	}

	smb := slices.ContainsFunc(offered, func(s Share) bool { return s.SMB && offeredOverSMB(s) })
	nfs := slices.ContainsFunc(offered, func(s Share) bool { return s.NFS.Enabled })
	if err := m.service(ctx, smb, "smbd.service", "reload-or-restart"); err != nil {
		return err
	}
	// Discovery: wsdd2 for the Windows network view, avahi for macOS and Linux.
	for _, unit := range []string{"wsdd2.service", "avahi-daemon.service"} {
		if err := m.service(ctx, smb, unit, "start"); err != nil {
			slog.Warn("network discovery", "unit", unit, "err", err)
		}
	}
	if err := m.bindShares(ctx, offered); err != nil {
		return err
	}
	if err := m.service(ctx, nfs, "nfs-server.service", "start"); err != nil {
		return err
	}
	if nfs {
		if _, err := m.runner.Run(ctx, "", "exportfs", "-ra"); err != nil {
			return err
		}
	}
	// Only now, when nothing exports them any more.
	m.unbindShares(ctx, offered)

	m.mu.Lock()
	m.applied = signature
	m.mu.Unlock()
	slog.Info("shares applied", "shares", len(offered), "accounts", len(users), "smb", smb, "nfs", nfs)
	return nil
}

func (m *Manager) service(ctx context.Context, wanted bool, unit, startVerb string) error {
	verb := "stop"
	if wanted {
		verb = startVerb
	}
	_, err := m.runner.Run(ctx, "", "systemctl", verb, unit)
	return err
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".titanos-new"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// syncAccounts creates the Linux user of every account and gives Samba
// their password hashes. Accounts that are gone are removed from both.
func (m *Manager) syncAccounts(ctx context.Context, users []auth.User, passwords string) error {
	for _, user := range users {
		if _, err := m.runner.Run(ctx, "", "getent", "passwd", user.Name); err == nil {
			continue
		}
		// No login shell and no home: the account is for file access.
		if _, err := m.runner.Run(ctx, "", "useradd", "--uid", fmt.Sprint(user.UID), "--gid", "users",
			"--no-create-home", "--no-user-group", "--shell", "/usr/sbin/nologin", user.Name); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(m.runDir, 0o700); err != nil {
		return err
	}
	file := filepath.Join(m.runDir, "smbpasswd")
	if err := os.WriteFile(file, []byte(passwords), 0o600); err != nil {
		return err
	}
	defer os.Remove(file)
	if passwords != "" {
		if _, err := m.runner.Run(ctx, "", "pdbedit", "--import=smbpasswd:"+file); err != nil {
			return err
		}
	}

	known, err := m.runner.Run(ctx, "", "pdbedit", "--list")
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(known, "\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok || slices.ContainsFunc(users, func(u auth.User) bool { return u.Name == name && u.SMBHash != "" }) {
			continue
		}
		if _, err := m.runner.Run(ctx, "", "pdbedit", "--delete", "--user="+name); err != nil {
			slog.Warn("remove Samba account", "user", name, "err", err)
		}
		if !slices.ContainsFunc(users, func(u auth.User) bool { return u.Name == name }) {
			if _, err := m.runner.Run(ctx, "", "userdel", name); err != nil {
				slog.Warn("remove Linux user", "user", name, "err", err)
			}
		}
	}
	return nil
}

// renderSMBPasswd writes the accounts in the format of Samba's smbpasswd
// file, which pdbedit can import. Accounts without an SMB hash are left out;
// they get one at their next login.
func renderSMBPasswd(users []auth.User) string {
	var b strings.Builder
	for _, user := range users {
		if user.SMBHash == "" {
			continue
		}
		// The time the password was last changed must not be zero: Samba
		// reads that as "must change the password before logging in".
		changed := max(user.Created.Unix(), 1)
		fmt.Fprintf(&b, "%s:%d:%s:%s:[U          ]:LCT-%08X:\n", user.Name, user.UID, strings.Repeat("X", 32), user.SMBHash, changed)
	}
	return b.String()
}

// accessLists returns who may use a share at all and who may write.
func accessLists(share Share) (all, writers []string) {
	for user, level := range share.Access {
		all = append(all, user)
		if level == ReadWrite {
			writers = append(writers, user)
		}
	}
	sort.Strings(all)
	sort.Strings(writers)
	return all, writers
}

// offeredOverSMB reports whether anyone could open the share. Samba treats
// an empty list of valid users as "everyone", so a share without any
// access must not be written to smb.conf at all.
func offeredOverSMB(share Share) bool {
	return share.Guest || len(share.Access) > 0
}

func renderSMBConf(shares []Share, path func(Share) string) string {
	guests := slices.ContainsFunc(shares, func(s Share) bool { return s.SMB && s.Guest })
	var b strings.Builder
	b.WriteString("# Written by titand from /config/shares/shares.json. Do not edit: it is\n# replaced whenever shares or accounts change.\n\n[global]\n")
	global := [][2]string{
		{"server role", "standalone server"},
		{"workgroup", "WORKGROUP"},
		{"server string", "TitanOS"},
		{"security", "user"},
		{"passdb backend", "tdbsam"},
		{"server min protocol", "SMB2"},
		{"disable netbios", "yes"},
		{"smb ports", "445"},
		{"logging", "systemd"},
		{"log level", "1"},
		{"load printers", "no"},
		{"printing", "bsd"},
		{"printcap name", "/dev/null"},
		{"disable spoolss", "yes"},
	}
	if guests {
		global = append(global, [2]string{"map to guest", "bad user"}, [2]string{"guest account", "nobody"})
	} else {
		global = append(global, [2]string{"map to guest", "never"})
	}
	for _, option := range global {
		fmt.Fprintf(&b, "   %s = %s\n", option[0], option[1])
	}

	for _, share := range shares {
		if !share.SMB || !offeredOverSMB(share) {
			continue
		}
		all, writers := accessLists(share)
		options := [][2]string{
			{"path", path(share)},
			{"browseable", yesNo(share.Browseable)},
			// Read-only for everyone except the write list.
			{"read only", "yes"},
			{"guest ok", yesNo(share.Guest)},
			{"force group", "users"},
			{"create mask", "0660"},
			{"force create mode", "0660"},
			{"directory mask", "2770"},
			{"force directory mode", "2770"},
		}
		if !share.Guest {
			options = append(options, [2]string{"valid users", strings.Join(all, " ")})
		}
		if len(writers) > 0 {
			options = append(options, [2]string{"write list", strings.Join(writers, " ")})
		}
		var vfs []string
		if share.TimeMachine.Enabled {
			vfs = append(vfs, "catia", "fruit", "streams_xattr")
			options = append(options, [2]string{"fruit:time machine", "yes"})
			if share.TimeMachine.SizeGB > 0 {
				options = append(options, [2]string{"fruit:time machine max size", fmt.Sprintf("%dG", share.TimeMachine.SizeGB)})
			}
		}
		if share.Recycle.Enabled {
			vfs = append(vfs, "recycle")
			options = append(options,
				[2]string{"recycle:repository", recycleDir + "/%U"},
				[2]string{"recycle:keeptree", "yes"},
				[2]string{"recycle:versions", "yes"},
				// The time of deletion decides when a file is removed for good.
				[2]string{"recycle:touch_mtime", "yes"},
				[2]string{"recycle:directory_mode", "0770"},
			)
		}
		if len(vfs) > 0 {
			options = append(options, [2]string{"vfs objects", strings.Join(vfs, " ")})
		}
		fmt.Fprintf(&b, "\n[%s]\n", share.Name)
		for _, option := range options {
			fmt.Fprintf(&b, "   %s = %s\n", option[0], option[1])
		}
	}
	return b.String()
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// renderExports writes /etc/exports. Every NFS share is bind-mounted into
// root (see syncBinds), which becomes the root of the NFSv4 tree: a client
// mounts "nas:/<share>". Without it NFSv4 would start at "/", an overlay in
// RAM that cannot be exported, and it could not step from one filesystem
// into a folder of another.
func renderExports(shares []Share, root string) string {
	var b strings.Builder
	b.WriteString("# Written by titand from /config/shares/shares.json. Do not edit.\n")
	var clients []string
	for _, share := range shares {
		if share.NFS.Enabled {
			for _, client := range share.NFS.Clients {
				if !slices.Contains(clients, client) {
					clients = append(clients, client)
				}
			}
		}
	}
	if len(clients) == 0 {
		return b.String()
	}
	sort.Strings(clients)
	// Read-only, and only a way in: it holds nothing but the mount points.
	b.WriteString(exportPath(root))
	for _, client := range clients {
		fmt.Fprintf(&b, " %s(ro,fsid=0,sync,no_subtree_check,all_squash)", client)
	}
	b.WriteString("\n")
	for _, share := range shares {
		if !share.NFS.Enabled {
			continue
		}
		b.WriteString(exportPath(filepath.Join(root, share.Name)))
		for _, client := range share.NFS.Clients {
			// Every NFS client writes as nobody:users, so that files fit the
			// share's group whatever user numbers the client has.
			fmt.Fprintf(&b, " %s(rw,nohide,sync,no_subtree_check,all_squash,anonuid=65534,anongid=%d)", client, UsersGID)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// exportPath writes a path for /etc/exports, where a blank would end it:
// exportfs reads blanks and backslashes as octal escapes, like fstab.
func exportPath(path string) string {
	return strings.NewReplacer("\\", `\134`, " ", `\040`, "\t", `\011`).Replace(path)
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
	if m.Disabled {
		return
	}
	for _, share := range m.List() {
		if !share.Recycle.Enabled || !slices.Contains(mounted, share.Volume) {
			continue
		}
		bin := filepath.Join(m.Path(share), recycleDir)
		cutoff := now.AddDate(0, 0, -share.Recycle.Days)
		removed := 0
		var dirs []string
		filepath.WalkDir(bin, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || path == bin {
				return nil
			}
			if entry.IsDir() {
				dirs = append(dirs, path)
				return nil
			}
			if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
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
