package backup

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs in CI with its installed restic. It also checks that a failed export
// never leaves a partial snapshot in an already populated repository.
func TestPersonalJobWithRestic(t *testing.T) {
	program, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic is not installed")
	}
	e := newEnv(t)
	e.m.restic = program
	e.m.mountsFile = filepath.Join(e.dir, "mounts")
	if err := os.WriteFile(e.m.mountsFile, []byte("disk "+filepath.Join(e.m.mountRoot, "cold")+" ext4 rw 0 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	job := s3Job()
	job.ID = "private"
	job.Sources = nil
	job.Keep = Keep{}
	job.Destination = Destination{Type: "local", Volume: "cold", Folder: "vault"}
	job.Personal = &PersonalSource{Account: "alex", ID: "0123456789abcdef0123456789abcdef"}
	const archive = "age-encryption.org/v1\nfixture"
	e.m.PersonalExport = func(_ context.Context, out io.Writer, source PersonalSource, local string) error {
		if source != *job.Personal || local != "cold" {
			t.Fatal("wrong source or destination")
		}
		_, err := io.WriteString(out, archive)
		return err
	}
	if err = e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	if err = e.m.Start(job.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.result(t); got != "private: ok" {
		t.Fatalf("run: %s", got)
	}
	snapshots, err := e.m.Snapshots(t.Context(), job.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots: %v %v", snapshots, err)
	}
	password, _ := e.m.Password(job.ID)
	cmd := exec.CommandContext(t.Context(), program, "--no-cache", "--repo", filepath.Join(e.m.mountRoot, "cold", "vault"), "dump", snapshots[0].ID, "/TitanOS-alex.tar.age")
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+password)
	out, err := cmd.Output()
	if err != nil || string(out) != archive {
		t.Fatalf("restored bytes differ: %v", err)
	}
	e.m.PersonalExport = func(_ context.Context, out io.Writer, _ PersonalSource, _ string) error {
		_, _ = io.WriteString(out, "incomplete")
		return io.ErrUnexpectedEOF
	}
	if err = e.m.Start(job.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.result(t); !strings.Contains(got, "unexpected EOF") {
		t.Fatal(got)
	}
	snapshots, err = e.m.Snapshots(t.Context(), job.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("incomplete snapshot: %v %v", snapshots, err)
	}
}
