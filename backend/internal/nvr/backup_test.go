package nvr

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFace(t *testing.T, dir, person, file string, size int) {
	t.Helper()
	folder := filepath.Join(dir, "media", "clips", "faces", person)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, file), []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFacesGoIntoTheBackup(t *testing.T) {
	r := newRig(t, true)
	r.up()
	if got := r.m.BackupFiles(); len(got) != 0 {
		t.Fatalf("no library yet: %v", got)
	}
	writeFace(t, r.dir, "Anna", "Anna-1.webp", 3000)
	writeFace(t, r.dir, "Anna", "Anna-2.webp", 100)
	writeFace(t, r.dir, "Ben", "Ben-1.webp", 200)
	// Unassigned faces, other files and odd names stay out.
	writeFace(t, r.dir, "train", "x-1-unknown-0.3.webp", 100)
	writeFace(t, r.dir, "Anna", "notes.txt", 100)
	writeFace(t, r.dir, "Anna", "huge.webp", maxBackupFaceFile+1)
	got := r.m.BackupFiles()
	names := []string{}
	for _, f := range got {
		names = append(names, f.Name)
	}
	want := []string{"volume-data/nvr/faces/Anna/Anna-1.webp", "volume-data/nvr/faces/Anna/Anna-2.webp", "volume-data/nvr/faces/Ben/Ben-1.webp"}
	if strings.Join(names, ",") != strings.Join(want, ",") || len(got[0].Data) != 3000 {
		t.Fatalf("%v", names)
	}
	// The disks sleep, or the volume is gone: the library as last read,
	// and nothing on the volume is touched.
	asleep := true
	r.m.Asleep = func(string) bool { return asleep }
	writeFace(t, r.dir, "Clara", "Clara-1.webp", 100)
	if got := r.m.BackupFiles(); len(got) != 3 {
		t.Errorf("sleeping volume was read: %d files", len(got))
	}
	asleep = false
	if got := r.m.BackupFiles(); len(got) != 4 {
		t.Errorf("awake: %d files", len(got))
	}
	// A library larger than a backup takes is left out, not cut.
	for i := range 7 {
		writeFace(t, r.dir, "Big", "Big-"+string(rune('a'+i))+".webp", maxBackupFaceFile)
	}
	if got := r.m.BackupFiles(); len(got) != 4 {
		t.Errorf("too large: %d files (the last good state is kept)", len(got))
	}
}

func TestRestoredFacesGoBackToTheVolume(t *testing.T) {
	r := newRig(t, true)
	// What a restore of /config wrote to the stick.
	for name, data := range map[string]string{
		"volume-data/nvr/faces/Anna/Anna-1.webp": "restored-1",
		"volume-data/nvr/faces/Anna/Anna-2.webp": "restored-2",
		"volume-data/nvr/faces/Ben/Ben-1.webp":   "restored-3",
		"volume-data/nvr/faces/../x/evil.webp":   "no",
		"volume-data/nvr/faces/train/t.webp":     "no",
	} {
		if strings.Contains(name, "..") {
			continue
		}
		if err := r.store.Write(name, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	// A picture beyond the limits of a backup is not taken.
	if err := r.store.Write("volume-data/nvr/faces/Ben/Ben-big.webp", make([]byte, maxBackupFaceFile+1)); err != nil {
		t.Fatal(err)
	}
	// One picture is on the volume already and stays as it is.
	writeFace(t, r.dir, "Anna", "Anna-1.webp", 5)
	r.m = r.manager(r.m.FrigateBase(""))
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	// Until the NVR runs the faces are still part of the next backup.
	if got := r.m.BackupFiles(); len(got) != 3 || string(got[0].Data) != "restored-1" {
		t.Fatalf("waiting faces: %d", len(got))
	}
	r.enable()
	r.look()
	read := func(person, file string) string {
		data, _ := os.ReadFile(filepath.Join(r.dir, "media", "clips", "faces", person, file))
		return string(data)
	}
	if read("Anna", "Anna-1.webp") != "xxxxx" || read("Anna", "Anna-2.webp") != "restored-2" || read("Ben", "Ben-1.webp") != "restored-3" {
		t.Errorf("library: %q %q %q", read("Anna", "Anna-1.webp"), read("Anna", "Anna-2.webp"), read("Ben", "Ben-1.webp"))
	}
	if _, err := os.Stat(filepath.Join(r.dir, "media", "clips", "faces", "train")); err == nil {
		t.Error("unassigned faces were restored")
	}
	// What was put back is off the stick, with the folders that are empty
	// now; what was not taken stays there (never removed unseen).
	if left, _ := r.store.List("volume-data/nvr/faces"); !slices.Equal(left, []string{"Ben", "train"}) {
		t.Errorf("left on the stick: %v", left)
	}
	if left, _ := r.store.List("volume-data/nvr/faces/Ben"); !slices.Equal(left, []string{"Ben-big.webp"}) {
		t.Errorf("left of Ben: %v", left)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "media", "clips", "faces", "Ben", "Ben-big.webp")); err == nil {
		t.Error("the picture beyond the limits was put back")
	}
	if got := r.m.BackupFiles(); len(got) != 3 || string(got[0].Data) != "xxxxx" {
		t.Errorf("after the import: %d", len(got))
	}
}

// Everything was put back: the stick keeps no empty folders of it.
func TestRestoredFacesLeaveNoFolders(t *testing.T) {
	r := newRig(t, true)
	if err := r.store.Write("volume-data/nvr/faces/Anna/Anna-1.webp", []byte("restored")); err != nil {
		t.Fatal(err)
	}
	r.m = r.manager(r.m.FrigateBase(""))
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	r.enable()
	r.look()
	if left, _ := r.store.List("volume-data"); len(left) != 0 {
		t.Errorf("left on the stick: %v", left)
	}
	if data, _ := os.ReadFile(filepath.Join(r.dir, "media", "clips", "faces", "Anna", "Anna-1.webp")); string(data) != "restored" {
		t.Errorf("library: %q", data)
	}
}
