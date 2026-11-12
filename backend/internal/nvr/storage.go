package nvr

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// storageRuntime is what the watch knows of the space (guarded by
// Manager.mu).
type storageRuntime struct {
	at                  time.Time
	recordings, exports *uint64
	oldest              *float64 // start of the oldest recording
	cap                 string
	lastCleanup         *int64
}

// storageEvery is how often the space is looked at.
const storageEvery = 5 * time.Minute

// The cap: recordings are deleted until capTarget of the cap is used,
// in steps of capStep, at most capSteps per look.
const (
	capTarget = 0.95
	capStep   = time.Hour
	capSteps  = 72
	// capKeep is the youngest stretch the cap never deletes: what is
	// being recorded right now.
	capKeep = time.Hour
	// keptMargin is left around a kept event on both sides.
	keptMargin = 60.0
)

func (m *Manager) storageStatusLocked() StorageStatus {
	st := m.rt.storage
	out := StorageStatus{RecordingsBytes: st.recordings, ExportsBytes: st.exports, LimitGB: m.settings.Storage.LimitGB, Cap: st.cap, LastCleanup: st.lastCleanup}
	if st.recordings != nil {
		used := *st.recordings
		if st.exports != nil {
			used += *st.exports
		}
		out.UsedBytes = &used
	}
	if st.oldest != nil {
		days := max(0, float64(m.now().Unix())-*st.oldest) / 86400
		out.SpanDays = &days
	}
	if m.settings.Storage.LimitGB == 0 {
		out.Cap = CapOff
	} else if out.Cap == "" || out.Cap == CapOff {
		out.Cap = CapOK
	}
	return out
}

// exportsUsage is the size of all finished exports.
func (m *Manager) exportsUsage(ctx context.Context) (uint64, error) {
	list, err := m.Exports(ctx, "", true)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, e := range list {
		if e.SizeBytes != nil && *e.SizeBytes > 0 {
			total += uint64(*e.SizeBytes)
		}
	}
	return total, nil
}

// oldestRecording is the start of the oldest hour with recordings over
// all cameras, 0 if there is none.
func (m *Manager) oldestRecording(ctx context.Context) float64 {
	oldest := 0.0
	for _, c := range m.Cameras() {
		days, err := m.RecordingsSummary(ctx, c.ID)
		if err != nil {
			continue
		}
		for _, d := range days {
			for _, h := range d.Hours {
				if start := float64(h.Start); oldest == 0 || start < oldest {
					oldest = start
				}
			}
		}
	}
	return oldest
}

// watchStorage reads the space every storageEvery and enforces the cap
// (Settings.Storage.LimitGB): Frigate itself only frees space when the
// volume is nearly full.
//
// Above the cap the oldest recordings of all cameras are deleted hour by
// hour until capTarget of the cap is used. Never deleted: exports, the
// recordings of kept events (with a margin), and the last hour. If the
// exports alone are above the cap nothing is deleted and the status says
// so (CapExportsExceed). The owner is told at most once a day.
func (m *Manager) watchStorage(ctx context.Context, base string, now time.Time) {
	m.mu.Lock()
	due := now.Sub(m.rt.storage.at) >= storageEvery
	limitGB := m.settings.Storage.LimitGB
	m.mu.Unlock()
	if !due {
		return
	}
	// The look itself may take a while (one request per camera and per
	// new export); it has its own time.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	recordings, err := m.frigate.recordingsUsage(ctx, base)
	if err != nil {
		m.mu.Lock()
		m.rt.storage.at = now
		m.mu.Unlock()
		return
	}
	exports, exportsErr := m.exportsUsage(ctx)
	oldest := m.oldestRecording(ctx)
	state, cleaned := CapOff, false
	if limitGB > 0 && exportsErr == nil {
		limit := uint64(limitGB) << 30
		state, cleaned, recordings, oldest = m.enforceCap(ctx, base, now, limit, recordings, exports, oldest)
	}
	m.mu.Lock()
	st := &m.rt.storage
	st.at, st.recordings, st.cap = now, &recordings, state
	if exportsErr == nil {
		st.exports = &exports
	}
	st.oldest = nil
	if oldest > 0 {
		st.oldest = &oldest
	}
	tell := false
	if cleaned {
		at := now.Unix()
		st.lastCleanup = &at
		// Once a day: the day is kept on the volume, so a restart of
		// titand does not tell again.
		if day := now.In(m.zone()).Format("2006-01-02"); m.state.CapNoticeDay != day {
			m.state.CapNoticeDay, m.rt.stateDirty, tell = day, true, true
		}
	}
	m.mu.Unlock()
	if tell {
		m.notify("info", "nvr_storage_cap", fmt.Sprintf("%d GB", limitGB))
	}
}

// enforceCap deletes the oldest recordings while the space is above the
// cap. It returns the state of the cap, whether something was deleted,
// and the size and the oldest hour of the recordings afterwards.
func (m *Manager) enforceCap(ctx context.Context, base string, now time.Time, limit, recordings, exports uint64, oldest float64) (string, bool, uint64, float64) {
	if recordings+exports <= limit {
		return CapOK, false, recordings, oldest
	}
	if exports > limit {
		return CapExportsExceed, false, recordings, oldest
	}
	target := uint64(float64(limit) * capTarget)
	cleaned := false
	for step := 0; step < capSteps && recordings+exports > target; step++ {
		if oldest <= 0 || oldest+capStep.Seconds() > float64(now.Add(-capKeep).Unix()) {
			break // nothing old enough is left
		}
		from, to := oldest, oldest+capStep.Seconds()
		for _, span := range m.withoutKept(ctx, base, from, to) {
			path := fmt.Sprintf("/api/recordings/start/%s/end/%s", formatTime(span[0]), formatTime(span[1]))
			if err := m.frigate.call(ctx, http.MethodDelete, base, path, nil, nil, nil); err != nil {
				slog.Warn("NVR: oldest recordings not deleted", "err", err)
				return CapCleaning, cleaned, recordings, oldest
			}
			cleaned = true
		}
		after, err := m.frigate.recordingsUsage(ctx, base)
		if err != nil {
			break
		}
		recordings = after
		next := m.oldestRecording(ctx)
		if next > 0 && next <= oldest {
			// What is left of this hour is kept; go on behind it.
			next = to
		}
		oldest = next
	}
	if cleaned {
		slog.Info("NVR: storage cap reached, oldest recordings deleted", "limitGB", limit>>30, "usedGB", (recordings+exports)>>30)
	}
	if recordings+exports > limit {
		return CapCleaning, cleaned, recordings, oldest
	}
	return CapOK, cleaned, recordings, oldest
}

// withoutKept returns the parts of [from, to) that hold no kept event
// (Frigate: "retain indefinitely"), with a margin around each.
func (m *Manager) withoutKept(ctx context.Context, base string, from, to float64) [][2]float64 {
	var kept []frigateEvent
	// An event is found by its start: look far enough back for long ones.
	query := url.Values{"favorites": {"1"}, "after": {formatTime(from - 6*3600)}, "before": {formatTime(to + keptMargin)}, "limit": {"500"}, "include_thumbnails": {"0"}}
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/events", query, nil, &kept); err != nil {
		// Not knowing what is kept, nothing of this hour is deleted.
		return nil
	}
	var holes [][2]float64
	for _, e := range kept {
		end := e.Start
		if e.End != nil {
			end = *e.End
		} else {
			end = to // still going on
		}
		if lo, hi := e.Start-keptMargin, end+keptMargin; hi > from && lo < to {
			holes = append(holes, [2]float64{max(lo, from), min(hi, to)})
		}
	}
	slices.SortFunc(holes, func(a, b [2]float64) int {
		switch {
		case a[0] < b[0]:
			return -1
		case a[0] > b[0]:
			return 1
		}
		return 0
	})
	var spans [][2]float64
	at := from
	for _, hole := range holes {
		if hole[0] > at {
			spans = append(spans, [2]float64{at, hole[0]})
		}
		at = max(at, hole[1])
	}
	if at < to {
		spans = append(spans, [2]float64{at, to})
	}
	return spans
}
