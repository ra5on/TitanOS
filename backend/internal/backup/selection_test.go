package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFoldersAndExcludes(t *testing.T) {
	e := newEnv(t)
	root := e.m.mountRoot
	for _, dir := range []string{"tank/media/Fotos/2024", "tank/media/Fotos/2025", "tank/media/Filme", "tank/media/.recycle/x", "tank/appdata/nextcloud", "cold/appdata/old", "cold/vms/win"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	os.Symlink("/etc", filepath.Join(root, "tank/media/link"))
	e.m.configDir = filepath.Join(filepath.Dir(root), "config")
	e.mount(t, "cold")

	job := s3Job()
	job.Folders = map[string][]string{"media": {"Fotos", "Gone"}}
	job.Skip = []string{"media/Fotos/2024"}
	job.Patterns = []string{"*.tmp"}
	job.Extras = []string{"appdata", "vms", "config"}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Start("cloud"); err != nil {
		t.Fatal(err)
	}
	if got := e.result(t); got != "cloud: ok" {
		t.Fatal(got)
	}
	calls := e.calls()
	for _, want := range []string{
		"--exclude " + filepath.Join(root, "tank/media/Fotos/2024"), "--exclude *.tmp",
		filepath.Join(root, "tank/media/Fotos"), filepath.Join(root, "tank/appdata"), filepath.Join(root, "cold/appdata"), filepath.Join(root, "cold/vms"), e.m.configDir,
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("%q missing in:\n%s", want, calls)
		}
	}
	// The whole share is not given when folders are chosen.
	if strings.Contains(calls, " "+filepath.Join(root, "tank/media")+"\n") || strings.HasSuffix(strings.TrimSpace(calls), filepath.Join(root, "tank/media")) {
		t.Errorf("whole share saved:\n%s", calls)
	}
	// A folder that is gone is said, and the backup still happens.
	if state := e.state("cloud"); !strings.Contains(state.Warning, "media/Gone") {
		t.Errorf("warning: %q", state.Warning)
	}
}

func TestLocalDestinationIsNotSaved(t *testing.T) {
	e := newEnv(t)
	root := e.m.mountRoot
	os.MkdirAll(filepath.Join(root, "tank/appdata"), 0o755)
	os.MkdirAll(filepath.Join(root, "cold/appdata"), 0o755)
	e.mount(t, "cold")
	sel := e.m.selection(Job{ID: "x", Extras: []string{"appdata"}, Destination: Destination{Type: "local", Volume: "cold"}})
	if len(sel.paths) != 1 || sel.paths[0] != filepath.Join(root, "tank/appdata") {
		t.Errorf("paths = %v", sel.paths)
	}
	// With nothing but extras, the job keeps what it knows on a volume that is saved from.
	job := Job{ID: "x", Extras: []string{"appdata"}, Destination: Destination{Type: "local", Volume: "tank"}}
	if file := e.m.stateFile(job); file != "" && strings.Contains(file, "/tank/") {
		t.Errorf("state file on the destination: %s", file)
	}
}

func TestInvalidSelection(t *testing.T) {
	e := newEnv(t)
	for name, change := range map[string]func(*Job){
		"nothing":         func(j *Job) { j.Sources = nil },
		"extra":           func(j *Job) { j.Extras = []string{"docker"} },
		"folder up":       func(j *Job) { j.Folders = map[string][]string{"media": {"../etc"}} },
		"absolute folder": func(j *Job) { j.Folders = map[string][]string{"media": {"/etc"}} },
		"foreign share":   func(j *Job) { j.Folders = map[string][]string{"photos": {"a"}} },
		"skip no share":   func(j *Job) { j.Skip = []string{"photos/a"} },
		"skip whole":      func(j *Job) { j.Skip = []string{"media"} },
		"pattern option":  func(j *Job) { j.Patterns = []string{"--password-file"} },
	} {
		job := s3Job()
		change(&job)
		if err := e.m.Create(job, volumes); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	job := s3Job()
	job.Sources, job.Extras = nil, []string{"config"}
	if err := e.m.Create(job, volumes); err != nil {
		t.Errorf("only the settings: %v", err)
	}
}

func TestFolderList(t *testing.T) {
	e := newEnv(t)
	root := e.m.mountRoot
	for _, dir := range []string{"tank/media/b", "tank/media/a/sub", "tank/media/.recycle"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	os.WriteFile(filepath.Join(root, "tank/media/file"), nil, 0o644)
	os.Symlink("/etc", filepath.Join(root, "tank/media/link"))
	got, err := e.m.Folders("media", "")
	if err != nil || strings.Join(got, ",") != "a,b" {
		t.Errorf("top level: %v, %v", got, err)
	}
	if got, err := e.m.Folders("media", "a"); err != nil || strings.Join(got, ",") != "sub" {
		t.Errorf("a: %v, %v", got, err)
	}
	if _, err := e.m.Folders("media", "../.."); !errors.Is(err, ErrInvalid) {
		t.Errorf("leaving the share: %v", err)
	}
	if _, err := e.m.Folders("media", "link"); err == nil {
		t.Error("followed a link out of the share")
	}
	if _, err := e.m.Folders("nope", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown share: %v", err)
	}
}

// mount makes a folder under the mount root count as a mounted volume.
func (e *env) mount(t *testing.T, volume string) {
	t.Helper()
	file := filepath.Join(filepath.Dir(e.m.mountRoot), "mounts")
	old, _ := os.ReadFile(file)
	line := "/dev/x " + filepath.Join(e.m.mountRoot, volume) + " ext4 rw 0 0\n"
	if err := os.WriteFile(file, append(old, line...), 0o644); err != nil {
		t.Fatal(err)
	}
	e.m.mountsFile = file
}
