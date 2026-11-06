package protect

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Client is who wrote: the account and device of an SMB session, or the
// process for NFS and local programs.
type Client struct {
	User    string `json:"user,omitempty"`
	IP      string `json:"ip,omitempty"`
	Machine string `json:"machine,omitempty"`
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
}

// Label is a short name of the client for logs.
func (c Client) Label() string {
	switch {
	case c.Machine != "" && c.IP != "" && c.Machine != c.IP:
		return c.Machine + " (" + c.IP + ")"
	case c.IP != "":
		return c.IP
	case c.Process != "":
		return c.Process
	}
	return "?"
}

// SMBSessions maps the pid of each smbd child to its session.
func SMBSessions(ctx context.Context, r run.Runner) (map[int]Client, error) {
	out, err := r.Run(ctx, "", "smbstatus", "--json", "--processes")
	if err != nil {
		return nil, err
	}
	return ParseSMBStatus([]byte(out))
}

// ParseSMBStatus reads the JSON of smbstatus (Samba 4.16 and newer).
func ParseSMBStatus(data []byte) (map[int]Client, error) {
	var doc struct {
		Sessions map[string]struct {
			ServerID struct {
				PID json.RawMessage `json:"pid"`
			} `json:"server_id"`
			Username      string `json:"username"`
			RemoteMachine string `json:"remote_machine"`
			Hostname      string `json:"hostname"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := map[int]Client{}
	for _, s := range doc.Sessions {
		pid := rawInt(s.ServerID.PID)
		if pid <= 0 {
			continue
		}
		c := Client{User: s.Username, PID: pid, Process: "smbd"}
		// hostname is "ipv4:192.168.1.57:51234" or "ipv6:[fe80::1]:51234".
		if ip, ok := addressOf(s.Hostname); ok {
			c.IP = ip
		}
		if s.RemoteMachine != "" {
			c.Machine = s.RemoteMachine
		}
		if c.IP == "" {
			c.IP = c.Machine
		}
		out[pid] = c
	}
	return out, nil
}

func rawInt(raw json.RawMessage) int {
	s := strings.Trim(string(raw), `"`)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func addressOf(host string) (string, bool) {
	for _, prefix := range []string{"ipv4:", "ipv6:"} {
		if rest, ok := strings.CutPrefix(host, prefix); ok {
			if i := strings.LastIndex(rest, ":"); i > 0 {
				rest = rest[:i]
			}
			return strings.Trim(rest, "[]"), true
		}
	}
	return "", false
}

// Disconnect ends the SMB connections of a client and closes the share for
// open handles, so a session that is open keeps no write access.
func Disconnect(ctx context.Context, r run.Runner, share, ip string) error {
	var first error
	if share != "" {
		if _, err := r.Run(ctx, "", "smbcontrol", "smbd", "close-share", share); err != nil {
			first = err
		}
	}
	if ip != "" && validIP(ip) {
		if _, err := r.Run(ctx, "", "smbcontrol", "smbd", "kill-client-ip", ip); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func validIP(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r == '.' || r == ':') {
			return false
		}
	}
	return s != ""
}
