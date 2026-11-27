// A Location pins one feature's writable state to one authoritative volume.
// A move records both endpoints before its first copy. Even when the desired
// volume changes twice or the daemon restarts, the first move finishes before
// writers (including background workers) receive a volume again.
package volumedata

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
)

type locationRecord struct {
	Current string `json:"current"`
	To      string `json:"to,omitempty"`
}

// Location belongs to exactly one feature of a Home. Its feature mutex
// protects the cached record; the configuration medium is read once at startup.
type Location struct {
	store   *config.Store
	file    string
	record  locationRecord
	loadErr error
}

func NewLocation(store *config.Store, file string) *Location {
	l := &Location{store: store, file: file}
	var r locationRecord
	err := store.ReadJSON(file, &r)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// First installation. Only claim a volume after validating its mount.
	case err != nil:
		l.loadErr = err
	case !naming.ValidVolume(r.Current) || r.To != "" && (!naming.ValidVolume(r.To) || r.To == r.Current):
		l.loadErr = fmt.Errorf("invalid data volume in %s", file)
	default:
		l.record = r
	}
	if l.loadErr != nil {
		slog.Error("recorded volume is unreadable; feature storage is unavailable", "file", file, "err", l.loadErr)
	}
	return l
}

func (l *Location) save(r locationRecord) error {
	if err := l.store.WriteJSON(l.file, r); err != nil {
		return err
	}
	l.record = r
	return nil
}

func (l *Location) volume(h *Home, feature, want string) string {
	state := h.feature(feature)
	state.mu.Lock()
	defer state.mu.Unlock()
	if l.loadErr != nil || h.context().Err() != nil || state.moving != nil {
		return ""
	}
	mounted := h.Mounted()
	target := l.record.To
	if target == "" {
		target = want
	}
	if !slices.Contains(mounted, target) || (l.record.Current != "" && !slices.Contains(mounted, l.record.Current)) {
		return ""
	}
	if l.record.Current == target && l.record.To == "" && state.cleaned[target] {
		return target
	}
	if until := state.retry[target]; h.clock().Before(until) {
		return ""
	}
	if l.record.To == "" && (l.record.Current == "" || l.record.Current == target) {
		// Pin the actual mounted root before claiming it; CleanLeftovers also
		// validates an existing feature directory without creating one.
		err := CleanLeftovers(h.MountRoot, target, feature, mounted)
		if err == nil && l.record.Current == "" {
			err = l.save(locationRecord{Current: target})
		}
		if err == nil {
			state.cleaned[target] = true
			return target
		}
		state.retry[target] = h.clock().Add(time.Minute)
		slog.Warn("feature volume unavailable", "feature", feature, "volume", target, "err", err)
		return ""
	}

	from := l.record.Current
	if l.record.To == "" {
		// If this write fails neither volume has been modified.
		if err := l.save(locationRecord{Current: from, To: target}); err != nil {
			state.retry[target] = h.clock().Add(time.Minute)
			slog.Warn("unable to record feature volume change", "feature", feature, "err", err)
			return ""
		}
	}
	state.moving = make(chan struct{})
	h.work.Go(func() {
		state.io.Lock()
		defer state.io.Unlock()
		limit := h.MoveLimit
		if limit <= 0 {
			limit = 15 * time.Minute
		}
		ctx, cancel := context.WithTimeout(h.context(), limit)
		defer cancel()
		mounted := h.Mounted()
		err := CleanLeftovers(h.MountRoot, target, feature, mounted)
		if err == nil {
			_, err = adoptEntries(ctx, h.MountRoot, from, target, feature, mounted, true)
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		if err == nil {
			err = l.save(locationRecord{Current: target})
		}
		if err != nil {
			delay := h.RetryAfter
			if delay <= 0 {
				delay = time.Minute
			}
			state.retry[target] = h.clock().Add(delay)
			slog.Warn("feature storage waits for volume move", "feature", feature, "from", from, "to", target, "err", err)
		} else {
			state.cleaned[target] = true
			delete(state.retry, target)
		}
		close(state.moving)
		state.moving = nil
	})
	return ""
}
