package docker

import (
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
)

// TestHostPortsIncludeStoppedContainers: a stopped container still holds
// its published ports; Docker lists them only in its port bindings.
func TestHostPortsIncludeStoppedContainers(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, ok := m.HostPorts(t.Context()); ok {
		t.Fatal("ports known before Docker runs")
	}
	socket := filepath.Join(t.TempDir(), "engine.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.43/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
		 {"Id":"aaaaaaaaaaaa1111","State":"running","Labels":{},"Ports":[{"IP":"0.0.0.0","PrivatePort":80,"PublicPort":32768,"Type":"tcp"},{"PrivatePort":53,"PublicPort":5353,"Type":"udp"}]},
		 {"Id":"bbbbbbbbbbbb2222","State":"exited","Labels":{},"Ports":[]}]`))
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "aaaaaaaaaaaa1111" {
			w.Write([]byte(`{"HostConfig":{"PortBindings":{"80/tcp":[{"HostIp":"","HostPort":""}]}}}`))
			return
		}
		w.Write([]byte(`{"HostConfig":{"PortBindings":{
		 "80/tcp":[{"HostIp":"0.0.0.0","HostPort":"20001"},{"HostIp":"::","HostPort":"20001"}],
		 "9000-9001/tcp":[{"HostPort":"20005-20006"}],
		 "53/udp":[{"HostPort":"20010"}],
		 "81/tcp":[{"HostPort":"nonsense"}]}}}`))
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	m.api = newClient(socket)
	m.applied = "/somewhere"
	ports, ok := m.HostPorts(t.Context())
	if want := []int{20001, 20005, 20006, 32768}; !ok || !slices.Equal(ports, want) {
		t.Errorf("ports = %v %v, want %v", ports, ok, want)
	}
}
