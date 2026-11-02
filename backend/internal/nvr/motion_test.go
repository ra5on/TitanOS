package nvr

import (
	"testing"
	"time"
)

func gray(v byte) []byte {
	f := make([]byte, MotionW*MotionH)
	for i := range f {
		f[i] = v
	}
	return f
}

// blob paints a bright square covering the given share of the picture.
func blob(base byte, share float64) []byte {
	f := gray(base)
	n := int(share * float64(len(f)))
	for i := 0; i < n; i++ {
		f[i] = 255
	}
	return f
}

func TestMotionStartsAndEndsWithHysteresis(t *testing.T) {
	m := &Motion{}
	at := time.Unix(1000, 0)
	next := func() time.Time { at = at.Add(500 * time.Millisecond); return at }
	for i := 0; i < 5; i++ {
		if ev := m.Feed(gray(60), next()); ev != nil {
			t.Fatal("event on a still picture")
		}
	}
	// One noisy frame is not an event.
	if ev := m.Feed(blob(60, 0.3), next()); ev != nil {
		t.Fatal("single frame started motion")
	}
	m.Feed(gray(60), next())
	// Two frames in a row are.
	var started *MotionEvent
	for i := 0; i < 3 && started == nil; i++ {
		started = m.Feed(blob(60, 0.3), next())
	}
	if started == nil || !started.Start || started.Score < 0.2 {
		t.Fatalf("start: %+v", started)
	}
	// It ends only after a quiet period.
	var ended *MotionEvent
	frames := 0
	for ; ended == nil && frames < 100; frames++ {
		ended = m.Feed(gray(60), next())
	}
	if ended == nil || ended.Start || frames < endFrames-2 {
		t.Fatalf("end after %d frames: %+v", frames, ended)
	}
}

func TestMotionIgnoresSlowLightChangesAndSmallNoise(t *testing.T) {
	m := &Motion{}
	at := time.Unix(0, 0)
	m.Feed(gray(60), at)
	// A slow brightening is followed by the background, not an event.
	for v := 60; v < 120; v++ {
		at = at.Add(time.Second)
		if ev := m.Feed(gray(byte(v)), at); ev != nil {
			t.Fatalf("event at brightness %d", v)
		}
	}
	// A tiny change (0.1 % of the picture) stays below the share.
	for i := 0; i < 10; i++ {
		at = at.Add(500 * time.Millisecond)
		if ev := m.Feed(blob(120, 0.001), at); ev != nil {
			t.Fatal("event for a few pixels")
		}
	}
}

func TestMotionSensitivity(t *testing.T) {
	trigger := func(s int, share float64) bool {
		m := &Motion{Sensitivity: s}
		at := time.Unix(0, 0)
		m.Feed(gray(60), at)
		for i := 0; i < 4; i++ {
			at = at.Add(500 * time.Millisecond)
			if ev := m.Feed(blob(60, share), at); ev != nil && ev.Start {
				return true
			}
		}
		return false
	}
	if !trigger(100, 0.01) || trigger(1, 0.01) {
		t.Fatal("sensitivity has no effect")
	}
	if !trigger(50, 0.2) {
		t.Fatal("a large change must trigger at the default")
	}
}
