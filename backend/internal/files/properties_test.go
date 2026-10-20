package files

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPropertiesChecksumAndHiddenPaths(t *testing.T) {
	m, root, _ := newRoot(t)
	p, err := m.Properties(context.Background(), root, "notiz.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if p.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("notiz"))) || p.Size != 5 || p.Permissions == "" {
		t.Fatalf("properties: %+v", p)
	}
	root.Hide = []string{"notiz.txt"}
	if _, err = m.Properties(context.Background(), root, "notiz.txt", false); err != ErrNotFound {
		t.Fatalf("hidden: %v", err)
	}
	root.Hide = nil
	os.Symlink("/etc/passwd", filepath.Join(root.Dir, "outside"))
	if _, err = m.Properties(context.Background(), root, "outside", true); err != ErrInvalid {
		t.Fatalf("symlink checksum: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.Properties(ctx, root, "notiz.txt", true); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err = m.Properties(context.Background(), root, "../outside", false); err == nil {
		t.Fatal("allowed escape")
	}
}

func TestTrashFlatAndRestorePreview(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Kind = "share"
	for _, user := range []string{"alex", "other"} {
		os.MkdirAll(filepath.Join(root.Dir, RecycleDir, user, "Fotos"), 0700)
		os.WriteFile(filepath.Join(root.Dir, RecycleDir, user, "Fotos/a.txt"), []byte("deleted"), 0600)
	}
	list, err := m.TrashFlat(context.Background(), root, "alex", false)
	if err != nil || len(list) != 1 || list[0].Path != "alex/Fotos/a.txt" || list[0].Original != "Fotos/a.txt" {
		t.Fatalf("list: %+v %v", list, err)
	}
	all, err := m.TrashFlat(context.Background(), root, "alex", true)
	if err != nil || len(all) != 2 {
		t.Fatalf("admin: %+v %v", all, err)
	}
	os.MkdirAll(filepath.Join(root.Dir, "Fotos"), 0700)
	os.WriteFile(filepath.Join(root.Dir, "Fotos/a.txt"), []byte("current"), 0600)
	preview, err := m.TrashPreview(root, []string{list[0].Path}, "alex", false)
	if err != nil || len(preview) != 1 || !preview[0].Conflict || preview[0].Target != "Fotos/a.txt" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if read(t, filepath.Join(root.Dir, "Fotos/a.txt")) != "current" {
		t.Fatal("preview modified target")
	}
	if _, err = m.TrashPreview(root, []string{"other/Fotos/a.txt"}, "alex", false); err != ErrNotFound {
		t.Fatalf("other account: %v", err)
	}
}

func TestTrashDirectoryMetadataPreservesOriginalAndDeletionTime(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Recycle = true
	first := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return first }
	if err := m.Delete(root, []string{"urlaub"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	list, err := m.TrashFlat(context.Background(), root, "alex", false)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if !list[0].Modified.Equal(first) || list[0].Original != "urlaub/strand.jpg" {
		t.Fatalf("directory deletion metadata: %+v", list)
	}
	os.MkdirAll(filepath.Join(root.Dir, "urlaub"), 0700)
	os.WriteFile(filepath.Join(root.Dir, "urlaub/another.txt"), []byte("new"), 0600)
	second := first.Add(time.Hour)
	m.now = func() time.Time { return second }
	if err := m.Delete(root, []string{"urlaub"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	list, err = m.TrashFlat(context.Background(), root, "alex", false)
	if err != nil || len(list) != 2 {
		t.Fatalf("second list: %+v %v", list, err)
	}
	for _, entry := range list {
		if entry.Name == "another.txt" {
			if entry.Original != "urlaub/another.txt" || !entry.Modified.Equal(second) {
				t.Fatalf("original name lost: %+v", entry)
			}
			preview, err := m.TrashPreview(root, []string{entry.Path}, "alex", false)
			if err != nil || preview[0].Target != "urlaub/another.txt" {
				t.Fatalf("preview=%+v %v", preview, err)
			}
			if err = m.TrashRestore(root, []string{entry.Path}, "alex", os.Getuid(), false); err != nil {
				t.Fatal(err)
			}
			if read(t, filepath.Join(root.Dir, "urlaub/another.txt")) != "new" {
				t.Fatal("incorrect destination")
			}
		}
	}
}

func TestTrashAllocatedSpaceCountsHardlinksOnce(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Kind = "share"
	dir := filepath.Join(root.Dir, RecycleDir, "alex")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "first"), make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "first"), filepath.Join(dir, "second")); err != nil {
		t.Fatal(err)
	}
	list, err := m.TrashFlat(context.Background(), root, "alex", false)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].Allocated == nil || list[1].Allocated == nil || *list[0].Allocated <= 0 || *list[1].Allocated != 0 {
		t.Fatalf("allocation: %+v", list)
	}
}
