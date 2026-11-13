package nvr

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"
)

// Keeping an event ("Behalten", PLAN 88). Frigate's "retain indefinitely"
// (POST/DELETE /api/events/{id}/retain, role admin, frigate/api/event.py)
// keeps the event and its snapshot from Frigate's cleanup
// (frigate/events/cleanup.py leaves retain_indefinitely out), but not its
// video: Frigate deletes recording segments by the days of the review
// items alone (frigate/record/cleanup.py). So titand also saves the
// event's video – its span with the seconds before and after – as an
// export, noted in exports.json with the event's ID. Exports are never
// deleted by Frigate's retention nor by the storage cap; the export is
// seen by every account that sees the event, changed only by
// administrators. Only administrators keep (like deleting events).
//
// Un-keeping gives the event back to the camera's days and deletes the
// saved video; deleting a kept event deletes the video with it. Both ask
// first in the pages (the video may be the only copy left).

var (
	// ErrEventRunning: the event is still going on (or its last seconds
	// are not recorded yet); it can be kept once it is over.
	ErrEventRunning = errors.New("nvr: the event is still going on")
	// ErrEventKept: the event is kept; deleting it needs a confirmation.
	ErrEventKept = errors.New("nvr: the event is kept; confirm deleting it")
)

// keepSettle is how long after an event's end (and its seconds after)
// its last segment is written: Frigate writes segments of about 10 s from
// its cache (/tmp/cache) to the volume.
const keepSettle = 20 * time.Second

// keptExports maps kept events to the exports holding their video (from
// exports.json; none while it cannot be read).
func (m *Manager) keptExports() map[string]string {
	records, err := m.exportRecords()
	out := map[string]string{}
	if err != nil {
		return out
	}
	for id, record := range records {
		if record.Event != "" && !record.Failed {
			out[record.Event] = id
		}
	}
	return out
}

// eventCapture is the seconds a camera keeps before and after an event.
func (m *Manager) eventCapture(camera string) (pre, post int) {
	if c, ok := m.Camera(camera); ok {
		return c.Record.PreSeconds, c.Record.PostSeconds
	}
	return 5, 5
}

// rawEvent asks Frigate for one event.
func (m *Manager) rawEvent(ctx context.Context, base, id string) (frigateEvent, error) {
	var raw frigateEvent
	if !ValidEventID(id) {
		return raw, ErrEventNotFound
	}
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/events/"+url.PathEscape(id), nil, nil, &raw); err != nil {
		if frigateStatus(err) == http.StatusNotFound {
			return raw, ErrEventNotFound
		}
		return raw, err
	}
	if raw.ID != id {
		return raw, ErrEventNotFound
	}
	return raw, nil
}

// retain sets or clears Frigate's "retain indefinitely" of an event.
func (m *Manager) retain(ctx context.Context, base, id string, on bool) error {
	method := http.MethodPost
	if !on {
		method = http.MethodDelete
	}
	err := m.frigate.call(ctx, method, base, "/api/events/"+url.PathEscape(id)+"/retain", nil, nil, nil)
	if frigateStatus(err) == http.StatusNotFound {
		return ErrEventNotFound
	}
	return err
}

// KeepEvent keeps an event for good: Frigate keeps the event and its
// snapshot, titand saves its video as an export. by is the administrator
// (noted with the export). An event that is still going on is refused
// (ErrEventRunning); one without a recording of its time (the camera did
// not record) is kept without a video (KeptExport "").
func (m *Manager) KeepEvent(ctx context.Context, id, by string) (Event, error) {
	base, err := m.base()
	if err != nil {
		return Event{}, err
	}
	raw, err := m.rawEvent(ctx, base, id)
	if err != nil {
		return Event{}, err
	}
	pre, post := m.eventCapture(raw.Camera)
	now := m.now()
	if raw.End == nil || now.Before(time.Unix(0, int64(*raw.End*1e9)).Add(time.Duration(post)*time.Second+keepSettle)) {
		return Event{}, ErrEventRunning
	}
	if _, saved := m.keptExports()[id]; saved && raw.Retain {
		return m.Event(ctx, id, "")
	}
	// The video's record must be written: not over a file that could not
	// be read.
	if err := m.exportsWritable(); err != nil {
		return Event{}, err
	}
	if err := m.retain(ctx, base, id, true); err != nil {
		return Event{}, err
	}
	if _, saved := m.keptExports()[id]; !saved {
		from := math.Floor(raw.Start) - float64(pre)
		to := math.Ceil(*raw.End) + float64(post)
		if to-from > MaxExportSeconds {
			// Longer than an export may be: the end of it (where the
			// event ended) is cut off, never its beginning.
			to = from + MaxExportSeconds
		}
		_, err := m.createExport(ctx, exportRecord{Event: id, KeptBy: by}, raw.Camera, from, to, "")
		switch {
		case errors.Is(err, ErrExportFailed):
			// Nothing recorded in that time: the event and its snapshot
			// are kept, there is no video to save.
			slog.Info("NVR: kept event has no recording to save", "camera", raw.Camera)
		case err != nil:
			// Not half kept: Frigate's flag goes again.
			if undo := m.retain(context.WithoutCancel(ctx), base, id, false); undo != nil {
				slog.Warn("NVR: kept flag of an event not taken back", "camera", raw.Camera, "err", undo)
			}
			return Event{}, err
		}
	}
	return m.Event(ctx, id, "")
}

// UnkeepEvent gives a kept event back to the camera's days and deletes
// its saved video. A video still being made is waited for
// (ErrExportRunning): the event stays kept.
func (m *Manager) UnkeepEvent(ctx context.Context, id string) (Event, error) {
	base, err := m.base()
	if err != nil {
		return Event{}, err
	}
	if _, err := m.rawEvent(ctx, base, id); err != nil {
		return Event{}, err
	}
	export, saved := m.keptExports()[id]
	if saved {
		if e, err := m.Export(ctx, export, "", true); err == nil && e.State == ExportRunning {
			return Event{}, ErrExportRunning
		}
	}
	if err := m.retain(ctx, base, id, false); err != nil {
		return Event{}, err
	}
	if saved {
		if err := m.DeleteExport(ctx, export, "", true); err != nil && !errors.Is(err, ErrExportNotFound) {
			// The video stays: then the event stays kept too.
			if undo := m.retain(context.WithoutCancel(ctx), base, id, true); undo != nil {
				slog.Warn("NVR: kept flag of an event not set again", "err", undo)
			}
			return Event{}, err
		}
	}
	return m.Event(ctx, id, "")
}

// forgetKeptVideo deletes the saved video of an event that is gone.
func (m *Manager) forgetKeptVideo(ctx context.Context, id string) {
	export, saved := m.keptExports()[id]
	if !saved {
		return
	}
	if err := m.DeleteExport(ctx, export, "", true); err != nil && !errors.Is(err, ErrExportNotFound) {
		slog.Warn("NVR: saved video of a deleted event not deleted; it stays under the exports", "err", err)
	}
}
