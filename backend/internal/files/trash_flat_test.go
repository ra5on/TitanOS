package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// One folder of the bin that cannot be read leaves it out; the rest of the
// bin is still listed.
func TestTrashFlatPassesOverAnUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	m, root, _ := newRoot(t)
	bin := filepath.Join(root.Dir, RecycleDir, "alex")
	os.MkdirAll(filepath.Join(bin, "locked"), 0o755)
	os.WriteFile(filepath.Join(bin, "locked", "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(bin, "b.txt"), []byte("b"), 0o644)
	os.Chmod(filepath.Join(bin, "locked"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(bin, "locked"), 0o755) })
	list, err := m.TrashFlat(context.Background(), root, "alex", false)
	if err != nil || len(list) != 1 || list[0].Name != "b.txt" {
		t.Fatalf("%+v %v", list, err)
	}
}

// More files than the flat view takes is an error of its own, not "this
// name is not allowed".
func TestTrashFlatTooManyFiles(t *testing.T) {
	m, root, _ := newRoot(t)
	bin := filepath.Join(root.Dir, RecycleDir, "alex")
	os.MkdirAll(bin, 0o755)
	for i := range 4 {
		os.WriteFile(filepath.Join(bin, fmt.Sprintf("%d.txt", i)), nil, 0o644)
	}
	defer func(limit int) { trashFlatLimit = limit }(trashFlatLimit)
	trashFlatLimit = 3
	if _, err := m.TrashFlat(context.Background(), root, "alex", false); !errors.Is(err, ErrTrashTooMany) {
		t.Fatalf("too many: %v", err)
	}
}
