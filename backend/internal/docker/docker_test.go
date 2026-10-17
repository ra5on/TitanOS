package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct {
	mu       sync.Mutex
	commands []string
	stdin    []string
	fail     string
}

func (r *fakeRunner) Run(_ context.Context, stdin string, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	r.stdin = append(r.stdin, stdin)
	if r.fail != "" && strings.Contains(command, r.fail) {
		return "services.web.image must be a string", errors.New("exit status 15")
	}
	return "", nil
}

// frame encodes text the way the logs endpoint does.
func frame(stream byte, text string) []byte {
	header := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(header[4:], uint32(len(text)))
	return append(header, text...)
}

// fakeEngine answers like the Docker Engine API on a unix socket.
func fakeEngine(t *testing.T) (socket string, calls *[]string) {
	t.Helper()
	socket = filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var log []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.43/version", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"Version":"26.1.5"}`)) })
	mux.HandleFunc("GET /v1.43/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
		 {"Id":"aaaaaaaaaaaa1111","Names":["/whoami-web-1"],"Image":"traefik/whoami","ImageID":"sha256:img1","State":"running",
		  "Labels":{"com.docker.compose.project":"whoami","com.docker.compose.service":"web","titanos.webui":"8080","titanos.public-url":"https://cloud.example.org/app","titanos.icon":"https://example.org/icon.png"},
		  "Ports":[{"IP":"0.0.0.0","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},{"IP":"::","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},{"PrivatePort":443,"Type":"tcp"}]},
		 {"Id":"bbbbbbbbbbbb2222","Names":["/alt"],"Image":"busybox","ImageID":"sha256:img2","State":"exited","Labels":{},"Ports":[]}]`))
	})
	mux.HandleFunc("GET /v1.43/images/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"RepoDigests":["traefik/whoami@sha256:old"]}`))
	})
	mux.HandleFunc("GET /v1.43/distribution/{ref...}", func(w http.ResponseWriter, r *http.Request) {
		log = append(log, "registry "+r.PathValue("ref"))
		digest := "sha256:old"
		if r.PathValue("ref") == "traefik/whoami/json" {
			digest = "sha256:new"
		}
		w.Write([]byte(`{"Descriptor":{"digest":"` + digest + `"}}`))
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"State":{"StartedAt":"2026-05-01T10:00:00Z","FinishedAt":"2026-05-01T09:00:00Z"}}`))
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/stats", func(w http.ResponseWriter, r *http.Request) {
		// Each call: 50 ms more CPU time of 1 s system time on 2 CPUs → 10 %.
		n := uint64(len(log)+1) * 1000000000
		log = append(log, "stats")
		w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":` + itoa(n/20) + `},"system_cpu_usage":` + itoa(n) + `,"online_cpus":2},"memory_stats":{"usage":104857600,"stats":{"inactive_file":4857600}}}`))
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tail") == "" {
			t.Error("logs requested without a limit")
		}
		w.Write(append(frame(1, "erste Zeile\n"), frame(2, "ein Fehler\n")...))
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		log = append(log, r.PathValue("action")+" "+r.PathValue("id"))
		if r.PathValue("id") == "cccccccccccc3333" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"No such container"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1.43/containers/{id}", func(w http.ResponseWriter, r *http.Request) {
		log = append(log, "delete "+r.PathValue("id")+" force="+r.URL.Query().Get("force"))
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return socket, &log
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner, *[]string) {
	t.Helper()
	dir := t.TempDir()
	socket, calls := fakeEngine(t)
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(configDir, 0o700)
	m := NewManager(config.Open(configDir), configDir)
	runner := &fakeRunner{}
	m.runner = runner
	m.api = newClient(socket)
	m.mountRoot = filepath.Join(dir, "mnt")
	m.daemonConfig = filepath.Join(dir, "etc/docker/daemon.json")
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, runner, calls
}

func TestEngineFollowsSettingsAndVolume(t *testing.T) {
	m, runner, _ := newTestManager(t)
	ctx := context.Background()

	if err := m.SetSettings(Settings{Enabled: true, Volume: "nope"}, []string{"data"}); !errors.Is(err, ErrUnknownVolume) {
		t.Fatalf("unknown volume: %v", err)
	}
	if err := m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"}); err != nil {
		t.Fatal(err)
	}

	// Enabled, but the volume is not mounted: the engine must stay off.
	if err := m.reconcile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(runner.commands, "\n"); strings.Contains(all, "start") {
		t.Errorf("engine started without its volume:\n%s", all)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data")); err == nil {
		t.Error("a data folder was created for a volume that is not mounted")
	}

	// The volume appears.
	if err := m.reconcile(ctx, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if last := runner.commands[len(runner.commands)-1]; last != "systemctl start docker.service" {
		t.Errorf("last command = %q", last)
	}
	daemon, _ := os.ReadFile(m.daemonConfig)
	if !strings.Contains(string(daemon), `"cdi": true`) {
		t.Error("GPU plugins cannot provide native CDI devices")
	}
	if !strings.Contains(string(daemon), `"data-root": "`+filepath.Join(m.mountRoot, "data/.titanos/docker")+`"`) || !strings.Contains(string(daemon), `"max-size": "10m"`) {
		t.Errorf("daemon.json:\n%s", daemon)
	}
	// Nothing changed: nothing is run.
	before := len(runner.commands)
	m.reconcile(ctx, []string{"data"})
	if len(runner.commands) != before {
		t.Errorf("reconcile without a change ran %v", runner.commands[before:])
	}
	// The volume goes away: the engine is stopped.
	m.reconcile(ctx, nil)
	if last := runner.commands[len(runner.commands)-1]; last != "systemctl stop docker.socket docker.service" {
		t.Errorf("after losing the volume: %q", last)
	}

	// The settings survive a restart of titand.
	again := NewManager(m.store, m.configDir)
	if err := again.Load(); err != nil || again.current() != (Settings{Enabled: true, Volume: "data"}) {
		t.Errorf("reloaded settings = %+v, %v", again.current(), err)
	}
}

func TestStatusAndSampling(t *testing.T) {
	m, _, _ := newTestManager(t)
	ctx := context.Background()
	m.applied = "/somewhere"
	m.store.Write("apps/whoami/compose.yaml", []byte("services: {}"))

	m.sample(ctx)
	m.sample(ctx)
	status := m.Status(ctx)
	if !status.Running || status.Version != "26.1.5" || len(status.Containers) != 2 {
		t.Fatalf("status = %+v", status)
	}
	stopped, web := status.Containers[0], status.Containers[1]
	if web.Name != "whoami-web-1" || web.App != "whoami" || web.Service != "web" || !web.Managed {
		t.Errorf("container = %+v", web)
	}
	// One entry per published port, although Docker lists IPv4 and IPv6; unpublished ports are left out.
	if !slices.Equal(web.Ports, []Port{{Host: 8080, Container: 80, Protocol: "tcp"}}) {
		t.Errorf("ports = %+v", web.Ports)
	}
	if web.CPUPercent < 9.9 || web.CPUPercent > 10.1 || web.MemoryBytes != 100000000 || len(web.CPUHistory) != 2 {
		t.Errorf("cpu = %v, memory = %d, history = %v", web.CPUPercent, web.MemoryBytes, web.CPUHistory)
	}
	if !web.Since.Equal(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)) || !stopped.Since.Equal(time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("since: running %v, stopped %v", web.Since, stopped.Since)
	}
	if stopped.Managed || stopped.CPUPercent != 0 || !slices.Equal(status.Apps, []string{"whoami"}) {
		t.Errorf("stopped = %+v, apps = %v", stopped, status.Apps)
	}
}

func TestActionsAndLogs(t *testing.T) {
	m, _, calls := newTestManager(t)
	ctx := context.Background()

	if err := m.Action(ctx, "aaaaaaaaaaaa1111", "restart"); err != nil {
		t.Fatal(err)
	}
	if err := m.Action(ctx, "cccccccccccc3333", "start"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown container: %v", err)
	}
	// Only the three actions, and only something that looks like an ID, reach the engine.
	before := len(*calls)
	for _, bad := range [][2]string{{"aaaaaaaaaaaa1111", "kill"}, {"../../version", "start"}, {"aaaaaaaaaaaa1111/exec", "start"}, {"", "stop"}} {
		if err := m.Action(ctx, bad[0], bad[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("Action(%q, %q) = %v", bad[0], bad[1], err)
		}
	}
	if len(*calls) != before {
		t.Errorf("invalid requests reached the engine: %v", (*calls)[before:])
	}
	if err := m.Remove(ctx, "bbbbbbbbbbbb2222"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(*calls, "restart aaaaaaaaaaaa1111") || !slices.Contains(*calls, "delete bbbbbbbbbbbb2222 force=true") {
		t.Errorf("engine calls = %v", *calls)
	}

	logs, err := m.Logs(ctx, "aaaaaaaaaaaa1111", 100)
	if err != nil || logs != "erste Zeile\nein Fehler\n" {
		t.Errorf("logs = %q, %v", logs, err)
	}

	m.Disabled = true
	if err := m.Action(ctx, "aaaaaaaaaaaa1111", "stop"); !errors.Is(err, ErrDisabled) {
		t.Errorf("development mode: %v", err)
	}
}

func TestDemuxPlainText(t *testing.T) {
	// A container with a terminal sends its output without frames.
	text, _ := demux(bufio.NewReader(strings.NewReader("Welcome to the shell\n$ ")))
	if text != "Welcome to the shell\n$ " {
		t.Errorf("demux = %q", text)
	}
}

func TestApps(t *testing.T) {
	m, runner, _ := newTestManager(t)
	ctx := context.Background()
	compose := "services:\n  web:\n    image: traefik/whoami\n    volumes:\n      - ./data:/data\n"

	if err := m.CreateApp(ctx, "whoami", compose, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("without the engine: %v", err)
	}
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")

	for name, want := range map[string]error{"Whoami": ErrInvalidName, "../x": ErrInvalidName, "": ErrInvalidName} {
		if err := m.CreateApp(ctx, name, compose, nil); !errors.Is(err, want) {
			t.Errorf("CreateApp(%q) = %v", name, err)
		}
	}
	if err := m.CreateApp(ctx, "whoami", "  ", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty compose file: %v", err)
	}

	// A file Docker rejects is not kept.
	runner.fail = "config --quiet"
	var invalid *InvalidComposeError
	if err := m.CreateApp(ctx, "kaputt", "services: 42", nil); !errors.As(err, &invalid) || !strings.Contains(invalid.Detail, "must be a string") {
		t.Errorf("rejected compose file: %v", err)
	}
	if _, err := m.Compose("kaputt"); !errors.Is(err, ErrNotFound) {
		t.Error("a rejected compose file was stored")
	}
	runner.fail = ""

	for _, env := range []map[string]string{{"1BAD": "x"}, {"TOKEN": "it's"}, {"TOKEN": "two\nlines"}} {
		if err := m.CreateApp(ctx, "whoami", compose, env); !errors.Is(err, ErrInvalidEnv) {
			t.Errorf("CreateApp with %v = %v", env, err)
		}
	}
	if err := m.CreateApp(ctx, "whoami", compose, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateApp(ctx, "whoami", compose, nil); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	// The setup runs in the background.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && m.Status(ctx).Jobs["whoami"].State == "running" {
		time.Sleep(10 * time.Millisecond)
	}
	if job := m.Status(ctx).Jobs["whoami"]; job.State != "done" {
		t.Errorf("job = %+v", job)
	}
	up := "docker compose --project-name whoami --file " + filepath.Join(m.configDir, "apps/whoami/compose.yaml") +
		" --project-directory " + filepath.Join(m.mountRoot, "data/appdata/whoami") + " up --detach --remove-orphans --quiet-pull"
	if !slices.Contains(runner.commands, up) {
		t.Errorf("missing %q in %v", up, runner.commands)
	}
	if stored, err := m.Compose("whoami"); err != nil || stored != compose {
		t.Errorf("stored compose file = %q, %v", stored, err)
	}

	// Secrets go into a file of their own, which Compose is pointed to.
	if err := m.CreateApp(ctx, "geheim", "services:\n  web:\n    image: traefik/whoami\n    environment:\n      TOKEN: ${TOKEN}\n", map[string]string{"TOKEN": "s3cr3t", "A_KEY": "x y"}); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) && m.Status(ctx).Jobs["geheim"].State == "running" {
		time.Sleep(10 * time.Millisecond)
	}
	if env, err := m.store.Read("apps/geheim/.env"); err != nil || string(env) != "A_KEY='x y'\nTOKEN='s3cr3t'\n" {
		t.Errorf(".env = %q, %v", env, err)
	}
	withEnv := "--env-file " + filepath.Join(m.configDir, "apps/geheim/.env") + " up --detach"
	if !slices.ContainsFunc(runner.commands, func(c string) bool { return strings.Contains(c, withEnv) }) {
		t.Errorf("no %q in %v", withEnv, runner.commands)
	}
	if stored, _ := m.Compose("geheim"); strings.Contains(stored, "s3cr3t") {
		t.Error("the secret is in the compose file")
	}

	// Removing the app keeps its data folder.
	data := filepath.Join(m.mountRoot, "data/appdata/whoami")
	os.WriteFile(filepath.Join(data, "wichtig.db"), []byte("x"), 0o644)
	if err := m.RemoveApp(ctx, "whoami"); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(runner.commands, func(c string) bool { return strings.HasSuffix(c, "down --remove-orphans") }) {
		t.Errorf("no compose down in %v", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(data, "wichtig.db")); err != nil {
		t.Error("removing the app deleted its data")
	}
	if _, err := m.Compose("whoami"); !errors.Is(err, ErrNotFound) {
		t.Error("compose file still stored")
	}
	if err := m.RemoveApp(ctx, "whoami"); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove twice: %v", err)
	}
}

func TestUpdateHints(t *testing.T) {
	m, runner, calls := newTestManager(t)
	ctx := context.Background()
	m.applied = "/somewhere"
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.store.Write("apps/whoami/compose.yaml", []byte("services: {}"))

	// Before the registries were asked nothing is claimed.
	for _, c := range m.Status(ctx).Containers {
		if c.UpdateAvailable {
			t.Errorf("%s: update claimed without a check", c.Name)
		}
	}
	if err := m.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	status := m.Status(ctx)
	stopped, web := status.Containers[0], status.Containers[1]
	// The registry has "new" for traefik/whoami while the container runs "old".
	if !web.UpdateAvailable || stopped.UpdateAvailable || status.UpdatesChecked.IsZero() {
		t.Errorf("update hints: web=%v stopped=%v checked=%v", web.UpdateAvailable, stopped.UpdateAvailable, status.UpdatesChecked)
	}
	if web.PublicURL != "https://cloud.example.org/app" || web.Icon != "https://example.org/icon.png" {
		t.Fatalf("app metadata lost: %+v", web)
	}
	if web.WebUI != "8080" {
		t.Errorf("webui = %q", web.WebUI)
	}
	if !slices.Contains(*calls, "registry traefik/whoami/json") {
		t.Errorf("registry not asked: %v", *calls)
	}

	if err := m.UpdateApp(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("updating an unknown app: %v", err)
	}
	if err := m.UpdateApp(ctx, "whoami"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && m.Status(ctx).Jobs["whoami"].State == "running" {
		time.Sleep(10 * time.Millisecond)
	}
	all := strings.Join(runner.commands, "\n")
	pull := strings.Index(all, "pull --quiet")
	up := strings.Index(all, "up --detach")
	if pull < 0 || up < pull {
		t.Errorf("update must pull, then recreate:\n%s", all)
	}
}

func TestSetCompose(t *testing.T) {
	m, runner, _ := newTestManager(t)
	ctx := context.Background()
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.store.Write("apps/whoami/compose.yaml", []byte("services:\n  web:\n    image: old\n"))

	if err := m.SetCompose(ctx, "nope", "services: {}"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown app: %v", err)
	}
	runner.fail = "config --quiet"
	if err := m.SetCompose(ctx, "whoami", "services: 1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("rejected file: %v", err)
	}
	if stored, _ := m.Compose("whoami"); !strings.Contains(stored, "image: old") {
		t.Error("a rejected compose file replaced the stored one")
	}
	runner.fail = ""
	if err := m.SetCompose(ctx, "whoami", "services:\n  web:\n    image: new\n"); err != nil {
		t.Fatal(err)
	}
	if stored, _ := m.Compose("whoami"); !strings.Contains(stored, "image: new") {
		t.Errorf("stored = %q", stored)
	}
	// A second change while the first is being applied has to wait.
	if err := m.SetCompose(ctx, "whoami", "services:\n  web:\n    image: newer\n"); err != nil && !errors.Is(err, ErrBusy) {
		t.Errorf("concurrent change: %v", err)
	}
}

func TestEngineDoesNotReportReadyBeforeDataRootApplied(t *testing.T) {
	m, _, _ := newTestManager(t)
	if m.Status(context.Background()).Running {
		t.Fatal("socket answering before reconciliation is not ready for app installation")
	}
	m.applied = m.dataRoot("data")
	if !m.Status(context.Background()).Running {
		t.Fatal("applied engine should be ready")
	}
}
