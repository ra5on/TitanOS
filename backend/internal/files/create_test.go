package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateTextDoesNotOverwriteAndKeepsCase(t *testing.T) {
	m, root, _ := newRoot(t)
	for _, name := range []string{"Meine Datei.TXT", "Notizen.md", "Config.JSON"} {
		if err := m.CreateText(root, name, os.Getuid()); err != nil {
			t.Fatal(err)
		}
		if err := m.CreateText(root, name, os.Getuid()); err != ErrExists {
			t.Fatalf("overwrite %s: %v", name, err)
		}
	}
	if string(readBytes(t, filepath.Join(root.Dir, "Config.JSON"))) != "{}\n" {
		t.Fatal("invalid JSON template")
	}
	for _, name := range []string{"../escape.txt", "bad.exe", ".titanos/private.txt"} {
		if err := m.CreateText(root, name, os.Getuid()); err == nil {
			t.Fatal(name)
		}
	}
	root.Writable = false
	if err := m.CreateText(root, "readonly.txt", os.Getuid()); err != ErrReadOnly {
		t.Fatal(err)
	}
}
func readBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestCreateTextProtectsResolvedParent(t *testing.T) {
	m, root, _ := newRoot(t)
	protected := filepath.Join(root.Dir, "private")
	os.Mkdir(protected, 0700)
	os.Symlink("private", filepath.Join(root.Dir, "alias"))
	m.Protected = func(name string) bool { return name == filepath.Join(protected, "a.txt") }
	if err := m.CreateText(root, "alias/a.txt", os.Getuid()); err != ErrProtected {
		t.Fatalf("protected alias: %v", err)
	}
	if _, err := os.Stat(filepath.Join(protected, "a.txt")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
