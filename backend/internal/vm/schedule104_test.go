package vm

import (
	"context"
	"testing"
	"time"
)

// A scheduled snapshot that finds the VM busy (a backup holds it) is not
// lost for the day: it is tried again within its hour once the VM is free.
func TestBusyVMGetsItsScheduledSnapshotLater(t *testing.T) {
	m, v := snapshotFixture(t)
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	if e := m.SetSnapshotSchedule(v.Name, SnapshotSchedule{Enabled: true, Time: "03:00", Days: []int{5}, Keep: 3}); e != nil {
		t.Fatal(e)
	}
	lock, err := m.operation(v.Name)
	if err != nil {
		t.Fatal(err)
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), now)
	lock.Unlock()
	// Not before the wait: the loop does not hammer a busy VM.
	m.runSnapshotSchedules(context.Background(), v.volumes(), now.Add(5*time.Second))
	if s := waitSnapshot(t, m, v); len(s.Snapshots) != 0 {
		t.Fatalf("tried again at once: %+v", s.Snapshots)
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), now.Add(2*time.Minute))
	if s := waitSnapshot(t, m, v); len(s.Snapshots) != 1 {
		t.Fatalf("the day's snapshot is lost: %+v", s)
	}
}
