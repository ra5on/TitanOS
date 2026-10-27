package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestBandwidthValidation(t *testing.T) {
	for _, good := range []*Bandwidth{nil, {}, {Limit: 10}, {Limit: maxBandwidth}, {Limit: 10, WindowFrom: "22:00", WindowTo: "06:00"}, {WindowFrom: "08:00", WindowTo: "18:00", WindowLimit: 5}} {
		if err := checkBandwidth(good); err != nil {
			t.Errorf("%+v: %v", good, err)
		}
	}
	for _, bad := range []*Bandwidth{{Limit: -1}, {Limit: maxBandwidth + 1}, {WindowLimit: -1, WindowFrom: "01:00", WindowTo: "02:00"},
		{Limit: 1, WindowFrom: "01:00"}, {Limit: 1, WindowTo: "01:00"}, {Limit: 1, WindowFrom: "25:00", WindowTo: "01:00"},
		{Limit: 1, WindowFrom: "01:00", WindowTo: "01:00"}, {WindowLimit: 5}} {
		if err := checkBandwidth(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted: %v", bad, err)
		}
	}
	// A setting that limits nothing is not kept.
	if normalBandwidth(&Bandwidth{WindowFrom: "01:00", WindowTo: "02:00"}) != nil || normalBandwidth(&Bandwidth{Limit: 3}) == nil {
		t.Error("normalBandwidth")
	}
	job := Job{ID: "nightly", Sources: []string{"media"}, Every: "daily", Time: "02:00", Bandwidth: &Bandwidth{Limit: -5},
		Destination: Destination{Type: "local", Volume: "cold", Folder: "backups/nightly"}}
	if err := check(job, []Source{{Name: "media", Volume: "tank"}}, volumes); !errors.Is(err, ErrInvalid) {
		t.Errorf("check took a negative limit: %v", err)
	}
}

func TestBandwidthWindowSelection(t *testing.T) {
	day := func(hour, minute int) time.Time { return time.Date(2026, 10, 27, hour, minute, 0, 0, time.Local) }
	// Daytime 10 MiB/s, at night (across midnight) unlimited.
	night := &Bandwidth{Limit: 10, WindowFrom: "22:00", WindowTo: "06:00", WindowLimit: 0}
	for _, tc := range []struct {
		at   time.Time
		want int
	}{{day(12, 0), 10}, {day(21, 59), 10}, {day(22, 0), 0}, {day(23, 59), 0}, {day(0, 0), 0}, {day(5, 59), 0}, {day(6, 0), 10}} {
		if got := night.at(tc.at); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.at.Format("15:04"), got, tc.want)
		}
	}
	// Only working hours limited, the rest of the day free.
	office := &Bandwidth{WindowFrom: "08:00", WindowTo: "18:00", WindowLimit: 5}
	if office.at(day(7, 59)) != 0 || office.at(day(8, 0)) != 5 || office.at(day(17, 59)) != 5 || office.at(day(18, 0)) != 0 {
		t.Error("window within a day")
	}
	if (*Bandwidth)(nil).at(day(12, 0)) != 0 || (&Bandwidth{Limit: 7}).at(day(3, 0)) != 7 {
		t.Error("without a window")
	}
}

func TestResticLimitsAreKiB(t *testing.T) {
	if got := resticLimits(10); !slices.Equal(got, []string{"--limit-upload", "10240", "--limit-download", "10240"}) {
		t.Errorf("10 MiB/s: %v", got)
	}
	if resticLimits(0) != nil {
		t.Error("unlimited has arguments")
	}
}

// fakePace is a clock that moves only when the limiter sleeps.
type fakePace struct {
	now   time.Time
	slept time.Duration
}

func (f *fakePace) limiter(rate func() int64, change func(int64)) *limiter {
	l := newLimiter(rate, change)
	l.now = func() time.Time { return f.now }
	l.sleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f.slept += d
		f.now = f.now.Add(d)
		return nil
	}
	return l
}

func TestLimiterPacesAndFollowsTheWindow(t *testing.T) {
	f := &fakePace{now: time.Date(2026, 10, 27, 21, 59, 0, 0, time.Local)}
	b := &Bandwidth{Limit: 1, WindowFrom: "22:00", WindowTo: "06:00", WindowLimit: 4}
	var changes []int64
	l := f.limiter(func() int64 { return int64(b.at(f.now)) << 20 }, func(rate int64) { changes = append(changes, rate) })
	ctx := context.Background()
	// 4 MiB at 1 MiB/s take four seconds.
	for range 16 {
		if err := l.wait(ctx, 256<<10); err != nil {
			t.Fatal(err)
		}
	}
	if f.slept < 4*time.Second-time.Millisecond || f.slept > 4*time.Second+time.Millisecond {
		t.Errorf("slept %s for 4 MiB at 1 MiB/s", f.slept)
	}
	// Past 22:00 the window limit holds from the next look at the clock.
	f.now, f.slept = time.Date(2026, 10, 27, 22, 0, 0, 0, time.Local), 0
	for range 16 {
		l.wait(ctx, 256<<10)
	}
	if f.slept < time.Second-time.Millisecond || f.slept > time.Second+time.Millisecond {
		t.Errorf("slept %s for 4 MiB at 4 MiB/s", f.slept)
	}
	if !slices.Equal(changes, []int64{1 << 20, 4 << 20}) {
		t.Errorf("changes: %v", changes)
	}
	// Time spent elsewhere earns no burst.
	f.now, f.slept = f.now.Add(time.Hour), 0
	for range 16 {
		l.wait(ctx, 256<<10)
	}
	if f.slept < time.Second-time.Millisecond {
		t.Errorf("burst after a pause: slept %s", f.slept)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := l.wait(cancelled, 4<<20); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait: %v", err)
	}
	// Unlimited and nil do not wait.
	var none *limiter
	if none.wait(ctx, 1<<30) != nil {
		t.Error("nil limiter")
	}
	free := f.limiter(func() int64 { return 0 }, nil)
	f.slept = 0
	free.wait(ctx, 1<<30)
	if f.slept != 0 {
		t.Error("unlimited waited")
	}
}

func TestResticRunKeepsTheLimitOfItsStart(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	job.Bandwidth = &Bandwidth{Limit: 10, WindowFrom: "22:00", WindowTo: "06:00"}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	if got := e.m.List()[0].Job.Bandwidth; got == nil || got.Limit != 10 || got.WindowFrom != "22:00" {
		t.Fatalf("stored: %+v", got)
	}
	// Daytime (12:00): 10 MiB/s for every restic call of the run.
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatal(got)
	}
	for _, line := range strings.Split(strings.TrimSpace(e.calls()), "\n") {
		if !strings.Contains(line, "--limit-upload 10240 --limit-download 10240") {
			t.Errorf("without limit: %s", line)
		}
	}
	if e.state("cloud").BandwidthLimit != 0 {
		t.Error("limit shown after the run")
	}
	// At night the window holds: no limit.
	os.Remove(filepath.Join(e.dir, "calls"))
	e.now = time.Date(2026, 10, 14, 23, 30, 0, 0, time.Local)
	e.m.Start("cloud")
	e.result(t)
	if strings.Contains(e.calls(), "--limit-") {
		t.Errorf("limited in the window: %s", e.calls())
	}
	// While it runs the page sees the limit.
	os.Remove(filepath.Join(e.dir, "calls"))
	e.now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)
	e.flag(t, "slow", true)
	e.m.Start("cloud")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(e.calls(), "backup --json") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if state := e.state("cloud"); !state.Running || state.BandwidthLimit != 10 {
		t.Errorf("running: %+v", state)
	}
	e.m.Stop("cloud")
	for e.state("cloud").Running && time.Now().Before(deadline.Add(20*time.Second)) {
		time.Sleep(20 * time.Millisecond)
	}
	if state := e.state("cloud"); state.Running || state.BandwidthLimit != 0 {
		t.Errorf("after stopping: %+v", state)
	}
}

func TestCopyFollowsTheLimit(t *testing.T) {
	var slept time.Duration
	clock := time.Now()
	paceNow = func() time.Time { return clock }
	paceSleep = func(_ context.Context, d time.Duration) error { slept += d; clock = clock.Add(d); return nil }
	t.Cleanup(func() { paceNow, paceSleep = time.Now, sleepContext })
	source, target := t.TempDir(), t.TempDir()
	m := NewManager(config.Open(t.TempDir()))
	m.mountRoot = t.TempDir()
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	var seen []int
	m.WithUSB = func(ctx context.Context, id string, work func(*os.Root) error) error {
		root, err := os.OpenRoot(target)
		if err != nil {
			return err
		}
		defer root.Close()
		err = work(root)
		seen = append(seen, m.states["copy"].BandwidthLimit)
		return err
	}
	os.WriteFile(filepath.Join(source, "big"), make([]byte, 3<<20), 0o600)
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", DeviceID: "ABCD-1234", Folder: "backups/copy"}, Bandwidth: &Bandwidth{Limit: 1}}
	if err := m.copyUSB(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	// 3 MiB at 1 MiB/s: three seconds of waiting.
	if slept < 3*time.Second-time.Millisecond || slept > 3*time.Second+time.Millisecond {
		t.Errorf("slept %s", slept)
	}
	// The second run compares; reading the destination back counts too.
	slept = 0
	if err := m.copyUSB(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if slept < 3*time.Second-time.Millisecond {
		t.Errorf("comparison unpaced: slept %s", slept)
	}
	if !slices.Equal(seen, []int{1, 1}) {
		t.Errorf("shown limits: %v", seen)
	}
	// Without a limit nothing waits.
	slept = 0
	job.Bandwidth = nil
	os.WriteFile(filepath.Join(source, "big"), make([]byte, 3<<20+1), 0o600)
	if err := m.copyUSB(context.Background(), job); err != nil || slept != 0 {
		t.Errorf("unlimited: %v, slept %s", err, slept)
	}
}
