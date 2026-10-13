package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRemove(t *testing.T) {
	s := Open(t.TempDir())

	type doc struct{ Name string }
	if err := s.WriteJSON("users/alex/account.json", doc{"alex"}); err != nil {
		t.Fatal(err)
	}
	var got doc
	if err := s.ReadJSON("users/alex/account.json", &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "alex" {
		t.Errorf("read back %+v", got)
	}

	// Overwriting replaces the content and leaves no temporary files behind.
	if err := s.WriteJSON("users/alex/account.json", doc{"alex2"}); err != nil {
		t.Fatal(err)
	}
	names, err := s.List("users/alex")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "account.json" {
		t.Errorf("directory holds %v, want only account.json", names)
	}

	if err := s.Remove("users/alex/account.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("users/alex/account.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("read after remove: %v, want ErrNotExist", err)
	}
	if err := s.Remove("users/alex/account.json"); err != nil {
		t.Errorf("removing a missing file: %v", err)
	}
}

func TestRejectsPathsOutsideTheStore(t *testing.T) {
	dir := t.TempDir()
	s := Open(filepath.Join(dir, "config"))
	if err := os.Mkdir(filepath.Join(dir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", "/etc/passwd", "a/../../b", "", "."} {
		if err := s.Write(name, []byte("x")); err == nil {
			t.Errorf("Write(%q) succeeded", name)
		}
		if _, err := s.Read(name); err == nil {
			t.Errorf("Read(%q) succeeded", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); err == nil {
		t.Error("a file was written outside the store")
	}
}

func TestListMissing(t *testing.T) {
	s := Open(t.TempDir())
	names, err := s.List("users")
	if err != nil || len(names) != 0 {
		t.Errorf("List of a missing directory = %v, %v; want empty", names, err)
	}

	gone := Open(filepath.Join(t.TempDir(), "no-stick"))
	if _, err := gone.List("users"); err == nil {
		t.Error("List succeeded although the store does not exist")
	}
}
