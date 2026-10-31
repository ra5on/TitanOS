package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
)

// The built-in AI assistant was removed in 0.41.0 (MCP replaces it). What
// it left is deleted once: its settings on the stick, with the keys of the
// model providers, and .titanos/assistant on every volume (chats, tasks,
// knowledge, documents). Personal notifications held back for quiet hours
// lived there too; they move to .titanos/notify first.

const retiredAssistantSettings = "system/assistant.json"

var notifyAccountName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// RemoveRetiredAssistant looks for the leftovers at the start and, for
// volumes that are mounted later, every two minutes during the first hour.
func (s *Server) RemoveRetiredAssistant(ctx context.Context) {
	s.removeRetiredAssistant()
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	until := time.After(time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-until:
			return
		case <-ticker.C:
			s.removeRetiredAssistant()
		}
	}
}

func (s *Server) removeRetiredAssistant() {
	var removed []string
	if s.opts.Store != nil && s.opts.Users != nil {
		// The chat page kept its preferences per account.
		for _, user := range s.opts.Users.List() {
			name := preferenceFile(user.Name, "assistant")
			if _, err := s.opts.Store.Read(name); err == nil && s.opts.Store.Remove(name) == nil {
				removed = append(removed, "stick:"+user.Name)
			}
		}
	}
	if s.opts.Store != nil {
		if _, err := s.opts.Store.Read(retiredAssistantSettings); err == nil {
			if err := s.opts.Store.Remove(retiredAssistantSettings); err != nil {
				slog.Warn("old assistant settings not removed from the stick", "err", err)
			} else {
				removed = append(removed, "stick")
			}
		}
	}
	if s.opts.Storage != nil {
		for _, volume := range s.opts.Storage.Mounted() {
			base := filepath.Join(s.opts.Storage.MountPoint(volume), ".titanos")
			old := filepath.Join(base, "assistant")
			if _, err := os.Lstat(old); err != nil {
				continue
			}
			if err := moveHeldNotifications(old, filepath.Join(base, "notify")); err != nil {
				slog.Warn("held notifications not moved", "volume", volume, "err", err)
			}
			if err := os.RemoveAll(old); err != nil {
				slog.Warn("old assistant data not removed", "volume", volume, "err", err)
				continue
			}
			removed = append(removed, volume)
		}
	}
	if len(removed) == 0 {
		return
	}
	slog.Info("old assistant data removed", "from", removed)
	if s.opts.Activity != nil {
		for _, where := range removed {
			s.opts.Activity.Record(activity.Entry{User: "system", Via: activity.ViaSystem, Action: "retired_assistant_removed", Target: where, Status: 200, OK: true})
		}
	}
}

// moveHeldNotifications moves <user>/notifications of the old folder to
// the new one, unless the new one has them already.
func moveHeldNotifications(from, to string) error {
	users, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	var errs []error
	for _, user := range users {
		if !user.IsDir() || !notifyAccountName.MatchString(user.Name()) {
			continue
		}
		src := filepath.Join(from, user.Name(), "notifications")
		info, err := os.Lstat(src)
		if err != nil || !info.IsDir() {
			continue
		}
		dst := filepath.Join(to, user.Name(), "notifications")
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, os.Rename(src, dst))
	}
	return errors.Join(errs...)
}
