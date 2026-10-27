package shares

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The ransomware guard. Shares with Guard on log every rename and delete
// over SMB through Samba's full_audit module into the journal; titand
// follows it. When one account renames many files of a share to an
// extension known from ransomware, or gives many files the same new
// extension within a minute, the share becomes read-only for everyone
// (GuardLocked) and its open connections are closed. An administrator lifts
// the lock after looking. NFS has no such log; a locked share is exported
// read-only there too.

// shadowFormat is the name Windows expects for a "Previous Version".
const shadowFormat = "@GMT-%Y.%m.%d-%H.%M.%S"

// GuardLock says why and since when a share is read-only.
type GuardLock struct {
	At     time.Time `json:"at"`
	User   string    `json:"user"`
	Reason string    `json:"reason"`
	// Files is how many suspicious renames were seen.
	Files int `json:"files"`
}

const (
	guardWindow = time.Minute
	// A handful of renames to a known ransomware extension is enough.
	guardKnown = 5
	// Many files of the same new extension in a minute, where the extension
	// was something else before.
	guardSameExtension = 40
)

// ransomExtensions are extensions known from ransomware families. Kept
// short on purpose: the second rule catches what is not listed.
var ransomExtensions = []string{
	".locked", ".lock", ".encrypted", ".enc", ".crypt", ".crypted", ".cry", ".crypto", ".ryk", ".conti",
	".lockbit", ".blackcat", ".akira", ".royal", ".play", ".phobos", ".dharma", ".wncry", ".wcry", ".wnry",
	".locky", ".zepto", ".cerber", ".petya", ".ezz", ".ecc", ".exx", ".xyz", ".zzz", ".aaa", ".micro",
	".vvv", ".ttt", ".abc", ".rrk", ".djvu", ".stop", ".medusa", ".hive", ".basta", ".clop",
}

// auditEvent is one line of full_audit: user|share|op|ok|args...
type auditEvent struct {
	at          time.Time
	user, share string
	op          string
	from, to    string
}

func parseAudit(message string) (auditEvent, bool) {
	fields := strings.Split(message, "|")
	if len(fields) < 5 || fields[3] != "ok" {
		return auditEvent{}, false
	}
	e := auditEvent{user: fields[0], share: fields[1], op: fields[2], from: fields[4]}
	if len(fields) > 5 {
		e.to = fields[5]
	}
	return e, e.op == "renameat" || e.op == "unlinkat"
}

// guardState counts per share and account what happened in the window.
type guardState struct {
	mu     sync.Mutex
	events map[string][]auditEvent // share|user
}

// observe adds an event and reports a reason when the share must be locked.
func (g *guardState) observe(e auditEvent) (reason string, files int) {
	if e.op != "renameat" || e.to == "" {
		return "", 0
	}
	oldExt, newExt := strings.ToLower(path.Ext(e.from)), strings.ToLower(path.Ext(e.to))
	if newExt == "" || newExt == oldExt {
		return "", 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.events == nil {
		g.events = map[string][]auditEvent{}
	}
	key := e.share + "|" + e.user
	recent := slices.DeleteFunc(g.events[key], func(old auditEvent) bool { return e.at.Sub(old.at) > guardWindow })
	recent = append(recent, e)
	g.events[key] = recent
	known, same := 0, 0
	for _, old := range recent {
		ext := strings.ToLower(path.Ext(old.to))
		if slices.Contains(ransomExtensions, ext) {
			known++
		}
		if ext == newExt {
			same++
		}
	}
	switch {
	case known >= guardKnown:
		return "known-extension", known
	case same >= guardSameExtension:
		return "same-extension", same
	}
	return "", 0
}

// FollowAudit follows the full_audit lines of Samba until ctx is done and
// locks shares that look attacked. onLock is told about each new lock.
func (m *Manager) FollowAudit(ctx context.Context, onLock func(Share)) {
	if m.Disabled {
		return
	}
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "journalctl", "--follow", "--lines=0", "--output=json", "SYSLOG_IDENTIFIER=smbd_audit")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			err = m.readAudit(ctx, out, onLock)
			cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("following the SMB audit log stopped; trying again", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (m *Manager) readAudit(ctx context.Context, r io.Reader, onLock func(Share)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			Message  json.RawMessage `json:"MESSAGE"`
			Realtime string          `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		var message string
		if json.Unmarshal(entry.Message, &message) != nil {
			continue
		}
		event, ok := parseAudit(message)
		if !ok {
			continue
		}
		event.at = time.Now()
		if usec, err := strconv.ParseInt(entry.Realtime, 10, 64); err == nil {
			event.at = time.UnixMicro(usec)
		}
		m.observeAudit(ctx, event, onLock)
	}
	return scanner.Err()
}

func (m *Manager) observeAudit(ctx context.Context, event auditEvent, onLock func(Share)) {
	reason, files := m.guard.observe(event)
	if reason == "" {
		return
	}
	share, locked := m.lockGuard(event.share, GuardLock{At: event.at.UTC(), User: event.user, Reason: reason, Files: files})
	if !locked {
		return
	}
	slog.Error("ransomware guard locked a share", "share", share.Name, "user", event.user, "reason", reason, "files", files)
	// Connections that are open keep their write access until they end.
	if _, err := m.runner.Run(ctx, "", "smbcontrol", "smbd", "close-share", share.Name); err != nil {
		slog.Warn("open connections to a locked share not closed", "share", share.Name, "err", err)
	}
	if onLock != nil {
		onLock(share)
	}
}

// lockGuard marks a guarded share read-only. It reports false when the
// share is unknown, unguarded or locked already.
func (m *Manager) lockGuard(name string, lock GuardLock) (Share, bool) {
	m.mu.Lock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 || !m.shares[i].Guard || m.shares[i].GuardLocked != nil {
		m.mu.Unlock()
		return Share{}, false
	}
	shares := slices.Clone(m.shares)
	shares[i].GuardLocked = &lock
	err := m.save(shares)
	share := shares[i]
	m.mu.Unlock()
	if err != nil {
		// Not saved on the stick: still locked until titand restarts.
		slog.Error("ransomware lock not saved", "share", name, "err", err)
		m.mu.Lock()
		if j := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name }); j >= 0 {
			m.shares[j].GuardLocked = &lock
		}
		m.mu.Unlock()
	}
	m.Kick()
	return share, true
}

// UnlockGuard makes a share locked by the guard writable again.
func (m *Manager) UnlockGuard(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	if m.shares[i].GuardLocked == nil {
		return nil
	}
	shares := slices.Clone(m.shares)
	shares[i].GuardLocked = nil
	if err := m.save(shares); err != nil {
		return err
	}
	go m.Kick()
	return nil
}

// shadowOf is the @GMT folder for a share, "" when it offers no previous versions.
func shadowOf(share Share, shadow func(Share) string) string {
	if !share.PreviousVersions || shadow == nil {
		return ""
	}
	return shadow(share)
}

// PreviousVersionVolumes lists the volumes with at least one share that
// offers previous versions: their snapshots are made visible to Samba.
func (m *Manager) PreviousVersionVolumes() []string {
	var volumes []string
	for _, share := range m.List() {
		if share.PreviousVersions && !slices.Contains(volumes, share.Volume) {
			volumes = append(volumes, share.Volume)
		}
	}
	return volumes
}
