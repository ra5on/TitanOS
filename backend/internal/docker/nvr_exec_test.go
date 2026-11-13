package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// An NVIDIA GPU that decodes reaches the container through Docker's own
// CDI support (PLAN 82-4), as for apps and the remote view.
func TestNVRCreateBodyCDI(t *testing.T) {
	spec := NVRSpec{Image: "sha256:abc", Config: "/c", Media: "/m", Devices: []string{"/dev/dri/renderD128"}, CDIDevices: []string{"nvidia.com/gpu=GPU-1"}}
	data, _ := json.Marshal(nvrCreateBody(spec))
	var body struct {
		HostConfig struct {
			DeviceRequests []struct {
				Driver    string
				DeviceIDs []string
			}
			Devices []struct{ PathOnHost string }
		}
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	r := body.HostConfig.DeviceRequests
	if len(r) != 1 || r[0].Driver != "cdi" || len(r[0].DeviceIDs) != 1 || r[0].DeviceIDs[0] != "nvidia.com/gpu=GPU-1" || len(body.HostConfig.Devices) != 1 {
		t.Errorf("%s", data)
	}
	spec.CDIDevices = nil
	if data, _ := json.Marshal(nvrCreateBody(spec)); strings.Contains(string(data), "DeviceRequests") {
		t.Errorf("without CDI: %s", data)
	}
	// The spec's hash is made from its JSON: an empty list leaves it as it
	// was before the field existed.
	if data, _ := json.Marshal(spec); strings.Contains(string(data), "CDIDevices") {
		t.Errorf("%s", data)
	}
}

// execEngine answers the calls of NVRExec like the Docker engine: the
// start of an exec hijacks the connection and writes the multiplexed
// stream until the command ends.
func execEngine(t *testing.T, labels string, running bool, output []byte) (socket string, created *[]map[string]any) {
	t.Helper()
	socket = filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bodies []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.43/containers/titanos-nvr/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"Id":"c0ffee","Name":"/titanos-nvr","State":{"Running":%v},"Config":{"Labels":%s}}`, running, labels)
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/exec", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["container"] = r.PathValue("id")
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"Id":"e1"}`))
	})
	mux.HandleFunc("POST /v1.43/exec/e1/start", func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
		buf.Write(output)
		buf.Flush()
	})
	mux.HandleFunc("GET /v1.43/exec/e1/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Running":false,"ExitCode":7}`))
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return socket, &bodies
}

func TestNVRExec(t *testing.T) {
	own := `{"titanos.nvr":"1"}`
	output := append(frame(1, `{"cam":{"query":""}}`), frame(2, "warn\n")...)
	socket, created := execEngine(t, own, true, output)
	m := &Manager{api: newClient(socket)}
	cmd := []string{"curl", "-s", "-m", "5", "http://127.0.0.1:1984/api/preload"}
	out, code, err := m.NVRExec(context.Background(), cmd, 1<<10)
	if err != nil || code != 7 || out != `{"cam":{"query":""}}warn`+"\n" {
		t.Fatalf("%q %d %v", out, code, err)
	}
	body := (*created)[0]
	if body["container"] != "c0ffee" || body["Tty"] != false || body["AttachStdin"] != false || fmt.Sprint(body["Cmd"]) != fmt.Sprint([]any{"curl", "-s", "-m", "5", "http://127.0.0.1:1984/api/preload"}) {
		t.Errorf("exec created with %v", body)
	}
	if _, ok := body["User"]; ok {
		t.Errorf("exec as another user: %v", body)
	}
	// The output is cut at the limit.
	if out, _, err := m.NVRExec(context.Background(), cmd, 5); err != nil || out != `{"cam` {
		t.Errorf("limit 5: %q %v", out, err)
	}

	// A container of that name that is not the NVR's own is never asked.
	socket, created = execEngine(t, `{}`, true, output)
	m = &Manager{api: newClient(socket)}
	if _, _, err := m.NVRExec(context.Background(), cmd, 1<<10); !errors.Is(err, ErrNotNVR) || len(*created) != 0 {
		t.Errorf("foreign container: %v, %d execs", err, len(*created))
	}
	// Nor one that does not run.
	socket, created = execEngine(t, own, false, output)
	m = &Manager{api: newClient(socket)}
	if _, _, err := m.NVRExec(context.Background(), cmd, 1<<10); !errors.Is(err, ErrNVRNotRunning) || len(*created) != 0 {
		t.Errorf("stopped container: %v, %d execs", err, len(*created))
	}
	if _, _, err := m.NVRExec(context.Background(), nil, 1<<10); err == nil {
		t.Error("no command")
	}
}
