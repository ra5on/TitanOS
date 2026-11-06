package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// sandboxEngine answers the calls of a sandbox run and keeps what was
// created.
func sandboxEngine(t *testing.T, waitFor time.Duration) (*client, *sync.Mutex, *[]string, *map[string]any) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var log []string
	created := map[string]any{}
	mux := http.NewServeMux()
	note := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
	mux.HandleFunc("GET /v1.43/containers/json", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("filters"), sandboxLabel) {
			w.Write([]byte(`[{"Id":"old1","Labels":{"titanos.sandbox":"1"}}]`))
			return
		}
		w.Write([]byte(`[{"Id":"app1","Names":["/app"],"Labels":{}},{"Id":"sb1","Names":["/sb"],"Labels":{"titanos.sandbox":"1"}}]`))
	})
	mux.HandleFunc("GET /v1.43/images/{ref...}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"no such image"}`))
	})
	mux.HandleFunc("POST /v1.43/images/create", func(w http.ResponseWriter, r *http.Request) {
		note("pull " + r.URL.Query().Get("fromImage") + " " + r.URL.Query().Get("tag"))
		w.Write([]byte(`{"status":"Pulling"}` + "\n" + `{"status":"Done"}`))
	})
	mux.HandleFunc("POST /v1.43/containers/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&created)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"Id":"run1"}`))
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		note("start " + r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/wait", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(waitFor):
			w.Write([]byte(`{"StatusCode":3}`))
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/kill", func(w http.ResponseWriter, r *http.Request) {
		note("kill " + r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Write(append(frame(1, "42\n"), frame(2, "warnung\n")...))
	})
	mux.HandleFunc("DELETE /v1.43/containers/{id}", func(w http.ResponseWriter, r *http.Request) {
		note("delete " + r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return newClient(socket), &mu, &log, &created
}

func TestSandboxRunsLockedDown(t *testing.T) {
	m, _, _ := newTestManager(t)
	api, mu, log, created := sandboxEngine(t, 0)
	m.api = api
	m.settings.Enabled = true
	if _, err := m.Sandbox(context.Background(), SandboxRequest{Code: "print(6*7)", Work: "/mnt/tank/KI-Agent/alex", UID: 1001, GID: 100, User: "alex"}); err != nil {
		t.Fatal(err)
	}
	res, err := m.Sandbox(context.Background(), SandboxRequest{Language: "python", Code: "print(6*7)", Work: "/mnt/tank/KI-Agent/alex", UID: 1001, GID: 100, User: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.TimedOut || res.Output != "42\nwarnung\n" {
		t.Fatalf("result %+v", res)
	}
	c := *created
	host := c["HostConfig"].(map[string]any)
	if c["Image"] != SandboxImage || c["User"] != "1001:100" || c["NetworkDisabled"] != true || host["NetworkMode"] != "none" || host["ReadonlyRootfs"] != true ||
		host["Privileged"] != false || host["PidsLimit"].(float64) != 128 || host["Memory"].(float64) != 512<<20 {
		t.Fatalf("not locked down: %v", c)
	}
	if binds := host["Binds"].([]any); len(binds) != 1 || binds[0] != "/mnt/tank/KI-Agent/alex:/work:rw" {
		t.Fatalf("binds %v", binds)
	}
	if caps := host["CapDrop"].([]any); len(caps) != 1 || caps[0] != "ALL" {
		t.Fatalf("caps %v", caps)
	}
	if cmd := c["Cmd"].([]any); cmd[0] != "python3" || cmd[3] != "print(6*7)" {
		t.Fatalf("cmd %v", cmd)
	}
	mu.Lock()
	got := strings.Join(*log, ",")
	mu.Unlock()
	if !strings.Contains(got, "pull python sha256:") || !strings.Contains(got, "delete run1") || !strings.Contains(got, "delete old1") {
		t.Fatalf("calls %s", got)
	}
	// A sandbox is never an app.
	list, err := api.containers(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != "app1" {
		t.Fatalf("listed %v %v", list, err)
	}
}

func TestSandboxTimeoutKillsAndRemoves(t *testing.T) {
	m, _, _ := newTestManager(t)
	api, mu, log, _ := sandboxEngine(t, time.Hour)
	m.api = api
	m.settings.Enabled = true
	res, err := m.Sandbox(context.Background(), SandboxRequest{Language: "sh", Code: "sleep 999", Work: "/w", UID: 1001, GID: 100, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := strings.Join(*log, ",")
	mu.Unlock()
	if !res.TimedOut || !strings.Contains(got, "kill run1") || !strings.Contains(got, "delete run1") {
		t.Fatalf("result %+v calls %s", res, got)
	}
	if _, err := m.Sandbox(context.Background(), SandboxRequest{Language: "ruby", Code: "x", Work: "/w", UID: 1}); err == nil {
		t.Fatal("unknown language ran")
	}
	if _, err := m.Sandbox(context.Background(), SandboxRequest{Code: "x", Work: "", UID: 1}); err == nil {
		t.Fatal("ran without a folder")
	}
}
