package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// Times survive the IDs being reassigned when titand loads its bell after a restart.
type bellRead struct {
	Times []string `json:"times"`
}

func readBellTimes(raw json.RawMessage) (bellRead, error) {
	var state bellRead
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return state, err
		}
	}
	return state, nil
}

func (a notifyArea) handleRead(w http.ResponseWriter, r *http.Request) {
	var req bellRead
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Times) > 200 {
		writeError(w, 400, "invalid_request", "too many notifications")
		return
	}
	user := currentUser(r).Name
	err := a.prefs.update(user, "notification-read", func(raw json.RawMessage) (json.RawMessage, error) {
		old, err := readBellTimes(raw)
		if err != nil {
			return nil, err
		}
		next := bellRead{}
		for _, note := range a.notify.MessagesFor(user) {
			at := note.Time.Format(time.RFC3339Nano)
			if slices.Contains(old.Times, at) || slices.Contains(req.Times, at) {
				next.Times = append(next.Times, at)
			}
		}
		return json.Marshal(next)
	})
	if err != nil {
		writeError(w, 503, "config_unavailable", "could not save the read notifications")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Mirrors the bell filters in notify-groups.ts, including the always visible storage errors.
func bellGroup(kind string) string {
	for _, rule := range []struct{ prefix, group string }{
		{"volume_", "storage"}, {"snapshot_restore_", "storage"}, {"disk_", "disks"}, {"backup_", "backups"},
		{"config_backup_", "backups"}, {"update_", "updates"}, {"ups_", "ups"}, {"usb_", "usb"}, {"nvr_", "cameras"},
	} {
		if strings.HasPrefix(kind, rule.prefix) {
			return rule.group
		}
	}
	return "system"
}

func (a notifyArea) badge(user string) *int {
	raw, err := a.prefs.get(user, "notification-read")
	if err != nil {
		return nil
	}
	read, err := readBellTimes(raw)
	if err != nil {
		return nil
	}
	raw, err = a.prefs.get(user, "bell")
	if err != nil {
		return nil
	}
	var filter struct {
		MinSeverity string          `json:"minSeverity"`
		Groups      map[string]bool `json:"groups"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &filter) != nil {
		return nil
	}
	ranks := map[string]int{notify.Warning: 1, notify.Error: 2}
	count := 0
	for _, note := range a.notify.MessagesFor(user) {
		if slices.Contains(read.Times, note.Time.Format(time.RFC3339Nano)) {
			continue
		}
		group := bellGroup(note.Kind)
		show, known := filter.Groups[group]
		critical := note.Severity == notify.Error && (group == "storage" || group == "disks")
		if critical || ((!known || show) && ranks[note.Severity] >= ranks[filter.MinSeverity]) {
			count++
		}
	}
	return &count
}
