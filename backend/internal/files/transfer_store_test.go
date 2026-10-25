package files

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	bolt "go.etcd.io/bbolt"
)

func newStoreFixture(t *testing.T) (*Manager, *Job, Root, Root) {
	t.Helper()
	m, src, dst := durableRoots(t)
	if err := os.WriteFile(filepath.Join(src.Dir, "a"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	sr, err := os.OpenRoot(src.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	job := &Job{ID: "0123456789abcdef", From: src.ID, To: dst.ID, Names: []string{"a"}, Files: 1, Bytes: 1, State: JobRunning, user: "alex", c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}}
	if err := m.prepareTransfer(job, src, dst, sr); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(job.closeStore)
	return m, job, src, dst
}

func TestPagedTransferBeyondMillionEntriesAndTwoGiB(t *testing.T) {
	if testing.Short() {
		t.Skip("large on-volume index regression")
	}
	m, j, _, _ := newStoreFixture(t)
	const count = 1_000_001
	store := j.durable.store
	store.keepOpen()
	// Stress metadata rather than allocating a million actual source inodes.
	// Source enumeration/copying are covered separately with real fixture trees.
	for start := 0; start < count; start += 1024 {
		if err := store.transaction(true, func(tx *bolt.Tx) error {
			b := tx.Bucket(entryBucket)
			for i := start; i < min(start+1024, count); i++ {
				if err := putStoreEntry(b, fmt.Sprintf("photos/%09d", i), transferEntry{Source: "fixture-version"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if len(j.durable.Sources) != 0 || len(j.completed) != 0 || len(j.targets) != 0 {
		t.Fatal("indexed tree retained in RAM maps")
	}
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.OpenFile(j.ID+".db", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	// A sparse extension proves that file size itself is no longer a 2-GiB
	// rejection criterion, without writing gigabytes of artificial test data.
	if err := file.Truncate((2 << 30) + 4096); err != nil {
		t.Fatal(err)
	}
	file.Close()
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	jobs := restored.Jobs("alex")
	if len(jobs) != 1 || jobs[0].Blocked || !jobs[0].Retryable {
		t.Fatal(jobs)
	}
	entry, err := restored.jobs.list[0].entry("photos/001000000")
	if err != nil || entry.Source != "fixture-version" {
		t.Fatal(entry, err)
	}
}

func TestPagedDescriptorBlocksOldAppsAndUnknownOutcomeSurvives(t *testing.T) {
	m, j, _, _ := newStoreFixture(t)
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var descriptor transfer
	if err := volumedata.ReadJSON(root, j.ID+".json", &descriptor, 1<<20); err != nil {
		t.Fatal(err)
	}
	if descriptor.Format != 2 || !descriptor.Job.Blocked || descriptor.Job.Operation == "" {
		t.Fatal("old app could replay unsupported checkpoint", descriptor.Job)
	}
	if err := m.saveTransfer(j, "publish a"); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	jobs := restored.Jobs("alex")
	if len(jobs) != 1 || !jobs[0].Blocked || jobs[0].Retryable {
		t.Fatal("unknown publication replayable", jobs)
	}
}

func TestIncompleteDatabaseIsVisibleAndRemovable(t *testing.T) {
	m, j, _, _ := newStoreFixture(t)
	if err := j.durable.store.transaction(true, func(tx *bolt.Tx) error {
		b := tx.Bucket(stateBucket)
		var state transferStoreState
		if err := json.Unmarshal(b.Get(stateKey), &state); err != nil {
			return err
		}
		state.Ready = false
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return b.Put(stateKey, data)
	}); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	jobs := restored.Jobs("alex")
	if len(jobs) != 1 || !jobs[0].Blocked || jobs[0].Retryable {
		t.Fatal(jobs)
	}
	if err := restored.ForgetTransfer("alex", j.ID); err != nil {
		t.Fatal(err)
	}
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := root.Stat(j.ID + ".db"); !os.IsNotExist(err) {
		t.Fatal("database not removed", err)
	}
}

func TestMissingDatabaseNeverBecomesAnEmptyReplayablePlan(t *testing.T) {
	m, j, _, _ := newStoreFixture(t)
	root, err := m.transferRoot("data", "alex")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Remove(j.ID + ".db"); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(os.Getgid())
	restored.MountRoot = m.MountRoot
	restored.Mounted = m.Mounted
	jobs := restored.Jobs("alex")
	if len(jobs) != 1 || !jobs[0].Blocked || jobs[0].Retryable {
		t.Fatal(jobs)
	}
	if _, err := root.Stat(j.ID + ".db"); !os.IsNotExist(err) {
		t.Fatal("missing database silently recreated", err)
	}
}

func TestCopyLargerThanFreeSpaceDoesNotWriteUserData(t *testing.T) {
	m, src, dst := durableRoots(t)
	_, free := Usage(dst)
	file, err := os.Create(filepath.Join(src.Dir, "too-large"))
	if err != nil {
		t.Fatal(err)
	}
	// Sparse test input uses no data blocks. The one-GiB margin tolerates small
	// unrelated writes/cleanup on the shared test filesystem.
	if err := file.Truncate(int64(free) + (1 << 30)); err != nil {
		file.Close()
		t.Skip(err)
	}
	file.Close()
	if _, err := m.StartCopy(src, []string{"too-large"}, dst, ".", false, ConflictRename, "alex", os.Getuid()); err != ErrNoSpace {
		t.Fatal("insufficient target space accepted", err)
	}
	entries, err := os.ReadDir(dst.Dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("destination changed", entries, err)
	}
}
