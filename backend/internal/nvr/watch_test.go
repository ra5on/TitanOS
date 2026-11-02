package nvr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stand-in for ffmpeg: as the motion check it sends still frames, a few
// bright ones and still ones again; as a snapshot it sends a "picture".
const fakeMotionFFmpeg = `#!/bin/sh
case "$*" in
*image2pipe*) printf 'JPEGDATA'; exit 0;;
esac
still() { head -c 14400 /dev/zero; }
bright() { head -c 3600 /dev/zero | tr '\000' '\377'; head -c 10800 /dev/zero; }
i=0; while [ $i -lt 6 ]; do still; i=$((i+1)); done
i=0; while [ $i -lt 4 ]; do bright; i=$((i+1)); done
i=0; while [ $i -lt 30 ]; do still; i=$((i+1)); done
sleep 30
`

type labelled struct{}

func (labelled) Detect(jpeg []byte) ([]string, error) {
	if string(jpeg) == "JPEGDATA" {
		return []string{"person"}, nil
	}
	return nil, nil
}

func TestWatcherStoresMotionEventsWithPicture(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(prog, []byte(fakeMotionFFmpeg), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newTestStore(t)
	c, _ := s.Add(Camera{Name: "Hof", User: "v", MainURL: "rtsp://192.168.1.5/main", SubURL: "rtsp://192.168.1.5/sub", Detect: true}, "pw")
	root := filepath.Join(dir, "rec")
	os.MkdirAll(root, 0o755)
	var mu sync.Mutex
	var seen []Event
	w := &Watcher{Store: s, Rec: &Recorder{Store: s, FFmpeg: prog}, Root: func() string { return root }, Detector: labelled{}, Backoff: time.Hour,
		OnEvent: func(_ Camera, e Event) { mu.Lock(); seen = append(seen, e); mu.Unlock() }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Apply(ctx, true)
	defer w.Apply(ctx, false)

	waitFor(t, func() bool {
		ev := Events(root, c.ID, 10)
		return len(ev) == 1 && !ev[0].End.IsZero() && ev[0].Snapshot
	})
	e := Events(root, c.ID, 10)[0]
	if e.Kind != "motion" || len(e.Labels) != 1 || e.Labels[0] != "person" {
		t.Fatalf("event: %+v", e)
	}
	day, file, _ := strings.Cut(e.ID, "/")
	if data, err := os.ReadFile(SnapshotPath(root, c.ID, day, file+".jpg")); err != nil || string(data) != "JPEGDATA" {
		t.Fatalf("picture: %q %v", data, err)
	}
	mu.Lock()
	n := len(seen)
	mu.Unlock()
	if n < 2 {
		t.Fatalf("callback called %d times, want start and end", n)
	}
	// Switching the check off stops it.
	w.Apply(ctx, false)
	if w.Status()[c.ID].Watching {
		t.Fatal("still watching")
	}
}

func TestPruneEventsByAge(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	for _, d := range []int{1, 20} {
		at := now.AddDate(0, 0, -d)
		e := Event{ID: eventID(at), Camera: "cam-a", Kind: "motion", Start: at}
		if err := WriteEvent(root, e); err != nil {
			t.Fatal(err)
		}
	}
	if got := pruneEvents(root, "cam-a", 7, now); got != 1 {
		t.Fatalf("removed %d days", got)
	}
	if got := Events(root, "cam-a", 0); len(got) != 1 {
		t.Fatalf("left: %+v", got)
	}
}
