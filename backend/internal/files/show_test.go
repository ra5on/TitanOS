package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Show lists all a root shows; everything else is a secret.
func TestRootShowIsAPositiveList(t *testing.T) {
	root := Root{Hide: []string{"users"}, Show: []string{"apps/", "system/settings.json"}}
	for rel, want := range map[string]bool{
		".": false, "": false, "apps": false, "apps/web/compose.yaml": false, "apps/web/.env": true,
		"system": false, "system/settings.json": false, "system/settings.json.prev": false,
		"system/ups.json": true, "homeassistant": true, "homeassistant/settings.json": true,
		"users": true, "users/alex/account.json": true, "appsx": true, "system/settings.jsonx": true,
	} {
		if got := root.secret(rel); got != want {
			t.Errorf("secret(%q) = %v, want %v", rel, got, want)
		}
	}
}

// A root handed to a token, an MCP client or the AI agent hides what TitanOS
// keeps on the volume and every .env file, on any level.
func TestMachineRootHidesInternalFiles(t *testing.T) {
	machine, person := Root{Machine: true}, Root{}
	for rel, want := range map[string]bool{
		".titanos": true, ".titanos/docker/app/.env": true, "Docs/.TitanOS/x": true,
		"app/.env": true, "app/prod.env": true, "up/film.mkv.titanos-part": true,
		"Docs/readme.txt": false, "Docs/environment.txt": false, "": false,
	} {
		if got := machine.secret(rel); got != want {
			t.Errorf("machine root: secret(%q) = %v, want %v", rel, got, want)
		}
		if person.secret(rel) {
			t.Errorf("a person in the web interface must still see %q", rel)
		}
	}
}

// What a machine root hides cannot be renamed, deleted or copied under
// another name either: a token could otherwise rename an .env into sight
// and read it, or destroy it.
func TestMachineRootCannotMoveHiddenFiles(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Machine = true
	env := filepath.Join(root.Dir, "urlaub/.env")
	os.WriteFile(env, []byte("DB_PASSWORD=hunter2"), 0o600)
	if err := m.Rename(root, "urlaub/.env", "urlaub/env.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename: %v", err)
	}
	if err := m.Rename(root, "notiz.txt", "urlaub/.env"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename onto a hidden name: %v", err)
	}
	if err := m.Delete(root, []string{"urlaub/.env"}, "alex", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete for good: %v", err)
	}
	if _, err := m.DeleteWithUndo(root, []string{"urlaub/.env"}, "alex", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete into the bin: %v", err)
	}
	if _, err := m.StartCopyChoices(root, []string{"urlaub/.env"}, root, "", false, ConflictRename, "alex", os.Getuid(), map[string]CopyChoice{"urlaub/.env": {Action: "copy", Name: "leak.txt"}}); err == nil {
		t.Error("copy of a hidden file started")
	}
	// Inside a folder that is copied, the hidden file stays behind, also
	// with a choice that names it.
	if _, err := m.StartCopyChoices(root, []string{"urlaub"}, root, "", false, ConflictRename, "alex", os.Getuid(), map[string]CopyChoice{"urlaub/.env": {Action: "copy", Name: "leak.txt"}}); err == nil {
		t.Error("choice for a hidden file accepted")
	}
	job, err := m.StartCopy(root, []string{"urlaub"}, root, "", false, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && m.Busy() {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "urlaub (2)", "strand.jpg")); err != nil {
		t.Fatalf("copy %s did not run: %v", job.ID, err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "urlaub (2)", ".env")); err == nil {
		t.Error("the hidden file was copied along")
	}
	if data, _ := os.ReadFile(env); string(data) != "DB_PASSWORD=hunter2" {
		t.Errorf("the .env changed: %q", data)
	}
}
