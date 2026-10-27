package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// steps renders the step marks of an operation as "id=state" for comparison.
func steps(p Progress) string {
	out := []string{}
	for _, step := range p.Steps {
		out = append(out, step.ID+"="+step.State)
	}
	return strings.Join(out, " ")
}

func lastProgress(t *testing.T, m *Manager) Progress {
	t.Helper()
	list := m.Progress()
	if len(list) == 0 {
		t.Fatal("no plugin operation recorded")
	}
	return list[len(list)-1]
}

func stepOf(p Progress, id string) Step {
	for _, step := range p.Steps {
		if step.ID == id {
			return step
		}
	}
	return Step{}
}

func TestInstallReportsEachStep(t *testing.T) {
	m, _ := newTestManager(t)
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) {
		p.Modules = []string{"i915"}
		p.Services = []string{"gpu.service"}
	})
	if err := m.Install(context.Background(), path, "data", "gpu", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	p := lastProgress(t, m)
	want := "signature=skipped checksum=done write=done sysext=done modules=done services=done health=done"
	if p.Plugin != "gpu" || p.Title != "Plugin gpu" || p.Action != "install" || p.State != "done" || steps(p) != want || p.Finished == nil {
		t.Fatalf("install progress: %+v (%s)", p, steps(p))
	}
}

func TestDeferredInstallHasNoActivationSteps(t *testing.T) {
	m, _ := newTestManager(t)
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) { p.Services = []string{"gpu.service"} })
	if err := m.InstallWithActivation(context.Background(), path, "data", "gpu", []string{"data"}, false); err != nil {
		t.Fatal(err)
	}
	if p := lastProgress(t, m); steps(p) != "signature=skipped checksum=done write=done" || p.State != "done" {
		t.Fatalf("deferred install: %s", steps(p))
	}
}

// A failure in a middle step marks exactly that step with its error; earlier
// steps stay done and later ones are shown as not reached.
func TestInstallFailureMarksTheFailingStep(t *testing.T) {
	for _, tc := range []struct{ fail, failed, want string }{
		{"systemd-sysext", "sysext", "signature=skipped checksum=done write=done sysext=failed modules=pending services=pending health=pending"},
		{"modprobe", "modules", "signature=skipped checksum=done write=done sysext=done modules=failed services=pending health=pending"},
		{"systemctl start", "services", "signature=skipped checksum=done write=done sysext=done modules=done services=failed health=pending"},
	} {
		t.Run(tc.failed, func(t *testing.T) {
			m, r := newTestManager(t)
			path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) {
				p.Modules = []string{"i915"}
				p.Services = []string{"gpu.service"}
			})
			r.fail = tc.fail
			err := m.Install(context.Background(), path, "data", "gpu", []string{"data"})
			if err == nil {
				t.Fatal("failure ignored")
			}
			p := lastProgress(t, m)
			if p.State != "failed" || steps(p) != tc.want {
				t.Fatalf("progress %s, state %s", steps(p), p.State)
			}
			if step := stepOf(p, tc.failed); !strings.Contains(step.Error, "injected failure") || p.Error == "" {
				t.Fatalf("failing step without its error: %+v", step)
			}
		})
	}
}

func TestBrokenImageFailsAtChecksum(t *testing.T) {
	m, _ := newTestManager(t)
	share := filepath.Join(m.mountRoot, "data/media")
	path := plugin(t, share, "gpu", nil)
	os.WriteFile(filepath.Join(share, "gpu.raw"), []byte("changed"), 0o644)
	if err := m.Install(context.Background(), path, "data", "gpu", []string{"data"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if p := lastProgress(t, m); !strings.HasPrefix(steps(p), "signature=skipped checksum=failed write=pending") {
		t.Fatalf("broken image: %s", steps(p))
	}
}

// A refused request never starts a step; the API answer is enough and the
// jobs list does not fill with refusals.
func TestRefusedRequestLeavesNoProgress(t *testing.T) {
	m, _ := newTestManager(t)
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", nil)
	if err := m.Install(context.Background(), path, "data", "", []string{"data"}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal(err)
	}
	if err := m.Remove(context.Background(), "unknown", []string{"data"}); err == nil {
		t.Fatal(err)
	}
	if list := m.Progress(); len(list) != 0 {
		t.Fatalf("refusals recorded: %+v", list)
	}
}

func TestUpdateRollbackIsItsOwnStep(t *testing.T) {
	m, r := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	first := plugin(t, filepath.Join(m.mountRoot, "data", "media"), "werkzeug", func(p *Manifest) { p.Services = []string{"werkzeug.service"} })
	if err := m.Install(ctx, first, "data", "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(m.mountRoot, "data", "neu")
	os.MkdirAll(next, 0o755)
	path := secondVersion(t, next, "werkzeug", func(p *Manifest) { p.Services = []string{"werkzeug-neu.service"} })
	r.fail = "start werkzeug-neu.service"
	if err := m.Update(ctx, "werkzeug", path, "werkzeug", mounted); err == nil {
		t.Fatal("failed start accepted")
	}
	p := lastProgress(t, m)
	want := "signature=skipped checksum=done write=done stop=done sysext=done services=failed health=pending rollback=done"
	if p.Action != "update" || p.State != "failed" || steps(p) != want {
		t.Fatalf("update progress: %s (%s)", steps(p), p.State)
	}
	if !strings.Contains(stepOf(p, "services").Error, "injected failure") {
		t.Fatalf("services step error: %+v", stepOf(p, "services"))
	}

	r.fail = ""
	if err := m.Update(ctx, "werkzeug", path, "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	if p := lastProgress(t, m); p.State != "done" || steps(p) != "signature=skipped checksum=done write=done stop=done sysext=done services=done health=done" {
		t.Fatalf("successful update: %s", steps(p))
	}
}

func TestRemoveReportsBusyStop(t *testing.T) {
	m, r := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) { p.Modules = []string{"nvidia"} })
	if err := m.Install(ctx, path, "data", "gpu", mounted); err != nil {
		t.Fatal(err)
	}
	r.fail = "-r nvidia"
	if err := m.Remove(ctx, "gpu", mounted); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if p := lastProgress(t, m); p.Action != "remove" || steps(p) != "stop=failed sysext=pending remove=pending" {
		t.Fatalf("busy removal: %s", steps(p))
	}
	r.fail = ""
	if err := m.Remove(ctx, "gpu", mounted); err != nil {
		t.Fatal(err)
	}
	if p := lastProgress(t, m); p.State != "done" || steps(p) != "stop=done sysext=done remove=done" {
		t.Fatalf("removal: %s", steps(p))
	}
}

func TestFinishedProgressIsBoundedAndCanBeForgotten(t *testing.T) {
	m, _ := newTestManager(t)
	running := m.track("long", "install", StepDownload)
	running.begin(StepDownload)
	for range keptProgress + 5 {
		tr := m.track("x", "install", StepWrite)
		tr.begin(StepWrite)
		tr.finish(nil)
	}
	list := m.Progress()
	if len(list) != keptProgress+1 || list[0].ID != running.progress.ID {
		t.Fatalf("kept %d operations, first %s", len(list), list[0].Plugin)
	}
	if err := m.ForgetProgress(running.progress.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("forgot a running operation: %v", err)
	}
	if err := m.ForgetProgress(list[1].ID); err != nil || len(m.Progress()) != keptProgress {
		t.Fatalf("forget: %v", err)
	}
	if err := m.ForgetProgress("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

// The download reports bytes and total while it runs, and the progress can be
// read while the plugin lock is held by another action.
func TestOfficialDownloadReportsBytes(t *testing.T) {
	f := newCatalogFixture(t)
	data, _ := os.ReadFile(filepath.Join(f.dir, "intel-gpu-tools.raw"))
	started, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.imageHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data[:1])
		w.(http.Flusher).Flush()
		close(started)
		<-release
		w.Write(data[1:])
	}
	f.mu.Unlock()
	installed := make(chan error, 1)
	go func() {
		installed <- f.m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data"})
	}()
	<-started
	f.m.actions.Lock() // a long action elsewhere must not stall the progress
	deadline := time.Now().Add(5 * time.Second)
	var p Progress
	for time.Now().Before(deadline) {
		p = lastProgress(t, f.m)
		if p.Bytes == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.m.actions.Unlock()
	if p.State != "running" || p.Bytes != 1 || p.Total != int64(len(data)) || !strings.HasPrefix(steps(p), "signature=done download=running checksum=pending write=pending") {
		t.Fatalf("during the download: %+v (%s)", p, steps(p))
	}
	close(release)
	if err := <-installed; err != nil {
		t.Fatal(err)
	}
	p = lastProgress(t, f.m)
	if p.State != "done" || p.Bytes != p.Total || p.SpeedBytesPerSecond != 0 || strings.Contains(steps(p), "pending") || strings.Contains(steps(p), "failed") {
		t.Fatalf("after the download: %+v (%s)", p, steps(p))
	}
}

func TestOfficialChecksumMismatchFailsAtChecksum(t *testing.T) {
	f := newCatalogFixture(t)
	data, _ := os.ReadFile(filepath.Join(f.dir, "intel-gpu-tools.raw"))
	f.mu.Lock()
	f.imageHandler = func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", len(data)))) }
	f.mu.Unlock()
	if err := f.m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data"}); err == nil {
		t.Fatal("wrong image accepted")
	}
	if p := lastProgress(t, f.m); !strings.HasPrefix(steps(p), "signature=done download=done checksum=failed write=pending") || p.State != "failed" {
		t.Fatalf("checksum mismatch: %s", steps(p))
	}
}

// The page and the jobs list read these field names.
func TestProgressJSON(t *testing.T) {
	m, _ := newTestManager(t)
	tr := m.track("gpu", "install", StepDownload, StepChecksum)
	tr.begin(StepDownload)
	tr.bytes(5, 10)
	tr.finish(errors.New("boom"))
	data, err := json.Marshal(m.Progress())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"plugin":"gpu"`, `"action":"install"`, `"state":"failed"`, `{"id":"download","state":"failed","error":"boom"}`, `{"id":"checksum","state":"pending"}`, `"bytes":5`, `"total":10`, `"speedBytesPerSecond":0`, `"error":"boom"`, `"finished":`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("%s missing in %s", field, data)
		}
	}
}
