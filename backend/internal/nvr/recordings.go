package nvr

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RecordedHour is one hour with recordings.
type RecordedHour struct {
	Hour int `json:"hour"`
	// Start is the beginning of the hour in Unix seconds.
	Start int64 `json:"start"`
	// Seconds of recording in the hour; Motion and Objects are counters
	// (0: nothing), Events the events with a clip.
	Seconds int `json:"seconds"`
	Motion  int `json:"motion"`
	Objects int `json:"objects"`
	Events  int `json:"events"`
}

// RecordedDay is one day with recordings, in the time zone of the NAS.
type RecordedDay struct {
	Day   string         `json:"day"`
	Hours []RecordedHour `json:"hours"`
}

// RecordingsSummary lists the days and hours a camera has recordings of,
// newest day first, in the time zone of the NAS.
func (m *Manager) RecordingsSummary(ctx context.Context, camera string) ([]RecordedDay, error) {
	if _, ok := m.recordedCamera(camera); !ok {
		return nil, ErrNotFound
	}
	base, err := m.base()
	if err != nil {
		return nil, err
	}
	zone := m.zone()
	var raw []struct {
		Day   string `json:"day"`
		Hours []struct {
			Hour     string   `json:"hour"`
			Events   int      `json:"events"`
			Motion   *float64 `json:"motion"`
			Objects  *float64 `json:"objects"`
			Duration float64  `json:"duration"`
		} `json:"hours"`
	}
	query := url.Values{"timezone": {zone.String()}}
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/"+url.PathEscape(camera)+"/recordings/summary", query, nil, &raw); err != nil {
		return nil, err
	}
	days := []RecordedDay{}
	for _, d := range raw {
		date, err := time.ParseInLocation("2006-01-02", d.Day, zone)
		if err != nil {
			continue
		}
		day := RecordedDay{Day: d.Day, Hours: []RecordedHour{}}
		for _, h := range d.Hours {
			hour, err := strconv.Atoi(h.Hour)
			if err != nil || hour < 0 || hour > 23 {
				continue
			}
			entry := RecordedHour{Hour: hour, Seconds: int(h.Duration), Events: h.Events,
				Start: time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, zone).Unix()}
			if h.Motion != nil {
				entry.Motion = int(*h.Motion)
			}
			if h.Objects != nil {
				entry.Objects = int(*h.Objects)
			}
			day.Hours = append(day.Hours, entry)
		}
		slices.SortFunc(day.Hours, func(a, b RecordedHour) int { return a.Hour - b.Hour })
		days = append(days, day)
	}
	slices.SortFunc(days, func(a, b RecordedDay) int { return strings.Compare(b.Day, a.Day) })
	return days, nil
}

// Segment is one piece of recording (about ten seconds).
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// Motion and Objects are counters; 0 means nothing moved, nothing
	// was detected.
	Motion  int `json:"motion"`
	Objects int `json:"objects"`
}

// MaxSegmentWindow is the longest stretch Segments lists at once.
const MaxSegmentWindow = 24 * time.Hour

// Segments lists the recordings of a camera between from and to (Unix
// seconds), oldest first. What is missing between two segments was not
// recorded.
func (m *Manager) Segments(ctx context.Context, camera string, from, to float64) ([]Segment, error) {
	if _, ok := m.recordedCamera(camera); !ok {
		return nil, ErrNotFound
	}
	if from <= 0 || to <= from || to-from > MaxSegmentWindow.Seconds() {
		return nil, invalid("from/to: at most 24 hours")
	}
	base, err := m.base()
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Start   float64  `json:"start_time"`
		End     float64  `json:"end_time"`
		Motion  *float64 `json:"motion"`
		Objects *float64 `json:"objects"`
	}
	query := url.Values{"after": {formatTime(from)}, "before": {formatTime(to)}}
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/"+url.PathEscape(camera)+"/recordings", query, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Segment, 0, len(raw))
	for _, r := range raw {
		s := Segment{Start: r.Start, End: r.End}
		if r.Motion != nil {
			s.Motion = int(*r.Motion)
		}
		if r.Objects != nil {
			s.Objects = int(*r.Objects)
		}
		out = append(out, s)
	}
	return out, nil
}

// zone is the time zone of the NAS (Timezone), UTC if it is not known.
func (m *Manager) zone() *time.Location {
	if m.Timezone != nil {
		if zone, err := time.LoadLocation(m.Timezone()); err == nil && zone.String() != "Local" && zone.String() != "" {
			return zone
		}
	}
	return time.UTC
}
