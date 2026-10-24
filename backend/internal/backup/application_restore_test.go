package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplicationRestoreWithRealResticCreatesIndependentVerifiedCopies(t *testing.T) {
	binary := os.Getenv("RESTIC_TEST_BINARY")
	if binary == "" {
		t.Skip("RESTIC_TEST_BINARY selects isolated restic integration")
	}
	e := newEnv(t)
	e.m.restic = binary
	e.mount(t, "cold")
	job := s3Job()
	job.Destination = Destination{Type: "local", Volume: "cold", Folder: "app-repository"}
	source := e.m.sources()[0].Path
	marker := filepath.Join(source, "application.txt")
	if err := os.WriteFile(marker, []byte("saved application state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	wait := func() State {
		t.Helper()
		for range 3000 {
			state := e.state(job.ID)
			if !state.Running {
				return state
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("restic timeout")
		return State{}
	}
	if err := e.m.Start(job.ID); err != nil {
		t.Fatal(err)
	}
	if st := wait(); st.LastOK == nil {
		t.Fatal(st)
	}
	job.Applications = []Application{{Kind: "docker", Name: "test-app", Method: "stop"}}
	job.AllowStop = true
	if err := e.m.Update(job.ID, job, volumes); err != nil {
		t.Fatal(err)
	}
	snapshots, err := e.m.Snapshots(context.Background(), job.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatal(snapshots, err)
	}
	var previous string
	for range 2 {
		target, err := e.m.RestoreApplication(context.Background(), job.ID, snapshots[0].ID, "tank")
		if err != nil {
			t.Fatal(err)
		}
		st := wait()
		if st.ApplicationRestore == nil || !st.ApplicationRestore.Verified || st.ApplicationRestore.Error != "" {
			t.Fatal(st)
		}
		if previous == target {
			t.Fatal("reused destination")
		}
		previous = target
		data, err := os.ReadFile(filepath.Join(target, marker[1:]))
		if err != nil || string(data) != "saved application state" {
			t.Fatal(string(data), err)
		}
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "saved application state" {
		t.Fatal("changed original", err)
	}
}

func TestInvalidBackupStateIsUnknownAndCanBeReloaded(t *testing.T) {
	e := newEnv(t)
	id := localJob(t, e)
	job, _ := e.m.find(id)
	file := e.m.stateFile(job)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	e.m.mu.Lock()
	delete(e.m.states, id)
	e.m.mu.Unlock()
	views := e.m.List()
	if len(views) != 1 || views[0].StateKnown || views[0].State.StatusError == "" {
		t.Fatal(views)
	}
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if !e.m.List()[0].StateKnown {
		t.Fatal("transient state error was cached permanently")
	}
}
