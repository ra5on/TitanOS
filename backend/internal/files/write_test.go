package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(0)
	root := Root{ID: "share/a", Kind: "share", Dir: dir, Writable: true}
	if err := m.WriteFile(root, "Berichte/2026/plan.docx", []byte("eins"), os.Getuid(), false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Berichte/2026/plan.docx")); string(data) != "eins" {
		t.Fatalf("content %q", data)
	}
	if err := m.WriteFile(root, "Berichte/2026/plan.docx", []byte("zwei"), os.Getuid(), false); !errors.Is(err, ErrExists) {
		t.Fatalf("replaced without asking: %v", err)
	}
	if err := m.WriteFile(root, "Berichte/2026/plan.docx", []byte("zwei"), os.Getuid(), true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Berichte/2026/plan.docx.prev")); string(data) != "eins" {
		t.Fatalf("previous %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Berichte/2026/plan.docx")); string(data) != "zwei" {
		t.Fatalf("content %q", data)
	}
	for _, bad := range []string{"../x.txt", "", "/"} {
		if err := m.WriteFile(root, bad, []byte("x"), os.Getuid(), false); err == nil {
			t.Fatalf("wrote %q", bad)
		}
	}
	os.Symlink("/etc", filepath.Join(dir, "out"))
	if err := m.WriteFile(root, "out/evil.txt", []byte("x"), os.Getuid(), false); err == nil {
		t.Fatal("wrote through a link out of the root")
	}
	if err := m.WriteFile(Root{Dir: dir}, "a.txt", []byte("x"), os.Getuid(), false); !errors.Is(err, ErrReadOnly) {
		t.Fatal("wrote without write access")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "Berichte/2026"))
	if len(entries) != 2 {
		t.Fatalf("leftovers: %v", entries)
	}
}
