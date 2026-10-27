package sshd

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// SSH logins for the activity log. sshd writes at its default log level
//
//	Accepted publickey for alex from 192.0.2.5 port 50122 ssh2: ED25519 SHA256:…
//
// to the journal; titand follows the journal and records who came in with
// which key. The comment of the key comes from the account, not from sshd.

// Login is one SSH login (or one turned away by the network list).
type Login struct {
	At   time.Time
	User string
	IP   string
	// KeyType and Fingerprint as sshd logs them: ED25519, SHA256:….
	KeyType     string
	Fingerprint string
	// Comment of the matching key of the account, if any.
	Comment string
	// Blocked means the address is outside the allowed networks.
	Blocked bool
}

// Target is how the login appears in the activity log.
func (l Login) Target() string {
	if l.Blocked {
		return ""
	}
	target := l.KeyType + " " + l.Fingerprint
	if l.Comment != "" {
		target += " (" + l.Comment + ")"
	}
	return target
}

var (
	acceptedLine = regexp.MustCompile(`^Accepted publickey for (\S+) from (\S+) port (\d+) ssh2: (\S+) (SHA256:[A-Za-z0-9+/=]+)`)
	blockedLine  = regexp.MustCompile(`^User (\S+) from (\S+) not allowed because not listed in AllowUsers`)
)

// parseLogin reads one message of sshd.
func parseLogin(message string) (Login, bool) {
	if m := acceptedLine.FindStringSubmatch(message); m != nil {
		return Login{User: m[1], IP: m[2], KeyType: m[4], Fingerprint: m[5]}, true
	}
	if m := blockedLine.FindStringSubmatch(message); m != nil {
		return Login{User: m[1], IP: m[2], Blocked: true}, true
	}
	return Login{}, false
}

// journalEntry is the part of `journalctl -o json` that matters here.
// MESSAGE is a string, or an array of bytes if it is not valid UTF-8.
type journalEntry struct {
	Message  json.RawMessage `json:"MESSAGE"`
	Realtime string          `json:"__REALTIME_TIMESTAMP"`
}

func (e journalEntry) text() string {
	var s string
	if json.Unmarshal(e.Message, &s) == nil {
		return s
	}
	var raw []byte
	var ints []int
	if json.Unmarshal(e.Message, &ints) == nil {
		for _, b := range ints {
			raw = append(raw, byte(b))
		}
	}
	return string(raw)
}

// readLogins reads journal lines and hands every login to record. Logins
// of names that are no account with SSH access are left out: probes of
// system users would only fill the log.
func readLogins(r io.Reader, users func() []auth.User, record func(Login)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry journalEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		login, ok := parseLogin(entry.text())
		if !ok {
			continue
		}
		if usec, err := strconv.ParseInt(entry.Realtime, 10, 64); err == nil {
			login.At = time.UnixMicro(usec)
		}
		var account *auth.User
		for _, user := range eligible(users()) {
			if user.Name == login.User {
				account = &user
				break
			}
		}
		if account == nil && login.Blocked {
			continue
		}
		if account != nil && !login.Blocked {
			for _, key := range account.DescribeSSHKeys() {
				if key.Fingerprint == login.Fingerprint {
					login.Comment = key.Comment
					break
				}
			}
		}
		record(login)
	}
	return scanner.Err()
}

// FollowLogins follows the journal of sshd until ctx is done and records
// every login. Logins while titand is not running are not caught up on.
func (m *Manager) FollowLogins(ctx context.Context, users func() []auth.User, record func(Login)) {
	if m.Disabled {
		return
	}
	for ctx.Err() == nil {
		// sshd-session (OpenSSH 9.8 and later) logs the logins, older
		// sshd under its own name.
		cmd := exec.CommandContext(ctx, "journalctl", "--follow", "--lines=0", "--output=json",
			"SYSLOG_IDENTIFIER=sshd-session", "SYSLOG_IDENTIFIER=sshd")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			err = readLogins(out, users, record)
			cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("following SSH logins in the journal stopped; trying again", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}
