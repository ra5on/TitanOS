package nvr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventsModeKeepsOnlyWhatLiesAroundEvents(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	base := now.Add(-2 * time.Hour)
	for i := 0; i < 24; i++ { // two hours of five-minute files
		writeSeg(t, root, "cam-a", base.Add(time.Duration(i)*5*time.Minute), 10)
	}
	// One event at 10:31–10:33 keeps the file 10:30 (and nothing else, the
	// margin of a minute does not reach 10:25 or 10:35).
	ev := Event{ID: eventID(base.Add(31 * time.Minute)), Camera: "cam-a", Kind: "motion", Start: base.Add(31 * time.Minute), End: base.Add(33 * time.Minute)}
	if err := WriteEvent(root, ev); err != nil {
		t.Fatal(err)
	}
	res := PruneAll(root, []Camera{{ID: "cam-a", RecordMode: "events"}}, 7, 0, now, nil)
	left := segments(root, "cam-a")
	var names []string
	for _, s := range left {
		names = append(names, s.Start.In(time.Local).Format("15:04"))
	}
	// Kept: 10:30 (event) and everything younger than the grace period
	// (from 11:40 on, because its end 11:45 is within 15 minutes), plus the newest.
	want := []string{"10:30", "11:40", "11:45", "11:50", "11:55"}
	if len(names) != len(want) {
		t.Fatalf("kept %v, want %v (removed %d)", names, want, res.Files)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("kept %v, want %v", names, want)
		}
	}
	// Continuous mode keeps everything.
	root2 := t.TempDir()
	for i := 0; i < 5; i++ {
		writeSeg(t, root2, "cam-a", base.Add(time.Duration(i)*5*time.Minute), 10)
	}
	PruneAll(root2, []Camera{{ID: "cam-a", RecordMode: "continuous"}}, 7, 0, now, nil)
	if len(segments(root2, "cam-a")) != 5 {
		t.Fatal("continuous recording pruned without reason")
	}
}

func TestShortenedRetentionIsReported(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	for d := 5; d >= 0; d-- {
		writeSeg(t, root, "cam-a", now.AddDate(0, 0, -d), 10)
	}
	// 15 bytes are missing, and statfs does not notice deletions (Btrfs
	// before its commit): exactly two files of 10 bytes go, not all.
	free := func(string) (int64, error) { return 1<<30 - 15, nil }
	res := PruneAll(root, []Camera{{ID: "cam-a", KeepDays: 14}}, 7, 1<<30, now, free)
	if res.Files != 2 {
		t.Fatalf("removed %d", res.Files)
	}
	if left, ok := res.Shortened["cam-a"]; !ok || left != 3 {
		t.Fatalf("shortened: %+v", res.Shortened)
	}
}

func TestCheckOnlineReportsSilenceAndReturn(t *testing.T) {
	dir := t.TempDir()
	s := newTestStore(t)
	c, _ := s.Add(Camera{Name: "Hof", MainURL: "rtsp://192.168.1.5/a", RecordMode: "continuous"}, "")
	if err := s.SetSettings(Settings{Enabled: true, Volume: "v", KeepDays: 7}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "nvr"), 0o755)
	os.WriteFile(filepath.Join(dir, "nvr", Marker), nil, 0o644)
	m := &Manager{Store: s, VolumePath: func(string) (string, bool) { return dir, true }}
	var got []string
	m.Notify = func(_, kind, subject string) { got = append(got, kind) }
	root := m.Root()
	now := time.Now()
	m.since = now.Add(-time.Hour)
	// A file last written ten minutes ago: offline.
	writeSeg(t, root, c.ID, now.Add(-15*time.Minute), 10)
	p := filepath.Join(root, c.ID, now.Add(-15*time.Minute).UTC().Format("2006-01-02"), now.Add(-15*time.Minute).UTC().Format("15-04-05")+".mp4")
	old := now.Add(-10 * time.Minute)
	os.Chtimes(p, old, old)
	m.CheckOnline(now)
	m.CheckOnline(now) // reported once
	// It writes again: back.
	os.Chtimes(p, now, now)
	m.CheckOnline(now)
	if len(got) != 2 || got[0] != "nvr_camera_offline" || got[1] != "nvr_camera_online" {
		t.Fatalf("notices: %v", got)
	}
}
