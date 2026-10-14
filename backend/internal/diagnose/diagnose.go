// Package diagnose shows the logs of the NAS and packs a bundle for bug
// reports: logs and the state of the system, with addresses, names and
// everything that looks like a secret taken out.
package diagnose

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

// ErrInvalid means a query for logs is not acceptable.
var ErrInvalid = errors.New("diagnose: invalid query")

// Sources are the parts of the system whose logs can be picked, with the
// arguments that make journalctl show them. "" is everything.
var Sources = map[string][]string{
	"":        nil,
	"titand":  {"-u", "titand"},
	"kernel":  {"-k"},
	"storage": {"-t", "mdadm", "-t", "smartd", "-t", "smartctl", "-u", "mdmonitor", "-u", "smartmontools"},
	"shares":  {"-u", "smbd", "-u", "nmbd", "-u", "wsdd2", "-u", "nfs-server", "-u", "nfs-mountd", "-u", "rpcbind", "-u", "avahi-daemon"},
	"docker":  {"-u", "docker", "-u", "containerd"},
	"vms":     {"-u", "libvirtd", "-u", "virtlogd", "-u", "libvirt-guests"},
	"network": {"-u", "systemd-networkd", "-u", "systemd-resolved", "-u", "systemd-timesyncd"},
}

// Entry is one line of the log.
type Entry struct {
	Time time.Time `json:"time"`
	// Priority as in syslog: 0 (emergency) to 7 (debug); 3 is an error, 4 a warning.
	Priority int    `json:"priority"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// Query picks lines of the log.
type Query struct {
	Source string
	// Priority is the least severe level to show (3 = errors only, 7 = all).
	Priority int
	Lines    int
	// After is the cursor a former answer ended at; only newer lines come back.
	After  string
	Search string
}

// Logs is the answer to a query.
type Logs struct {
	Entries []Entry `json:"entries"`
	// Cursor marks the end; pass it as After to get what came since.
	Cursor string `json:"cursor"`
}

// Manager reads logs and packs bundles.
type Manager struct {
	store   *config.Store
	runner  run.Runner
	version string
	now     func() time.Time
	// hostname returns the name of the NAS, which a bundle hides.
	hostname func() string
	// sysRoot is where the kernel shows the hardware.
	sysRoot string
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store, version string) *Manager {
	return &Manager{store: store, runner: run.Exec{}, version: version, now: time.Now, sysRoot: "/sys", hostname: func() string {
		name, _ := os.Hostname()
		return name
	}}
}

var cursorForm = regexp.MustCompile(`^[A-Za-z0-9=;]{1,300}$`)

// Logs returns the newest lines of the journal that match the query, the
// oldest first.
func (m *Manager) Logs(ctx context.Context, q Query) (Logs, error) {
	source, known := Sources[q.Source]
	if !known || q.Priority < 0 || q.Priority > 7 || q.After != "" && !cursorForm.MatchString(q.After) || len(q.Search) > 200 {
		return Logs{}, ErrInvalid
	}
	if q.Lines < 1 || q.Lines > 2000 {
		q.Lines = 300
	}
	args := []string{"--no-pager", "-o", "json", "--output-fields=MESSAGE,PRIORITY,SYSLOG_IDENTIFIER,_SYSTEMD_UNIT,_COMM", "-p", strconv.Itoa(q.Priority)}
	args = append(args, source...)
	// The search runs here, so more lines than wanted are read first.
	fetch := q.Lines
	if q.Search != "" {
		fetch = 5000
	}
	if q.After != "" {
		args = append(args, "--after-cursor="+q.After)
	} else {
		args = append(args, "-n", strconv.Itoa(fetch))
	}
	output, err := m.runner.Run(ctx, "", "journalctl", args...)
	if err != nil {
		return Logs{}, err
	}
	logs := Logs{Entries: []Entry{}, Cursor: q.After}
	needle := strings.ToLower(q.Search)
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var raw struct {
			Message    any    `json:"MESSAGE"`
			Priority   string `json:"PRIORITY"`
			Identifier string `json:"SYSLOG_IDENTIFIER"`
			Unit       string `json:"_SYSTEMD_UNIT"`
			Comm       string `json:"_COMM"`
			Time       string `json:"__REALTIME_TIMESTAMP"`
			Cursor     string `json:"__CURSOR"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		if raw.Cursor != "" {
			logs.Cursor = raw.Cursor
		}
		entry := Entry{Priority: 6, Source: raw.Identifier, Message: messageText(raw.Message)}
		if priority, err := strconv.Atoi(raw.Priority); err == nil {
			entry.Priority = priority
		}
		if entry.Source == "" {
			entry.Source = strings.TrimSuffix(raw.Unit, ".service")
		}
		if entry.Source == "" {
			entry.Source = raw.Comm
		}
		if micros, err := strconv.ParseInt(raw.Time, 10, 64); err == nil {
			entry.Time = time.UnixMicro(micros)
		}
		if needle != "" && !strings.Contains(strings.ToLower(entry.Message), needle) && !strings.Contains(strings.ToLower(entry.Source), needle) {
			continue
		}
		logs.Entries = append(logs.Entries, entry)
	}
	if len(logs.Entries) > q.Lines {
		logs.Entries = logs.Entries[len(logs.Entries)-q.Lines:]
	}
	return logs, nil
}

// messageText reads a message as journalctl prints it: text, or a list of
// bytes when it is not valid UTF-8.
func messageText(message any) string {
	switch value := message.(type) {
	case string:
		return value
	case []any:
		bytes := make([]byte, 0, len(value))
		for _, item := range value {
			if number, ok := item.(float64); ok {
				bytes = append(bytes, byte(number))
			}
		}
		return strings.ToValidUTF8(string(bytes), "?")
	}
	return ""
}

// section is one file of the bundle: the output of some commands.
type section struct {
	file     string
	commands [][]string
}

var sections = []section{
	{"system.txt", [][]string{
		{"uname", "-a"}, {"cat", "/etc/os-release"}, {"uptime"}, {"cat", "/proc/cmdline"}, {"lscpu"}, {"free", "-m"},
		{"df", "-hT", "-x", "squashfs", "-x", "overlay"}, {"systemctl", "--failed", "--no-pager", "--plain"},
		{"systemctl", "list-units", "--type=service", "--state=running", "--no-pager", "--plain", "--no-legend"},
		{"ls", "-l", "/run/extensions"},
	}},
	{"journal.txt", [][]string{{"journalctl", "--no-pager", "-b", "-n", "6000", "-o", "short-iso"}}},
	{"kernel.txt", [][]string{{"journalctl", "--no-pager", "-k", "-b", "-n", "4000", "-o", "short-monotonic"}}},
	{"storage.txt", [][]string{
		{"lsblk", "-o", "NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT,MODEL,TRAN,ROTA"}, {"cat", "/proc/mdstat"},
		{"mdadm", "--detail", "--scan", "--verbose"}, {"findmnt", "-t", "ext4,xfs,vfat,btrfs,nfs4,virtiofs"},
	}},
	{"network.txt", [][]string{
		{"ip", "-br", "link"}, {"ip", "-br", "addr"}, {"ip", "route"}, {"networkctl", "status", "--no-pager", "--lines=0"},
		{"resolvectl", "status", "--no-pager"}, {"ss", "-tulnH"},
	}},
	{"docker.txt", [][]string{
		{"docker", "version", "--format", "{{.Server.Version}}"},
		{"docker", "ps", "-a", "--format", "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"},
	}},
	{"vms.txt", [][]string{{"ls", "/run/libvirt/qemu"}}},
}

// configFiles are the parts of /config that go into a bundle. Accounts,
// apps (their compose files hold what the user typed in), certificates and
// the notification settings (the addresses themselves are secrets) stay out.
var configFiles = []string{
	"storage/volumes.json", "storage/disk-sleep.json", "shares/shares.json", "network/network.json", "docker/settings.json", "docker/sources.json",
	"system/settings.json", "system/terminal.json", "system/backup.json", "plugins/settings.json",
}

const readme = `TitanOS diagnostic bundle
=========================

Made by "Tools > Diagnostic bundle" for a bug report.

Taken out before packing: IP and hardware addresses, e-mail addresses, the
name of the NAS, the names of the accounts, and whatever looks like a
password, token or key. The same address has the same stand-in everywhere
(<ip-1>, <mac-2>, <user-1> ...), so the logs can still be followed.

Not in here at all: accounts and password hashes, the compose files and
secrets of apps, certificates, notification settings, and no file of yours.

No filter is perfect. Have a look through the files before you attach the
bundle to a public issue - names of shares, containers and VMs are kept.
`

// Bundle writes the bundle as a zip file to w. accounts are the names to hide.
func (m *Manager) Bundle(ctx context.Context, w io.Writer, accounts []string) error {
	redactor := NewRedactor(m.hostname(), accounts)
	archive := zip.NewWriter(w)
	now := m.now()
	add := func(name, content string) error {
		file, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return err
		}
		_, err = io.WriteString(file, content)
		return err
	}

	if err := add("README.txt", readme+fmt.Sprintf("\nVersion: %s\nPacked:  %s\n", m.version, now.UTC().Format(time.RFC3339))); err != nil {
		return err
	}
	for _, section := range sections {
		var b strings.Builder
		for _, command := range section.commands {
			fmt.Fprintf(&b, "$ %s\n", strings.Join(command, " "))
			commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			output, err := m.runner.Run(commandCtx, "", command[0], command[1:]...)
			cancel()
			b.WriteString(strings.TrimRight(output, "\n"))
			if err != nil {
				// What is not installed or not running is worth knowing too.
				var failed *run.CommandError
				if errors.As(err, &failed) {
					fmt.Fprintf(&b, "\n(%v)", failed.Err)
				} else {
					fmt.Fprintf(&b, "\n(%v)", err)
				}
			}
			b.WriteString("\n\n")
		}
		if err := add(section.file, redactor.Text(b.String())); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	// The image carries neither lspci nor lsusb; the kernel knows the same.
	if err := add("hardware.txt", redactor.Text(hardware(m.sysRoot))); err != nil {
		return err
	}

	files := append([]string(nil), configFiles...)
	if names, err := m.store.List("vms"); err == nil {
		for _, name := range names {
			if strings.HasSuffix(name, ".json") {
				files = append(files, "vms/"+name)
			}
		}
	}
	sort.Strings(files)
	for _, name := range files {
		data, err := m.store.Read(name)
		if err != nil {
			continue
		}
		if err := add("config/"+name, redactor.Text(string(data))); err != nil {
			return err
		}
	}
	// Which apps there are, not what is in them.
	if apps, err := m.store.List("apps"); err == nil {
		sort.Strings(apps)
		if err := add("config/apps.txt", redactor.Text(strings.Join(apps, "\n")+"\n")); err != nil {
			return err
		}
	}
	return archive.Close()
}
