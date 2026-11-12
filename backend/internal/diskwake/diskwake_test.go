package diskwake

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func recorder() (*Recorder, *time.Time) {
	now := time.Unix(1_800_000_000, 0)
	r := &Recorder{Now: func() time.Time { return now }}
	r.Enable(true)
	return r, &now
}

func TestRecentWindowAndOrder(t *testing.T) {
	r, now := recorder()
	start := *now
	r.Note("tank", Journal, "")
	*now = now.Add(10 * time.Second)
	r.Note("tank", BackupJob, "nightly")
	*now = now.Add(10 * time.Second)
	r.Note("tank", Journal, "") // the same cause again: one entry, new time
	r.Note("other", FileIndex, "")
	got := r.Recent("tank", start, *now)
	if len(got) != 2 || got[0] != (Cause{Code: Journal}) || got[1] != (Cause{Code: BackupJob, Detail: "nightly"}) {
		t.Fatalf("%+v", got)
	}
	if got := r.Recent("tank", start, start.Add(5*time.Second)); len(got) != 0 {
		t.Fatalf("journal was noted again later: %+v", got)
	}
	if got := r.Recent("tank", now.Add(time.Second), now.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("outside the window: %+v", got)
	}
}

func TestNotesBounded(t *testing.T) {
	r, now := recorder()
	for i := range 100 {
		*now = now.Add(time.Second)
		r.Note("tank", FileManager, fmt.Sprint("user", i))
	}
	got := r.Recent("tank", time.Time{}, *now)
	if len(got) != maxNotes || got[0].Detail != "user99" {
		t.Fatalf("%d notes, newest %+v", len(got), got[0])
	}
	for i := range 200 {
		r.Note(fmt.Sprint("volume", i), Journal, "")
	}
	if len(r.notes) != maxTargets {
		t.Fatalf("%d volumes kept", len(r.notes))
	}
	long := make([]byte, 400)
	for i := range long {
		long[i] = "ä"[i%2]
	}
	r.Note("tank", WebDAV, string(long))
	if got := r.Recent("tank", time.Time{}, *now)[0]; len(got.Detail) > maxDetail || got.Detail == "" {
		t.Fatalf("detail of %d bytes", len(got.Detail))
	}
}

func TestOffKeepsNothing(t *testing.T) {
	r := &Recorder{}
	r.Note("tank", Journal, "")
	if r.notes != nil {
		t.Fatal("noted while off")
	}
	r.Enable(true)
	r.Note("tank", Journal, "")
	r.Enable(false)
	if r.notes != nil {
		t.Fatal("switching off must forget")
	}
}

// Noting an access must never touch a disk: it runs right before every
// access to a volume, also to one that sleeps. The volume named here does
// not even exist; nothing may be created, opened or looked at.
func TestNoteDoesNoIO(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone")
	r, _ := recorder()
	old := Default
	Default = r
	defer func() { Default = old }()
	allocs := testing.AllocsPerRun(100, func() {
		Note("tank", ActivityLog, "")
		NoteFeature("tank", "logs")
		NotePath(MountRoot+"tank/Fotos/a.jpg", FileManager, "anna")
		NotePath(missing, FileManager, "anna") // not on a volume
	})
	if allocs > 0 {
		t.Fatalf("%v allocations for accesses that were already noted", allocs)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("noting created something: %v %v", entries, err)
	}
	if got := r.Recent("tank", time.Time{}, time.Unix(1_900_000_000, 0)); len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if len(r.notes) != 1 {
		t.Fatalf("a path outside /mnt was noted: %v", r.notes)
	}
}

func TestFeatureCause(t *testing.T) {
	for feature, want := range map[string]Cause{
		"logs": {Code: Journal}, "activity": {Code: ActivityLog}, "backups": {Code: ConfigBackup},
		"maintenance": {Code: Maintenance, Detail: "plan"}, "vmclones": {Code: PrivateData, Detail: "vmclones"},
	} {
		if got := FeatureCause(feature); got != want {
			t.Errorf("%s: %+v, want %+v", feature, got, want)
		}
	}
}
