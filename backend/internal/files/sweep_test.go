package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An upload nobody finishes is removed a day later even when titand was
// restarted in between and no longer knows it.
func TestSweepRemovesUploadsForgottenByRestart(t *testing.T) {
	m, root, base := newRoot(t)
	m.MountRoot = filepath.Dir(base)
	root.Volume = filepath.Base(base)
	os.MkdirAll(filepath.Join(root.Dir, "urlaub/neu"), 0o755)
	stale, err := m.StartUpload(root, "urlaub/neu", "film.mkv", 6, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.WriteChunk(stale.ID, "alex", 0, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	fresh, err := m.StartUpload(root, "", "iso", 6, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	done, err := m.StartUpload(root, "", "fertig", 1, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	m.WriteChunk(done.ID, "alex", 0, strings.NewReader("x"))
	if _, err = m.FinishUpload(done.ID, "alex"); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(base, uploadNotes)
	if _, err = os.Stat(filepath.Join(notes, done.ID)); !os.IsNotExist(err) {
		t.Fatal("finished upload kept its note")
	}
	stalePart := filepath.Join(root.Dir, uploadPartName("urlaub/neu/film.mkv", stale.ID))
	freshPart := filepath.Join(root.Dir, uploadPartName("iso", fresh.ID))
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(stalePart, old, old)

	// A restart: the same volume, a manager that knows nothing.
	restarted := NewManager(os.Getgid())
	restarted.MountRoot = m.MountRoot
	m.SweepUploads([]string{root.Volume})
	if _, err = os.Stat(stalePart); err != nil {
		t.Fatal("removed an upload titand still knows")
	}
	restarted.SweepUploads([]string{"..", root.Volume})
	for _, gone := range []string{stalePart, stalePart + ".json", filepath.Join(notes, stale.ID)} {
		if _, err = os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s survived the sweep", gone)
		}
	}
	for _, kept := range []string{freshPart, freshPart + ".json", filepath.Join(notes, fresh.ID), filepath.Join(root.Dir, "fertig"), filepath.Join(root.Dir, "notiz.txt")} {
		if _, err = os.Stat(kept); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
}

// A note can never make the sweep delete something that is not a part file.
func TestSweepIgnoresForgedNotes(t *testing.T) {
	m, root, base := newRoot(t)
	m.MountRoot = filepath.Dir(base)
	notes := filepath.Join(base, uploadNotes)
	os.MkdirAll(notes, 0o700)
	old := time.Now().Add(-48 * time.Hour)
	victim := filepath.Join(root.Dir, "notiz.txt")
	os.Chmod(victim, 0o600)
	os.Chtimes(victim, old, old)
	for id, target := range map[string]string{
		"0123456789abcdef01234567": "share/notiz.txt",
		"1123456789abcdef01234567": "../geheim.txt",
		"2123456789abcdef01234567": "share/.x.2123456789abcdef01234567.titanos-part/../notiz.txt",
	} {
		os.WriteFile(filepath.Join(notes, id), []byte(target), 0o600)
	}
	m.SweepUploads([]string{filepath.Base(base)})
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("forged note deleted a file")
	}
	if _, err := os.Stat(filepath.Join(base, "geheim.txt")); err != nil {
		t.Fatal("forged note escaped the volume")
	}
}
