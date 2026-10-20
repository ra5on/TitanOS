package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectSelectionAndNestedConflicts(t *testing.T) {
	m, src, dst := twoRoots(t)
	os.MkdirAll(filepath.Join(dst.Dir, "ziel/urlaub"), 0755)
	os.WriteFile(filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg"), []byte("existing-photo"), 0600)
	os.Symlink("/etc/passwd", filepath.Join(src.Dir, "urlaub/link"))
	os.WriteFile(filepath.Join(src.Dir, "urlaub/.unfinished.titanos-part"), []byte("partial"), 0600)
	result, err := m.Inspect(context.Background(), src, []string{"urlaub", "notiz.txt"}, &dst, "ziel")
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 || result.Bytes != int64(len("strandnotiz")) || len(result.Conflicts) != 1 {
		t.Fatalf("inspection=%+v", result)
	}
	conflict := result.Conflicts[0]
	if conflict.Path != "ziel/urlaub/strand.jpg" || conflict.Target.Size != int64(len("existing-photo")) {
		t.Fatalf("conflict=%+v", conflict)
	}
	if read(t, filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg")) != "existing-photo" {
		t.Fatal("inspection modified destination")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.Inspect(ctx, src, []string{"urlaub"}, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	src.Hide = []string{"urlaub"}
	if _, err = m.Inspect(context.Background(), src, []string{"urlaub"}, nil, ""); err == nil {
		t.Fatal("inspected protected directory")
	}
	if _, err = m.Inspect(context.Background(), src, []string{"../outside"}, nil, ""); err == nil {
		t.Fatal("accepted traversal")
	}
}
