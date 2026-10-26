package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
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
		return []hardware.ContainerDevice{{ID: "card", Kind: "nvidia", Available: true}}
	}
	ctx := context.Background()
	if err := m.CreateApp(ctx, "slow", "services:\n  web:\n    image: busybox\n", nil); err != nil {
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
	within(t, "GPU VM", func() {
		// The app may still start on this GPU: refuse instead of waiting.
		if _, err := m.BeginVM(ctx, "gpu", []string{"card"}, nil, false); !errors.Is(err, ErrBusy) {
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
