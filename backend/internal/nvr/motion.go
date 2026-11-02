package nvr

import "time"

// Frame size of the motion check: small gray pictures are enough to see
// that something moved and cost almost no CPU.
const (
	MotionW = 160
	MotionH = 90
	// MotionFPS is how many pictures a second are compared.
	MotionFPS = 2
)

// Motion decides from gray frames when something moves. It compares each
// frame with a slowly following background, so light changes and a parked
// car do not count for long, and it needs a few frames in a row to start and
// a quiet period to end (hysteresis), so one noisy frame is not an event.
type Motion struct {
	// Sensitivity is 1 (hard to trigger) to 100 (easy); 0 means 50.
	Sensitivity int

	bg      []float32
	active  bool
	hits    int
	quiet   int
	started time.Time
}

// MotionEvent is a change of state.
type MotionEvent struct {
	Start bool // true: motion began, false: it ended
	At    time.Time
	// Score is the share of changed pixels (0..1) that triggered a start.
	Score float64
}

const (
	startFrames = 2 // frames in a row that must move
	endFrames   = 10 * MotionFPS
)

// Active reports whether motion is going on.
func (m *Motion) Active() bool { return m.active }

func (m *Motion) thresholds() (pixelDelta float32, share float64) {
	s := m.Sensitivity
	if s <= 0 {
		s = 50
	}
	if s > 100 {
		s = 100
	}
	// 100 -> 12 gray levels and 0.2 % of pixels; 1 -> 40 levels and 8 %.
	pixelDelta = 40 - float32(s-1)*28/99
	share = 0.08 - float64(s-1)*0.078/99
	return
}

// Feed takes one frame (MotionW*MotionH gray bytes) and returns an event
// when the state changes.
func (m *Motion) Feed(frame []byte, at time.Time) *MotionEvent {
	if len(frame) != MotionW*MotionH {
		return nil
	}
	if m.bg == nil {
		m.bg = make([]float32, len(frame))
		for i, v := range frame {
			m.bg[i] = float32(v)
		}
		return nil
	}
	delta, share := m.thresholds()
	// A change of light over the whole picture is not movement: take the
	// average shift out before comparing.
	var shift float32
	for i, v := range frame {
		shift += float32(v) - m.bg[i]
	}
	shift /= float32(len(frame))
	changed := 0
	for i, v := range frame {
		d := float32(v) - m.bg[i] - shift
		if d < 0 {
			d = -d
		}
		if d > delta {
			changed++
		}
	}
	score := float64(changed) / float64(len(frame))
	moving := score >= share
	// The background follows slowly; faster while nothing moves.
	rate := float32(0.02)
	if moving {
		rate = 0.002
	}
	for i, v := range frame {
		m.bg[i] += (float32(v) - m.bg[i]) * rate
	}
	switch {
	case !m.active && moving:
		m.hits++
		if m.hits >= startFrames {
			m.active, m.quiet, m.started = true, 0, at
			return &MotionEvent{Start: true, At: at, Score: score}
		}
	case !m.active:
		m.hits = 0
	case moving:
		m.quiet = 0
	default:
		m.quiet++
		if m.quiet >= endFrames {
			m.active, m.hits = false, 0
			return &MotionEvent{Start: false, At: at}
		}
	}
	return nil
}
