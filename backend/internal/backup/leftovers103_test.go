package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// A run cut off (power cut, drive pulled) leaves temporary files of the
// size of what it copied. The next run removes them, and nothing else.
func TestNextRunRemovesTheLeftoversOfAnInterruptedCopy(t *testing.T) {
	_, source, target, run := copyFixture(t)
	os.MkdirAll(filepath.Join(source, "fotos"), 0o755)
	os.WriteFile(filepath.Join(source, "fotos", "a.jpg"), []byte("photo"), 0o600)
	run()
	copied := filepath.Join(target, "backup", "docs")
	marker := filepath.Join(target, "backup", ".titanos-copy-run")
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("the marker of a run that ended stays: %v", err)
	}
	// What the cut-off run left.
	leftovers := []string{
		filepath.Join(copied, "fotos", ".titanos-copy-ABCDEFGHIJKLMNOPQRSTUVWXYZ"),
		filepath.Join(copied, ".titanos-copy-234567ABCDEFGHIJKLMNOPQRST"),
	}
	for _, f := range leftovers {
		if err := os.WriteFile(f, []byte("half a vm disk"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink("nowhere", filepath.Join(copied, ".titanos-copy-QRSTUVWXYZ234567ABCDEFGHIJ"))
	leftovers = append(leftovers, filepath.Join(copied, ".titanos-copy-QRSTUVWXYZ234567ABCDEFGHIJ"))
	// Never touched: the resumable part of a file manager copy, and a
	// folder of that kind of name.
	kept := []string{
		filepath.Join(copied, ".titanos-copy-0123456789abcdef01234567.titanos-part"),
		filepath.Join(copied, "fotos", "a.jpg"),
	}
	os.WriteFile(kept[0], []byte("part"), 0o600)
	folder := filepath.Join(copied, ".titanos-copy-ZZZZZZZZZZZZZZZZZZZZZZZZZZ")
	os.Mkdir(folder, 0o700)
	kept = append(kept, folder)
	os.WriteFile(marker, nil, 0o600)
	run()
	for _, f := range leftovers {
		if _, err := os.Lstat(f); !os.IsNotExist(err) {
			t.Errorf("leftover %s stays: %v", filepath.Base(f), err)
		}
	}
	for _, f := range kept {
		if _, err := os.Lstat(f); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(f), err)
		}
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker stays: %v", err)
	}
}
