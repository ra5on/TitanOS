package firewall

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/netip"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// titand follows the journal (in RAM) for two things: failed logins of
// sshd and Samba for the automatic blocking, and the kernel lines of
// dropped packets for "recently blocked". Logins of the web UI are
// reported by titand's own login handler (Failed).

// Drop is a packet the rules dropped, as the kernel logged it.
type Drop struct {
	At       time.Time `json:"at"`
	Source   string    `json:"source"`
	Zone     string    `json:"zone"`
	Protocol string    `json:"protocol"`
	Port     int       `json:"port,omitempty"`
	// App is set for published ports of Docker apps; Port is then the
	// port inside the container (the kernel logs it after Docker's DNAT).
	App       bool   `json:"app,omitempty"`
	Interface string `json:"interface,omitempty"`
}

const maxRecent = 200

var (
	// OpenSSH logs failures of unknown accounts, too many attempts and
	// keys it refused at its default level INFO.
	sshFailure = []*regexp.Regexp{
		regexp.MustCompile(`^Invalid user .* from (\S+) port \d+`),
		regexp.MustCompile(`^Failed \S+ for (?:invalid user )?.* from (\S+) port \d+`),
		regexp.MustCompile(`^User \S+ from (\S+) not allowed because`),
		regexp.MustCompile(`^Connection closed by authenticating user \S+ (\S+) port \d+ \[preauth\]`),
		regexp.MustCompile(`^Disconnecting authenticating user \S+ (\S+) port \d+: Too many authentication failures`),
	}
	// Samba's auth_audit at level 2 (smb.conf: log level = 1 auth_audit:2):
	// Auth: [SMB2,(null)] user [WORKGROUP]\[alex] at [...] with [NTLMv2]
	// status [NT_STATUS_WRONG_PASSWORD] workstation [PC] remote host
	// [ipv4:192.168.1.5:50123] ...
	smbFailure = regexp.MustCompile(`^Auth: \[.*\] status \[(NT_STATUS_[A-Z_]+)\] .*remote host \[ipv[46]:([^\]]+):\d+\]`)
)

// smbFailures are the statuses of a wrong name or password. With guest
// access, unknown names become the guest and are not counted by Samba.
var smbFailures = map[string]bool{"NT_STATUS_WRONG_PASSWORD": true, "NT_STATUS_NO_SUCH_USER": true, "NT_STATUS_LOGON_FAILURE": true}

func parseSSHFailure(message string) (netip.Addr, bool) {
	for _, re := range sshFailure {
		if m := re.FindStringSubmatch(message); m != nil {
			addr, err := netip.ParseAddr(m[1])
			return addr, err == nil
		}
	}
	return netip.Addr{}, false
}

func parseSMBFailure(message string) (netip.Addr, bool) {
	m := smbFailure.FindStringSubmatch(message)
	if m == nil || !smbFailures[m[1]] {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.Trim(m[2], "[]"))
	return addr, err == nil
}

// parseDrop reads a kernel line of the rules' log statements:
//
//	titanos-fw home IN=br0 OUT= MAC=… SRC=192.0.2.1 DST=… PROTO=TCP SPT=5000 DPT=22 …
func parseDrop(message string) (Drop, bool) {
	rest, ok := strings.CutPrefix(message, logPrefix)
	if !ok {
		// The kernel may put its own prefix in front.
		i := strings.Index(message, logPrefix)
		if i < 0 {
			return Drop{}, false
		}
		rest = message[i+len(logPrefix):]
	}
	zone, fields, _ := strings.Cut(rest, " ")
	drop := Drop{Zone: zone}
	if app, ok := strings.CutPrefix(zone, "app-"); ok {
		drop.Zone, drop.App = app, true
	}
	if drop.Zone != "blocked" && drop.Zone != string(Home) && drop.Zone != string(VPN) && drop.Zone != string(Internet) {
		return Drop{}, false
	}
	for _, field := range strings.Fields(fields) {
		key, value, _ := strings.Cut(field, "=")
		switch key {
		case "IN":
			drop.Interface = value
		case "SRC":
			drop.Source = value
		case "PROTO":
			drop.Protocol = strings.ToLower(value)
		case "DPT":
			drop.Port, _ = strconv.Atoi(value)
		}
	}
	if _, err := netip.ParseAddr(drop.Source); err != nil {
		return Drop{}, false
	}
	return drop, true
}

type journalEntry struct {
	Message    json.RawMessage `json:"MESSAGE"`
	Realtime   string          `json:"__REALTIME_TIMESTAMP"`
	Identifier string          `json:"SYSLOG_IDENTIFIER"`
	Transport  string          `json:"_TRANSPORT"`
	Unit       string          `json:"_SYSTEMD_UNIT"`
}

func (e journalEntry) text() string {
	var s string
	if json.Unmarshal(e.Message, &s) == nil {
		return s
	}
	var ints []int
	var raw []byte
	if json.Unmarshal(e.Message, &ints) == nil {
		for _, b := range ints {
			raw = append(raw, byte(b))
		}
	}
	return string(raw)
}

// readJournal reads journal lines and hands them on.
func (m *Manager) readJournal(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry journalEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		at := time.Now()
		if usec, err := strconv.ParseInt(entry.Realtime, 10, 64); err == nil {
			at = time.UnixMicro(usec)
		}
		message := entry.text()
		switch {
		case entry.Transport == "kernel":
			if drop, ok := parseDrop(message); ok {
				drop.At = at
				m.recordDrop(drop)
			}
		case entry.Identifier == "sshd" || entry.Identifier == "sshd-session":
			if addr, ok := parseSSHFailure(message); ok {
				m.Failed(addr.String(), SourceSSH)
			}
		case entry.Unit == "smbd.service" || strings.HasPrefix(entry.Identifier, "smbd"):
			if addr, ok := parseSMBFailure(message); ok {
				m.Failed(addr.String(), SourceSMB)
			}
		}
	}
	return scanner.Err()
}

// FollowJournal follows the journal until ctx is done. What happened while
// titand was not running is not caught up on.
func (m *Manager) FollowJournal(ctx context.Context) {
	if m.Disabled {
		return
	}
	for ctx.Err() == nil {
		// Fields of different names are ANDed by journalctl; "+" ORs.
		cmd := exec.CommandContext(ctx, "journalctl", "--follow", "--lines=0", "--output=json",
			"SYSLOG_IDENTIFIER=sshd-session", "SYSLOG_IDENTIFIER=sshd", "+", "_SYSTEMD_UNIT=smbd.service", "+", "_TRANSPORT=kernel")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			err = m.readJournal(out)
			cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("firewall: following the journal stopped; trying again", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (m *Manager) recordDrop(drop Drop) {
	m.recentMu.Lock()
	defer m.recentMu.Unlock()
	m.recent = append(m.recent, drop)
	if len(m.recent) > maxRecent {
		m.recent = m.recent[len(m.recent)-maxRecent:]
	}
}
