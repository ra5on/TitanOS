package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func localJob(t *testing.T, e *env) string {
	t.Helper()
	if err := e.m.Create(s3Job(), volumes); err != nil {
		t.Fatal(err)
	}
	return "cloud"
}

func waitIdle(t *testing.T, e *env, id string) State {
	t.Helper()
	for range 500 {
		if s := e.state(id); !s.Running {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("still running")
	return State{}
}

func TestCheckRepository(t *testing.T) {
	e := newEnv(t)
	id := localJob(t, e)
	var heard []string
	e.m.OnCheck = func(job string, err error) {
		heard = append(heard, job+":"+map[bool]string{true: "ok", false: "failed"}[err == nil])
	}
	if err := e.m.Check(id, 5); err != nil {
		t.Fatal(err)
	}
	s := waitIdle(t, e, id)
	if s.LastCheck == nil || s.CheckError != "" || !strings.Contains(e.calls(), "check --read-data-subset=5%") {
		t.Fatalf("%+v\n%s", s, e.calls())
	}
	e.flag(t, "corrupt", true)
	e.m.Check(id, 0)
	s = waitIdle(t, e, id)
	if !strings.Contains(s.CheckError, "data damaged") || s.LastError != "" {
		t.Fatalf("damaged: %+v", s)
	}
	if strings.Join(heard, ",") != id+":ok,"+id+":failed" {
		t.Fatalf("heard %v", heard)
	}
	if err := e.m.Check(id, 101); err == nil {
		t.Fatal("more than 100 %")
	}
}

func TestFilesOfASavedState(t *testing.T) {
	e := newEnv(t)
	id := localJob(t, e)
	entries, total, cut, err := e.m.Files(context.Background(), id, "22222222", "/mnt/tank/media", 0)
	if err != nil || total != 3 || cut {
		t.Fatalf("%v %d %v", err, total, cut)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	// Folders first, then by name; the folder itself is not listed.
	if strings.Join(names, ",") != "Alben,apfel.png,zebra.jpg" || entries[2].Size != 2048 {
		t.Fatalf("%v", entries)
	}
	for _, bad := range []string{"relative", "/mnt/../etc", "/a\nb"} {
		if _, _, _, err := e.m.Files(context.Background(), id, "22222222", bad, 0); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, _, _, err := e.m.Files(context.Background(), id, "--help", "/", 0); !errors.Is(err, ErrInvalid) {
		t.Fatal("snapshot option accepted")
	}
}

func TestRestoreChosenFiles(t *testing.T) {
	e := newEnv(t)
	id := localJob(t, e)
	target, err := e.m.RestoreFiles(id, "22222222", "tank", []string{"/mnt/tank/media/zebra.jpg", "/mnt/tank/media/Alben"})
	if err != nil {
		t.Fatal(err)
	}
	s := waitIdle(t, e, id)
	if s.RestoredTo != target || !strings.HasPrefix(target, filepath.Join(e.m.mountRoot, "tank", "restored")) {
		t.Fatalf("%+v %s", s, target)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.calls(), "--include /mnt/tank/media/zebra.jpg --include /mnt/tank/media/Alben") {
		t.Fatalf("%s", e.calls())
	}
	for _, paths := range [][]string{nil, {"/"}, {"../x"}, make([]string, 201)} {
		if _, err := e.m.RestoreFiles(id, "22222222", "tank", paths); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v accepted: %v", len(paths), err)
		}
	}
}

// PLAN 76 (F-9-8): a check that cannot even start (the target is away) is a
// failed check – a planned run must not report the success of last time.
func TestCheckThatCannotStartIsRecordedAsFailed(t *testing.T) {
	e := newEnv(t)
	local := Job{ID: "kopie", Sources: []string{"media"}, Every: "daily", Time: "02:00",
		Destination: Destination{Type: "local", Volume: "cold"}}
	if err := e.m.Create(local, volumes); err != nil {
		t.Fatal(err)
	}
	// An earlier check went well.
	earlier := e.m.now().Add(-24 * time.Hour)
	e.m.update(local, func(s *State) { s.LastCheck, s.CheckError = &earlier, "" })
	var heard error
	e.m.OnCheck = func(_ string, err error) { heard = err }
	err := e.m.MaintenanceRun(context.Background(), "kopie", "check", 0, ProbeRequest{})
	if err == nil || !strings.Contains(err.Error(), "not mounted") {
		t.Fatalf("planned check without its target: %v", err)
	}
	s := e.state("kopie")
	if !strings.Contains(s.CheckError, "not mounted") || s.LastCheck == nil || !s.LastCheck.After(earlier) || heard == nil {
		t.Fatalf("state %+v, heard %v", s, heard)
	}
}

// PLAN 76 (F-9-11): a deleted job takes its state along; a new job of the
// same name does not start with the old "last success".
func TestDeletedJobLeavesNoStateForANewOneOfItsName(t *testing.T) {
	e := newEnv(t)
	id := localJob(t, e)
	e.m.Start(id)
	e.result(t)
	if e.state(id).LastRun == nil {
		t.Fatal("no run recorded")
	}
	if err := e.m.Delete(id); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Create(s3Job(), volumes); err != nil {
		t.Fatal(err)
	}
	if s := e.state(id); s.LastRun != nil {
		t.Fatalf("new job inherited %+v", s)
	}
}
