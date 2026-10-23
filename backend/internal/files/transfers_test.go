package files

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"os"
	"path/filepath"
	"testing"
)

func durableRoots(t *testing.T) (*Manager, Root, Root) {
	t.Helper()
	base := t.TempDir()
	src := Root{ID: "share/source", Kind: "share", Volume: "data", Dir: filepath.Join(base, "data", "source"), Writable: true}
	dst := Root{ID: "share/target", Kind: "share", Volume: "data", Dir: filepath.Join(base, "data", "target"), Writable: true}
	os.MkdirAll(src.Dir, 0700)
	os.MkdirAll(dst.Dir, 0700)
	m := NewManager(os.Getgid())
	m.MountRoot = base
	m.Mounted = func() []string { return []string{"data"} }
	return m, src, dst
}
func TestTransferRecoveryRequiresAccessIdentityAndKnownPublication(t *testing.T) {
	m, src, dst := durableRoots(t)
	os.WriteFile(filepath.Join(src.Dir, "a"), []byte("a"), 0600)
	sr, _ := os.OpenRoot(src.Dir)
	defer sr.Close()
	j := &Job{ID: "0123456789abcdef", From: src.ID, To: dst.ID, Names: []string{"a"}, Dir: ".", State: JobRunning, Files: 1, Bytes: 1, user: "alex", c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}, conflict: ConflictRename, sourceDir: src.Dir, destinationDir: dst.Dir}
	if err := m.prepareTransfer(j, src, dst, sr); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	views := restored.Jobs("alex")
	if len(views) != 1 || !views[0].Durable || !views[0].Retryable {
		t.Fatal(views)
	}
	if len(restored.Jobs("sam")) != 0 {
		t.Fatal("foreign transfer exposed")
	}
	if _, err := restored.RetryCopy("alex", j.ID, src, Root{ID: dst.ID, Dir: dst.Dir, Writable: false}, os.Getuid()); err == nil {
		t.Fatal("current permissions ignored")
	}
	resumed, err := restored.RetryCopy("alex", j.ID, src, dst, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if finish(t, restored, resumed.ID).State != JobDone {
		t.Fatal("resume failed")
	}
	if err := m.saveTransfer(j, "publish a"); err != nil {
		t.Fatal(err)
	}
	unknown := NewManager(os.Getgid())
	unknown.MountRoot = m.MountRoot
	unknown.Mounted = m.Mounted
	v := unknown.Jobs("alex")
	if len(v) != 1 || !v[0].Blocked || v[0].Retryable {
		t.Fatal(v)
	}
	if _, err := unknown.RetryCopy("alex", j.ID, src, dst, os.Getuid()); err == nil {
		t.Fatal("unknown publication replayed")
	}
	if err := unknown.ForgetTransfer("alex", j.ID, j.ID); err != nil {
		t.Fatal(err)
	}
}
func TestTransferChangedSourceAndTargetAreRetained(t *testing.T) {
	m, src, dst := durableRoots(t)
	os.WriteFile(filepath.Join(src.Dir, "a"), []byte("old"), 0600)
	sr, _ := os.OpenRoot(src.Dir)
	defer sr.Close()
	dr, _ := os.OpenRoot(dst.Dir)
	defer dr.Close()
	j := &Job{ID: "0123456789abcdef", From: src.ID, To: dst.ID, Names: []string{"a"}, Dir: ".", State: JobRunning, user: "alex", c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}, conflict: ConflictRename, sourceDir: src.Dir, destinationDir: dst.Dir}
	j.Files = 1
	j.Bytes = 3
	m.prepareTransfer(j, src, dst, sr)
	os.WriteFile(filepath.Join(src.Dir, "a"), []byte("changed"), 0600)
	if err := m.copyEntry(context.Background(), j, sr, dr, "a", "a", ConflictRename, os.Getuid()); err == nil {
		t.Fatal("changed source accepted")
	}
	root, _ := volumedata.Open(m.MountRoot, "data", "transfers", m.Mounted())
	defer root.Close()
	if _, err := os.Stat(filepath.Join(dst.Dir, "a")); !os.IsNotExist(err) {
		t.Fatal("destination unexpectedly written")
	}
}

func TestTransferLogKeepsTornCompletionBlockedAndCleansOnlyKnownTemporaryInode(t *testing.T) {
	m, src, dst := durableRoots(t)
	os.WriteFile(filepath.Join(src.Dir, "a"), []byte("a"), 0600)
	sr, _ := os.OpenRoot(src.Dir)
	defer sr.Close()
	dr, _ := os.OpenRoot(dst.Dir)
	defer dr.Close()
	j := &Job{ID: "0123456789abcdef", From: src.ID, To: dst.ID, Names: []string{"a"}, Dir: ".", State: JobRunning, Files: 1, Bytes: 1, user: "alex", c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}, conflict: ConflictRename, sourceDir: src.Dir, destinationDir: dst.Dir}
	if err := m.prepareTransfer(j, src, dst, sr); err != nil {
		t.Fatal(err)
	}
	part := ".titanos-copy-0123456789ab.titanos-part"
	file, err := dr.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString("partial")
	if err := m.transferPart(j, file, part); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := m.verifyTransfer(j, src, dst, dr); err != nil {
		t.Fatal(err)
	}
	if _, err := dr.Stat(part); !os.IsNotExist(err) {
		t.Fatal("known partial file retained", err)
	}
	if err := m.saveTransfer(j, "publish a"); err != nil {
		t.Fatal(err)
	}
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	events, _ := root.OpenFile(j.ID+".events", os.O_APPEND|os.O_WRONLY, 0600)
	events.WriteString(`{"pending":"","rel":"a"`)
	events.Close()
	var saved transfer
	if err := volumedata.ReadJSON(root, j.ID+".json", &saved, 32<<20); err != nil {
		t.Fatal(err)
	}
	if err := readTransferEvents(root, j.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Pending != "publish a" {
		t.Fatal("torn completion erased publication fence")
	}
}
func TestTransferProgressPersistsOnlyCommittedBytes(t *testing.T) {
	j := &Job{State: JobRunning, c: &counters{}}
	j.c.copied.Store(300)
	j.c.committed.Store(100)
	if p := progressLocked(j); p.Copied != 100 {
		t.Fatal("partial bytes would be counted twice", p)
	}
}
