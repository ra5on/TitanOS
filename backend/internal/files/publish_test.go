package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishNoOverwriteAndConfinedParents(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	root.WriteFile("part", []byte("new"), 0600)
	root.WriteFile("target", []byte("old"), 0600)
	if err := publishNewFile(root, "part", "target"); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "target")) != "old" {
		t.Fatal("overwrote existing file")
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(dir, "outside"))
	if err := publishNewFile(root, "part", "outside/file"); err == nil {
		t.Fatal("escaped root")
	}
	if err := publishNewFile(root, "part", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("part"); !os.IsNotExist(err) {
		t.Fatal("part not removed")
	}
}
