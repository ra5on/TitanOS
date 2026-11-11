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

func TestLeftOutOfBackups(t *testing.T) {
	for _, name := range []string{"tls/ca-key.pem", "tls/ca-cert.pem", "tls/ca-leaf-ui.pem", "tls/ca-leaf-apps.pem", "tls/app-gate.key"} {
		if !LeftOutOfBackups(name) {
			t.Errorf("%s backed up", name)
		}
	}
	for _, name := range []string{"tls/custom-cert.pem", "tls/cert.pem", "users/alex/account.json", "tls/ca-leaf-ui.pem/x"} {
		if LeftOutOfBackups(name) {
			t.Errorf("%s left out", name)
		}
	}
}

// Sticks wear out: a write that changes nothing must not reach the stick,
// and must not look like a change of the configuration (PLAN 79-12).
func TestUnchangedWritesAndMissingRemovesDoNothing(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Write("system/energy.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if s.Changes() != 1 || s.Writes() != 1 {
		t.Fatalf("first write: changes=%d writes=%d", s.Changes(), s.Writes())
	}
	path := filepath.Join(s.Dir(), "system", "energy.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write("system/energy.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if s.Changes() != 1 || s.Writes() != 1 || !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the same content was written again: changes=%d writes=%d", s.Changes(), s.Writes())
	}
	if err := s.Write("system/energy.json", []byte("{ }\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := s.Read("system/energy.json"); string(data) != "{ }\n" || s.Changes() != 2 || s.Writes() != 2 {
		t.Fatalf("changed content not written: %q changes=%d writes=%d", data, s.Changes(), s.Writes())
	}
	// Same length, other bytes.
	if err := s.Write("system/energy.json", []byte("{x}\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := s.Read("system/energy.json"); string(data) != "{x}\n" || s.Writes() != 3 {
		t.Fatalf("content of the same length not written: %q", data)
	}
	for _, remove := range []func(string) error{s.Remove, s.RemoveAll} {
		if err := remove("system/never-there.json"); err != nil {
			t.Fatal(err)
		}
	}
	if s.Changes() != 3 || s.Writes() != 3 {
		t.Fatalf("removing what is not there counted: changes=%d writes=%d", s.Changes(), s.Writes())
	}
	if err := s.Remove("system/energy.json"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAll("system"); err != nil {
		t.Fatal(err)
	}
	if s.Changes() != 5 || s.Writes() != 5 {
		t.Fatalf("real removals not counted: changes=%d writes=%d", s.Changes(), s.Writes())
	}
}

// State of the moment and files no backup holds are written, but are no
// change of the configuration: they never start a backup of /config.
func TestStateFilesAreNoChangeOfTheConfiguration(t *testing.T) {
	s := Open(t.TempDir())
	for _, name := range append(append([]string{}, State...), "tls/ca-leaf-ui.pem", "tls/app-gate.key") {
		if err := s.Write(name, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if data, err := s.Read(name); err != nil || string(data) != "x" {
			t.Fatalf("%s not written: %q %v", name, data, err)
		}
		if err := s.Remove(name); err != nil {
			t.Fatal(err)
		}
		if s.Changes() != 0 {
			t.Fatalf("%s counted as a change of the configuration", name)
		}
	}
	if want := uint64(2 * (len(State) + 2)); s.Writes() != want {
		t.Fatalf("writes=%d, want %d", s.Writes(), want)
	}
	for _, name := range []string{"users/alex/account.json", "system/backup.json", "sessions/other.json", "firewall/settings.json"} {
		if IsState(name) {
			t.Fatalf("%s is configuration, not state", name)
		}
	}
	if err := s.Write("firewall/settings.json", []byte("{}")); err != nil || s.Changes() != 1 {
		t.Fatalf("configuration not counted: %v changes=%d", err, s.Changes())
	}
}
