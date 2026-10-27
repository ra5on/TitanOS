package diagnose

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

// fakeSystem answers commands from a table; what is not in it fails like
// a program that is not installed.
type fakeSystem struct {
	output map[string]string
	calls  []string
}

func (f *fakeSystem) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if output, ok := f.output[name]; ok {
		return output, nil
	}
	return "", &run.CommandError{Command: name, Err: errors.New("executable file not found")}
}

func newManager(t *testing.T, output map[string]string) (*Manager, *fakeSystem, *config.Store) {
	t.Helper()
	store := config.Open(t.TempDir())
	system := &fakeSystem{output: output}
	m := NewManager(store, "1.2.3")
	m.runner = system
	m.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	m.hostname = func() string { return "keller" }
	m.sysRoot = t.TempDir()
	pci := filepath.Join(m.sysRoot, "bus/pci/devices/0000:00:1f.2")
	os.MkdirAll(pci, 0o755)
	os.WriteFile(filepath.Join(pci, "class"), []byte("0x010601\n"), 0o644)
	os.WriteFile(filepath.Join(pci, "vendor"), []byte("0x8086\n"), 0o644)
	os.WriteFile(filepath.Join(pci, "device"), []byte("0x2922\n"), 0o644)
	os.Symlink("../../../bus/pci/drivers/ahci", filepath.Join(pci, "driver"))
	hwmon := filepath.Join(m.sysRoot, "class/hwmon/hwmon0")
	os.MkdirAll(hwmon, 0o755)
	os.WriteFile(filepath.Join(hwmon, "name"), []byte("coretemp\n"), 0o644)
	os.WriteFile(filepath.Join(hwmon, "temp1_input"), []byte("41000\n"), 0o644)
	return m, system, store
}

const journal = `{"__CURSOR":"s=abc;i=1","__REALTIME_TIMESTAMP":"1791900000000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"titand","MESSAGE":"titand started"}
{"__CURSOR":"s=abc;i=2","__REALTIME_TIMESTAMP":"1791900001000000","PRIORITY":"3","_SYSTEMD_UNIT":"smbd.service","MESSAGE":"Share media: disk full"}
{"__CURSOR":"s=abc;i=3","__REALTIME_TIMESTAMP":"1791900002000000","PRIORITY":"4","_COMM":"mdadm","MESSAGE":[72,105,255]}
-- No entries --
`

func TestLogs(t *testing.T) {
	m, system, _ := newManager(t, map[string]string{"journalctl": journal})
	logs, err := m.Logs(context.Background(), Query{Source: "shares", Priority: 4, Lines: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Entries) != 3 || logs.Cursor != "s=abc;i=3" {
		t.Fatalf("logs: %+v", logs)
	}
	first, second, third := logs.Entries[0], logs.Entries[1], logs.Entries[2]
	if first.Source != "titand" || first.Priority != 6 || first.Time.Unix() != 1791900000 || first.Message != "titand started" {
		t.Errorf("first: %+v", first)
	}
	if second.Source != "smbd" || second.Priority != 3 {
		t.Errorf("second: %+v", second)
	}
	// A message that is not text arrives as numbers.
	if third.Source != "mdadm" || third.Message != "Hi?" {
		t.Errorf("third: %+v", third)
	}
	call := system.calls[0]
	for _, want := range []string{"journalctl --no-pager -o json", "-p 4", "-u smbd", "-u nfs-server", "-n 50"} {
		if !strings.Contains(call, want) {
			t.Errorf("%q missing in %q", want, call)
		}
	}

	found, _ := m.Logs(context.Background(), Query{Priority: 7, Search: "DISK FULL"})
	if len(found.Entries) != 1 || found.Entries[0].Source != "smbd" {
		t.Errorf("search: %+v", found.Entries)
	}
	m.Logs(context.Background(), Query{Priority: 7, After: "s=abc;i=3"})
	if last := system.calls[len(system.calls)-1]; !strings.Contains(last, "--after-cursor=s=abc;i=3") || strings.Contains(last, " -n ") {
		t.Errorf("follow: %q", last)
	}
}

func TestLogsRefuseOddQueries(t *testing.T) {
	m, system, _ := newManager(t, map[string]string{"journalctl": ""})
	for name, q := range map[string]Query{
		"unknown source": {Source: "titand -D /etc", Priority: 7},
		"priority":       {Priority: 9},
		"cursor":         {Priority: 7, After: "x --file=/etc/shadow"},
		"long search":    {Priority: 7, Search: strings.Repeat("a", 300)},
	} {
		if _, err := m.Logs(context.Background(), q); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(system.calls) != 0 {
		t.Errorf("journalctl was called: %v", system.calls)
	}
}

func TestBundleHoldsNoSecrets(t *testing.T) {
	m, _, store := newManager(t, map[string]string{
		"journalctl": "2026-10-13T12:00:00+0200 keller titand[1]: login user=alex ip=192.168.1.20\n2026-10-13T12:00:01+0200 keller dockerd[2]: env DB_PASSWORD=hunter2 set\n",
		"ip":         "eth0 UP 52:54:00:12:34:56\neth0 UP 192.168.1.5/24 fe80::5054:ff:fe12:3456/64\n",
		"uname":      "Linux keller 6.12.111+deb13-amd64\n",
	})
	store.Write("users/alex/account.json", []byte(`{"name":"alex","passwordHash":"$argon2id$v=19$secret"}`))
	store.Write("apps/jellyfin/compose.yaml", []byte("services: {}\n"))
	store.Write("apps/jellyfin/.env", []byte("API_KEY=verysecret\n"))
	store.Write("notify/settings.json", []byte(`{"discord":{"webhook":"https://discord.example/api/webhooks/1/hook"}}`))
	store.Write("tls/key.pem", []byte("-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"))
	store.Write("network/network.json", []byte(`{"hostname":"keller","address":"192.168.1.5/24","gateway":"192.168.1.1"}`))
	store.Write("shares/shares.json", []byte(`[{"name":"media","access":{"alex":"rw"}}]`))
	store.Write("vms/win11.json", []byte(`{"name":"win11","memoryMiB":8192}`))

	var buf bytes.Buffer
	if err := m.Bundle(context.Background(), &buf, []string{"alex"}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	var all strings.Builder
	for _, file := range archive.File {
		r, _ := file.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		files[file.Name] = string(data)
		all.WriteString(file.Name + "\n" + string(data))
	}
	for _, secret := range []string{"hunter2", "verysecret", "argon2id", "webhooks/1/hook", "BEGIN PRIVATE KEY", "192.168.1.", "52:54:00", "fe80::", "alex", "keller"} {
		if strings.Contains(all.String(), secret) {
			t.Errorf("the bundle gives away %q", secret)
		}
	}
	for _, name := range []string{"README.txt", "system.txt", "journal.txt", "kernel.txt", "storage.txt", "network.txt", "docker.txt", "config/network/network.json", "config/shares/shares.json", "config/vms/win11.json", "config/apps.txt"} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is missing; have %v", name, len(files))
		}
	}
	for _, name := range []string{"config/users/alex/account.json", "config/apps/jellyfin/.env", "config/apps/jellyfin/compose.yaml", "config/notify/settings.json", "config/tls/key.pem"} {
		if _, ok := files[name]; ok {
			t.Errorf("%s must not be in a bundle", name)
		}
	}
	// The same address is the same stand-in in the log and in the configuration.
	if !strings.Contains(files["journal.txt"], "login user=<user-1> ip=<ip-") || !strings.Contains(files["config/network/network.json"], `"hostname":"<nas>"`) {
		t.Errorf("journal: %q\nnetwork: %q", files["journal.txt"], files["config/network/network.json"])
	}
	if !strings.Contains(files["config/apps.txt"], "jellyfin") || !strings.Contains(files["config/vms/win11.json"], "8192") {
		t.Errorf("apps: %q vm: %q", files["config/apps.txt"], files["config/vms/win11.json"])
	}
	// A program that is missing is noted, it does not stop the bundle.
	if !strings.Contains(files["storage.txt"], "$ lsblk") || !strings.Contains(files["storage.txt"], "not found") {
		t.Errorf("storage.txt: %q", files["storage.txt"])
	}
	if hw := files["hardware.txt"]; !strings.Contains(hw, "0000:00:1f.2  class 010601  8086:2922  driver ahci") || !strings.Contains(hw, "coretemp temp1 : 41 °C") {
		t.Errorf("hardware.txt: %q", hw)
	}
	if !strings.Contains(files["README.txt"], "Version: 1.2.3") {
		t.Errorf("README: %q", files["README.txt"])
	}
}
