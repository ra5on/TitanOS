package pimtest

import (
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Enabled skips a measurement unless TITANOS_PERF is set: they take
// minutes and their numbers belong in docs/validation/performance-113.md,
// not in every test run.
func Enabled(t *testing.T) {
	t.Helper()
	if os.Getenv("TITANOS_PERF") == "" {
		t.Skip("measurement: TITANOS_PERF=1 go test -run TestPerf -v")
	}
}

// Runs is how often a measurement repeats; the median counts.
const Runs = 5

// Bench runs fn as a benchmark Runs times and logs the median time and
// the allocations of one call.
func Bench(t *testing.T, name string, fn func()) time.Duration {
	t.Helper()
	var ns []int64
	var allocs, bytes int64
	for range Runs {
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				fn()
			}
		})
		ns = append(ns, r.NsPerOp())
		allocs, bytes = r.AllocsPerOp(), r.AllocedBytesPerOp()
	}
	slices.Sort(ns)
	med := time.Duration(ns[len(ns)/2])
	t.Logf("%-58s median %12s  (min %s, max %s)  %d allocs %d B", name, med, time.Duration(ns[0]), time.Duration(ns[len(ns)-1]), allocs, bytes)
	return med
}

// Blocked measures probe while busy runs again and again in the
// background (a slow read or write of another request): median and
// maximum of `samples` probes.
func Blocked(t *testing.T, name string, samples int, busy, probe func()) (median, worst time.Duration) {
	t.Helper()
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			busy()
		}
	}()
	time.Sleep(20 * time.Millisecond) // the background call is under way
	var took []time.Duration
	for range samples {
		start := time.Now()
		probe()
		took = append(took, time.Since(start))
		time.Sleep(7 * time.Millisecond)
	}
	stop.Store(true)
	wg.Wait()
	slices.Sort(took)
	median, worst = took[len(took)/2], took[len(took)-1]
	t.Logf("%-58s median %12s  max %s", name, median, worst)
	return median, worst
}

// Delay is a pause the Volume hook of a test sleeps before it opens the
// volume: a disk that has to spin up (docs/DISK-SLEEP.md).
type Delay struct{ d atomic.Int64 }

// Set changes the pause (0: none).
func (d *Delay) Set(v time.Duration) { d.d.Store(int64(v)) }

// Wait sleeps the pause.
func (d *Delay) Wait() {
	if v := d.d.Load(); v > 0 {
		time.Sleep(time.Duration(v))
	}
}
