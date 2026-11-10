package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

// titand ended between moving the old version aside and putting the new
// one in place: the old version comes back, the rests go.
func TestInterruptedUpdateIsUndoneOnce(t *testing.T) {
	dir := t.TempDir()
	previous := filepath.Join(dir, ".previous-123")
	stage := filepath.Join(dir, ".update-456")
	for _, d := range []string{previous, stage} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(previous, "demo.raw"), "old")
	write(filepath.Join(dir, "demo.raw"), "half of the new one")
	write(filepath.Join(stage, "demo.raw"), "new")

	m := &Manager{}
	m.undoInterruptedUpdate(dir)
	if data, _ := os.ReadFile(filepath.Join(dir, "demo.raw")); string(data) != "old" {
		t.Fatalf("installed plugin is %q", data)
	}
	for _, d := range []string{previous, stage} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s is still there", filepath.Base(d))
		}
	}
	// A running update has such a folder: it is left alone from now on.
	if err := os.Mkdir(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(previous, "demo.raw"), "moved aside by a running update")
	m.undoInterruptedUpdate(dir)
	if data, _ := os.ReadFile(filepath.Join(dir, "demo.raw")); string(data) != "old" {
		t.Fatalf("a running update was undone: %q", data)
	}
}
