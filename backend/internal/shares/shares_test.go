package shares

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct {
	commands []string
	// passdb is what `pdbedit --list` reports.
	passdb string
	// smbstatus is what `smbstatus --processes --json` reports.
	smbstatus string
	// fail makes commands starting with it fail.
	fail string
}

func (r *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	switch {
	case name == "getent":
		return "", errors.New("exit status 2") // no Linux user exists yet
	case name == "pdbedit" && slices.Contains(args, "--list"):
		return r.passdb, nil
	case name == "smbstatus":
		return r.smbstatus, nil
	case r.fail != "" && strings.HasPrefix(command, r.fail):
		return "", errors.New("exit status 1")
	}
	r.commands = append(r.commands, command)
	return "", nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(config.Open(filepath.Join(dir, "config")))
	runner := &fakeRunner{}
	m.runner = runner
	m.mountRoot = filepath.Join(dir, "mnt")
	m.gid = os.Getgid()
	m.sambaConf = filepath.Join(dir, "etc/samba/smb.conf")
	m.exports = filepath.Join(dir, "etc/exports")
	m.nfsRoot = filepath.Join(dir, "srv/nfs")
	m.runDir = filepath.Join(dir, "run")
	os.MkdirAll(filepath.Join(dir, "config"), 0o700)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, runner
}

var (
	volumes = []string{"data", "fast"}
	users   = []string{"alex", "gast"}
)

func TestValidation(t *testing.T) {
	m, _ := newTestManager(t)
	for name, tc := range map[string]struct {
		share Share
		want  error
	}{
		"Samba variable":         {Share{Name: "fotos %U", Volume: "data"}, ErrInvalidName},
		"Samba section end":      {Share{Name: "fotos]", Volume: "data"}, ErrInvalidName},
		"hidden folder":          {Share{Name: ".titanos", Volume: "data"}, ErrInvalidName},
		"leading blank":          {Share{Name: " Fotos", Volume: "data"}, ErrInvalidName},
		"trailing dot":           {Share{Name: "Fotos.", Volume: "data"}, ErrInvalidName},
		"Windows reserved":       {Share{Name: "a:b", Volume: "data"}, ErrInvalidName},
		"too long":               {Share{Name: strings.Repeat("ä", 33), Volume: "data"}, ErrInvalidName},
		"samba section any case": {Share{Name: "Global", Volume: "data"}, ErrInvalidName},
		"path in name":           {Share{Name: "../etc", Volume: "data"}, ErrInvalidName},
		"samba section":          {Share{Name: "global", Volume: "data"}, ErrInvalidName},
		"unknown volume":         {Share{Name: "fotos", Volume: "nope"}, ErrUnknownVolume},
		"unknown account":        {Share{Name: "fotos", Volume: "data", Access: map[string]string{"eve": "rw"}}, ErrUnknownUser},
		"bad access level":       {Share{Name: "fotos", Volume: "data", Access: map[string]string{"alex": "admin"}}, ErrInvalidAccess},
		"NFS without hosts":      {Share{Name: "fotos", Volume: "data", NFS: NFS{Enabled: true}}, ErrInvalidClient},
		"NFS option smuggled in": {Share{Name: "fotos", Volume: "data", NFS: NFS{Enabled: true, Clients: []string{"*(rw,no_root_squash)"}}}, ErrInvalidClient},
		"NFS client with space":  {Share{Name: "fotos", Volume: "data", NFS: NFS{Enabled: true, Clients: []string{"host /etc"}}}, ErrInvalidClient},
		"recycle days":           {Share{Name: "fotos", Volume: "data", Recycle: Recycle{Enabled: true, Days: -1}}, ErrInvalidOption},
	} {
		if err := m.Create(tc.share, volumes, users); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	if len(m.List()) != 0 {
		t.Errorf("rejected shares were stored: %v", m.List())
	}
}

func TestNamesWithCapitalsAndBlanks(t *testing.T) {
	m, _ := newTestManager(t)
	for _, name := range []string{"Fotos 2024", "Musik", "Büro-Daten", "a.b_c", strings.Repeat("ä", 32)} {
		if err := m.Create(Share{Name: name, Volume: "data"}, volumes, users); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	// SMB clients do not tell "Musik" and "musik" apart.
	if err := m.Create(Share{Name: "musik", Volume: "data"}, volumes, users); !errors.Is(err, ErrExists) {
		t.Errorf("same name in other case: %v", err)
	}
}

func TestCreateUpdateDelete(t *testing.T) {
	m, _ := newTestManager(t)
	share := Share{Name: "fotos", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}}
	if err := m.Create(share, volumes, users); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(share, volumes, users); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	if got := m.List()[0]; got.Recycle.Days != 30 {
		t.Errorf("default retention = %d days", got.Recycle.Days)
	}

	share.Volume = "fast"
	if err := m.Update("fotos", share, volumes, users); !errors.Is(err, ErrVolumeChange) {
		t.Errorf("moving to another volume: %v", err)
	}
	share.Volume = "data"
	share.Access["gast"] = ReadOnly
	if err := m.Update("fotos", share, volumes, users); err != nil {
		t.Fatal(err)
	}
	if err := m.ForgetUser("gast"); err != nil {
		t.Fatal(err)
	}
	if _, still := m.List()[0].Access["gast"]; still {
		t.Error("deleted account still has access")
	}

	// A fresh manager reads the same shares from the stick.
	again := NewManager(m.store)
	if err := again.Load(); err != nil || len(again.List()) != 1 {
		t.Fatalf("reload: %v %v", again.List(), err)
	}

	// Deleting a share leaves the files alone.
	path := m.Path(share)
	os.MkdirAll(path, 0o755)
	os.WriteFile(filepath.Join(path, "bild.jpg"), []byte("x"), 0o644)
	if err := m.Delete("fotos"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "bild.jpg")); err != nil {
		t.Errorf("deleting the share removed its files: %v", err)
	}
	if err := m.Delete("fotos"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete twice: %v", err)
	}
}

func TestRenderSMBConf(t *testing.T) {
	path := func(s Share) string { return "/mnt/" + s.Volume + "/" + s.Name }
	conf := renderSMBConf([]Share{
		{Name: "fotos", Volume: "data", SMB: true, Browseable: true, Access: map[string]string{"gast": ReadOnly, "alex": ReadWrite, "bea": ReadWrite},
			Recycle: Recycle{Enabled: true, Days: 30}},
		{Name: "backup", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}, TimeMachine: TimeMachine{Enabled: true, SizeGB: 500}},
		{Name: "nobody", Volume: "data", SMB: true},
		{Name: "nfsonly", Volume: "data", NFS: NFS{Enabled: true, Clients: []string{"192.168.1.0/24"}}, Access: map[string]string{"alex": ReadWrite}},
	}, path)

	for _, want := range []string{
		"map to guest = never",
		"server min protocol = SMB2",
		"[fotos]\n   path = /mnt/data/fotos\n   browseable = yes\n   read only = yes\n   guest ok = no",
		"valid users = alex bea gast",
		"write list = alex bea",
		"vfs objects = recycle",
		"recycle:repository = .recycle/%U",
		"[backup]",
		"fruit:time machine max size = 500G",
		"vfs objects = catia fruit streams_xattr",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("smb.conf lacks %q:\n%s", want, conf)
		}
	}
	// Samba reads an empty "valid users" as "everyone": a share nobody has
	// access to must not appear at all. Neither must a share without SMB.
	for _, unwanted := range []string{"[nobody]", "[nfsonly]", "valid users = \n", "guest ok = yes"} {
		if strings.Contains(conf, unwanted) {
			t.Errorf("smb.conf contains %q:\n%s", unwanted, conf)
		}
	}

	guest := renderSMBConf([]Share{{Name: "public", Volume: "data", SMB: true, Guest: true, Access: map[string]string{"alex": ReadWrite}}}, path)
	if !strings.Contains(guest, "map to guest = bad user") || !strings.Contains(guest, "guest ok = yes") || strings.Contains(guest, "valid users") {
		t.Errorf("guest share:\n%s", guest)
	}
	// Guests can read, never write.
	if !strings.Contains(guest, "read only = yes") || !strings.Contains(guest, "write list = alex") {
		t.Errorf("guest share must stay read-only for guests:\n%s", guest)
	}
}

func TestRenderExportsAndPasswords(t *testing.T) {
	exports := renderExports([]Share{
		{Name: "media", Volume: "data", NFS: NFS{Enabled: true, Clients: []string{"192.168.1.0/24", "tv.lan"}}},
		{Name: "privat", Volume: "data", SMB: true},
	}, "/srv/nfs")
	want := "/srv/nfs 192.168.1.0/24(ro,fsid=0,sync,no_subtree_check,all_squash) tv.lan(ro,fsid=0,sync,no_subtree_check,all_squash)\n" +
		"/srv/nfs/media 192.168.1.0/24(rw,nohide,sync,no_subtree_check,all_squash,anonuid=65534,anongid=100) tv.lan(rw,nohide,sync,no_subtree_check,all_squash,anonuid=65534,anongid=100)\n"
	if !strings.HasSuffix(exports, want) || strings.Contains(exports, "privat") {
		t.Errorf("exports:\n%s", exports)
	}
	// Whole volumes are never exported, only the folders of shares.
	if strings.Contains(exports, "crossmnt") || strings.Contains(exports, "/mnt") {
		t.Errorf("exports reach beyond the shares:\n%s", exports)
	}
	spaced := renderExports([]Share{{Name: "Fotos 2024", Volume: "data", NFS: NFS{Enabled: true, Clients: []string{"tv.lan"}}}}, "/srv/nfs")
	if !strings.Contains(spaced, "\n/srv/nfs/Fotos\\0402024 tv.lan(") {
		t.Errorf("blank not escaped:\n%s", spaced)
	}
	if got := parseMounts("/dev/md0 /srv/nfs/Fotos\\0402024 ext4 rw 0 0\n"); !got["/srv/nfs/Fotos 2024"] {
		t.Errorf("mount point with blank not read: %v", got)
	}
	if none := renderExports([]Share{{Name: "privat", Volume: "data", SMB: true}}, "/srv/nfs"); strings.Contains(none, "/srv/nfs") {
		t.Errorf("nothing is exported, yet:\n%s", none)
	}

	created := time.Unix(0x65000000, 0)
	passwords := renderSMBPasswd([]auth.User{{Name: "alex", UID: 2000, SMBHash: "8846F7EAEE8FB117AD06BDD830B7586C", Created: created}, {Name: "alt", UID: 2001}})
	if passwords != "alex:2000:XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX:8846F7EAEE8FB117AD06BDD830B7586C:[U          ]:LCT-65000000:\n" {
		t.Errorf("smbpasswd = %q", passwords)
	}
}

func TestApply(t *testing.T) {
	m, runner := newTestManager(t)
	runner.passdb = "alex:2000:\nweg:2005:\n"
	m.Create(Share{Name: "fotos", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite}}, volumes, users)
	m.Create(Share{Name: "filme", Volume: "fast", SMB: true, NFS: NFS{Enabled: true, Clients: []string{"*"}}, Access: map[string]string{"alex": ReadWrite}}, volumes, users)
	state := State{Users: []auth.User{{Name: "alex", UID: 2000, SMBHash: "AA"}, {Name: "gast", UID: 2001}}, Mounted: []string{"data"}}
	ctx := context.Background()

	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"useradd --uid 2000 --gid users --no-create-home --no-user-group --shell /usr/sbin/nologin alex",
		"useradd --uid 2001 --gid users --no-create-home --no-user-group --shell /usr/sbin/nologin gast",
		"pdbedit --import=smbpasswd:" + m.runDir + "/smbpasswd",
		"pdbedit --delete --user=weg", // an account that no longer exists
		"userdel weg",
		"systemctl reload-or-restart smbd.service",
		"systemctl stop nfs-server.service", // the only NFS share is on a volume that is not mounted
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "--user=alex") {
		t.Errorf("an existing account was removed:\n%s", all)
	}

	// Only the share on the mounted volume is offered and gets its folder.
	conf, _ := os.ReadFile(m.sambaConf)
	if !strings.Contains(string(conf), "[fotos]") || strings.Contains(string(conf), "[filme]") {
		t.Errorf("smb.conf:\n%s", conf)
	}
	info, err := os.Stat(filepath.Join(m.mountRoot, "data/fotos"))
	if err != nil || info.Mode().Perm() != 0o770 || info.Mode()&os.ModeSetgid == 0 {
		t.Errorf("share folder: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "fast")); err == nil {
		t.Error("a folder was created for a volume that is not mounted (it would live in RAM)")
	}
	if _, err := os.Stat(filepath.Join(m.runDir, "smbpasswd")); err == nil {
		t.Error("the file with the password hashes was left behind")
	}

	// Nothing changed: nothing is run again.
	runner.commands = nil
	if err := m.Apply(ctx, state); err != nil || len(runner.commands) != 0 {
		t.Errorf("second apply ran %v (%v)", runner.commands, err)
	}
	// The second volume appears: its share is offered now.
	state.Mounted = []string{"data", "fast"}
	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	bind := "mount --bind " + filepath.Join(m.mountRoot, "fast/filme") + " " + filepath.Join(m.nfsRoot, "filme")
	if all := strings.Join(runner.commands, "\n"); !strings.Contains(all, "systemctl start nfs-server.service") || !strings.Contains(all, "exportfs -ra") || !strings.Contains(all, bind) {
		t.Errorf("NFS not started:\n%s", all)
	}
	// The share stops being offered over NFS: its place in the NFS tree goes.
	filme := m.List()[0]
	filme.NFS.Enabled = false
	m.Update("filme", filme, volumes, users)
	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.nfsRoot, "filme")); err == nil {
		t.Error("the folder of a share without NFS is still in the NFS tree")
	}

	m.Disabled = true
	runner.commands = nil
	m.Delete("fotos")
	if err := m.Apply(ctx, state); err != nil || len(runner.commands) != 0 {
		t.Errorf("disabled manager ran %v", runner.commands)
	}
}

func TestSweepRecycle(t *testing.T) {
	m, _ := newTestManager(t)
	m.Create(Share{Name: "fotos", Volume: "data", SMB: true, Recycle: Recycle{Enabled: true, Days: 30}, Access: map[string]string{"alex": ReadWrite}}, volumes, users)
	root := filepath.Join(m.mountRoot, "data/fotos")
	now := time.Now()
	write := func(rel string, age time.Duration) string {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("x"), 0o644)
		os.Chtimes(path, now.Add(-age), now.Add(-age))
		return path
	}
	old := write(".recycle/alex/urlaub/alt.jpg", 40*24*time.Hour)
	fresh := write(".recycle/alex/neu.jpg", 5*24*time.Hour)
	live := write("urlaub/uralt.jpg", 400*24*time.Hour)

	m.sweepRecycle([]string{"data"}, now)
	if _, err := os.Stat(old); err == nil {
		t.Error("a file past its retention time is still in the recycle bin")
	}
	if _, err := os.Stat(filepath.Dir(old)); err == nil {
		t.Error("the emptied folder was left in the recycle bin")
	}
	for _, keep := range []string{fresh, live} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was deleted", keep)
		}
	}
}

func TestLockedAccountLosesSMB(t *testing.T) {
	m, runner := newTestManager(t)
	m.Create(Share{Name: "fotos", Volume: "data", SMB: true, Access: map[string]string{"alex": ReadWrite, "gast": ReadOnly}}, volumes, users)
	created := time.Unix(0x65000000, 0)
	accounts := []auth.User{{Name: "alex", UID: 2000, SMBHash: "AA", Created: created}, {Name: "gast", UID: 2001, SMBHash: "BB", Created: created}}
	state := State{Users: accounts, Mounted: []string{"data"}}
	ctx := context.Background()
	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(runner.commands, "\n")
	if !strings.Contains(all, "smbpasswd -e gast") || strings.Contains(all, "smbpasswd -d") || strings.Contains(all, "smbstatus") {
		t.Errorf("before the lock:\n%s", all)
	}

	runner.commands = nil
	runner.smbstatus = `{"sessions": {
		"1": {"username": "gast", "uid": 2001, "server_id": {"pid": "4711"}},
		"2": {"username": "alex", "uid": 2000, "server_id": {"pid": "4712"}},
		"3": {"username": "GAST", "server_id": {"pid": 4713}},
		"4": {"username": "gast", "server_id": {"pid": "1"}}
	}}`
	accounts[1].Disabled = true
	if err := m.Apply(ctx, state); err != nil {
		t.Fatal(err)
	}
	all = strings.Join(runner.commands, "\n")
	for _, want := range []string{"smbpasswd -d gast", "smbpasswd -e alex", "kill -TERM 4711", "kill -TERM 4713"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "4712") || strings.Contains(all, "kill -TERM 1\n") || strings.HasSuffix(all, "kill -TERM 1") {
		t.Errorf("another connection was ended:\n%s", all)
	}
	// The account and its rights stay in smb.conf, only Samba refuses it.
	if passwords := renderSMBPasswd(accounts); !strings.Contains(passwords, "gast:2001:XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX:BB:[DU         ]:") || !strings.Contains(passwords, "alex:2000:XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX:AA:[U          ]:") {
		t.Errorf("smbpasswd:\n%s", passwords)
	}
	if conf, _ := os.ReadFile(m.sambaConf); !strings.Contains(string(conf), "valid users = alex gast") {
		t.Errorf("smb.conf:\n%s", conf)
	}

	// A lock that Samba does not take fails the round, which is tried again.
	runner.commands, runner.fail = nil, "smbpasswd -d"
	accounts[0].Disabled = true
	if err := m.Apply(ctx, state); err == nil {
		t.Error("a failed lock was not reported")
	}
	runner.fail = ""
	if err := m.Apply(ctx, state); err != nil || !strings.Contains(strings.Join(runner.commands, "\n"), "smbpasswd -d alex") {
		t.Errorf("retry: %v\n%s", err, strings.Join(runner.commands, "\n"))
	}
}
