package vm

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestCloneIndependentDisksFirmwareAndIdentifiers(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	v.MAC = newMAC()
	v.Autostart = true
	m.vms[v.Name] = v
	if err := m.Clone(context.Background(), v.Name, CloneRequest{Name: "copy", Volume: "fast"}, []string{"data", "fast"}); err != nil {
		t.Fatal(err)
	}
	status := waitSnapshot(t, m, v)
	if status.Job != nil {
		t.Fatal(status.Job)
	}
	clone, err := m.get("copy")
	if err != nil {
		t.Fatal(err)
	}
	if clone.UUID == v.UUID || clone.MAC == v.MAC || clone.Autostart || clone.Volume != "fast" || clone.Disks[0].Path != "" {
		t.Fatalf("bad clone %+v", clone)
	}
	for _, file := range []string{"vdisk1.raw", "nvram.fd"} {
		b, err := os.ReadFile(filepath.Join(m.dir("fast", "copy"), file))
		if err != nil || len(b) == 0 {
			t.Fatal(file, err)
		}
	}
	os.WriteFile(m.diskPath(clone, clone.Disks[0]), []byte("new data"), 0600)
	original, _ := os.ReadFile(m.diskPath(v, v.Disks[0]))
	if string(original) != "original disk.raw" {
		t.Fatal("source changed")
	}
	ops, err := m.CloneOperations("fast", []string{"fast"})
	if err != nil || len(ops) != 0 {
		t.Fatal(ops, err)
	}
}
func TestCloneRecoveryDoesNotReplayCopyOrOverwrite(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	m.vms[v.Name] = v
	store := m.store
	broken := filepath.Join(t.TempDir(), "file")
	os.WriteFile(broken, []byte("no directory"), 0600)
	m.store = config.Open(broken)
	if err := m.Clone(context.Background(), v.Name, CloneRequest{Name: "copy", Volume: "fast"}, []string{"data", "fast"}); err != nil {
		t.Fatal(err)
	}
	status := waitSnapshot(t, m, v)
	if status.Job == nil || status.Job.State != "failed" {
		t.Fatal("configuration failure not retained")
	}
	ops, err := m.CloneOperations("fast", []string{"fast"})
	if err != nil || len(ops) != 1 || ops[0].Phase != "publishing" {
		t.Fatal(ops, err)
	}
	m.store = store
	if err := m.RecoverClone("fast", ops[0].ID, "finish", "wrong", []string{"fast"}); err == nil {
		t.Fatal("unconfirmed recovery")
	}
	if err := m.RecoverClone("fast", ops[0].ID, "finish", ops[0].ID, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.get("copy"); err != nil {
		t.Fatal(err)
	}
}
func TestCloneRejectsRunningAndBackingImages(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	old := m.diskPath(v, v.Disks[0])
	v.Disks[0].File = "overlay.raw"
	os.Rename(old, m.diskPath(v, v.Disks[0]))
	m.vms[v.Name] = v
	if err := m.Clone(context.Background(), v.Name, CloneRequest{"copy", "fast"}, []string{"data", "fast"}); err == nil {
		t.Fatal("backing chain accepted")
	}
}

func TestCloneTPMStateIsIndependent(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	v.TPM = true
	dir := filepath.Join(m.dir(v.Volume, v.folder()), "tpm")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, "permall"), []byte("TPM state"), 0600)
	m.vms[v.Name] = v
	if err := m.Clone(context.Background(), v.Name, CloneRequest{"copy", "fast"}, []string{"data", "fast"}); err != nil {
		t.Fatal(err)
	}
	status := waitSnapshot(t, m, v)
	if status.Job != nil {
		t.Fatal(status.Job)
	}
	data, err := os.ReadFile(filepath.Join(m.dir("fast", "copy"), "tpm", "permall"))
	if err != nil || string(data) != "TPM state" {
		t.Fatal(string(data), err)
	}
	os.WriteFile(filepath.Join(m.dir("fast", "copy"), "tpm", "permall"), []byte("changed"), 0600)
	original, _ := os.ReadFile(filepath.Join(dir, "permall"))
	if string(original) != "TPM state" {
		t.Fatal("TPM source changed")
	}
}
func TestCloneRecoveryRejectsExternalDiskPaths(t *testing.T) {
	m, v := snapshotFixture(t)
	v.UUID = newUUID()
	v.MAC = newMAC()
	v.Disks[0].SizeBytes = 3 * gib
	v.Disks[0].File = "../../outside.raw"
	v.Autostart = false
	if err := m.validClone(v, "data"); err == nil {
		t.Fatal("unsafe recovered disk accepted")
	}
}
