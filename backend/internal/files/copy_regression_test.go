package files

// Regression tests for the copy/move review of 4.11.2026 (findings F1–F14
// and the owner decisions of that day). Each test names its finding.

import (
	"context"
	"fmt"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// bothKinds runs a test with a RAM job (USB target) and a durable job
// (target on a data volume).
func bothKinds(t *testing.T, fn func(t *testing.T, m *Manager, src, dst Root)) {
	t.Run("ram", func(t *testing.T) { m, src, dst := twoRoots(t); fn(t, m, src, dst) })
	t.Run("durable", func(t *testing.T) { m, src, dst := durableRoots(t); fn(t, m, src, dst) })
}

// F1: an unreadable file is listed with its reason; the others are copied,
// and a retry after fixing it copies only that file.
func TestCopyGoesOnAfterAnUnreadableFile(t *testing.T) {
	skipIfRoot(t)
	bothKinds(t, func(t *testing.T, m *Manager, src, dst Root) {
		for _, n := range []string{"1", "2", "3"} {
			put(t, filepath.Join(src.Dir, "e", n+".jpg"), n)
		}
		locked := filepath.Join(src.Dir, "e/2.jpg")
		os.Chmod(locked, 0)
		defer os.Chmod(locked, 0o644)
		job, err := m.StartCopy(src, []string{"e"}, dst, ".", false, ConflictRename, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || job.Failed != 1 || problemCodes(job)["e/2.jpg"] != "permission" || !job.Retryable {
			t.Fatalf("job = %+v", job)
		}
		if read(t, filepath.Join(dst.Dir, "e/1.jpg")) != "1" || read(t, filepath.Join(dst.Dir, "e/3.jpg")) != "3" || exists(filepath.Join(dst.Dir, "e/2.jpg")) {
			t.Fatal(tree(dst.Dir))
		}
		os.Chmod(locked, 0o644)
		if _, err := m.RetryCopy("alex", job.ID, src, dst, os.Getuid()); err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || job.Failed != 0 || len(job.Problems) != 0 || read(t, filepath.Join(dst.Dir, "e/2.jpg")) != "2" {
			t.Fatalf("retry = %+v, %s", job, tree(dst.Dir))
		}
		if exists(filepath.Join(dst.Dir, "e/1 (2).jpg")) {
			t.Fatal("the retry copied a finished file again")
		}
	})
}

// F1/F10: system errors get their own codes (FAT32 size limit, names a
// drive refuses, input/output errors).
func TestCopyProblemCodes(t *testing.T) {
	for errno, want := range map[syscall.Errno]string{syscall.EFBIG: "too_large", syscall.EINVAL: "name_not_allowed", syscall.EIO: "write_error"} {
		m, src, dst := twoRoots(t)
		m.syncFile = func(*os.File) error { return &fs.PathError{Op: "sync", Path: "x", Err: errno} }
		job, err := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", false, ConflictRename, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || problemCodes(job)["notiz.txt"] != want {
			t.Errorf("%v: %+v", errno, job.Problems)
		}
		if entries, _ := os.ReadDir(filepath.Join(dst.Dir, "ziel")); len(entries) != 0 {
			t.Errorf("%v left %v", errno, entries)
		}
	}
	// No space ends the whole job with its own code.
	m, src, dst := twoRoots(t)
	m.syncFile = func(*os.File) error { return &fs.PathError{Op: "sync", Path: "x", Err: syscall.ENOSPC} }
	job, _ := m.StartCopy(src, []string{"notiz.txt", "urlaub"}, dst, "ziel", false, ConflictRename, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobFailed || job.ErrorCode != "no_space" {
		t.Fatalf("no space: %+v", job)
	}
}

// F1: a symbolic link in a folder moved across roots stays at the source,
// is listed, and does not stop the move of the other files (durable job).
func TestDurableMoveKeepsLinksAndMovesTheRest(t *testing.T) {
	m, src, dst := durableRoots(t)
	put(t, filepath.Join(src.Dir, "g/a.txt"), "a")
	os.Symlink("a.txt", filepath.Join(src.Dir, "g/link"))
	put(t, filepath.Join(src.Dir, "g/z.txt"), "z")
	job, err := m.StartCopy(src, []string{"g"}, dst, ".", true, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone || job.Failed != 0 || job.Kept != 1 || problemCodes(job)["g/link"] != "kept_link" {
		t.Fatalf("job = %+v", job)
	}
	if exists(filepath.Join(src.Dir, "g/a.txt")) || exists(filepath.Join(src.Dir, "g/z.txt")) || !exists(filepath.Join(src.Dir, "g/link")) {
		t.Fatal(tree(src.Dir))
	}
	if read(t, filepath.Join(dst.Dir, "g/z.txt")) != "z" {
		t.Fatal(tree(dst.Dir))
	}
}

// F1/F9: a part file left by an interrupted copy no longer blocks a move.
// An old one is removed with its folder, a recent one is listed as kept.
func TestMoveWithLeftoverPartFile(t *testing.T) {
	for _, old := range []bool{true, false} {
		m, src, dst := durableRoots(t)
		put(t, filepath.Join(src.Dir, "p/a.jpg"), "a")
		part := filepath.Join(src.Dir, "p/.titanos-copy-0123456789ab.titanos-part")
		put(t, part, "leftover")
		os.Chmod(part, 0o600)
		if old {
			past := time.Now().Add(-48 * time.Hour)
			os.Chtimes(part, past, past)
		}
		job, err := m.StartCopy(src, []string{"p"}, dst, ".", true, ConflictRename, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || job.Failed != 0 || read(t, filepath.Join(dst.Dir, "p/a.jpg")) != "a" {
			t.Fatalf("old=%v: %+v", old, job)
		}
		if old && (exists(filepath.Join(src.Dir, "p")) || job.Kept != 0) {
			t.Fatalf("stale part kept: %s %+v", tree(src.Dir), job)
		}
		if !old && (!exists(part) || problemCodes(job)["p/.titanos-copy-0123456789ab.titanos-part"] != "kept_internal") {
			t.Fatalf("recent part: %s %+v", tree(src.Dir), job)
		}
	}
}

// F2: files added or changed at the source after planning are copied as
// they are now; a durable job no longer fails on them.
func TestDurableCopyFollowsSourceChanges(t *testing.T) {
	m, src, dst := durableRoots(t)
	put(t, filepath.Join(src.Dir, "a/1.txt"), "one")
	put(t, filepath.Join(src.Dir, "a/z/3.txt"), "three")
	first := true
	m.syncFile = func(f *os.File) error {
		if first {
			first = false
			put(t, filepath.Join(src.Dir, "a/z/2.txt"), "new")
			put(t, filepath.Join(src.Dir, "a/z/3.txt"), "three, edited")
		}
		return f.Sync()
	}
	job, err := m.StartCopy(src, []string{"a"}, dst, ".", false, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone || job.Failed != 0 || job.Files != 3 {
		t.Fatalf("job = %+v", job)
	}
	if read(t, filepath.Join(dst.Dir, "a/z/2.txt")) != "new" || read(t, filepath.Join(dst.Dir, "a/z/3.txt")) != "three, edited" {
		t.Fatal(tree(dst.Dir))
	}
}

// F2: a retry is not refused because the source's change time moved (a
// chmod, a new hard link) or a copied target file was edited meanwhile.
func TestDurableRetryAfterMetadataChanges(t *testing.T) {
	m, src, dst := durableRoots(t)
	put(t, filepath.Join(src.Dir, "a/1.txt"), "one")
	put(t, filepath.Join(src.Dir, "a/2.txt"), "two")
	calls := 0
	m.syncFile = func(f *os.File) error {
		if calls++; calls == 2 {
			return errors.New("injected failure")
		}
		return f.Sync()
	}
	job, _ := m.StartCopy(src, []string{"a"}, dst, ".", false, ConflictRename, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.Failed != 1 {
		t.Fatalf("job = %+v", job)
	}
	os.Chmod(filepath.Join(src.Dir, "a/2.txt"), 0o640)
	os.Link(filepath.Join(src.Dir, "a/1.txt"), filepath.Join(src.Dir, "hardlink"))
	put(t, filepath.Join(dst.Dir, "a/1.txt"), "edited at the target")
	if _, err := m.RetryCopy("alex", job.ID, src, dst, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobDone || job.Failed != 0 || read(t, filepath.Join(dst.Dir, "a/2.txt")) != "two" {
		t.Fatalf("retry = %+v", job)
	}
	if read(t, filepath.Join(dst.Dir, "a/1.txt")) != "edited at the target" {
		t.Fatal("the retry touched a finished file")
	}
}

// F2: after a restart of the NAS a saved job is not offered for a retry;
// it says so, and copying the missing files is the way on.
func TestSavedJobAfterRebootIsNotRetryable(t *testing.T) {
	m, src, dst := durableRoots(t)
	put(t, filepath.Join(src.Dir, "a/1.txt"), "one")
	put(t, filepath.Join(src.Dir, "a/2.txt"), "two")
	calls := 0
	m.syncFile = func(f *os.File) error {
		if calls++; calls == 2 {
			return errors.New("injected failure")
		}
		return f.Sync()
	}
	job, _ := m.StartCopy(src, []string{"a"}, dst, ".", false, ConflictRename, "alex", os.Getuid())
	job = finish(t, m, job.ID)
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	var descriptor map[string]any
	if err := volumedata.ReadJSON(root, job.ID+".json", &descriptor, 1<<20); err != nil {
		t.Fatal(err)
	}
	descriptor["boot"] = "an-earlier-boot"
	if err := volumedata.WriteJSON(root, job.ID+".json", descriptor); err != nil {
		t.Fatal(err)
	}
	root.Close()
	restarted := NewManager(os.Getgid())
	restarted.MountRoot, restarted.Mounted = m.MountRoot, m.Mounted
	jobs := restarted.Jobs("alex")
	if len(jobs) != 1 || !jobs[0].Rebooted || jobs[0].Retryable || jobs[0].Failed != 1 {
		t.Fatalf("after reboot: %+v", jobs)
	}
	if _, err := restarted.RetryCopy("alex", job.ID, src, dst, os.Getuid()); !errors.Is(err, ErrRebooted) {
		t.Fatal("retry after reboot", err)
	}
	// The way on: copy what is missing.
	again, err := restarted.StartCopy(src, []string{"a"}, dst, ".", false, ConflictMissing, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if again = finishJob(t, restarted, "alex", again.ID); again.State != JobDone || again.Identical != 1 || read(t, filepath.Join(dst.Dir, "a/2.txt")) != "two" {
		t.Fatalf("missing files: %+v %s", again, tree(dst.Dir))
	}
	if exists(filepath.Join(dst.Dir, "a/1 (2).txt")) {
		t.Fatal("duplicate")
	}
}

// F3 / decision 2: "missing or changed" skips identical files, replaces
// changed ones and copies new ones, without "(2)" duplicates; inspect counts
// the identical files.
func TestMissingOrChangedMode(t *testing.T) {
	bothKinds(t, func(t *testing.T, m *Manager, src, dst Root) {
		put(t, filepath.Join(src.Dir, "urlaub/same.jpg"), "same")
		put(t, filepath.Join(src.Dir, "urlaub/changed.jpg"), "new version")
		put(t, filepath.Join(src.Dir, "urlaub/new.jpg"), "new")
		put(t, filepath.Join(dst.Dir, "urlaub/same.jpg"), "same")
		put(t, filepath.Join(dst.Dir, "urlaub/changed.jpg"), "old")
		info, _ := os.Stat(filepath.Join(src.Dir, "urlaub/same.jpg"))
		// FAT keeps two-second steps: one second apart is still the same.
		os.Chtimes(filepath.Join(dst.Dir, "urlaub/same.jpg"), info.ModTime(), info.ModTime().Add(time.Second))
		inspection, err := m.Inspect(context.Background(), src, []string{"urlaub"}, &dst, ".")
		if err != nil || inspection.Identical != 1 || inspection.Different != 1 || inspection.DifferentBytes != int64(len("new version")) {
			t.Fatalf("inspect = %+v %v", inspection, err)
		}
		job, err := m.StartCopy(src, []string{"urlaub"}, dst, ".", false, ConflictMissing, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || job.Identical != 1 || job.Failed != 0 {
			t.Fatalf("job = %+v", job)
		}
		if read(t, filepath.Join(dst.Dir, "urlaub/changed.jpg")) != "new version" || read(t, filepath.Join(dst.Dir, "urlaub/new.jpg")) != "new" {
			t.Fatal(tree(dst.Dir))
		}
		if strings.Contains(tree(dst.Dir), "(2)") {
			t.Fatal("duplicates:", tree(dst.Dir))
		}
	})
}

// F4: the free-space check counts only bytes that will really be written.
func TestFreeSpaceCountsOnlyWhatIsWritten(t *testing.T) {
	m, src, dst := durableRoots(t)
	_, free := Usage(dst)
	size := int64(free) + 1<<30
	for _, p := range []string{filepath.Join(src.Dir, "d/big"), filepath.Join(dst.Dir, "d/big")} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		f, _ := os.Create(p)
		if err := f.Truncate(size); err != nil {
			f.Close()
			t.Skip(err)
		}
		f.Close()
		os.Chtimes(p, time.Unix(1700000000, 0), time.Unix(1700000000, 0))
	}
	put(t, filepath.Join(src.Dir, "d/small"), "missing at target")
	for _, mode := range []string{ConflictSkip, ConflictMissing} {
		job, err := m.StartCopy(src, []string{"d"}, dst, ".", false, mode, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		if job = finish(t, m, job.ID); job.State != JobDone || read(t, filepath.Join(dst.Dir, "d/small")) != "missing at target" {
			t.Fatalf("%s: %+v", mode, job)
		}
		os.Remove(filepath.Join(dst.Dir, "d/small"))
	}
	job, _ := m.StartCopy(src, []string{"d"}, dst, ".", false, ConflictReplace, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobFailed || job.ErrorCode != "no_space" {
		t.Fatalf("replace needs the room: %+v", job)
	}
}

// F5: a move with an individual "skip" for a file inside a folder leaves
// that file at the source and moves the rest.
func TestMoveWithNestedSkipChoice(t *testing.T) {
	bothKinds(t, func(t *testing.T, m *Manager, src, dst Root) {
		os.MkdirAll(filepath.Join(dst.Dir, "ziel"), 0o755)
		put(t, filepath.Join(src.Dir, "f/keep.txt"), "src")
		put(t, filepath.Join(src.Dir, "f/new.txt"), "new")
		put(t, filepath.Join(dst.Dir, "ziel/f/keep.txt"), "existing")
		job, err := m.StartCopyChoices(src, []string{"f"}, dst, "ziel", true, ConflictSkip, "alex", os.Getuid(), map[string]CopyChoice{"f/keep.txt": {Action: "skip"}})
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || job.Failed != 0 || problemCodes(job)["f/keep.txt"] != "kept_skipped" {
			t.Fatalf("job = %+v", job)
		}
		if read(t, filepath.Join(src.Dir, "f/keep.txt")) != "src" || exists(filepath.Join(src.Dir, "f/new.txt")) {
			t.Fatal(tree(src.Dir))
		}
		if read(t, filepath.Join(dst.Dir, "ziel/f/keep.txt")) != "existing" || read(t, filepath.Join(dst.Dir, "ziel/f/new.txt")) != "new" {
			t.Fatal(tree(dst.Dir))
		}
	})
}

// F6: global replace where a folder meets a file (either way round) lists
// the entry and goes on with the rest.
func TestReplaceWithTypeMismatchGoesOn(t *testing.T) {
	bothKinds(t, func(t *testing.T, m *Manager, src, dst Root) {
		os.MkdirAll(filepath.Join(dst.Dir, "ziel/c/x"), 0o755)
		put(t, filepath.Join(src.Dir, "c/x"), "x-file")
		put(t, filepath.Join(src.Dir, "c/y"), "y-file")
		put(t, filepath.Join(src.Dir, "b/x.txt"), "x")
		put(t, filepath.Join(dst.Dir, "ziel/b"), "i am a file")
		job, err := m.StartCopy(src, []string{"b", "c"}, dst, "ziel", false, ConflictReplace, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		codes := problemCodes(job)
		if job.State != JobDone || job.Failed != 2 || codes["b"] != "type_mismatch" || codes["c/x"] != "type_mismatch" {
			t.Fatalf("job = %+v", job)
		}
		if read(t, filepath.Join(dst.Dir, "ziel/c/y")) != "y-file" || read(t, filepath.Join(dst.Dir, "ziel/b")) != "i am a file" {
			t.Fatal(tree(dst.Dir))
		}
	})
}

// F7: an unreadable folder does not fail the inspection; conflicts are
// counted beyond the 200 listed ones.
func TestInspectUnreadableFolderAndCountsBeyondTheList(t *testing.T) {
	m, src, dst := durableRoots(t)
	for i := range 205 {
		name := filepath.Join("k", fmt.Sprintf("f%03d", i))
		put(t, filepath.Join(src.Dir, name), "x")
		put(t, filepath.Join(dst.Dir, name), "x")
		stamp := time.Unix(1700000000, 0)
		os.Chtimes(filepath.Join(src.Dir, name), stamp, stamp)
		os.Chtimes(filepath.Join(dst.Dir, name), stamp, stamp)
	}
	result, err := m.Inspect(context.Background(), src, []string{"k"}, &dst, ".")
	if err != nil || len(result.Conflicts) != 200 || !result.Truncated || result.Identical != 205 {
		t.Fatalf("%v %d %v %d", err, len(result.Conflicts), result.Truncated, result.Identical)
	}
	if os.Geteuid() == 0 {
		return
	}
	put(t, filepath.Join(src.Dir, "k/priv/x"), "x")
	os.Chmod(filepath.Join(src.Dir, "k/priv"), 0)
	defer os.Chmod(filepath.Join(src.Dir, "k/priv"), 0o755)
	result, err = m.Inspect(context.Background(), src, []string{"k"}, &dst, ".")
	if err != nil || result.UnreadableCount != 1 || result.Unreadable[0] != "k/priv" {
		t.Fatalf("unreadable: %v %+v", err, result.Unreadable)
	}
	job, _ := m.StartCopy(src, []string{"k"}, dst, ".", false, ConflictMissing, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobDone || problemCodes(job)["k/priv"] != "unreadable_folder" {
		t.Fatalf("job = %+v", job)
	}
}

// F8: moving a folder within a share into a folder that has one of the same
// name merges them for every rule, also with individual choices.
func TestSameShareMoveMergesFolders(t *testing.T) {
	for _, mode := range []string{ConflictReplace, ConflictRename, ConflictSkip, ConflictMissing} {
		m, src, _ := durableRoots(t)
		put(t, filepath.Join(src.Dir, "q/new.jpg"), "new")
		put(t, filepath.Join(src.Dir, "q/both.jpg"), "incoming!")
		put(t, filepath.Join(src.Dir, "ziel/q/old.jpg"), "old")
		put(t, filepath.Join(src.Dir, "ziel/q/both.jpg"), "existing")
		job, err := m.StartCopy(src, []string{"q"}, src, "ziel", true, mode, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || job.Failed != 0 || read(t, filepath.Join(src.Dir, "ziel/q/new.jpg")) != "new" || read(t, filepath.Join(src.Dir, "ziel/q/old.jpg")) != "old" {
			t.Fatalf("%s: %+v %s", mode, job, tree(src.Dir))
		}
		both := read(t, filepath.Join(src.Dir, "ziel/q/both.jpg"))
		switch mode {
		case ConflictReplace, ConflictMissing:
			if both != "incoming!" || exists(filepath.Join(src.Dir, "q")) {
				t.Fatalf("%s: %s", mode, tree(src.Dir))
			}
		case ConflictRename:
			if both != "existing" || read(t, filepath.Join(src.Dir, "ziel/q/both (2).jpg")) != "incoming!" || exists(filepath.Join(src.Dir, "q")) {
				t.Fatalf("%s: %s", mode, tree(src.Dir))
			}
		case ConflictSkip:
			if both != "existing" || read(t, filepath.Join(src.Dir, "q/both.jpg")) != "incoming!" || job.Kept != 1 {
				t.Fatalf("%s: %+v %s", mode, job, tree(src.Dir))
			}
		}
	}
	// Individual choices keep the rename path.
	m, src, _ := twoRoots(t)
	put(t, filepath.Join(src.Dir, "ziel/urlaub/strand.jpg"), "existing")
	info, _ := os.Lstat(filepath.Join(src.Dir, "urlaub/strand.jpg"))
	job, err := m.StartCopyChoices(src, []string{"urlaub"}, src, "ziel", true, ConflictSkip, "alex", os.Getuid(), map[string]CopyChoice{"urlaub/strand.jpg": {Action: "rename", Name: "strand-2.jpg", SourceVersion: entryVersion(info), TargetVersion: "absent"}})
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobDone || job.Failed != 0 || read(t, filepath.Join(src.Dir, "ziel/urlaub/strand-2.jpg")) != "strand" || exists(filepath.Join(src.Dir, "urlaub")) {
		t.Fatalf("choices: %+v %s", job, tree(src.Dir))
	}
	moved, _ := os.Lstat(filepath.Join(src.Dir, "ziel/urlaub/strand-2.jpg"))
	if !os.SameFile(info, moved) {
		t.Fatal("individual choices copied instead of renaming")
	}
}

// F9: a part file of a copy interrupted by a crash is swept after a day,
// unless a running job holds it.
func TestSweepRemovesStaleCopyParts(t *testing.T) {
	m, _, dst := durableRoots(t)
	id := randomID()
	part := "ziel/.titanos-copy-" + id + partSuffix
	put(t, filepath.Join(dst.Dir, part), "half")
	os.Chmod(filepath.Join(dst.Dir, part), 0o600)
	m.holdCopyPart(dst, id, part)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(dst.Dir, part), past, past)
	m.SweepUploads([]string{"data"})
	if !exists(filepath.Join(dst.Dir, part)) {
		t.Fatal("swept the part of a running copy")
	}
	// After a crash nobody holds it any more.
	restarted := NewManager(os.Getgid())
	restarted.MountRoot = m.MountRoot
	restarted.SweepUploads([]string{"data"})
	if exists(filepath.Join(dst.Dir, part)) {
		t.Fatal("stale copy part kept")
	}
}

// F10: the stick is refused as a copy source with its own error, not as a
// virtual machine file.
func TestCopyFromStickHasItsOwnError(t *testing.T) {
	m, src, dst := twoRoots(t)
	src.Hide = []string{"users"}
	if _, err := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", false, "", "alex", os.Getuid()); !errors.Is(err, ErrSourceProtected) {
		t.Fatal(err)
	}
}

// F11: the request returns before the walk; the job list answers while a
// big plan is being made.
func TestPlanningRunsInTheJob(t *testing.T) {
	m, src, dst := durableRoots(t)
	put(t, filepath.Join(src.Dir, "a/1.txt"), "one")
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	m.planHook = func(string) {
		entered <- struct{}{}
		<-release
	}
	job, err := m.StartCopy(src, []string{"a"}, dst, ".", false, "", "alex", os.Getuid())
	if err != nil || !job.Planning {
		t.Fatal(job, err)
	}
	<-entered
	listed := make(chan []Job)
	go func() { listed <- m.Jobs("alex") }()
	select {
	case jobs := <-listed:
		if len(jobs) != 1 || !jobs[0].Planning {
			t.Fatal(jobs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job list waited for the planning walk")
	}
	close(release)
	if job = finish(t, m, job.ID); job.State != JobDone || job.Files != 1 {
		t.Fatal(job)
	}
}

// F13: copying a folder or file into its own folder with "replace" says
// what is wrong; "keep both" makes a copy.
func TestCopyIntoItsOwnFolder(t *testing.T) {
	m, src, _ := twoRoots(t)
	for _, p := range []string{"urlaub", "notiz.txt"} {
		if _, err := m.StartCopy(src, []string{p}, src, ".", false, ConflictReplace, "alex", os.Getuid()); !errors.Is(err, ErrSameFolder) {
			t.Fatal(p, err)
		}
	}
	job, _ := m.StartCopy(src, []string{"notiz.txt"}, src, ".", false, ConflictRename, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobDone || read(t, filepath.Join(src.Dir, "notiz (2).txt")) != "notiz" {
		t.Fatal(job)
	}
}

// F14 / decision 3: a replaced file goes to the share's recycle bin, also
// when the target is the volume the share is on; without a bin it is gone.
func TestReplacedFilesGoToTheRecycleBin(t *testing.T) {
	m, src, base := newRoot(t)
	usb := Root{ID: "usb/stick", Kind: "usb", Dir: filepath.Join(base, "usb"), Writable: true}
	put(t, filepath.Join(usb.Dir, "notiz.txt"), "from usb")
	put(t, filepath.Join(src.Dir, "ziel/notiz.txt"), "old")
	job, _ := m.StartCopy(usb, []string{"notiz.txt"}, src, "ziel", false, ConflictReplace, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobDone || read(t, filepath.Join(src.Dir, "ziel/notiz.txt")) != "from usb" {
		t.Fatal(job)
	}
	if read(t, filepath.Join(src.Dir, ".recycle/alex/ziel/notiz.txt")) != "old" {
		t.Fatal("replaced file not in the bin:", tree(src.Dir))
	}
	// The same share seen from its volume.
	volume := Root{ID: "volume/data", Kind: "volume", Dir: base, Writable: true, Bins: []string{"share"}}
	put(t, filepath.Join(usb.Dir, "b.txt"), "new b")
	put(t, filepath.Join(src.Dir, "b.txt"), "old b")
	job, _ = m.StartCopy(usb, []string{"b.txt"}, volume, "share", false, ConflictReplace, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobDone || read(t, filepath.Join(src.Dir, ".recycle/alex/b.txt")) != "old b" {
		t.Fatal(job, tree(src.Dir))
	}
	// A move within the share replaces into the bin as well.
	put(t, filepath.Join(src.Dir, "c.txt"), "new c")
	put(t, filepath.Join(src.Dir, "ziel/c.txt"), "old c")
	job, _ = m.StartCopy(src, []string{"c.txt"}, src, "ziel", true, ConflictReplace, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobDone || read(t, filepath.Join(src.Dir, ".recycle/alex/ziel/c.txt")) != "old c" || read(t, filepath.Join(src.Dir, "ziel/c.txt")) != "new c" {
		t.Fatal(job, tree(src.Dir))
	}
	// No bin: the old file is gone.
	put(t, filepath.Join(usb.Dir, "ziel/x.txt"), "old x")
	put(t, filepath.Join(src.Dir, "x.txt"), "new x")
	job, _ = m.StartCopy(src, []string{"x.txt"}, usb, "ziel", false, ConflictReplace, "alex", os.Getuid())
	if job = finish(t, m, job.ID); job.State != JobDone || read(t, filepath.Join(usb.Dir, "ziel/x.txt")) != "new x" || exists(filepath.Join(usb.Dir, ".recycle")) {
		t.Fatal(job, tree(usb.Dir))
	}
}

// Owner request of 4.11.2026: deleting in the volume view goes to the bin
// of the share the path is in; elsewhere it needs confirming, and the bin
// is reachable as a root of its own.
func TestVolumeDeleteUsesTheShareBin(t *testing.T) {
	m, share, base := newRoot(t)
	volume := Root{ID: "volume/data", Kind: "volume", Dir: base, Writable: true, Bins: []string{"share"}}
	put(t, filepath.Join(base, "other/x.txt"), "x")
	if _, err := m.DeleteWithUndo(volume, []string{"share/notiz.txt"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(share.Dir, ".recycle/alex/notiz.txt")) != "notiz" {
		t.Fatal(tree(base))
	}
	if err := m.Delete(volume, []string{"other/x.txt"}, "alex", false); !errors.Is(err, ErrConfirm) {
		t.Fatal("outside a share:", err)
	}
	if err := m.Delete(volume, []string{"share"}, "alex", false); !errors.Is(err, ErrConfirm) {
		t.Fatal("the share folder itself:", err)
	}
	if err := m.Delete(volume, []string{"other/x.txt"}, "alex", true); err != nil || exists(filepath.Join(base, "other/x.txt")) {
		t.Fatal(err)
	}
	bin, ok := volume.BinRoot("share")
	if !ok {
		t.Fatal("no bin root")
	}
	if _, ok := volume.BinRoot("other"); ok {
		t.Fatal("bin root for a folder without bin")
	}
	entries, err := m.TrashList(bin, "alex", "alex", false)
	if err != nil || names(entries) != "notiz.txt" {
		t.Fatal(entries, err)
	}
	if err := m.TrashRestore(bin, []string{"alex/notiz.txt"}, "alex", os.Getuid(), false); err != nil || read(t, filepath.Join(share.Dir, "notiz.txt")) != "notiz" {
		t.Fatal(err, tree(base))
	}
}

// A copy never writes over, or moves to the recycle bin, a path a virtual
// machine holds – also with "replace" and without individual choices.
func TestCopyLeavesVMFilesAlone(t *testing.T) {
	m, share, base := newRoot(t)
	usb := Root{ID: "usb/stick", Kind: "usb", Dir: filepath.Join(base, "usb"), Writable: true}
	put(t, filepath.Join(usb.Dir, "vm.img"), "new image")
	put(t, filepath.Join(usb.Dir, "other.txt"), "other")
	put(t, filepath.Join(share.Dir, "ziel/vm.img"), "running disk")
	m.Protected = func(p string) bool { return p == filepath.Join(share.Dir, "ziel/vm.img") }
	for _, mode := range []string{ConflictReplace, ConflictMissing} {
		job, err := m.StartCopy(usb, []string{"vm.img", "other.txt"}, share, "ziel", false, mode, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		job = finish(t, m, job.ID)
		if job.State != JobDone || problemCodes(job)["vm.img"] != "protected" || read(t, filepath.Join(share.Dir, "ziel/other.txt")) != "other" {
			t.Fatalf("%s: %+v", mode, job)
		}
		if read(t, filepath.Join(share.Dir, "ziel/vm.img")) != "running disk" || exists(filepath.Join(share.Dir, ".recycle")) {
			t.Fatalf("%s: VM file touched: %s", mode, tree(share.Dir))
		}
	}
}

// Copying into the folder the selection is in: the inspection reports the
// files themselves neither as conflicts nor as identical, so the dialog
// keeps "keep both".
func TestInspectIntoOwnFolderFindsNoConflicts(t *testing.T) {
	m, src, _ := twoRoots(t)
	result, err := m.Inspect(context.Background(), src, []string{"urlaub", "notiz.txt"}, &src, ".")
	if err != nil || len(result.Conflicts) != 0 || result.Identical != 0 || result.Files != 2 {
		t.Fatalf("%+v %v", result, err)
	}
}
