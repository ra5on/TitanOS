package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrashRestore(t *testing.T) {
	m, root, _ := newRoot(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	if err := m.Delete(root, []string{"urlaub"}, "alex", false); err != nil {
		t.Fatal(err)
	}

	entries, err := m.TrashList(root, "alex", "alex", false)
	if err != nil || names(entries) != "urlaub" {
		t.Fatalf("bin of alex = %q, %v", names(entries), err)
	}
	// Someone else's part of the bin, and the bin as a whole, are not there
	// for accounts that do not administer.
	if _, err := m.TrashList(root, "alex", "sam", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("sam lists the bin of alex: %v", err)
	}
	if _, err := m.TrashList(root, "", "sam", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("sam lists the whole bin: %v", err)
	}
	if err := m.TrashRestore(root, []string{"alex/urlaub"}, "sam", os.Getuid(), false); !errors.Is(err, ErrNotFound) {
		t.Errorf("sam restores for alex: %v", err)
	}
	if entries, err := m.TrashList(root, "", "admin", true); err != nil || names(entries) != "alex" {
		t.Errorf("whole bin for an administrator = %q, %v", names(entries), err)
	}
	// An account that never deleted anything has an empty bin.
	if entries, err := m.TrashList(root, "sam", "sam", false); err != nil || len(entries) != 0 {
		t.Errorf("empty bin = %q, %v", names(entries), err)
	}

	if err := m.TrashRestore(root, []string{"alex/urlaub"}, "alex", os.Getuid(), false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root.Dir, "urlaub/strand.jpg")); err != nil || string(data) != "strand" {
		t.Fatalf("restored file = %q, %v", data, err)
	}
	// Leftover folders of the bin go away with what was taken out.
	if _, err := os.Stat(filepath.Join(root.Dir, ".recycle/alex")); !os.IsNotExist(err) {
		t.Errorf("empty folder of alex stays in the bin: %v", err)
	}
}

func TestTrashRestoreSingleFile(t *testing.T) {
	m, root, _ := newRoot(t)
	m.now = func() time.Time { return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) }
	if err := m.Delete(root, []string{"urlaub/strand.jpg"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	// The folder it was in is gone, and another file has its name now.
	os.Remove(filepath.Join(root.Dir, "urlaub"))
	if err := m.TrashRestore(root, []string{"alex/urlaub/strand.jpg"}, "alex", os.Getuid(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "urlaub/strand.jpg")); err != nil {
		t.Fatalf("file not back in a new folder: %v", err)
	}

	m.Delete(root, []string{"notiz.txt"}, "alex", false)
	os.WriteFile(filepath.Join(root.Dir, "notiz.txt"), []byte("neu"), 0o644)
	if err := m.TrashRestore(root, []string{"alex/notiz.txt"}, "alex", os.Getuid(), false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "notiz.txt")); string(data) != "neu" {
		t.Error("restoring replaced the file that is there now")
	}
	if data, err := os.ReadFile(filepath.Join(root.Dir, "notiz (2026-05-01 12.00.00).txt")); err != nil || string(data) != "notiz" {
		t.Errorf("restored file with the time in its name = %q, %v", data, err)
	}
}

func TestTrashDelete(t *testing.T) {
	m, root, _ := newRoot(t)
	m.Delete(root, []string{"urlaub", "notiz.txt"}, "alex", false)

	if err := m.TrashDelete(root, []string{"alex/notiz.txt"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	if entries, _ := m.TrashList(root, "alex", "alex", false); names(entries) != "urlaub" {
		t.Errorf("bin after deleting one file = %q", names(entries))
	}
	if err := m.TrashDelete(root, []string{""}, "admin", true); !errors.Is(err, ErrInvalid) {
		t.Errorf("deleting the whole bin at once: %v", err)
	}
	if err := m.TrashDelete(root, []string{"../notiz.txt"}, "alex", false); err == nil {
		t.Error("a path out of the bin was accepted")
	}
	// Emptying is deleting the account's part.
	if err := m.TrashDelete(root, []string{"alex"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, ".recycle/alex")); !os.IsNotExist(err) {
		t.Errorf("bin of alex not emptied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "urlaub")); !os.IsNotExist(err) {
		t.Errorf("emptying touched the share: %v", err)
	}

	root.Writable = false
	if err := m.TrashDelete(root, []string{"alex"}, "alex", false); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only account empties the bin: %v", err)
	}
}

// The folders the file manager makes in the recycle bin are writable for
// the group of the shares: Samba works as the account and deletes a file
// for good when it cannot move it into the bin. Folders an older version
// left writable for root only are repaired.
func TestRecycleBinFoldersAreGroupWritable(t *testing.T) {
	m, root, _ := newRoot(t)
	// Left behind by an older version.
	stale := filepath.Join(root.Dir, ".recycle")
	if err := os.Mkdir(stale, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(root, []string{"urlaub/strand.jpg"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".recycle", ".recycle/alex", ".recycle/alex/urlaub"} {
		info, err := os.Stat(filepath.Join(root.Dir, dir))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o770 || info.Mode()&os.ModeSetgid == 0 {
			t.Errorf("%s has mode %v, want group-writable with setgid", dir, info.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(root.Dir, ".recycle/alex/urlaub/strand.jpg")); err != nil {
		t.Errorf("the deleted file is not in the bin: %v", err)
	}
}

// A copy that finishes after another file took its name (another job, an
// upload, a save over SMB) does not replace that file.
func TestFinishedCopyDoesNotReplaceAFileThatAppeared(t *testing.T) {
	m, root, _ := newRoot(t)
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	os.WriteFile(filepath.Join(root.Dir, ".film.part"), []byte("copy"), 0o644)
	os.WriteFile(filepath.Join(root.Dir, "film.mkv"), []byte("arrived meanwhile"), 0o644)
	final, err := m.publishCopy(&Job{}, r, "film.mkv", ".film.part", "film.mkv", false)
	if err != nil || final != "film (2).mkv" {
		t.Fatalf("published as %q, %v", final, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "film.mkv")); string(data) != "arrived meanwhile" {
		t.Fatalf("the file that appeared was replaced: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, final)); string(data) != "copy" {
		t.Fatalf("the copy is not there: %q", data)
	}
}
