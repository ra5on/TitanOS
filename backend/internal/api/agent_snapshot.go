package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Snapshot before changes (PLAN 51): before the agent updates or removes
// an app, or moves, renames or replaces files (deleted files go to the
// recycle bin of the share; a snapshot would keep their space for a week
// when the agent frees space), titand takes a read-only
// Btrfs snapshot of the volume concerned, at most one per volume in ten
// minutes. Single files come back from it on the storage page (restore
// makes a writable copy; the volume itself is never rolled back). Agent
// snapshots are kept seven days. Volumes without Btrfs get none; the agent
// says so in the chat. Switched off with noSnapshots in agent.json.

const (
	agentSnapPrefix = "agent-"
	agentSnapEvery  = 10 * time.Minute
	agentSnapKeep   = 7 * 24 * time.Hour
)

type agentSnapState struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// agentSnapTarget names the volume a change touches, or "".
func (b *agentToolbox) agentSnapTarget(tool string, args json.RawMessage) string {
	var a map[string]any
	_ = json.Unmarshal(args, &a)
	str := func(k string) string { v, _ := a[k].(string); return v }
	switch tool {
	case "app_action":
		switch str("action") {
		case "update", "remove", "remove_with_data":
			if b.s.opts.Docker != nil {
				return b.s.opts.Docker.Current().Volume
			}
		}
	case "files_move", "file_rename", "files_rename":
		return b.rootVolume(str("root"))
	case "file_write":
		if replace, _ := a["replace"].(bool); replace {
			return b.rootVolume(str("root"))
		}
	}
	return ""
}

func (b *agentToolbox) rootVolume(id string) string {
	for _, r := range b.s.filesArea().fileRoots(b.user) {
		if r.ID == id {
			return r.Volume
		}
	}
	return ""
}

// agentSnapshotBefore takes the snapshot and returns the note for the
// chat ("" when none was needed or possible).
func (b *agentToolbox) agentSnapshotBefore(ctx context.Context, tool string, args json.RawMessage) string {
	if b.settings.NoSnapshots || b.s.opts.Storage == nil {
		return ""
	}
	volume := b.agentSnapTarget(tool, args)
	if volume == "" {
		return ""
	}
	if b.s.opts.Storage.FilesystemOf(volume) != "btrfs" {
		return fmt.Sprintf("Kein Snapshot vor der Änderung: das Volume „%s“ hat kein Btrfs.", volume)
	}
	st := &b.s.agentSnaps
	st.mu.Lock()
	if st.last == nil {
		st.last = map[string]time.Time{}
	}
	if time.Since(st.last[volume]) < agentSnapEvery {
		st.mu.Unlock()
		return ""
	}
	st.last[volume] = time.Now()
	st.mu.Unlock()
	name := agentSnapPrefix + time.Now().In(b.s.agentLocation()).Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := b.s.opts.Storage.SnapshotAction(ctx, volume, name, "create", ""); err != nil {
		slog.Warn("snapshot before an agent change failed", "volume", volume, "err", err)
		return fmt.Sprintf("Snapshot vor der Änderung nicht möglich (%v).", err)
	}
	slog.Info("snapshot before an agent change", "volume", volume, "snapshot", name, "account", b.user.Name)
	return fmt.Sprintf("📸 Snapshot „%s“ des Volumes „%s“ vor der Änderung angelegt (7 Tage aufbewahrt; einzelne Dateien auf der Speicher-Seite unter Snapshots zurückholen).", name, volume)
}

// agentPruneSnapshots removes agent snapshots older than seven days; it
// runs with the nightly work of the agent.
func (s *Server) agentPruneSnapshots(now time.Time) {
	if s.opts.Storage == nil {
		return
	}
	for _, volume := range s.opts.Storage.Mounted() {
		if s.opts.Storage.FilesystemOf(volume) != "btrfs" {
			continue
		}
		list, err := s.opts.Storage.Snapshots(volume)
		if err != nil {
			continue
		}
		for _, snap := range list {
			stamp, ok := strings.CutPrefix(snap.Name, agentSnapPrefix)
			if !ok {
				continue
			}
			at, err := time.ParseInLocation("20060102-150405", stamp, s.agentLocation())
			if err != nil || now.Sub(at) < agentSnapKeep {
				continue
			}
			if err := s.opts.Storage.SnapshotAction(context.Background(), volume, snap.Name, "delete", snap.Name); err != nil {
				slog.Warn("old agent snapshot not removed", "volume", volume, "snapshot", snap.Name, "err", err)
			}
		}
	}
}
