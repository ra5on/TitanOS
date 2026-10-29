package homeassistant

// Network storage of Home Assistant: an SMB share of this NAS that Home
// Assistant OS mounts for its backups, its media or as a share for add-ons.
// Home Assistant manages such mounts through its Supervisor; TitanOS asks
// for them the way Home Assistant's own web interface does (supervisor/api
// over the WebSocket API), which needs a token of an administrator.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Mount is network storage as the Supervisor knows it.
type Mount struct {
	Name   string `json:"name"`
	Usage  string `json:"usage"` // backup, media or share
	Type   string `json:"type"`  // cifs or nfs
	Server string `json:"server"`
	Share  string `json:"share,omitempty"`
	Path   string `json:"path,omitempty"`
	State  string `json:"state,omitempty"`
}

// MountRequest adds SMB storage to Home Assistant. The password goes to
// Home Assistant only and is not kept by TitanOS.
type MountRequest struct {
	Name     string
	Usage    string
	Server   string
	Share    string
	Username string
	Password string
}

// Mount names are what Home Assistant accepts: letters, digits, underscore.
var mountNameRE = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)

// MountName is the name TitanOS gives the storage of a share.
func MountName(share string) string {
	var b strings.Builder
	b.WriteString("titanos_")
	for _, r := range strings.ToLower(share) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

// ValidUsage says whether Home Assistant knows this use of storage.
func ValidUsage(usage string) bool {
	return slices.Contains([]string{"backup", "media", "share"}, usage)
}

// Mounts lists the network storage of Home Assistant.
func (m *Manager) Mounts(ctx context.Context) ([]Mount, error) {
	if !m.Active() {
		return nil, ErrOff
	}
	result, err := m.supervisor(ctx, "get", "/mounts", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Mounts []Mount `json:"mounts"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("%w: unexpected answer to /mounts", ErrUnreachable)
	}
	return list.Mounts, nil
}

// AddMount adds SMB storage to Home Assistant. The Supervisor mounts it
// right away and refuses it if it cannot; the error says why.
func (m *Manager) AddMount(ctx context.Context, req MountRequest) error {
	if !m.Active() {
		return ErrOff
	}
	if !mountNameRE.MatchString(req.Name) || !ValidUsage(req.Usage) || req.Server == "" || req.Share == "" || req.Username == "" || req.Password == "" {
		return invalid("network storage")
	}
	_, err := m.supervisor(ctx, "post", "/mounts", map[string]any{
		"name": req.Name, "usage": req.Usage, "type": "cifs",
		"server": req.Server, "share": req.Share,
		"username": req.Username, "password": req.Password,
	})
	return err
}

// Backup is one backup Home Assistant knows of.
type Backup struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Date   time.Time `json:"date"`
	Agents []string  `json:"agents"`
}

// Backups lists the backups of Home Assistant, newest first (backup/info,
// Home Assistant 2025.1 and later).
func (m *Manager) Backups(ctx context.Context) ([]Backup, error) {
	if !m.Active() {
		return nil, ErrOff
	}
	result, err := m.command(ctx, 60*time.Second, map[string]any{"type": "backup/info"})
	if err != nil {
		return nil, err
	}
	return parseBackups(result)
}

func parseBackups(result json.RawMessage) ([]Backup, error) {
	var info struct {
		Backups []struct {
			BackupID string    `json:"backup_id"`
			Name     string    `json:"name"`
			Date     time.Time `json:"date"`
			// 2025.2 and later: the locations as a map; 2025.1: a list.
			Agents   map[string]json.RawMessage `json:"agents"`
			AgentIDs []string                   `json:"agent_ids"`
		} `json:"backups"`
	}
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("%w: unexpected answer to backup/info", ErrUnreachable)
	}
	backups := make([]Backup, 0, len(info.Backups))
	for _, b := range info.Backups {
		agents := slices.Clone(b.AgentIDs)
		for id := range b.Agents {
			if !slices.Contains(agents, id) {
				agents = append(agents, id)
			}
		}
		sort.Strings(agents)
		backups = append(backups, Backup{ID: b.BackupID, Name: b.Name, Date: b.Date, Agents: agents})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Date.After(backups[j].Date) })
	return backups, nil
}

// MountAgent is how Home Assistant names network storage as a backup
// location.
func MountAgent(mount string) string { return "hassio." + mount }

// LatestOn returns the newest backup that lies on the given location.
func LatestOn(backups []Backup, agent string) (Backup, bool) {
	for _, b := range backups {
		if slices.Contains(b.Agents, agent) {
			return b, true
		}
	}
	return Backup{}, false
}
