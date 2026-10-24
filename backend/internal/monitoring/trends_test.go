package monitoring

import (
	"testing"
	"time"
)

func TestCapacityTrendsRequireRecentContinuousStableObservations(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	samples := []Sample{}
	for i := range 25 {
		samples = append(samples, Sample{At: now.Add(time.Duration(i-24) * time.Hour), Volumes: []Volume{{Name: "data", Total: 1000, Used: uint64(200 + i*10)}}})
	}
	trend := CapacityTrends(samples, now)[0]
	if !trend.Known || trend.DailyBytes < 239 || trend.DailyBytes > 241 || trend.DaysTo90 == nil || !trend.Warning {
		t.Fatal(trend)
	}
	if CapacityTrends(samples[:10], now)[0].Known {
		t.Fatal("invented a prediction from insufficient data")
	}
	samples[20].Gap = true
	if CapacityTrends(samples, now)[0].Known {
		t.Fatal("bridged an observation gap")
	}
	samples[20].Gap = false
	samples[20].Volumes[0].Total = 2000
	if CapacityTrends(samples, now)[0].Known {
		t.Fatal("bridged a capacity change")
	}
	samples[20].Volumes[0].Total = 1000
	if CapacityTrends(samples, now.Add(3*time.Hour))[0].Known {
		t.Fatal("predicted with stale observations")
	}
}

func TestCachedTrendsBoundedGapAndVolumeSwitch(t *testing.T) {
	now := time.Now().Truncate(time.Hour)
	m := &Manager{settings: Settings{Enabled: true, Volume: "Data"}}
	var samples []Sample
	for i := 0; i < 800; i++ {
		samples = append(samples, Sample{At: now.Add(time.Duration(i-799) * time.Hour), Volumes: []Volume{{Name: "Data", Total: 100000, Used: uint64(1000 + i)}}})
	}
	m.remember(samples, "Data")
	if len(m.cachedSamples) != 721 || !m.CachedTrends(now)[0].Known {
		t.Fatal("bounded cache did not produce a trend")
	}
	// A gap earlier in the same hour cannot disappear behind a later sample.
	m.remember([]Sample{{At: now.Add(-30 * time.Minute), Gap: true, Volumes: []Volume{{Name: "Data", Total: 100000, Used: 1798}}}}, "Data")
	if m.CachedTrends(now)[0].Known {
		t.Fatal("gap was hidden by hourly aggregation")
	}
	m.mu.Lock()
	m.settings.Volume = "Other"
	m.cachedSamples = nil
	m.mu.Unlock()
	m.remember(samples, "Data")
	if len(m.CachedTrends(now)) != 0 {
		t.Fatal("old volume warmed new cache")
	}
	m.remember(samples, "Other")
	if m.CachedTrends(now.Add(3 * time.Hour))[0].Known {
		t.Fatal("stale forecast considered valid")
	}
}
