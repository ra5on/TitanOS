package nvr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOnEventNotifiesOnlyWhenArmedAndNotTooOften(t *testing.T) {
	s := newTestStore(t)
	m := &Manager{Store: s}
	var got []string
	m.Notify = func(sev, kind, subject string) { got = append(got, kind+":"+subject) }
	cam := Camera{ID: "cam-1", Name: "Hof"}
	start := Event{Camera: "cam-1", Kind: "motion", Start: time.Now()}

	m.onEvent(cam, start) // not armed
	if len(got) != 0 {
		t.Fatalf("notified while disarmed: %v", got)
	}
	st := s.Settings()
	st.Arming = Arming{Mode: "on"}
	if err := s.SetSettings(st); err != nil {
		t.Fatal(err)
	}
	m.onEvent(cam, start)
	m.onEvent(cam, start) // within five minutes
	end := start
	end.End = time.Now()
	m.onEvent(cam, end) // the end of an event is no news
	if len(got) != 1 || got[0] != "nvr_motion:Hof" {
		t.Fatalf("notifications: %v", got)
	}
	m.onEvent(Camera{ID: "cam-2", Name: "Garten"}, start) // another camera has its own limit
	if len(got) != 2 {
		t.Fatalf("notifications: %v", got)
	}
}

func TestExpiryRunsWhenOffOnFormerVolumesAndForRemovedCameras(t *testing.T) {
	vols := map[string]string{"a": t.TempDir(), "b": t.TempDir()}
	s := newTestStore(t)
	m := &Manager{Store: s, VolumePath: func(n string) (string, bool) { p, ok := vols[n]; return p, ok }}
	now := time.Now()
	old := now.AddDate(0, 0, -30)
	for _, v := range []string{"a", "b"} {
		root := filepath.Join(vols[v], "nvr")
		os.MkdirAll(root, 0o755)
		os.WriteFile(filepath.Join(root, Marker), nil, 0o644)
		writeSeg(t, root, "cam-x", old, 10)
		writeSeg(t, root, "cam-x", old.Add(time.Hour), 10)
		evAt := now.AddDate(0, 0, -40) // beyond the 30 days of events
		WriteEvent(root, Event{ID: eventID(evAt), Camera: "cam-gone", Kind: "motion", Start: evAt})
	}
	// First on volume a, then moved to b; the NVR is switched off.
	if err := s.SetSettings(Settings{Volume: "a", KeepDays: 7}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(Settings{Volume: "b", KeepDays: 7}); err != nil {
		t.Fatal(err)
	}
	if got := s.Settings().FormerVolumes; len(got) != 1 || got[0] != "a" {
		t.Fatalf("former volumes: %v", got)
	}
	m.Prune(now)
	for _, v := range []string{"a", "b"} {
		root := filepath.Join(vols[v], "nvr")
		if n := len(segments(root, "cam-x")); n != 0 {
			t.Errorf("volume %s keeps %d expired recordings", v, n)
		}
		if n := len(Events(root, "cam-gone", 0)); n != 0 {
			t.Errorf("volume %s keeps events of a removed camera", v)
		}
	}
}

func TestOwnFolderNamedNvrIsNotTakenOver(t *testing.T) {
	vol := t.TempDir()
	os.MkdirAll(filepath.Join(vol, "nvr"), 0o755)
	os.WriteFile(filepath.Join(vol, "nvr", "mine.txt"), []byte("x"), 0o644)
	s := newTestStore(t)
	s.SetSettings(Settings{Volume: "v", KeepDays: 7})
	m := &Manager{Store: s, VolumePath: func(string) (string, bool) { return vol, true }}
	if m.Root() != "" {
		t.Fatal("an own folder called nvr was taken over")
	}
	// Empty or marked folders are fine.
	os.Remove(filepath.Join(vol, "nvr", "mine.txt"))
	if m.Root() == "" {
		t.Fatal("empty folder refused")
	}
}
