package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFirstVolumeFoldersAcrossFilesystems(t *testing.T) {
	for _, filesystem := range []string{"ext4", "xfs", "btrfs"} {
		t.Run(filesystem, func(t *testing.T) {
			m, _, store := newTestManager(t)
			req := CreateRequest{Name: "first", Level: "raid1", Filesystem: filesystem, Disks: []string{"sde", "sdf"}, Erase: []string{"sde", "sdf"}}
			if err := createVolume(m, context.Background(), req); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"vms", "appdata", "iso"} {
				if info, err := os.Stat(filepath.Join(m.MountPoint("first"), name)); err != nil || !info.IsDir() {
					t.Fatalf("missing %s: %v", name, err)
				}
			}
			var volumes []VolumeConfig
			if err := store.ReadJSON(configFile, &volumes); err != nil || volumes[0].InitializeFolders {
				t.Fatalf("initialization not completed: %+v, %v", volumes, err)
			}
			os.Remove(filepath.Join(m.MountPoint("first"), "iso"))
			m.mountAll(context.Background())
			if _, err := os.Stat(filepath.Join(m.MountPoint("first"), "iso")); !os.IsNotExist(err) {
				t.Fatal("recreated a deliberately deleted folder")
			}
			req.Name = "second"
			if err := createVolume(m, context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(m.MountPoint("second"), "vms")); !os.IsNotExist(err) {
				t.Fatal("populated a later volume")
			}
		})
	}
}

func TestFolderSetupDoesNotWriteToOverlayAndResumesAfterMount(t *testing.T) {
	m, runner, store := newTestManager(t)
	runner.fail = "mount "
	req := CreateRequest{Name: "first", Level: "raid0", Filesystem: "ext4", Disks: []string{"sde"}, Erase: []string{"sde"}}
	if err := createVolume(m, context.Background(), req); err == nil {
		t.Fatal("mount failure ignored")
	}
	if _, err := os.Stat(filepath.Join(m.MountPoint("first"), "vms")); !os.IsNotExist(err) {
		t.Fatal("created folders without a mounted volume")
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	volume, _ := m.find("first")
	if !volume.InitializeFolders {
		t.Fatal("lost pending setup after restart")
	}
	if err := m.initializeFolders(volume); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("unmounted setup: %v", err)
	}
	runner.mounted[m.MountPoint("first")] = true
	m.mountAll(context.Background())
	if volume, _ := m.find("first"); volume.InitializeFolders {
		t.Fatal("did not resume setup after mount")
	}
	writes := store.Changes()
	m.mountAll(context.Background())
	if store.Changes() != writes {
		t.Fatal("writes configuration every poll")
	}
}

func TestFolderSetupPreservesExistingContentsAndRejectsSymlinks(t *testing.T) {
	m, runner, _ := newTestManager(t)
	volume := VolumeConfig{Name: "first", InitializeFolders: true}
	m.volumes = []VolumeConfig{volume}
	point := m.MountPoint(volume.Name)
	os.MkdirAll(filepath.Join(point, "vms"), 0o700)
	file := filepath.Join(point, "vms/keep")
	os.WriteFile(file, []byte("keep"), 0o600)
	runner.mounted[point] = true
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(point, "appdata"))
	if err := m.initializeFolders(volume); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("followed a symlink: %v", err)
	}
	if data, _ := os.ReadFile(file); string(data) != "keep" {
		t.Fatal("changed existing content")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote outside volume")
	}
	m.ReadOnly = true
	if err := m.initializeFolders(volume); !errors.Is(err, ErrReadOnly) {
		t.Fatal("development mode wrote folders")
	}
}
