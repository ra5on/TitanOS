package lxc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Removing a container with its data forgets it only once the data are
// gone. If deleting them fails halfway (a snapshot that cannot be
// removed), the container stays listed as failed, cannot be started on
// what is left, and removing it can be tried again.
func TestFailedDataDeletionKeepsTheContainer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root deletes from a read-only folder")
	}
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	env.btrfs = true
	env.create(t, Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie"})
	dir := filepath.Join(m.mountRoot, "data", "lxc", "web")
	stuck := filepath.Join(dir, "snapshots", "20261027-143000")
	os.MkdirAll(filepath.Join(stuck, "etc"), 0o755)
	os.WriteFile(filepath.Join(stuck, "etc", "hostname"), []byte("web"), 0o644)
	os.Chmod(stuck, 0o500)
	t.Cleanup(func() { os.Chmod(stuck, 0o755) })
	if err := m.Delete(ctx, "web", true, "web"); err == nil {
		t.Fatal("a deletion that failed reported success")
	}
	var listed *Status
	for _, s := range m.List(mounted) {
		if s.Name == "web" {
			listed = &s
		}
	}
	if listed == nil || listed.Job == nil || listed.Job.State != "failed" || listed.Job.Code != "delete_failed" {
		t.Fatalf("the container with half its data is gone from the list or not failed: %+v", listed)
	}
	if err := m.Start(ctx, "web", mounted); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("started on half deleted data: %v", err)
	}
	// After a restart it is still there and cannot start.
	m.Load()
	if err := m.Start(ctx, "web", mounted); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("after a restart: %v", err)
	}
	os.Chmod(stuck, 0o755)
	if err := m.Delete(ctx, "web", true, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("folder left")
	}
	if _, err := m.get("web"); err == nil {
		t.Fatal("container still known")
	}
}
