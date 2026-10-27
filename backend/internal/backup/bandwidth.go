package backup

import (
	"context"
	"fmt"
	"time"
)

// maxBandwidth is the highest limit in MiB/s; more is the same as none for
// any disk or network a NAS has.
const maxBandwidth = 10000

// Bandwidth limits how fast a backup moves data, in MiB/s; 0 is no limit.
// Limit holds all the time, or outside the window if there is one;
// WindowLimit holds from WindowFrom to WindowTo (HH:MM, may pass midnight,
// the same as the start window of the schedule). Older versions of titand
// do not know the field and run without a limit.
type Bandwidth struct {
	Limit       int    `json:"limit"`
	WindowFrom  string `json:"windowFrom,omitempty"`
	WindowTo    string `json:"windowTo,omitempty"`
	WindowLimit int    `json:"windowLimit,omitempty"`
}

func checkBandwidth(b *Bandwidth) error {
	if b == nil {
		return nil
	}
	for _, limit := range []int{b.Limit, b.WindowLimit} {
		if limit < 0 || limit > maxBandwidth {
			return invalid("bandwidth limit (0 to %d MiB/s)", maxBandwidth)
		}
	}
	if (b.WindowFrom == "") != (b.WindowTo == "") ||
		b.WindowFrom != "" && (!clockForm.MatchString(b.WindowFrom) || !clockForm.MatchString(b.WindowTo) || b.WindowFrom == b.WindowTo) {
		return invalid("bandwidth time window")
	}
	if b.WindowFrom == "" && b.WindowLimit != 0 {
		return invalid("bandwidth window limit without a window")
	}
	return nil
}

// normalBandwidth drops a setting that limits nothing, so the job looks the
// same as one that never had it.
func normalBandwidth(b *Bandwidth) *Bandwidth {
	if b == nil || b.Limit == 0 && b.WindowLimit == 0 {
		return nil
	}
	return b
}

// at returns the limit in MiB/s that holds at this moment (local time of
// the NAS); 0 means none.
func (b *Bandwidth) at(now time.Time) int {
	if b == nil {
		return 0
	}
	if b.WindowFrom != "" && inClockWindow(b.WindowFrom, b.WindowTo, now) {
		return b.WindowLimit
	}
	return b.Limit
}

// resticLimits are the arguments for restic: its limits are in KiB/s and
// fixed for the whole call. A run that passes a window boundary keeps the
// limit it started with.
func resticLimits(mib int) []string {
	if mib <= 0 {
		return nil
	}
	kib := fmt.Sprint(mib * 1024)
	return []string{"--limit-upload", kib, "--limit-download", kib}
}

// limiter paces a copy made in titand itself. Unlike restic it looks at the
// clock as it goes: a copy that passes a window boundary changes its pace.
type limiter struct {
	// rate returns the limit in bytes per second, 0 for none.
	rate   func() int64
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	change func(int64)

	known   bool
	current int64
	checked time.Time
	start   time.Time
	moved   int64
}

// paceNow and paceSleep are the clock of a limiter; tests replace them.
var (
	paceNow   = time.Now
	paceSleep = sleepContext
)

func newLimiter(rate func() int64, change func(int64)) *limiter {
	return &limiter{rate: rate, now: paceNow, sleep: paceSleep, change: change}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// wait is told that n bytes were moved and returns once moving them fits
// the limit. A nil limiter does not wait.
func (l *limiter) wait(ctx context.Context, n int) error {
	if l == nil || n <= 0 {
		return nil
	}
	now := l.now()
	// The limit is looked up once a second at most, not for every block.
	if !l.known || now.Sub(l.checked) >= time.Second {
		rate := l.rate()
		l.checked = now
		if !l.known || rate != l.current {
			l.known, l.current, l.start, l.moved = true, rate, now, 0
			if l.change != nil {
				l.change(rate)
			}
		}
	}
	if l.current <= 0 {
		return nil
	}
	// Time spent elsewhere (opening files, a slow disk) earns no burst of
	// more than a second.
	if due := l.start.Add(time.Duration(float64(l.moved) / float64(l.current) * float64(time.Second))); now.Sub(due) > time.Second {
		l.start, l.moved = now, 0
	}
	l.moved += int64(n)
	due := l.start.Add(time.Duration(float64(l.moved) / float64(l.current) * float64(time.Second)))
	if wait := due.Sub(now); wait > 0 {
		return l.sleep(ctx, wait)
	}
	return nil
}
