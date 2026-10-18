package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// finish waits for a job to leave the running state.
func finish(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	for range 400 {
		for _, job := range m.Jobs("alex") {
			if job.ID == id && job.State != JobRunning {
				return job
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the job did not finish")
	return Job{}
}

func twoRoots(t *testing.T) (*Manager, Root, Root) {
	t.Helper()
	m, src, base := newRoot(t)
	dstDir := filepath.Join(base, "usb")
	os.MkdirAll(filepath.Join(dstDir, "ziel"), 0o755)
	dst := Root{ID: "usb/stick", Name: "stick", Kind: "usb", Dir: dstDir, Writable: true}
	return m, src, dst
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCopyFolderBetweenRoots(t *testing.T) {
	m, src, dst := twoRoots(t)
	os.WriteFile(filepath.Join(src.Dir, "urlaub/.hidden.titanos-part"), []byte("half"), 0o600)
	os.Symlink("/etc/passwd", filepath.Join(src.Dir, "urlaub/link"))
	job, err := m.StartCopy(src, []string{"urlaub", "notiz.txt"}, dst, "ziel", false, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if job.Files != 2 || job.Bytes != int64(len("strand")+len("notiz")) {
		t.Errorf("plan = %+v", job)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone || job.Copied != job.Bytes || job.Done != 2 {
		t.Fatalf("job = %+v", job)
	}
	if read(t, filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg")) != "strand" || read(t, filepath.Join(dst.Dir, "ziel/notiz.txt")) != "notiz" {
		t.Error("content")
	}
	// Half uploads and links are not copied; the original stays.
	entries, _ := os.ReadDir(filepath.Join(dst.Dir, "ziel/urlaub"))
	if len(entries) != 1 {
		t.Errorf("copied internals or links: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(src.Dir, "notiz.txt")); err != nil {
		t.Error("a copy removed the original")
	}
}

func TestCopyConflicts(t *testing.T) {
	m, src, dst := twoRoots(t)
	os.WriteFile(filepath.Join(dst.Dir, "ziel/notiz.txt"), []byte("old"), 0o644)
	for conflict, check := range map[string]func(){
		ConflictSkip: func() {
			if read(t, filepath.Join(dst.Dir, "ziel/notiz.txt")) != "old" {
				t.Error("skip replaced the file")
			}
		},
		ConflictReplace: func() {
			if read(t, filepath.Join(dst.Dir, "ziel/notiz.txt")) != "notiz" {
				t.Error("replace kept the old file")
			}
		},
		ConflictRename: func() {
			if read(t, filepath.Join(dst.Dir, "ziel/notiz (2).txt")) != "notiz" {
				t.Error("rename made no second file")
			}
		},
	} {
		os.WriteFile(filepath.Join(dst.Dir, "ziel/notiz.txt"), []byte("old"), 0o644)
		os.Remove(filepath.Join(dst.Dir, "ziel/notiz (2).txt"))
		job, err := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", false, conflict, "alex", os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		if got := finish(t, m, job.ID); got.State != JobDone {
			t.Fatalf("%s: %+v", conflict, got)
		}
		check()
	}
}

func TestMove(t *testing.T) {
	m, src, dst := twoRoots(t)
	job, err := m.StartCopy(src, []string{"urlaub"}, dst, "ziel", true, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if got := finish(t, m, job.ID); got.State != JobDone {
		t.Fatalf("job = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(src.Dir, "urlaub")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the original is still there after a move")
	}
	if read(t, filepath.Join(dst.Dir, "ziel/urlaub/strand.jpg")) != "strand" {
		t.Error("moved content")
	}
	// Inside one root a move is a rename.
	os.MkdirAll(filepath.Join(src.Dir, "archiv"), 0o755)
	job, err = m.StartCopy(src, []string{"notiz.txt"}, src, "archiv", true, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	finish(t, m, job.ID)
	if read(t, filepath.Join(src.Dir, "archiv/notiz.txt")) != "notiz" {
		t.Error("rename inside the root")
	}
}

func TestCopyRefusals(t *testing.T) {
	m, src, dst := twoRoots(t)
	readOnly := dst
	readOnly.Writable = false
	for name, tc := range map[string]struct {
		from  Root
		paths []string
		to    Root
		dir   string
		move  bool
		want  error
	}{
		"read-only target":      {src, []string{"notiz.txt"}, readOnly, "ziel", false, ErrReadOnly},
		"move from read-only":   {readOnly, []string{"ziel"}, src, "", true, ErrReadOnly},
		"into itself":           {src, []string{"urlaub"}, src, "urlaub", false, ErrInvalid},
		"leaves the root":       {src, []string{"../geheim.txt"}, dst, "ziel", false, ErrInvalid},
		"internal name":         {src, []string{".recycle"}, dst, "ziel", false, ErrInvalid},
		"missing target folder": {src, []string{"notiz.txt"}, dst, "nirgends", false, ErrNotFound},
		"missing source":        {src, []string{"nicht-da"}, dst, "ziel", false, ErrNotFound},
		"no paths":              {src, nil, dst, "ziel", false, ErrInvalid},
	} {
		if _, err := m.StartCopy(tc.from, tc.paths, tc.to, tc.dir, tc.move, "", "alex", os.Getuid()); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	if _, err := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", false, "overwrite", "alex", 1); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown conflict rule: %v", err)
	}
}

func TestJobsBelongToTheirUser(t *testing.T) {
	m, src, dst := twoRoots(t)
	job, _ := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", false, "", "alex", os.Getuid())
	finish(t, m, job.ID)
	if len(m.Jobs("mia")) != 0 {
		t.Error("another user sees the job")
	}
	if err := m.CancelJob("mia", job.ID); !errors.Is(err, ErrNoJob) {
		t.Errorf("cancelling another's job: %v", err)
	}
	if err := m.CancelJob("alex", job.ID); err != nil {
		t.Errorf("cancel: %v", err)
	}
}

func TestCopyFullLengthFileName(t *testing.T) {
	m, src, dst := twoRoots(t)
	name := strings.Repeat("a", 251) + ".txt"
	if err := os.WriteFile(filepath.Join(src.Dir, name), []byte("long name"), 0600); err != nil {
		t.Fatal(err)
	}
	job, err := m.StartCopy(src, []string{name}, dst, "ziel", false, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if job = finish(t, m, job.ID); job.State != JobDone {
		t.Fatalf("%+v", job)
	}
	if read(t, filepath.Join(dst.Dir, "ziel", name)) != "long name" {
		t.Fatal("wrong content")
	}
}
