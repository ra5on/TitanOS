package monitoring

import (
	"math"
	"sort"
	"time"
)

type Trend struct {
	Volume     string   `json:"volume"`
	Known      bool     `json:"known"`
	Used       uint64   `json:"used"`
	Total      uint64   `json:"total"`
	DailyBytes float64  `json:"dailyBytes"`
	DaysTo90   *float64 `json:"daysTo90"`
	DaysToFull *float64 `json:"daysToFull"`
	Warning    bool     `json:"warning"`
	Hours      int      `json:"hours"`
}

// CapacityTrends fits hourly observations from the latest uninterrupted segment.
// Capacity changes, gaps and stale samples invalidate a prediction. It is a
// projection of measured filesystem usage, not a guarantee of future capacity.
func CapacityTrends(samples []Sample, now time.Time) []Trend {
	names := map[string]bool{}
	for _, s := range samples {
		for _, v := range s.Volumes {
			names[v.Name] = true
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	out := []Trend{}
	for _, name := range ordered {
		trend := Trend{Volume: name}
		hours := map[int64]Volume{}
		var previous time.Time
		var last time.Time
		for _, s := range samples {
			var volume *Volume
			for i := range s.Volumes {
				if s.Volumes[i].Name == name {
					volume = &s.Volumes[i]
					break
				}
			}
			if volume == nil {
				hours = map[int64]Volume{}
				previous = time.Time{}
				last = time.Time{}
				trend.Total = 0
				trend.Used = 0
				continue
			}
			if s.Gap || (!previous.IsZero() && (s.At.Sub(previous) > 150*time.Minute || !s.At.After(previous))) || (trend.Total != 0 && trend.Total != volume.Total) {
				hours = map[int64]Volume{}
			}
			trend.Total, trend.Used = volume.Total, volume.Used
			hours[s.At.Unix()/3600] = *volume
			previous, last = s.At, s.At
		}
		recent := !last.IsZero() && now.Sub(last) <= 2*time.Hour && !last.After(now.Add(5*time.Minute))
		trend.Warning = recent && trend.Total > 0 && float64(trend.Used)/float64(trend.Total) >= 0.9
		keys := make([]int64, 0, len(hours))
		for key := range hours {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		trend.Hours = len(keys)
		if len(keys) >= 24 && keys[len(keys)-1]-keys[0] >= 23 && recent && trend.Total > 0 {
			var sx, sy, sxx, sxy, syy float64
			baseUsage := float64(hours[keys[0]].Used)
			for _, key := range keys {
				x := float64(key-keys[0]) / 24
				y := float64(hours[key].Used) - baseUsage
				sx += x
				sy += y
				sxx += x * x
				sxy += x * y
				syy += y * y
			}
			n := float64(len(keys))
			variance := n*sxx - sx*sx
			slope := (n*sxy - sx*sy) / variance
			residual := n*syy - sy*sy
			correlation := 1.0
			if residual > 0 {
				correlation = math.Pow(n*sxy-sx*sy, 2) / (variance * residual)
			}
			if variance > 0 && !math.IsNaN(slope) && !math.IsInf(slope, 0) && correlation >= 0.5 {
				trend.Known = true
				trend.DailyBytes = slope
				if slope > 0 {
					d90 := math.Max(0, (float64(trend.Total)*0.9-float64(trend.Used))/slope)
					full := math.Max(0, (float64(trend.Total)-float64(trend.Used))/slope)
					trend.DaysTo90 = &d90
					trend.DaysToFull = &full
					trend.Warning = trend.Warning || d90 <= 7
				}
			}
		}
		out = append(out, trend)
	}
	return out
}
