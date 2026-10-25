package files

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"os"
	"path/filepath"
	"strings"
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
	if err := unknown.ForgetTransfer("alex", j.ID); err != nil {
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
	// Retain coverage of the old append journal's torn-tail import.
	j.durable.store = nil
	j.durable.Format = 0
	j.durable.initialized = false
	j.durable.Sources = map[string]string{}
	info, _ := sr.Lstat("a")
	j.durable.Sources["a"] = entryVersion(info)
	j.durable.Completed = map[string]bool{}
	j.durable.CompletedTop = map[string]bool{}
	j.durable.Targets = map[string]string{}
	j.durable.Skipped = map[string]bool{}
	j.durable.Destinations = map[string]string{}
	if err := m.saveTransfer(j, ""); err != nil {
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

func TestLargeTransferPlanAndRecovery(t *testing.T) {
	m, src, dst := durableRoots(t)
	if err := os.Mkdir(filepath.Join(src.Dir, "photos"), 0700); err != nil {
		t.Fatal(err)
	}
	// Match the rejected 85,177-file import. Only prepare/reload the real plan;
	// do not spend the test fsyncing a full copy for every empty fixture file.
	const count = 85177
	for i := range count {
		file, err := os.Create(filepath.Join(src.Dir, "photos", fmt.Sprintf("%06d.jpg", i)))
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	sr, err := os.OpenRoot(src.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	j := &Job{ID: "0123456789abcdef", user: "alex", Names: []string{"photos"}, Files: count, State: JobRunning, c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}}
	if err := m.prepareTransfer(j, src, dst, sr); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	jobs := restored.Jobs("alex")
	if len(jobs) != 1 || jobs[0].Files != count || !jobs[0].Retryable || jobs[0].Blocked {
		t.Fatal(jobs)
	}
	if version, err := restored.jobs.list[0].sourceVersion("photos/085176.jpg"); err != nil || version == "" {
		t.Fatal("large source plan lost", version, err)
	}
	if len(restored.jobs.list[0].durable.Sources) != 0 {
		t.Fatal("large plan loaded into a RAM map")
	}

}

func TestForgetAndClearTransfersPersistAndKeepUserFiles(t *testing.T) {
	m, src, dst := durableRoots(t)
	os.WriteFile(filepath.Join(src.Dir, "original"), []byte("source"), 0600)
	os.WriteFile(filepath.Join(dst.Dir, "copy"), []byte("destination"), 0600)
	os.WriteFile(filepath.Join(dst.Dir, "partial.titanos-part"), []byte("partial"), 0600)
	sr, _ := os.OpenRoot(src.Dir)
	defer sr.Close()
	for i := range 8 {
		j := &Job{ID: fmt.Sprintf("%016x", i), user: "alex", Names: []string{"original"}, Files: 1, Bytes: 6, State: JobDone, c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}}
		if err := m.prepareTransfer(j, src, dst, sr); err != nil {
			t.Fatal(err)
		}
		m.jobs.list = append(m.jobs.list, j)
	}
	m.jobs.list = append(m.jobs.list, &Job{ID: "running", user: "alex", State: JobRunning, c: &counters{}}, &Job{ID: "foreign", user: "sam", State: JobDone, c: &counters{}}, &Job{ID: "ram", user: "alex", State: JobFailed, c: &counters{}})
	if err := m.ForgetTransfer("sam", "0000000000000000"); !errors.Is(err, ErrNoJob) {
		t.Fatal("foreign removal", err)
	}
	if err := m.ForgetTransfer("alex", "running"); !errors.Is(err, ErrJobBusy) {
		t.Fatal("running removal", err)
	}
	removed := m.jobs.list[0]
	removed.durable.logMu.Lock()
	if err := m.ForgetTransfer("alex", removed.ID); !errors.Is(err, ErrJobBusy) {
		t.Fatal("active status write not protected", err)
	}
	removed.durable.logMu.Unlock()
	if err := m.ForgetTransfer("alex", "0000000000000000"); err != nil {
		t.Fatal(err)
	}
	if err := m.appendTransfer(removed, transferEvent{}); !errors.Is(err, ErrNoJob) {
		t.Fatal("late status recreated removed journal", err)
	}

	if err := m.ClearTransfers("alex"); err != nil {
		t.Fatal(err)
	}
	jobs := m.Jobs("alex")
	if len(jobs) != 1 || jobs[0].ID != "running" {
		t.Fatal(jobs)
	}
	if len(m.Jobs("sam")) != 1 {
		t.Fatal("foreign job removed")
	}
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	if jobs := restored.Jobs("alex"); len(jobs) != 0 {
		t.Fatal("forgotten entries returned", jobs)
	}
	for _, p := range []string{filepath.Join(src.Dir, "original"), filepath.Join(dst.Dir, "copy"), filepath.Join(dst.Dir, "partial.titanos-part")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("user file removed", p, err)
		}
	}
}

func TestClearTransferRetainsFailuresForRetry(t *testing.T) {
	m, src, dst := durableRoots(t)
	os.WriteFile(filepath.Join(src.Dir, "a"), []byte("a"), 0600)
	sr, _ := os.OpenRoot(src.Dir)
	defer sr.Close()
	j := &Job{ID: "0123456789abcdef", user: "alex", Names: []string{"a"}, Files: 1, Bytes: 1, State: JobDone, c: &counters{}}
	if err := m.prepareTransfer(j, src, dst, sr); err != nil {
		t.Fatal(err)
	}
	m.jobs.list = append(m.jobs.list, j)
	m.Mounted = func() []string { return nil }
	if err := m.ClearTransfers("alex"); err == nil {
		t.Fatal("unavailable volume hidden")
	}
	if len(m.Jobs("alex")) != 1 {
		t.Fatal("failed removal lost record")
	}
	m.Mounted = func() []string { return []string{"data"} }
	if err := m.ClearTransfers("alex"); err != nil {
		t.Fatal(err)
	}
}

func TestTransferJournalBeyondPreviousLimit(t *testing.T) {
	m, _, _ := durableRoots(t)
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.OpenFile("0123456789abcdef.events", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	writer := bufio.NewWriterSize(file, 256<<10)
	event := transferEvent{Progress: transferProgress{Copied: 123, Current: strings.Repeat("x", 16000)}}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	for size := 0; size <= 128<<20; size += len(data) {
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	var saved transfer
	if err := readTransferEvents(root, "0123456789abcdef", &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Job.Copied != 123 {
		t.Fatal("large journal lost progress")
	}
}
