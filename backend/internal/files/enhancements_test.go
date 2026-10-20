package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreviousTextRestoreAndBoundaries(t *testing.T) {
	m, root, _ := newRoot(t)
	original, e := m.ReadText(root, "notiz.txt")
	if e != nil {
		t.Fatal(e)
	}
	current, e := m.SaveTextVersion(root, "notiz.txt", "new content", original.Version)
	if e != nil {
		t.Fatal(e)
	}
	previous, e := m.ReadPreviousText(root, "notiz.txt")
	if e != nil || previous.Content != "notiz" {
		t.Fatalf("%+v %v", previous, e)
	}
	if _, e = m.SaveTextVersion(root, "notiz.txt", previous.Content, original.Version); !errors.Is(e, ErrTextConflict) {
		t.Fatal("stale restore accepted", e)
	}
	if _, e = m.SaveTextVersion(root, "notiz.txt", previous.Content, current.Version); e != nil {
		t.Fatal(e)
	}
	previous, e = m.ReadPreviousText(root, "notiz.txt")
	if e != nil || previous.Content != "new content" {
		t.Fatalf("restore lost current version: %+v %v", previous, e)
	}
	os.Remove(filepath.Join(root.Dir, "notiz.txt.prev"))
	os.Symlink("/etc/passwd", filepath.Join(root.Dir, "notiz.txt.prev"))
	if _, e = m.ReadPreviousText(root, "notiz.txt"); e == nil {
		t.Fatal("previous symlink read")
	}
	root.Writable = false
	if _, e = m.ReadPreviousText(root, "notiz.txt"); !errors.Is(e, ErrReadOnly) {
		t.Fatal(e)
	}
}
func TestDuplicateScanAndStaleDeletionCheck(t *testing.T) {
	m, root, _ := newRoot(t)
	os.WriteFile(filepath.Join(root.Dir, "copy.txt"), []byte("notiz"), 0600)
	os.WriteFile(filepath.Join(root.Dir, "different.txt"), []byte("other"), 0600)
	os.Symlink("notiz.txt", filepath.Join(root.Dir, "link.txt"))
	result, e := m.Duplicates(context.Background(), root, "", false)
	if e != nil || len(result.Groups) != 1 || len(result.Groups[0].Paths) != 2 || result.Hashed != 3 {
		t.Fatalf("%+v %v", result, e)
	}
	g := result.Groups[0]
	if len(g.Versions) != 2 {
		t.Fatal("unrelated versions leaked into group")
	}
	if e = m.CheckVersions(root, g.Paths, g.Versions); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root.Dir, "copy.txt"), []byte("changed"), 0600)
	if e = m.CheckVersions(root, g.Paths, g.Versions); !errors.Is(e, ErrTextConflict) {
		t.Fatal("stale duplicate accepted", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = m.Duplicates(ctx, root, "", false); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestPerFileCopyChoicesPreserveNames(t *testing.T) {
	m, src, dst := twoRoots(t)
	os.WriteFile(filepath.Join(dst.Dir, "ziel/notiz.txt"), []byte("old"), 0600)
	os.MkdirAll(filepath.Join(dst.Dir, "ziel/urlaub"), 0700)
	os.WriteFile(filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg"), []byte("old image"), 0600)
	inspection, e := m.Inspect(context.Background(), src, []string{"notiz.txt", "urlaub"}, &dst, "ziel")
	if e != nil {
		t.Fatal(e)
	}
	choices := map[string]CopyChoice{}
	for _, c := range inspection.Conflicts {
		choice := CopyChoice{Action: "replace", SourceVersion: c.SourceVersion, TargetVersion: c.TargetVersion}
		if c.SourcePath == "urlaub/strand.jpg" {
			choice.Action = "rename"
			choice.Name = "MeineFotos.JPG"
			choice.TargetVersion = "absent"
		}
		choices[c.SourcePath] = choice
	}
	job, e := m.StartCopyChoices(src, []string{"notiz.txt", "urlaub"}, dst, "ziel", false, "skip", "alex", os.Getuid(), choices)
	if e != nil {
		t.Fatal(e)
	}
	if end := finish(t, m, job.ID); end.State != JobDone {
		t.Fatal(end)
	}
	if read(t, filepath.Join(dst.Dir, "ziel/notiz.txt")) != "notiz" || read(t, filepath.Join(dst.Dir, "ziel/urlaub/MeineFotos.JPG")) != "strand" || read(t, filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg")) != "old image" {
		t.Fatal("choices not respected")
	}
}
func TestCompareCopyPreservesNestedPathAndRejectsStaleTargets(t *testing.T) {
	m, src, dst := twoRoots(t)
	info, e := os.Stat(filepath.Join(src.Dir, "urlaub/strand.jpg"))
	if e != nil {
		t.Fatal(e)
	}
	choice := CopyChoice{Action: "copy", Target: "ziel/Fotos/Urlaub/strand.jpg", SourceVersion: entryVersion(info), TargetVersion: "absent"}
	start := func(c CopyChoice) Job {
		job, e := m.StartCopyChoices(src, []string{"urlaub/strand.jpg"}, dst, "ziel", false, "skip", "alex", os.Getuid(), map[string]CopyChoice{"urlaub/strand.jpg": c})
		if e != nil {
			t.Fatal(e)
		}
		return finish(t, m, job.ID)
	}
	if end := start(choice); end.State != JobDone {
		t.Fatal(end)
	}
	if read(t, filepath.Join(dst.Dir, choice.Target)) != "strand" {
		t.Fatal("nested path lost")
	}
	if end := start(choice); end.State != JobFailed {
		t.Fatal("occupied destination replaced", end)
	}
	targetInfo, _ := os.Stat(filepath.Join(dst.Dir, choice.Target))
	choice.Action = "replace"
	choice.TargetVersion = entryVersion(targetInfo)
	os.WriteFile(filepath.Join(dst.Dir, choice.Target), []byte("externally changed"), 0600)
	if end := start(choice); end.State != JobFailed {
		t.Fatal("stale target replaced", end)
	}
	if read(t, filepath.Join(dst.Dir, choice.Target)) != "externally changed" {
		t.Fatal("changed data lost")
	}
	choice.Target = "../outside"
	if _, e = m.StartCopyChoices(src, []string{"urlaub/strand.jpg"}, dst, "ziel", false, "skip", "alex", os.Getuid(), map[string]CopyChoice{"urlaub/strand.jpg": choice}); !errors.Is(e, ErrInvalid) {
		t.Fatal("traversal", e)
	}
}

func TestCopyChoiceRechecksDestinationBeforePublishing(t *testing.T) {
	m, src, dst := twoRoots(t)
	dest := filepath.Join(dst.Dir, "ziel/notiz.txt")
	os.WriteFile(dest, []byte("old"), 0600)
	info, _ := os.Stat(dest)
	source, _ := os.Stat(filepath.Join(src.Dir, "notiz.txt"))
	m.syncFile = func(f *os.File) error {
		if e := os.WriteFile(dest, []byte("concurrent writer"), 0600); e != nil {
			return e
		}
		return f.Sync()
	}
	job, e := m.StartCopyChoices(src, []string{"notiz.txt"}, dst, "ziel", false, "skip", "alex", os.Getuid(), map[string]CopyChoice{"notiz.txt": {Action: "replace", SourceVersion: entryVersion(source), TargetVersion: entryVersion(info)}})
	if e != nil {
		t.Fatal(e)
	}
	if end := finish(t, m, job.ID); end.State != JobFailed {
		t.Fatal(end)
	}
	if read(t, dest) != "concurrent writer" {
		t.Fatal("concurrent target was overwritten")
	}
}

func TestSkippedIndividualCopyKeepsTargetAndCompletesProgress(t *testing.T) {
	m, src, dst := twoRoots(t)
	os.MkdirAll(filepath.Join(dst.Dir, "ziel/urlaub"), 0700)
	os.WriteFile(filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg"), []byte("keep"), 0600)
	job, e := m.StartCopyChoices(src, []string{"urlaub"}, dst, "ziel", false, "skip", "alex", os.Getuid(), map[string]CopyChoice{"urlaub/strand.jpg": {Action: "skip"}})
	if e != nil {
		t.Fatal(e)
	}
	end := finish(t, m, job.ID)
	if end.State != JobDone || end.Copied != end.Bytes || end.Done != end.Files {
		t.Fatal(end)
	}
	if read(t, filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg")) != "keep" {
		t.Fatal("skipped target changed")
	}
}
