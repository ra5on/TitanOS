package nvr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(config.Open(t.TempDir()))
}

func TestStoreKeepsPasswordApartFromCamera(t *testing.T) {
	s := newTestStore(t)
	c, err := s.Add(Camera{Name: "Hof", Host: "192.168.1.5", User: "view", MainURL: "rtsp://192.168.1.5:554/main", Record: true}, "s3cr:et@x")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Full(c, false); got != "rtsp://view:s3cr%3Aet%40x@192.168.1.5:554/main" {
		t.Fatalf("full address: %s", got)
	}
	data, _ := os.ReadFile(filepath.Join(s.cfg.Dir(), camerasFile))
	if strings.Contains(string(data), "s3cr") {
		t.Fatal("password must not be in the camera file")
	}
	if _, err := s.Add(Camera{Name: "x", MainURL: "rtsp://user:pw@1.2.3.4/x"}, ""); err == nil {
		t.Fatal("credentials in the address must be refused")
	}
	if _, err := s.Add(Camera{Name: "x", MainURL: "http://1.2.3.4/x"}, ""); err == nil {
		t.Fatal("only rtsp addresses are allowed")
	}
	// Survives a reload, password kept when none is sent.
	s2 := NewStore(s.cfg)
	if got := s2.Password(c.ID); got != "s3cr:et@x" {
		t.Fatalf("password after reload: %q", got)
	}
	c.Name = "Hofeinfahrt"
	if _, err := s2.Update(c, nil); err != nil || s2.Password(c.ID) != "s3cr:et@x" {
		t.Fatalf("update: %v", err)
	}
	if err := s2.Remove(c.ID); err != nil || s2.Password(c.ID) != "" || len(s2.Cameras()) != 0 {
		t.Fatalf("remove: %v", err)
	}
}

func TestScrubRemovesPasswords(t *testing.T) {
	line := scrub("Error opening rtsp://view:hunter2@192.168.1.5:554/main: 401", "hunter2")
	if strings.Contains(line, "hunter2") {
		t.Fatal(line)
	}
	if got := scrub("bad hunter2 here", "hunter2"); strings.Contains(got, "hunter2") {
		t.Fatal(got)
	}
}

// A stand-in for ffmpeg: writes its arguments to a file, makes one segment
// file and runs until interrupted.
const fakeFFmpeg = `#!/bin/sh
echo "$@" > "$FAKE_ARGS"
for last; do :; done
dir=$(dirname "$(dirname "$last")")/2026-11-02
mkdir -p "$dir"
echo data > "$dir/10-00-00.mp4"
echo "Error: rtsp://view:s3cret@192.168.1.5/main 401" >&2
trap 'exit 0' INT TERM
while true; do sleep 0.05; done
`

func TestRecorderStartsRestartsAndStops(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	t.Setenv("FAKE_ARGS", argsFile)
	prog := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(prog, []byte(fakeFFmpeg), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newTestStore(t)
	c, _ := s.Add(Camera{Name: "Hof", User: "view", MainURL: "rtsp://192.168.1.5/main", Record: true}, "s3cret")
	root := filepath.Join(dir, "rec")
	os.MkdirAll(root, 0o755)
	r := &Recorder{Store: s, FFmpeg: prog, Root: func() string { return root }, Backoff: 10 * time.Millisecond, ExtraEnv: []string{"FAKE_ARGS=" + argsFile}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.Apply(ctx, true)
	waitFor(t, func() bool {
		return r.Status()[c.ID].Recording && fileExists(filepath.Join(root, c.ID, "2026-11-02", "10-00-00.mp4"))
	})
	args, _ := os.ReadFile(argsFile)
	got := string(args)
	for _, want := range []string{"-c:v copy", "-an", "-rtsp_transport tcp", "-segment_time 300"} {
		if !strings.Contains(got, want) {
			t.Errorf("ffmpeg arguments lack %q: %s", want, got)
		}
	}
	waitFor(t, func() bool { return r.Status()[c.ID].LastError != "" })
	if e := r.Status()[c.ID].LastError; strings.Contains(e, "s3cret") {
		t.Fatalf("password in status: %s", e)
	}

	// Switching the service off stops it.
	r.Apply(ctx, false)
	if r.Status()[c.ID].Recording {
		t.Fatal("still recording after switch off")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func writeSeg(t *testing.T, root, cam string, at time.Time, size int) {
	t.Helper()
	p := filepath.Join(root, cam, at.UTC().Format("2006-01-02"), at.UTC().Format("15-04-05")+".mp4")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPruneByAgeKeepsNewestAndOwnRetention(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	for _, days := range []int{1, 3, 9, 20} {
		writeSeg(t, root, "cam-a", now.AddDate(0, 0, -days), 10)
		writeSeg(t, root, "cam-b", now.AddDate(0, 0, -days), 10)
	}
	// An old camera that is gone ages out with the general retention.
	writeSeg(t, root, "cam-gone", now.AddDate(0, 0, -30), 10)
	cams := []Camera{{ID: "cam-a"}, {ID: "cam-b", KeepDays: 2}}
	files, _ := Prune(root, cams, 7, 0, now, nil)
	if files != 2+3+1 {
		t.Fatalf("removed %d files", files)
	}
	if got := len(segments(root, "cam-a")); got != 2 {
		t.Fatalf("cam-a keeps %d", got)
	}
	if got := len(segments(root, "cam-b")); got != 1 {
		t.Fatalf("cam-b keeps %d", got)
	}
	if _, err := os.Stat(filepath.Join(root, "cam-gone", now.AddDate(0, 0, -30).UTC().Format("2006-01-02"))); err == nil {
		t.Fatal("empty day folder stays")
	}
}

func TestPruneNeverRemovesNewestWhenOutOfSpace(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	for i := 0; i < 5; i++ {
		writeSeg(t, root, "cam-a", now.Add(-time.Duration(i)*time.Hour), 10)
	}
	// A deficit larger than all files: everything but the newest file goes.
	free := func(string) (int64, error) { return 0, nil }
	files, _ := Prune(root, []Camera{{ID: "cam-a"}}, 7, 1<<30, now, free)
	left := segments(root, "cam-a")
	if files != 4 || len(left) != 1 || !left[0].Start.Equal(now) {
		t.Fatalf("removed %d, left %+v", files, left)
	}
}

func TestViewsAreCheckedAndFollowCameras(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Add(Camera{Name: "A", MainURL: "rtsp://192.168.1.5/a"}, "")
	b, _ := s.Add(Camera{Name: "B", MainURL: "rtsp://192.168.1.6/b"}, "")
	ok := []View{{Name: "Haus", Tiles: []Tile{{Camera: a.ID, X: 0, Y: 0, W: 4, H: 2}, {Camera: b.ID, X: 4, Y: 0, W: 2, H: 1}}}}
	if err := s.SetViews(ok); err != nil {
		t.Fatal(err)
	}
	if v := s.Views(); len(v) != 1 || v[0].ID == "" {
		t.Fatalf("views: %+v", v)
	}
	for name, bad := range map[string][]Tile{
		"outside the grid": {{Camera: a.ID, X: 5, W: 2, H: 1}},
		"overlap":          {{Camera: a.ID, X: 0, W: 3, H: 1}, {Camera: b.ID, X: 2, W: 2, H: 1}},
		"unknown camera":   {{Camera: "cam-nope", W: 1, H: 1}},
		"empty size":       {{Camera: a.ID, W: 0, H: 1}},
	} {
		if err := s.SetViews([]View{{Name: "x", Tiles: bad}}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := s.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	if tiles := s.Views()[0].Tiles; len(tiles) != 1 || tiles[0].Camera != b.ID {
		t.Fatalf("tiles after removing a camera: %+v", tiles)
	}
}

func TestQualityOriginalIsKeptOnceAndSurvivesEdits(t *testing.T) {
	s := newTestStore(t)
	c, _ := s.Add(Camera{Name: "A", MainURL: "rtsp://192.168.1.5/a"}, "")
	first := QualityChange{Token: "enc", Width: 2560, Height: 1440, FPS: 25, BitrateKb: 6144}
	second := QualityChange{Token: "enc", Width: 1280, Height: 720, FPS: 15, BitrateKb: 1024}
	if err := s.SetQualityOriginal(c.ID, "enc", &first); err != nil {
		t.Fatal(err)
	}
	// A second change must not overwrite what the camera had originally.
	if err := s.SetQualityOriginal(c.ID, "enc", &second); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Camera(c.ID)
	if got.QualityOriginal["enc"] != first {
		t.Fatalf("original: %+v", got.QualityOriginal)
	}
	// An edit from the page (which does not know the field) keeps it.
	got.QualityOriginal = nil
	got.Name = "B"
	if _, err := s.Update(got, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Camera(c.ID); again.QualityOriginal["enc"] != first {
		t.Fatalf("edit lost the original: %+v", again.QualityOriginal)
	}
	if err := s.SetQualityOriginal(c.ID, "enc", nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Camera(c.ID); len(again.QualityOriginal) != 0 {
		t.Fatalf("not cleared: %+v", again.QualityOriginal)
	}
}

func TestProtectedClipsSurviveRetentionAndCanBeReleased(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	old := now.AddDate(0, 0, -30)
	writeSeg(t, root, "cam-a", old, 10)
	writeSeg(t, root, "cam-a", now, 10)
	day, file := old.UTC().Format("2006-01-02"), old.UTC().Format("15-04-05")+".mp4"
	if err := Protect(root, "cam-a", day, file); err != nil {
		t.Fatal(err)
	}
	// The newest file may still be open.
	if err := Protect(root, "cam-a", now.UTC().Format("2006-01-02"), now.UTC().Format("15-04-05")+".mp4"); err == nil {
		t.Fatal("protected the file being written")
	}
	Prune(root, []Camera{{ID: "cam-a"}}, 7, 1<<40, now, func(string) (int64, error) { return 0, nil })
	if got := Protected(root, "cam-a"); len(got) != 1 || got[0].Name != day+"/"+file {
		t.Fatalf("protected clip lost: %+v", got)
	}
	if err := Unprotect(root, "cam-a", day, file); err != nil {
		t.Fatal(err)
	}
	Prune(root, []Camera{{ID: "cam-a"}}, 7, 0, now, nil)
	if len(Protected(root, "cam-a")) != 0 || len(segments(root, "cam-a")) != 1 {
		t.Fatal("released clip not subject to retention again")
	}
}

func TestStderrNeverBlocksAndNoticesRefusedLogins(t *testing.T) {
	var last string
	l := &lastLine{secret: "hunter2", set: func(s string) { last = s }}
	// Far more than a pipe buffer, with a line without end in between.
	big := strings.Repeat("x", 200000)
	for i := 0; i < 50; i++ {
		l.Write([]byte("[rtsp] Non-monotonous DTS\n"))
	}
	l.Write([]byte(big))
	l.Write([]byte("\nrtsp://view:hunter2@1.2.3.4/x: Server returned 401 Unauthorized (authorization failed)\n"))
	if !l.refused || strings.Contains(last, "hunter2") || !strings.Contains(last, "401") {
		t.Fatalf("refused=%v last=%q", l.refused, last)
	}
	if len(l.partial) > 4096 {
		t.Fatalf("kept %d bytes of an endless line", len(l.partial))
	}
}

func TestRefusedLoginsPauseTheCamera(t *testing.T) {
	r := &Recorder{}
	r.noteLogin("cam", true, 0)
	if w := r.blockedFor("cam"); w < 50*time.Second || w > time.Minute {
		t.Fatalf("first pause %v", w)
	}
	r.noteLogin("cam", true, 0)
	if w := r.blockedFor("cam"); w < 110*time.Second {
		t.Fatalf("second pause %v, want it doubled", w)
	}
	for i := 0; i < 10; i++ {
		r.noteLogin("cam", true, 0)
	}
	if w := r.blockedFor("cam"); w > maxLoginPause {
		t.Fatalf("pause %v above the limit", w)
	}
	if !r.Status()["cam"].LoginRefused {
		t.Fatal("status does not say the login was refused")
	}
	r.ClearLogin("cam")
	if r.Blocked("cam") || r.Status()["cam"].LoginRefused {
		t.Fatal("not cleared")
	}
}

func TestPinnedChecksTheAddressAtEveryStart(t *testing.T) {
	s := newTestStore(t)
	c, _ := s.Add(Camera{Name: "A", User: "v", MainURL: "rtsp://192.168.1.5:554/a"}, "p@ss")
	got, err := s.Pinned(context.Background(), c, false)
	if err != nil || got != "rtsp://v:p%40ss@192.168.1.5:554/a" {
		t.Fatalf("%q %v", got, err)
	}
	// A stored address that is (or became) outside the home network is refused.
	c.MainURL = "rtsp://127.0.0.1/a"
	if _, err := s.Pinned(context.Background(), c, false); err == nil {
		t.Fatal("loopback accepted")
	}
}
