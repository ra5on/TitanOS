package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRenameBatchPreviewConflictAndUndo(t *testing.T) {
	m, root, _ := newRoot(t)
	os.WriteFile(filepath.Join(root.Dir, "Backup.txt"), []byte("Backup"), 0600)
	opt := RenameOptions{Names: []string{"Backup.txt", "notiz.txt"}, Prefix: "Meine-", Number: true, Start: 1, Digits: 2}
	plan, err := m.RenamePreview(root, opt)
	if err != nil || plan[0].To != "Meine-Backup01.txt" || plan[1].To != "Meine-notiz02.txt" {
		t.Fatalf("preview: %+v %v", plan, err)
	}
	result, err := m.RenameBatch(context.Background(), root, plan, "alex")
	if err != nil || result.Done != 2 || result.Undo == "" {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if err = m.Undo(context.Background(), "other", result.Undo, map[string]Root{root.ID: root}, os.Getuid()); !errors.Is(err, ErrNoJob) {
		t.Fatalf("other user: %v", err)
	}
	if err = m.Undo(context.Background(), "alex", result.Undo, map[string]Root{root.ID: root}, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(root.Dir, "Backup.txt")) != "Backup" || len(m.UndoList("alex")) != 0 {
		t.Fatal("undo failed")
	}
	opt.Prefix = ""
	opt.Number = false
	opt.Find = "notiz"
	opt.Replace = "Backup"
	plan, err = m.RenamePreview(root, opt)
	if err != nil || plan[1].Problem == "" {
		t.Fatalf("collision not found: %+v %v", plan, err)
	}
}
func TestRenameBatchRejectsStalePreview(t *testing.T) {
	m, root, _ := newRoot(t)
	plan, err := m.RenamePreview(root, RenameOptions{Names: []string{"notiz.txt"}, Prefix: "new-", Start: 1, Digits: 1})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root.Dir, "notiz.txt"), []byte("changed after preview"), 0600)
	if _, err = m.RenameBatch(context.Background(), root, plan, "alex"); !errors.Is(err, ErrTextConflict) {
		t.Fatalf("stale preview: %v", err)
	}
}
func TestUndoTrashRefusesNewOriginalAndChangedContent(t *testing.T) {
	m, root, _ := newRoot(t)
	id, err := m.DeleteWithUndo(root, []string{"notiz.txt"}, "alex", false)
	if err != nil || id == "" {
		t.Fatalf("delete: %s %v", id, err)
	}
	os.WriteFile(filepath.Join(root.Dir, "notiz.txt"), []byte("new original"), 0600)
	if err = m.Undo(context.Background(), "alex", id, map[string]Root{root.ID: root}, os.Getuid()); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	os.Remove(filepath.Join(root.Dir, "notiz.txt"))
	if err = m.Undo(context.Background(), "alex", id, map[string]Root{root.ID: root}, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	id, err = m.DeleteWithUndo(root, []string{"notiz.txt"}, "alex", false)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root.Dir, ".recycle/alex/notiz.txt"), []byte("changed"), 0600)
	if err = m.Undo(context.Background(), "alex", id, map[string]Root{root.ID: root}, os.Getuid()); !errors.Is(err, ErrTextConflict) {
		t.Fatalf("changed item: %v", err)
	}
}
func TestMoveCreatesUndoAndRejectsNewChildren(t *testing.T) {
	m, src, dst := twoRoots(t)
	job, err := m.StartCopy(src, []string{"urlaub"}, dst, "ziel", true, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone {
		t.Fatal(job)
	}
	records := m.UndoList("alex")
	if len(records) != 1 {
		t.Fatalf("undo records: %+v", records)
	}
	extra := filepath.Join(dst.Dir, "ziel/urlaub/new.txt")
	os.WriteFile(extra, []byte("new"), 0600)
	if err = m.Undo(context.Background(), "alex", records[0].ID, map[string]Root{src.ID: src, dst.ID: dst}, os.Getuid()); !errors.Is(err, ErrTextConflict) {
		t.Fatalf("new child: %v", err)
	}
}
func TestMoveUndoRestoresOriginal(t *testing.T) {
	m, src, dst := twoRoots(t)
	job, err := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", true, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	finish(t, m, job.ID)
	records := m.UndoList("alex")
	if len(records) != 1 {
		t.Fatalf("records: %+v", records)
	}
	if err = m.Undo(context.Background(), "alex", records[0].ID, map[string]Root{src.ID: src, dst.ID: dst}, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(src.Dir, "notiz.txt")) != "notiz" {
		t.Fatal("source not restored")
	}
	if _, err = os.Stat(filepath.Join(dst.Dir, "ziel/notiz.txt")); !os.IsNotExist(err) {
		t.Fatal("target retained")
	}
}

func TestUndoCopyFallbackAndConcurrentDestination(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "new-original"}[collision], func(t *testing.T) {
			m, original, current := twoRoots(t)
			os.WriteFile(filepath.Join(current.Dir, "ziel/notiz.txt"), []byte("notiz"), 0600)
			os.Remove(filepath.Join(original.Dir, "notiz.txt"))
			sr, e := os.OpenRoot(current.Dir)
			if e != nil {
				t.Fatal(e)
			}
			defer sr.Close()
			dr, e := os.OpenRoot(original.Dir)
			if e != nil {
				t.Fatal(e)
			}
			defer dr.Close()
			move := UndoMove{CurrentRoot: current, OriginalRoot: original, Current: "ziel/notiz.txt", Original: "notiz.txt"}
			move.stamp, e = treeStamp(context.Background(), sr, move.Current, false)
			if e != nil {
				t.Fatal(e)
			}
			if collision {
				m.syncFile = func(f *os.File) error {
					if e := os.WriteFile(filepath.Join(original.Dir, "notiz.txt"), []byte("concurrent file"), 0600); e != nil {
						return e
					}
					return f.Sync()
				}
			}
			e = m.undoAcrossFilesystems(context.Background(), sr, dr, move, os.Getuid())
			if collision {
				if !os.IsExist(e) {
					t.Fatalf("collision: %v", e)
				}
				if read(t, filepath.Join(original.Dir, "notiz.txt")) != "concurrent file" || read(t, filepath.Join(current.Dir, "ziel/notiz.txt")) != "notiz" {
					t.Fatal("overwrote or removed a file")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				if read(t, filepath.Join(original.Dir, "notiz.txt")) != "notiz" {
					t.Fatal("wrong content")
				}
				if _, e = os.Stat(filepath.Join(current.Dir, "ziel/notiz.txt")); !os.IsNotExist(e) {
					t.Fatal("moved target not removed")
				}
			}
		})
	}
}
func TestUndoExpirationAndPermissions(t *testing.T) {
	m, root, _ := newRoot(t)
	id, e := m.DeleteWithUndo(root, []string{"notiz.txt"}, "alex", false)
	if e != nil {
		t.Fatal(e)
	}
	readOnly := root
	readOnly.Writable = false
	if e = m.Undo(context.Background(), "alex", id, map[string]Root{root.ID: readOnly}, os.Getuid()); !errors.Is(e, ErrReadOnly) {
		t.Fatalf("readonly undo: %v", e)
	}
	now := m.now()
	m.now = func() time.Time { return now.Add(31 * time.Minute) }
	if len(m.UndoList("alex")) != 0 {
		t.Fatal("undo record did not expire")
	}
}
