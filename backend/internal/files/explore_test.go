package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecursiveSearchFiltersAndBoundaries(t *testing.T) {
	m, root, _ := newRoot(t)
	os.MkdirAll(filepath.Join(root.Dir, ".recycle/alex"), 0700)
	os.WriteFile(filepath.Join(root.Dir, ".recycle/alex/strand.jpg"), []byte("secret"), 0600)
	os.WriteFile(filepath.Join(root.Dir, "urlaub/.hidden.jpg"), []byte("hidden"), 0600)
	os.Symlink("/etc", filepath.Join(root.Dir, "outside"))
	result, err := m.Search(context.Background(), root, SearchOptions{Query: "STRAND", Extension: "JPG", MinSize: 5, MaxSize: 10})
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Path != "urlaub/strand.jpg" {
		t.Fatalf("search: %+v %v", result, err)
	}
	result, err = m.Search(context.Background(), root, SearchOptions{Extension: "jpg", ShowHidden: true})
	if err != nil || len(result.Entries) != 2 {
		t.Fatalf("hidden search: %+v %v", result, err)
	}
	result, err = m.Search(context.Background(), root, SearchOptions{After: time.Now().Add(time.Hour)})
	if err != nil || len(result.Entries) != 0 {
		t.Fatalf("date filter: %+v %v", result, err)
	}
	root.Hide = []string{"urlaub"}
	result, err = m.Search(context.Background(), root, SearchOptions{Extension: "jpg", ShowHidden: true})
	if err != nil || len(result.Entries) != 0 {
		t.Fatalf("secret: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.Search(ctx, root, SearchOptions{}); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err = m.Search(context.Background(), root, SearchOptions{Path: "../"}); err == nil {
		t.Fatal("traversal allowed")
	}
}
func TestCompareDetectsSameSizeContentAndMissingEntries(t *testing.T) {
	m, left, right := twoRoots(t)
	os.MkdirAll(filepath.Join(right.Dir, "urlaub"), 0700)
	os.WriteFile(filepath.Join(right.Dir, "urlaub/strand.jpg"), []byte("anders"), 0600)
	info, _ := os.Stat(filepath.Join(left.Dir, "urlaub/strand.jpg"))
	os.Chtimes(filepath.Join(right.Dir, "urlaub/strand.jpg"), info.ModTime(), info.ModTime())
	metadata, err := m.Compare(context.Background(), left, "urlaub", right, "urlaub", false, false)
	if err != nil || metadata.Same != 1 || len(metadata.Entries) != 0 {
		t.Fatalf("metadata: %+v %v", metadata, err)
	}
	hashes, err := m.Compare(context.Background(), left, "urlaub", right, "urlaub", true, false)
	if err != nil || len(hashes.Entries) != 1 || hashes.Entries[0].State != "different" {
		t.Fatalf("hashes: %+v %v", hashes, err)
	}
	os.WriteFile(filepath.Join(right.Dir, "urlaub/only-right"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(left.Dir, "urlaub/only-left"), []byte("x"), 0600)
	comparison, err := m.Compare(context.Background(), left, "urlaub", right, "urlaub", true, false)
	if err != nil || len(comparison.Entries) != 3 {
		t.Fatalf("missing: %+v %v", comparison, err)
	}
}
