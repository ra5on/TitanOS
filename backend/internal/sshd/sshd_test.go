package sshd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

// fakeSystem knows which Linux users exist and who is in the group, and
// makes a real key when asked to.
type fakeSystem struct {
	linux   map[string]bool
	members []string
	calls   []string
}

func (f *fakeSystem) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, command)
	failed := &run.CommandError{Command: command, Err: errors.New("exit status 2")}
	switch name {
	case "getent":
		if args[0] == "group" {
			return "titanos-ssh:x:990:" + strings.Join(f.members, ",") + "\n", nil
		}
		if !f.linux[args[1]] {
			return "", failed
		}
	case "usermod":
		if user := args[len(args)-1]; strings.Contains(command, "--append") && !slices.Contains(f.members, user) {
			f.members = append(f.members, user)
		}
	case "gpasswd":
		f.members = slices.DeleteFunc(f.members, func(member string) bool { return member == args[1] })
	case "ssh-keygen":
		file := args[len(args)-1]
		public, private, _ := ed25519.GenerateKey(rand.Reader)
		block, _ := ssh.MarshalPrivateKey(private, "titanos")
		os.WriteFile(file, pem.EncodeToMemory(block), 0o600)
		key, _ := ssh.NewPublicKey(public)
		os.WriteFile(file+".pub", ssh.MarshalAuthorizedKey(key), 0o644)
	}
	return "", nil
}

func (f *fakeSystem) ran(part string) int {
	count := 0
	for _, call := range f.calls {
		if strings.Contains(call, part) {
			count++
		}
	}
	return count
}

func userKey(t *testing.T) string {
	t.Helper()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " test"
}

func newManager(t *testing.T) (*Manager, *fakeSystem, string) {
	t.Helper()
	root := t.TempDir()
	system := &fakeSystem{linux: map[string]bool{"alex": true, "bo": true, "gast": true}}
	m := NewManager(config.Open(filepath.Join(root, "config")), 100)
	m.runner, m.etcDir, m.homeRoot, m.workDir = system, filepath.Join(root, "etc/ssh"), filepath.Join(root, "home"), filepath.Join(root, "run")
	m.chown = func(string, int, int) error { return nil }
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.MkdirAll(m.etcDir, 0o755)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, system, root
}

func TestOffByDefault(t *testing.T) {
	m, system, _ := newManager(t)
	users := []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{userKey(t)}}}
	if complete, err := m.Apply(context.Background(), users); err != nil || !complete {
		t.Fatal(complete, err)
	}
	if system.ran("systemctl start") != 0 || system.ran("systemctl stop ssh.service") != 1 || system.ran("ssh-keygen") != 0 {
		t.Errorf("calls: %v", system.calls)
	}
	if status := m.Status(); status.Enabled || status.Running || len(status.Accounts) != 0 {
		t.Errorf("status: %+v", status)
	}
}

func TestOnlyAdministratorsWithAKeyGetIn(t *testing.T) {
	m, system, root := newManager(t)
	alexKey := userKey(t)
	users := []auth.User{
		{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{alexKey}},
		{Name: "bo", Role: auth.RoleAdmin, UID: 2001},                                 // no key
		{Name: "gast", Role: auth.RoleUser, UID: 2002, SSHKeys: []string{userKey(t)}}, // not an administrator
	}
	// However the directory came to be, sshd wants it closed to others.
	os.MkdirAll(filepath.Join(m.etcDir, "authorized_keys"), 0o777)
	os.Chmod(filepath.Join(m.etcDir, "authorized_keys"), 0o777)
	if err := m.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if complete, err := m.Apply(context.Background(), users); err != nil || !complete {
		t.Fatal(complete, err)
	}
	status := m.Status()
	if !status.Enabled || !status.Running || len(status.Accounts) != 1 || status.Accounts[0] != "alex" || !strings.HasPrefix(status.Fingerprint, "SHA256:") {
		t.Errorf("status: %+v", status)
	}
	if info, _ := os.Stat(filepath.Join(m.etcDir, "authorized_keys")); info.Mode().Perm() != 0o755 {
		t.Errorf("the directory of the keys is open to others: %v", info.Mode())
	}
	if keys, _ := os.ReadFile(filepath.Join(m.etcDir, "authorized_keys/alex")); string(keys) != alexKey+"\n" {
		t.Errorf("authorized keys of alex: %q", keys)
	}
	for _, name := range []string{"bo", "gast"} {
		if _, err := os.Stat(filepath.Join(m.etcDir, "authorized_keys", name)); err == nil {
			t.Errorf("%s got a key file", name)
		}
	}
	if system.ran("usermod --shell /bin/bash --home "+filepath.Join(root, "home/alex")+" --append --groups titanos-ssh alex") != 1 || system.ran("--groups titanos-ssh bo") != 0 || system.ran("--groups titanos-ssh gast") != 0 {
		t.Errorf("calls: %v", system.calls)
	}
	if info, err := os.Stat(filepath.Join(m.etcDir, "ssh_host_ed25519_key")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("key of the NAS: %v", err)
	}

	// The key of the NAS is made once and then comes from /config.
	first := status.Fingerprint
	os.Remove(filepath.Join(m.etcDir, "ssh_host_ed25519_key"))
	m.Apply(context.Background(), users)
	if system.ran("ssh-keygen") != 1 || m.Status().Fingerprint != first {
		t.Errorf("the key of the NAS changed: %d runs of ssh-keygen", system.ran("ssh-keygen"))
	}

	// alex loses the role: out of the group, logged out, key file gone.
	users[0].Role = auth.RoleUser
	m.Apply(context.Background(), users)
	if system.ran("gpasswd --delete alex titanos-ssh") != 1 || system.ran("loginctl terminate-user alex") != 1 || system.ran("usermod --shell /usr/sbin/nologin alex") != 1 {
		t.Errorf("calls: %v", system.calls)
	}
	if _, err := os.Stat(filepath.Join(m.etcDir, "authorized_keys/alex")); err == nil {
		t.Error("the key file of alex is still there")
	}
	if len(m.Status().Accounts) != 0 {
		t.Errorf("accounts: %v", m.Status().Accounts)
	}
}

func TestSwitchingOffLogsEveryoneOut(t *testing.T) {
	m, system, _ := newManager(t)
	users := []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{userKey(t)}}}
	m.SetEnabled(true)
	m.Apply(context.Background(), users)
	m.SetEnabled(false)
	if complete, err := m.Apply(context.Background(), users); err != nil || !complete {
		t.Fatal(complete, err)
	}
	if system.ran("systemctl stop ssh.service") != 1 || system.ran("loginctl terminate-user alex") != 1 || len(system.members) != 0 {
		t.Errorf("calls: %v, members %v", system.calls, system.members)
	}
	if entries, _ := os.ReadDir(filepath.Join(m.etcDir, "authorized_keys")); len(entries) != 0 {
		t.Errorf("key files left: %v", entries)
	}
	// The key of the NAS stays in /config for the next time.
	if _, err := m.store.Read(hostKeyFile); err != nil {
		t.Errorf("the key of the NAS is gone from /config: %v", err)
	}
}

func TestAccountWithoutLinuxUserWaits(t *testing.T) {
	m, system, _ := newManager(t)
	delete(system.linux, "alex")
	m.SetEnabled(true)
	complete, err := m.Apply(context.Background(), []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{userKey(t)}}})
	if err != nil || complete || system.ran("usermod") != 0 {
		t.Errorf("complete=%v err=%v calls=%v", complete, err, system.calls)
	}
}

func TestDevelopmentModeTouchesNothing(t *testing.T) {
	m, system, _ := newManager(t)
	m.Disabled = true
	if err := m.SetEnabled(true); !errors.Is(err, ErrDisabled) {
		t.Errorf("enable: %v", err)
	}
	m.Apply(context.Background(), nil)
	if len(system.calls) != 0 {
		t.Errorf("calls: %v", system.calls)
	}
}

// The key of the NAS is made only when there is none. A missing public half
// is derived from the private key, so the fingerprint stays the same.
func TestHostKeyKeptWhenOnlyPublicHalfIsMissing(t *testing.T) {
	m, _, _ := newManager(t)
	first, err := m.hostKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Remove(hostKeyFile + ".pub"); err != nil {
		t.Fatal(err)
	}
	second, err := m.hostKey(context.Background())
	if err != nil || second != first {
		t.Fatalf("fingerprint %q -> %q (%v)", first, second, err)
	}
}

func TestRemovedKeyEndsSessions(t *testing.T) {
	m, system, _ := newManager(t)
	first, second := userKey(t), userKey(t)
	m.SetEnabled(true)
	if _, err := m.Apply(context.Background(), []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{first, second}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(context.Background(), []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{first, second}}}); err != nil || system.ran("loginctl terminate-user alex") != 0 {
		t.Fatalf("unchanged keys ended sessions: %v", system.calls)
	}
	if _, err := m.Apply(context.Background(), []auth.User{{Name: "alex", Role: auth.RoleOwner, UID: 2000, SSHKeys: []string{first}}}); err != nil || system.ran("loginctl terminate-user alex") != 1 {
		t.Fatalf("removing a key kept the sessions: %v", system.calls)
	}
}
