package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// planOf walks a job's selection like planJob does, for tests that build
// jobs by hand.
func planOf(job *Job, sr *os.Root) func(func(string, transferEntry) error) error {
	return func(add func(string, transferEntry) error) error {
		for _, rel := range job.Names {
			if err := walkEntries(sr, rel, func(p string, info fs.FileInfo) error {
				if info.Mode().IsRegular() {
					return add(p, transferEntry{Source: copyVersion(info)})
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

// put writes a file and the folders on its way.
func put(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree lists the files and folders below dir, internal ones included.
func tree(dir string) string {
	var out []string
	_ = filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error { // the listing is for messages only
		if err == nil && p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, " ")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// finishAll waits for a job of any account state to end.
func finishJob(t *testing.T, m *Manager, user, id string) Job {
	t.Helper()
	for range 600 {
		for _, job := range m.Jobs(user) {
			if job.ID == id && job.State != JobRunning {
				return job
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the job did not finish")
	return Job{}
}

func problemCodes(job Job) map[string]string {
	out := map[string]string{}
	for _, p := range job.Problems {
		out[p.Path] = p.Code
	}
	return out
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads files without permission")
	}
}
