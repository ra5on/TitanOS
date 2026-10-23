package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreProbeWithRealRestic(t *testing.T) {
	binary := os.Getenv("RESTIC_TEST_BINARY")
	if binary == "" {
		t.Skip("RESTIC_TEST_BINARY selects an isolated real restic integration test")
	}
	e := newEnv(t)
	e.m.restic = binary
	e.mount(t, "cold")
	job := s3Job()
	job.Destination = Destination{Type: "local", Volume: "cold", Folder: "repository"}
	source := e.m.sources()[0].Path
	os.WriteFile(filepath.Join(source, "literal[*]?.txt"), []byte("verified content"), 0600)
	os.WriteFile(filepath.Join(source, "other.txt"), []byte("other content"), 0600)
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Start(job.ID); err != nil {
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
	state := wait()
	if state.LastOK == nil || state.LastError != "" {
		t.Fatalf("backup: %+v", state)
	}
	if err := e.m.Probe(job.ID, ProbeRequest{MaxBytes: 1024, MaxFiles: 1}); err != nil {
		t.Fatal(err)
	}
	state = wait()
	if state.Probe == nil || state.Probe.Error != "" || state.Probe.Files != 1 || state.Probe.Bytes == 0 {
		t.Fatalf("probe: %+v", state)
	}
	entries, err := os.ReadDir(filepath.Join(e.m.mountRoot, "tank", ".titanos", "probes"))
	if err != nil || len(entries) != 0 {
		t.Fatal("probe files retained", entries, err)
	}
}
func TestRestoreProbeRejectsUnboundedOrPlainCopy(t *testing.T) {
	e := newEnv(t)
	id := localJob(t, e)
	for _, request := range []ProbeRequest{{0, 1}, {1 << 31, 1}, {1, 0}, {1, 201}} {
		if err := e.m.Probe(id, request); err == nil {
			t.Fatal(request)
		}
	}
	for _, s := range []string{"/file[*]?.txt", "/back\\slash"} {
		if probePattern(s) == s {
			t.Fatal("pattern not escaped", s)
		}
	}
}

func TestCoordinatedBackupFailsClosedForOlderApplications(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	job.Applications = []Application{{Kind: "docker", Name: "database", Method: "stop"}}
	job.AllowStop = true
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	var stored []Job
	if err := e.m.store.ReadJSON(jobsFile, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || len(stored[0].Sources) != 0 || len(stored[0].Extras) != 0 || stored[0].CoordinatedSelection == nil {
		t.Fatal("older backup would silently save uncoordinated live application data", stored)
	}
	if err := e.m.Load(); err != nil {
		t.Fatal(err)
	}
	loaded, ok := e.m.find(job.ID)
	if !ok || len(loaded.Sources) != 1 || loaded.Sources[0] != "media" || len(loaded.Applications) != 1 {
		t.Fatal("current app lost selection", loaded)
	}
}
