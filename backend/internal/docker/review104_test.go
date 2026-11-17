package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// N-102-6: a container action holds hardwareMu through the stop (up to its
// stop time) and a VM start through virsh create. The engine check of the
// 5-second loop and the leased devices the NVR and the remote view ask
// for every round must not wait for that.
func TestLongLifecycleCallDoesNotHoldTheLoop(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.hardwareMu.Lock()
	m.gpuLeases["vm"] = gpuLease{Devices: []hardware.ContainerDevice{{ID: "card"}}}
	m.leasesChanged()
	within(t, "engine check of the loop", func() {
		if err := m.reconcile(context.Background(), []string{"data"}); err != nil {
			t.Error(err)
		}
	})
	within(t, "leased devices", func() {
		if got := m.LeasedHardware(); !got["card"] {
			t.Errorf("leased: %v", got)
		}
	})
	delete(m.gpuLeases, "vm")
	m.hardwareMu.Unlock()
	// Once the call is over the next round starts the engine.
	if err := m.reconcile(context.Background(), []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if !m.engineRunning() {
		t.Fatal("the engine was not started after the lock was free")
	}
}

// N-102-21: while unused images are deleted no app job may start: an
// update would otherwise lose the image it just pulled.
func TestAppJobsWaitForACleanup(t *testing.T) {
	m, e := resourceFixture(t)
	e.mu.Lock() // the engine answers nothing until the test lets it
	done := make(chan error, 1)
	go func() {
		_, err := m.PruneImages(context.Background(), true)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond) // the cleanup waits for its first answer
	err := m.UpdateApp(context.Background(), "kept")
	e.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("app update during a cleanup: %v", err)
	}
	if err := m.UpdateApp(context.Background(), "kept"); err != nil {
		t.Fatalf("app update after the cleanup: %v", err)
	}
	for m.Busy() {
		time.Sleep(time.Millisecond)
	}
}

// N-102-22: the ordered start at boot goes through the same GPU check as
// any container start: a container on a GPU a VM holds stays stopped.
func TestOrderedStartLeavesAVMsGPUAlone(t *testing.T) {
	m, _, _ := newTestManager(t)
	var mu sync.Mutex
	started := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path[strings.Index(r.URL.Path, "/containers"):]
		switch {
		case path == "/containers/json":
			json.NewEncoder(w).Encode([]map[string]any{
				{"Id": "gpu1", "State": "exited", "Labels": map[string]string{"com.docker.compose.project": "photos"}},
				{"Id": "web1", "State": "exited", "Labels": map[string]string{"com.docker.compose.project": "photos"}},
			})
		case strings.HasSuffix(path, "/json"):
			labels := map[string]string{}
			if strings.Split(path, "/")[2] == "gpu1" {
				labels["titanos.hardware"] = "card"
			}
			json.NewEncoder(w).Encode(map[string]any{"Config": map[string]any{"Labels": labels}})
		case strings.HasSuffix(path, "/start"):
			started[strings.Split(path, "/")[2]] = true
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	m.gpuLeases["vm"] = gpuLease{Devices: []hardware.ContainerDevice{{ID: "card"}}}
	err := m.StartApp(context.Background(), "photos")
	if err == nil || !strings.Contains(err.Error(), "reserved by VM") {
		t.Errorf("start on a leased GPU: %v", err)
	}
	if started["gpu1"] || !started["web1"] {
		t.Fatalf("started: %v", started)
	}
}

// N-102-23: an opener read from the compose file before a change of the
// app is not kept after the change.
func TestOpenerReadBeforeAChangeIsNotKept(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.store.Write("apps/viewer/compose.yaml", []byte("services:\n  web:\n    image: x\n"))
	openerRead = func() {
		// The compose file changes while the old one is being parsed.
		m.store.Write("apps/viewer/compose.yaml", []byte("services:\n  web:\n    image: y\n"))
		m.mu.Lock()
		m.forgetApps()
		m.mu.Unlock()
	}
	t.Cleanup(func() { openerRead = nil })
	m.opener("viewer")
	openerRead = nil
	m.mu.Lock()
	_, cached := m.opens["viewer"]
	m.mu.Unlock()
	if cached {
		t.Fatal("an opener of the old compose file stayed in the cache")
	}
}

// N-102-24: a settings file that cannot be read is not read again every
// five seconds: the tries back off.
func TestUnreadableSettingsAreNotReadEveryRound(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.loaded = false
	m.store.Write(settingsFile, []byte("{broken"))
	now := time.Now()
	m.retryLoad(now)
	if m.loaded {
		t.Fatal("broken settings loaded")
	}
	m.store.Write(settingsFile, []byte(`{"enabled":false}`))
	m.retryLoad(now.Add(sampleInterval))
	if m.loaded {
		t.Fatal("read again after one round")
	}
	m.retryLoad(now.Add(time.Hour))
	if !m.loaded {
		t.Fatal("not read again after the pause")
	}
}

// N-102-24: a failed stats call keeps the last CPU and memory values
// instead of showing 0.
func TestFailedStatsKeepTheLastValues(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.applied = "/somewhere"
	var mu sync.Mutex
	failing, calls := false, uint64(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Write([]byte(`[{"Id":"aaaaaaaaaaaa1111","Names":["/web"],"State":"running","Labels":{}}]`))
		case strings.HasSuffix(r.URL.Path, "/stats"):
			if failing {
				http.Error(w, `{"message":"busy"}`, 500)
				return
			}
			calls++
			n := calls * 1000000000
			w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":` + itoa(n/20) + `},"system_cpu_usage":` + itoa(n) + `,"online_cpus":2},"memory_stats":{"usage":104857600,"stats":{}}}`))
		default:
			w.Write([]byte(`{"State":{"StartedAt":"2026-05-01T10:00:00Z"}}`))
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	ctx := context.Background()
	m.sample(ctx)
	m.sample(ctx)
	mu.Lock()
	failing = true
	mu.Unlock()
	m.sample(ctx)
	m.mu.Lock()
	s := *m.samples["aaaaaaaaaaaa1111"]
	m.mu.Unlock()
	if s.memory != 104857600 || s.system == 0 {
		t.Fatalf("after a failed stats call: %+v", s)
	}
	if last := s.history[len(s.history)-1]; last == 0 || len(s.history) != 2 {
		t.Fatalf("history after a failed stats call: %v", s.history)
	}
}

// N-102-24: the update check (up to two minutes) runs beside the loop,
// one at a time.
func TestUpdateCheckDoesNotHoldTheLoop(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.applied = "/somewhere"
	release := make(chan struct{})
	var mu sync.Mutex
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		<-release
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	within(t, "update check", func() { m.checkUpdatesBeside(context.Background()) })
	within(t, "second update check", func() { m.checkUpdatesBeside(context.Background()) })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if asked != 1 {
		t.Fatalf("%d checks at once", asked)
	}
}
