package vm

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"strings"
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
	if err := m.RecoverClone(context.Background(), "fast", ops[0].ID, "finish", "wrong", []string{"fast"}); err == nil {
		t.Fatal("unconfirmed recovery")
	}
	if err := m.RecoverClone(context.Background(), "fast", ops[0].ID, "finish", ops[0].ID, []string{"fast"}); err != nil {
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
	if err := m.validClone(context.Background(), v, "data"); err == nil {
		t.Fatal("unsafe recovered disk accepted")
	}
}

func TestExternalDataFileImagesAreRejected(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	old := m.diskPath(v, v.Disks[0])
	v.Disks[0].File = "datafile.raw"
	if err := os.Rename(old, m.diskPath(v, v.Disks[0])); err != nil {
		t.Fatal(err)
	}
	m.vms[v.Name] = v
	if _, err := m.imageInfo(context.Background(), m.diskPath(v, v.Disks[0])); !errors.Is(err, ErrBadImage) {
		t.Fatal("external data file accepted", err)
	}
	if err := m.Clone(context.Background(), v.Name, CloneRequest{"copy", "fast"}, []string{"data", "fast"}); !errors.Is(err, ErrBadImage) {
		t.Fatal("clone with external data file accepted", err)
	}
	if err := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "test"}, v.volumes()); err == nil {
		if status := waitSnapshot(t, m, v); status.Job == nil || status.Job.State != "failed" || !strings.Contains(status.Job.Text, "external data file") {
			t.Fatalf("snapshot with external data file accepted: %+v", status.Job)
		}
	} else if !errors.Is(err, ErrBadImage) {
		t.Fatal(err)
	}
}

func TestCloneRefusesExistingTargetFolder(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	m.vms[v.Name] = v
	leftover := m.dir("fast", "copy")
	if err := os.MkdirAll(leftover, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := m.Clone(context.Background(), v.Name, CloneRequest{"copy", "fast"}, []string{"data", "fast"}); !errors.Is(err, ErrBadFolder) {
		t.Fatal("clone into an existing folder accepted", err)
	}
	if ops, err := m.CloneOperations("fast", []string{"fast"}); err != nil || len(ops) != 0 {
		t.Fatal("staging started", ops, err)
	}
}

// folderRacer creates the clone target while the copy is being verified,
// as a folder appearing between the up-front check and publication would.
type folderRacer struct {
	*fakeRunner
	target string
}

func (r *folderRacer) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "qemu-img" && args[0] == "compare" {
		if err := os.MkdirAll(r.target, 0o770); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(r.target, "foreign.raw"), []byte("foreign"), 0o600); err != nil {
			return "", err
		}
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

func TestClonePublishConflictKeepsForeignFolderAndCanBeDiscarded(t *testing.T) {
	m, v := snapshotFixture(t)
	v.Disks[0].SizeBytes = 3 * gib
	m.vms[v.Name] = v
	target := m.dir("fast", "copy")
	m.runner = &folderRacer{fakeRunner: m.runner.(*fakeRunner), target: target}
	if err := m.Clone(context.Background(), v.Name, CloneRequest{"copy", "fast"}, []string{"data", "fast"}); err != nil {
		t.Fatal(err)
	}
	if status := waitSnapshot(t, m, v); status.Job == nil || status.Job.State != "failed" {
		t.Fatalf("conflict not reported: %+v", status.Job)
	}
	ops, err := m.CloneOperations("fast", []string{"fast"})
	if err != nil || len(ops) != 1 || ops[0].Phase != "conflict" || !strings.Contains(ops[0].Error, "already exists") {
		t.Fatal(ops, err)
	}
	if err := m.RecoverClone(context.Background(), "fast", ops[0].ID, "finish", ops[0].ID, []string{"fast"}); !errors.Is(err, ErrBadFolder) {
		t.Fatal("finish over a foreign folder", err)
	}
	if _, err := m.get("copy"); err == nil {
		t.Fatal("conflicting clone registered")
	}
	// The journal stays readable by the previous app version: it keeps phase
	// "publishing" (which that version can finish or discard) and records the
	// conflict in the separate optional field.
	journal := filepath.Join(m.mountRoot, "fast", ".titanos", "vmclones", ops[0].ID, "operation.json")
	b, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Phase    string `json:"phase"`
		Conflict string `json:"conflict"`
	}
	if err := json.Unmarshal(b, &saved); err != nil || saved.Phase != "publishing" || saved.Conflict != target {
		t.Fatal("journal not compatible", string(b), err)
	}
	// Without the new field (as an older version would rewrite it) the
	// operation is a plain interrupted publication, still discardable.
	if err := os.WriteFile(journal, []byte(strings.Replace(string(b), `"conflict":`, `"ignored":`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if ops, _ := m.CloneOperations("fast", []string{"fast"}); len(ops) != 1 || ops[0].Phase != "publishing" {
		t.Fatal("journal not rewritten", ops)
	}
	if err := m.RecoverClone(context.Background(), "fast", ops[0].ID, "discard", ops[0].ID, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	if ops, err := m.CloneOperations("fast", []string{"fast"}); err != nil || len(ops) != 0 {
		t.Fatal("staged copy kept", ops, err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "foreign.raw")); err != nil || string(b) != "foreign" {
		t.Fatal("foreign folder touched", err)
	}
	if entries, _ := os.ReadDir(target); len(entries) != 1 {
		t.Fatal("foreign folder changed", entries)
	}
}

func TestCloneDiscardWhilePublishingOnlyBeforeRename(t *testing.T) {
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
	waitSnapshot(t, m, v)
	m.store = store
	ops, err := m.CloneOperations("fast", []string{"fast"})
	if err != nil || len(ops) != 1 || ops[0].Phase != "publishing" {
		t.Fatal(ops, err)
	}
	// The rename already happened: the published folder is the only copy.
	if err := m.RecoverClone(context.Background(), "fast", ops[0].ID, "discard", ops[0].ID, []string{"fast"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("discard after publication", err)
	}
	if err := m.RecoverClone(context.Background(), "fast", ops[0].ID, "finish", ops[0].ID, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
}
