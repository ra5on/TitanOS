package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ErrEventNotFound: no such event (or one of a camera the account may
// not see: the API answers both alike).
var ErrEventNotFound = errors.New("nvr: no such event")

// Event is one tracked object as the NVR's pages show it: a small shape
// of Frigate's event.
type Event struct {
	ID     string `json:"id"`
	Camera string `json:"camera"`
	// Label is what was detected (person, car, …).
	Label string `json:"label"`
	// SubLabel is the recognised face or the name of a known plate.
	SubLabel string `json:"subLabel"`
	// Plate is the license plate that was read.
	Plate string  `json:"plate"`
	Score float64 `json:"score"`
	// Start and End are Unix seconds; End is null while the event goes on.
	Start    float64  `json:"start"`
	End      *float64 `json:"end"`
	HasClip  bool     `json:"hasClip"`
	HasSnap  bool     `json:"hasSnapshot"`
	Zones    []string `json:"zones"`
	Reviewed bool     `json:"reviewed"`
	// Kept is Frigate's "retain indefinitely" (TitanOS cannot set it): the
	// storage cap leaves the event's recording out where it finds it, but
	// Frigate's retention in days deletes the recording all the same.
	Kept bool `json:"kept"`
	// stood: the object never moved since it was first seen (Frigate's
	// position_changes is 0; Frigate makes no review item of it,
	// review/maintainer.py). Unknown counts as moved.
	stood bool
}

// eventIDRE is the form of Frigate's event IDs ("1731412345.123456-abc123").
var eventIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidEventID tells whether id can be an event (or export, review) ID:
// nothing that could leave its place in a path.
func ValidEventID(id string) bool {
	return eventIDRE.MatchString(id) && !strings.Contains(id, "..")
}

// textOrFirst reads a value Frigate gives as a string or as [text, score].
func textOrFirst(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		if json.Unmarshal(list[0], &text) == nil {
			return text
		}
	}
	return ""
}

// frigateEvent is an event as /api/events and the "events" topic give it.
type frigateEvent struct {
	ID       string          `json:"id"`
	Camera   string          `json:"camera"`
	Label    string          `json:"label"`
	SubLabel json.RawMessage `json:"sub_label"`
	Start    float64         `json:"start_time"`
	End      *float64        `json:"end_time"`
	HasClip  bool            `json:"has_clip"`
	HasSnap  bool            `json:"has_snapshot"`
	Zones    []string        `json:"zones"`
	Entered  []string        `json:"entered_zones"`
	TopScore *float64        `json:"top_score"`
	Score    *float64        `json:"score"`
	Retain   bool            `json:"retain_indefinitely"`
	// PositionChanges counts how often the object moved; 0: it stood still
	// since it was first seen (a parked car after a restart of Frigate).
	PositionChanges *int            `json:"position_changes"`
	Plate           json.RawMessage `json:"recognized_license_plate"`
	Data            struct {
		Score    *float64        `json:"score"`
		TopScore *float64        `json:"top_score"`
		Plate    json.RawMessage `json:"recognized_license_plate"`
	} `json:"data"`
}

func (e frigateEvent) event() Event {
	out := Event{ID: e.ID, Camera: e.Camera, Label: clip(e.Label, 40), SubLabel: clip(textOrFirst(e.SubLabel), 100), Start: e.Start, End: e.End,
		HasClip: e.HasClip, HasSnap: e.HasSnap, Zones: e.Zones, Kept: e.Retain, stood: e.PositionChanges != nil && *e.PositionChanges == 0}
	if out.Zones == nil {
		out.Zones = e.Entered
	}
	if out.Zones == nil {
		out.Zones = []string{}
	}
	for _, score := range []*float64{e.TopScore, e.Data.TopScore, e.Score, e.Data.Score} {
		if score != nil {
			out.Score = *score
			break
		}
	}
	out.Plate = textOrFirst(e.Plate)
	if out.Plate == "" {
		out.Plate = textOrFirst(e.Data.Plate)
	}
	out.Plate = clip(out.Plate, 40)
	return out
}

// base is the address of Frigate's API while it runs.
func (m *Manager) base() (string, error) {
	m.mu.Lock()
	address, running := m.rt.address, m.rt.phase == PhaseRunning
	m.mu.Unlock()
	if !running || address == "" {
		return "", ErrNotRunning
	}
	return m.FrigateBase(address), nil
}

// EventFilter narrows a list of events.
type EventFilter struct {
	// Cameras are the cameras to list (the caller gives only cameras the
	// account may see); Labels narrows to labels, empty for all.
	Cameras, Labels []string
	// From and To bound the start time (0: open); Before is the cursor of
	// the previous page.
	From, To, Before float64
	// Limit is 1 to 200.
	Limit int
	// Reviewed filters by "seen": nil for both.
	Reviewed *bool
}

// Events lists events, newest first. next is the cursor for the page
// after this one, 0 at the end: the next page lists the events that began
// before it (Frigate's "before" is strict).
//
// Events that began at the very same moment (two objects of one frame)
// are never split by a page: the rest of such a group would fall between
// two pages (review PLAN 85-4). Frigate is asked for tieRoom more than a
// page, the page ends after a group that is complete in what was read,
// else before it.
func (m *Manager) Events(ctx context.Context, f EventFilter) (events []Event, next float64, err error) {
	events = []Event{}
	if len(f.Cameras) == 0 {
		return events, 0, nil
	}
	base, err := m.base()
	if err != nil {
		return nil, 0, err
	}
	limit := min(max(f.Limit, 1), 200)
	ask := limit + tieRoom
	before := f.To
	if f.Before > 0 && (before == 0 || f.Before < before) {
		before = f.Before
	}
	// A round after the first goes on from the start of the last event
	// read, that moment included (its group may go on); IDs read already
	// are skipped.
	inclusive, more := false, false
	read := map[string]bool{}
	// With a filter on "seen" pages are read until the list is full; a
	// few at most, the next page goes on from there.
	for range 5 {
		query := url.Values{"cameras": {strings.Join(f.Cameras, ",")}, "limit": {strconv.Itoa(ask)}, "include_thumbnails": {"0"}}
		if len(f.Labels) > 0 {
			query.Set("labels", strings.Join(f.Labels, ","))
		}
		if f.From > 0 {
			query.Set("after", formatTime(f.From))
		}
		if before > 0 {
			b := before
			if inclusive {
				b += 1e-6
			}
			query.Set("before", formatTime(b))
		}
		var page []frigateEvent
		if err := m.frigate.call(ctx, http.MethodGet, base, "/api/events", query, nil, &page); err != nil {
			return nil, 0, err
		}
		list := make([]Event, 0, len(page))
		for _, e := range page {
			// Frigate is asked for the cameras; this is the second fence.
			if slices.Contains(f.Cameras, e.Camera) && ValidEventID(e.ID) && !read[e.ID] {
				read[e.ID] = true
				list = append(list, e.event())
			}
		}
		m.markReviewed(ctx, base, f.Cameras, list)
		for _, e := range list {
			if f.Reviewed == nil || *f.Reviewed == e.Reviewed {
				events = append(events, e)
			}
		}
		more = len(page) >= ask
		if !more || len(list) == 0 {
			break
		}
		before, inclusive = page[len(page)-1].Start, true
		if len(events) > limit || f.Reviewed == nil {
			break
		}
	}
	total := len(events)
	cut := total
	if cut > limit {
		cut = limit
		// The group of the page's last event goes along whole.
		for cut < total && events[cut].Start == events[cut-1].Start {
			cut++
		}
	}
	if more && cut == total && cut > 0 {
		// Up to the end of what was read: its last group may go on in
		// Frigate. It goes to the next page whole (unless it is all there is).
		last := events[cut-1].Start
		for cut > 0 && events[cut-1].Start == last {
			cut--
		}
		if cut == 0 {
			cut = total
		}
	}
	events = events[:cut]
	switch {
	case cut > 0 && (cut < total || more):
		return events, events[cut-1].Start, nil
	case cut == 0 && more && before > 0:
		// Nothing of what was read passed the filter on "seen": the next
		// page goes on from there (that moment included).
		return events, before + 1e-6, nil
	}
	return events, 0, nil
}

// tieRoom is how many events beyond a page Frigate is asked for, so that
// a group of events of one moment is seen whole.
const tieRoom = 20

func formatTime(t float64) string { return strconv.FormatFloat(t, 'f', 6, 64) }

// frigateReview is a review segment: a stretch of activity of one camera
// with the events in it. "Seen" is kept per segment.
type frigateReview struct {
	ID       string  `json:"id"`
	Camera   string  `json:"camera"`
	Start    float64 `json:"start_time"`
	Reviewed bool    `json:"has_been_reviewed"`
	Data     struct {
		Detections []string `json:"detections"`
	} `json:"data"`
}

// markReviewed sets Reviewed on events from the review segments of their
// time. An event without a segment counts as not seen.
func (m *Manager) markReviewed(ctx context.Context, base string, cameras []string, events []Event) {
	if len(events) == 0 {
		return
	}
	first, last := events[0].Start, events[0].Start
	for _, e := range events {
		first, last = min(first, e.Start), max(last, e.Start)
	}
	// A segment starts before the events in it.
	query := url.Values{"cameras": {strings.Join(cameras, ",")}, "after": {formatTime(first - 3600)}, "before": {formatTime(last + 1)}, "limit": {"2000"}}
	var reviews []frigateReview
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/review", query, nil, &reviews); err != nil {
		return
	}
	seen := map[string]bool{}
	for _, r := range reviews {
		for _, id := range r.Data.Detections {
			seen[id] = r.Reviewed
		}
	}
	for i := range events {
		events[i].Reviewed = seen[events[i].ID]
	}
}

// Event returns one event.
func (m *Manager) Event(ctx context.Context, id string) (Event, error) {
	if !ValidEventID(id) {
		return Event{}, ErrEventNotFound
	}
	base, err := m.base()
	if err != nil {
		return Event{}, err
	}
	var raw frigateEvent
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/events/"+url.PathEscape(id), nil, nil, &raw); err != nil {
		if frigateStatus(err) == http.StatusNotFound {
			return Event{}, ErrEventNotFound
		}
		return Event{}, err
	}
	if raw.ID != id {
		return Event{}, ErrEventNotFound
	}
	event := raw.event()
	list := []Event{event}
	m.markReviewed(ctx, base, []string{event.Camera}, list)
	return list[0], nil
}

// DeleteEvent removes an event with its snapshot.
func (m *Manager) DeleteEvent(ctx context.Context, id string) error {
	if !ValidEventID(id) {
		return ErrEventNotFound
	}
	base, err := m.base()
	if err != nil {
		return err
	}
	if err := m.frigate.call(ctx, http.MethodDelete, base, "/api/events/"+url.PathEscape(id), nil, nil, nil); err != nil {
		if frigateStatus(err) == http.StatusNotFound {
			return ErrEventNotFound
		}
		return err
	}
	return nil
}

// SetReviewed marks the stretch of activity an event belongs to as seen
// or not. "Seen" is one flag for the NVR, not one per account.
func (m *Manager) SetReviewed(ctx context.Context, id string, reviewed bool) error {
	if !ValidEventID(id) {
		return ErrEventNotFound
	}
	base, err := m.base()
	if err != nil {
		return err
	}
	var review frigateReview
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/review/event/"+url.PathEscape(id), nil, nil, &review); err != nil {
		if frigateStatus(err) == http.StatusNotFound {
			return ErrEventNotFound
		}
		return err
	}
	if !ValidEventID(review.ID) {
		return ErrEventNotFound
	}
	return m.frigate.call(ctx, http.MethodPost, base, "/api/reviews/viewed", nil, map[string]any{"ids": []string{review.ID}, "reviewed": reviewed}, nil)
}

// Media opens a picture or video of Frigate for the API to pass on:
// path is Frigate's path, header the browser's (Range). The caller checks
// the right first and closes the body.
func (m *Manager) Media(ctx context.Context, path, rawQuery string, header http.Header) (*http.Response, error) {
	base, err := m.base()
	if err != nil {
		return nil, err
	}
	return m.frigate.open(ctx, base, path, rawQuery, header)
}
