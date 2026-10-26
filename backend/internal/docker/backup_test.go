package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

func TestBackupRejectsForcedKillAndOwnsContainerLifecycle(t *testing.T) {
	for _, code := range []int{0, 137} {
		m, _, _ := newTestManager(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				w.WriteHeader(204)
				return
			}
			fmt.Fprintf(w, `{"State":{"Running":false,"OOMKilled":false,"ExitCode":%d}}`, code)
		}))
		m.api.http = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}}}
		lease, err := m.BackupLease(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Action(context.Background(), "aaaaaaaaaaaa1111", "start"); !errors.Is(err, ErrBusy) {
			t.Fatal("backup did not own lifecycle:", err)
		}
		if _, err := m.BackupLease(context.Background()); err == nil {
			t.Fatal("second backup lease granted")
		}
		err = lease.Stop(context.Background(), "container")
		lease.Close()
		server.Close()
		if (err == nil) != (code == 0) {
			t.Fatal(code, err)
		}
	}
}

// blockingRunner holds `docker compose up` until release is closed.
type blockingRunner struct {
	fakeRunner
	started chan struct{}
	release chan struct{}
}

func (r *blockingRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	if slices.Contains(args, "up") {
		close(r.started)
		<-r.release
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// within fails the test if call does not return promptly.
func within(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { call(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

func TestLongComposeUpDoesNotBlockVMsOrContainers(t *testing.T) {
	m, _, _ := newTestManager(t)
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	m.runner = runner
	m.hardwareRunDir = t.TempDir()
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.HardwareDevices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{{ID: "card", Kind: "nvidia", Available: true}, {ID: "igpu", Kind: "igpu", Vendor: "8086", Available: true}}
	}
	ctx := context.Background()
	if err := m.CreateApp(ctx, "slow", "services:\n  web:\n    image: busybox\n    x-titanos-hardware: [card]\n", nil); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	within(t, "container action", func() {
		if err := m.Action(ctx, "aaaaaaaaaaaa1111", "stop"); err != nil {
			t.Error(err)
		}
	})
	within(t, "VM without hardware", func() {
		finish, err := m.BeginVM(ctx, "plain", nil, nil, false)
		if err != nil {
			t.Error(err)
		} else {
			finish(true)
		}
	})
	within(t, "VM on hardware the app does not use", func() {
		finish, err := m.BeginVM(ctx, "other", []string{"igpu"}, nil, false)
		if err != nil {
			t.Error(err)
		} else {
			finish(true)
		}
	})
	within(t, "GPU VM", func() {
		// The app may still start on this GPU: refuse instead of waiting.
		if _, err := m.BeginVM(ctx, "gpu", []string{"card"}, nil, false); !errors.Is(err, ErrHardwareBusy) {
			t.Error("GPU leased during compose up:", err)
		}
	})
	within(t, "device list", func() { m.ContainerDevices(ctx) })
	within(t, "engine reconcile", func() { m.reconcile(ctx, []string{"data"}) })
	if _, err := m.BackupLease(ctx); err == nil {
		t.Fatal("backup granted while an app starts")
	}
	close(runner.release)
	for m.Busy() {
		time.Sleep(time.Millisecond)
	}
	finish, err := m.BeginVM(ctx, "gpu", []string{"card"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
}

func TestComposeHardwareOnlyNamesUsedDevices(t *testing.T) {
	devices := []hardware.ContainerDevice{{ID: "0000:00:02.0", Kind: "igpu", Vendor: "8086", Available: true}}
	if use := composeHardware("services:\n  web:\n    image: x\n", devices); use.uses("0000:00:02.0") {
		t.Fatal("plain app claims a GPU")
	}
	if use := composeHardware("services:\n  web:\n    image: x\n    x-titanos-hardware: [gpu-intel]\n", devices); !use.uses("0000:00:02.0") {
		t.Fatal("selection missed")
	}
	if use := composeHardware("services:\n  web:\n    image: x\n    devices: [/dev/dri:/dev/dri]\n", devices); !use.uses("anything") {
		t.Fatal("raw mapping missed")
	}
}

// gpuContainerEngine has one stopped container that uses the GPU "card".
func gpuContainerEngine(t *testing.T, m *Manager) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			fmt.Fprint(w, `[]`)
		default:
			fmt.Fprint(w, `{"State":{"Running":false,"ExitCode":0},"Config":{"Labels":{"titanos.hardware":"card"}}}`)
		}
	}))
	t.Cleanup(server.Close)
	m.api = &client{http: &http.Client{Transport: rewriteTransport{server.URL}}}
}

func TestBackupKeepsHardwareOfStoppedContainers(t *testing.T) {
	m, _, _ := newTestManager(t)
	gpuContainerEngine(t, m)
	m.hardwareRunDir = t.TempDir()
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.HardwareDevices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{{ID: "card", Kind: "igpu", Available: true}}
	}
	ctx := context.Background()
	lease, err := m.BackupLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Stop(ctx, "jellyfin"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginVM(ctx, "vm", []string{"card"}, nil, false); !errors.Is(err, ErrHardwareBusy) {
		t.Fatal("VM took the GPU of a container the backup restarts:", err)
	}
	if err := lease.Start(ctx, "jellyfin"); err != nil {
		t.Fatal(err)
	}
	lease.Close()
	finish, err := m.BeginVM(ctx, "vm", []string{"card"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	// A later backup start (recovery) still respects the VM lease.
	lease, err = m.BackupLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := lease.Start(ctx, "jellyfin"); err == nil {
		t.Fatal("backup started a container on a VM GPU")
	}
}

func TestCreateAppRefusedByBackupLeavesNoFiles(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	ctx := context.Background()
	compose := "services:\n  web:\n    image: busybox\n"
	lease, err := m.BackupLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The early check passes before the lease exists; force the late one.
	m.mu.Lock()
	m.backups--
	m.mu.Unlock()
	validated := false
	m.runner = runnerFunc(func(args []string) {
		if !validated && slices.Contains(args, "config") {
			validated = true
			m.mu.Lock()
			m.backups++
			m.mu.Unlock()
		}
	})
	if err := m.CreateApp(ctx, "late", compose, map[string]string{"KEY": "v"}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := m.Compose("late"); !errors.Is(err, ErrNotFound) {
		t.Fatal("refused app left its compose file")
	}
	lease.Close()
	if err := m.CreateApp(ctx, "late", compose, nil); err != nil {
		t.Fatal("retry after the backup:", err)
	}
	for m.Busy() {
		time.Sleep(time.Millisecond)
	}
}

type runnerFunc func(args []string)

func (f runnerFunc) Run(_ context.Context, _ string, _ string, args ...string) (string, error) {
	f(args)
	return "", nil
}

func TestBackupLeaseDoesNotBlockVMsOrDocker(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.hardwareRunDir = t.TempDir()
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.HardwareDevices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{{ID: "card", Kind: "intel", Available: true}}
	}
	ctx := context.Background()
	lease, err := m.BackupLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	within(t, "VM start", func() {
		// No running container uses the card, so no lifecycle change.
		finish, err := m.BeginVM(ctx, "vm", []string{"card"}, nil, false)
		if err != nil {
			t.Error(err)
		} else {
			finish(true)
		}
	})
	within(t, "container action", func() {
		if err := m.Action(ctx, "aaaaaaaaaaaa1111", "stop"); !errors.Is(err, ErrBusy) {
			t.Error("container changed during backup:", err)
		}
	})
	within(t, "device list", func() { m.ContainerDevices(ctx) })
	within(t, "VM return", func() { m.ReturnVMHardware(ctx, map[string]bool{}) })
	if err := m.UpdateApp(ctx, "anything"); err == nil {
		t.Fatal("app job during backup")
	}
	lease.Close()
	if err := m.Action(ctx, "aaaaaaaaaaaa1111", "stop"); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresUserFromContainerEnv(t *testing.T) {
	if user := postgresUser([]string{"PATH=/usr/bin", "POSTGRES_USER=immich", "POSTGRES_DB=immich"}); user != "immich" {
		t.Fatal(user)
	}
	if user := postgresUser([]string{"POSTGRES_PASSWORD=x"}); user != "postgres" {
		t.Fatal(user)
	}
}
