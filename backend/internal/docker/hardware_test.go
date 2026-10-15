package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"go.yaml.in/yaml/v3"
)

func TestResolveStableHardwareAndPreserveCompose(t *testing.T) {
	device := hardware.ContainerDevice{ID: "0000:00:02.0", Kind: "igpu", Available: true, Mappings: []string{"/dev/dri/renderD129:/dev/dri/renderD129"}, Groups: []string{"107"}}
	input := "services:\n  media:\n    image: example\n    x-titanos-hardware: [\"0000:00:02.0\"]\n    ports: [\"8080:80\"]\n    environment:\n      PASSWORD: ${PASSWORD}\n    devices: [/dev/other:/dev/other]\n"
	output, err := resolveHardware(input, []hardware.ContainerDevice{device}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	yaml.Unmarshal([]byte(output), &doc)
	service := doc["services"].(map[string]any)["media"].(map[string]any)
	if len(service["devices"].([]any)) != 2 || service["environment"].(map[string]any)["PASSWORD"] != "${PASSWORD}" {
		t.Fatal(output)
	}
	if service["labels"].(map[string]any)["titanos.hardware"] != "0000:00:02.0" {
		t.Fatal("no stable lease label", output)
	}
	if _, err = resolveHardware(input, []hardware.ContainerDevice{device}, map[string]bool{device.ID: true}); err == nil {
		t.Fatal("started reserved GPU")
	}
	if _, err = resolveHardware(input, nil, nil); err == nil {
		t.Fatal("ignored missing hardware")
	}
}

func TestGPUHandoffRollbackAndRecovery(t *testing.T) {
	for _, failedStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "vm exits", true: "start fails"}[failedStart], func(t *testing.T) {
			var mu sync.Mutex
			running := true
			stops, starts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					json.NewEncoder(w).Encode([]map[string]any{{"Id": "aaaaaaaaaaaa", "Names": []string{"/media"}, "State": "running"}, {"Id": "bbbbbbbbbbbb", "Names": []string{"/unrelated"}, "State": "exited"}})
				case strings.HasSuffix(r.URL.Path, "/json"):
					json.NewEncoder(w).Encode(map[string]any{"State": map[string]any{"Running": running}, "Config": map[string]any{"Labels": map[string]string{"titanos.hardware": "0000:01:00.0"}}})
				case strings.HasSuffix(r.URL.Path, "/stop"):
					if r.URL.Query().Get("t") != "30" {
						t.Error("missing graceful timeout")
					}
					stops++
					running = false
					w.WriteHeader(204)
				case strings.HasSuffix(r.URL.Path, "/start"):
					starts++
					running = true
					w.WriteHeader(204)
				default:
					t.Error(r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			m := NewManager(config.Open(t.TempDir()), t.TempDir())
			m.hardwareRunDir = t.TempDir()
			m.applied = "data"
			m.api = &client{http: &http.Client{Transport: rewriteTransport{server.URL}}}
			available := true
			m.HardwareDevices = func(context.Context) []hardware.ContainerDevice {
				return []hardware.ContainerDevice{{ID: "0000:01:00.0", Kind: "nvidia", Available: available, UUID: "GPU-aaaa", Index: "0"}}
			}
			ctx := context.Background()
			if _, err := m.BeginVM(ctx, "vm", []string{"0000:01:00.0"}, nil, false); err == nil {
				t.Fatal("stopped GPU user without opt-in")
			}
			finish, err := m.BeginVM(ctx, "vm", []string{"0000:01:00.0"}, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			finish(!failedStart)
			if !failedStart {
				if err := m.Action(ctx, "aaaaaaaaaaaa", "start"); err == nil {
					t.Fatal("container stole VM GPU")
				}
				// Recovery reads /run after a daemon restart.
				m.gpuLeases = nil
				if err := m.LoadHardwareLeases(); err != nil {
					t.Fatal(err)
				}
				available = false
				if err := m.ReturnVMHardware(ctx, map[string]bool{}); err == nil {
					t.Fatal("restarted before driver returned")
				}
				available = true
				if err := m.ReturnVMHardware(ctx, map[string]bool{}); err != nil {
					t.Fatal(err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if stops != 1 || starts != 1 || len(m.gpuLeases) != 0 {
				t.Fatalf("stops=%d starts=%d leases=%v", stops, starts, m.gpuLeases)
			}
		})
	}
}

type rewriteTransport struct{ target string }

func (t rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(t.target, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}

func TestRawGPUCannotBypassLeaseWithAnotherSelection(t *testing.T) {
	input := "services:\n  media:\n    image: example\n    x-titanos-hardware: [available]\n    gpus: all\n"
	devices := []hardware.ContainerDevice{{ID: "available", Kind: "nvidia", Available: true}}
	if _, err := resolveHardware(input, devices, map[string]bool{"held": true}); err == nil {
		t.Fatal("raw GPU bypassed reservation")
	}
}

func TestSpecificNvidiaVisibilityAndCoralAccess(t *testing.T) {
	gpu := hardware.ContainerDevice{ID: "card", Kind: "nvidia", UUID: "GPU-aaaa", Index: "0"}
	var info apiInspect
	json.Unmarshal([]byte(`{"HostConfig":{"Runtime":"nvidia"},"Config":{"Env":["NVIDIA_VISIBLE_DEVICES=GPU-bbbb"]}}`), &info)
	if usesDevice(info, gpu) {
		t.Fatal("stopped container on another GPU")
	}
	json.Unmarshal([]byte(`{"HostConfig":{"Runtime":"nvidia"},"Config":{"Env":["NVIDIA_VISIBLE_DEVICES=GPU-aaaa"]}}`), &info)
	if !usesDevice(info, gpu) {
		t.Fatal("missed matching GPU")
	}
	info = apiInspect{}
	json.Unmarshal([]byte(`{"Mounts":[{"Source":"/dev/bus/usb"}]}`), &info)
	if !usesDevice(info, hardware.ContainerDevice{ID: "coral-usb", Kind: "coral"}) {
		t.Fatal("missed USB bus access")
	}
}

func TestDriverlessVMReservationCanBeReleased(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()), t.TempDir())
	m.hardwareRunDir = t.TempDir()
	m.HardwareDevices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{{ID: "card", Kind: "nvidia"}}
	}
	finish, err := m.BeginVM(context.Background(), "vm", []string{"card"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReturnVMHardware(context.Background(), nil); err != nil || len(m.gpuLeases) != 0 {
		t.Fatalf("lease stuck without any containers: %v", err)
	}
}

func TestNvidiaCDIBecomesEngineDeviceRequest(t *testing.T) {
	gpu := hardware.ContainerDevice{ID: "0000:01:00.0", Kind: "nvidia", Available: true, Mappings: []string{"nvidia.com/gpu=GPU-chosen"}}
	for _, selected := range []bool{false, true} {
		input := `services:
  ollama:
    image: ollama/ollama
    devices: [nvidia.com/gpu=all, /dev/other:/dev/other]
    deploy:
      resources:
        limits:
          memory: 8G
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
`
		if selected {
			input += "    x-titanos-hardware: ['0000:01:00.0']\n    runtime: nvidia\n    gpus: all\n"
		}
		output, err := resolveHardware(input, []hardware.ContainerDevice{gpu}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		yaml.Unmarshal([]byte(output), &doc)
		s := doc["services"].(map[string]any)["ollama"].(map[string]any)
		if mappings := s["devices"].([]any); len(mappings) != 1 || mappings[0] != "/dev/other:/dev/other" {
			t.Fatal(output)
		}
		resources := s["deploy"].(map[string]any)["resources"].(map[string]any)
		if resources["limits"].(map[string]any)["memory"] != "8G" {
			t.Fatal("lost limits")
		}
		requests := resources["reservations"].(map[string]any)["devices"].([]any)
		req := requests[len(requests)-1].(map[string]any)
		want := "nvidia.com/gpu=all"
		if selected {
			want = "nvidia.com/gpu=GPU-chosen"
			if len(requests) != 1 || s["gpus"] != nil || s["runtime"] != nil {
				t.Fatal(output)
			}
		}
		if req["driver"] != "cdi" || req["count"] != nil || len(req["device_ids"].([]any)) != 1 || req["device_ids"].([]any)[0] != want {
			t.Fatal(output)
		}
		twice, err := resolveHardware(output, []hardware.ContainerDevice{gpu}, nil)
		if err != nil || twice != output {
			t.Fatal("not idempotent", err, twice)
		}
	}
}
func TestCDIRequestRecognizedDuringVMHandoff(t *testing.T) {
	gpu := hardware.ContainerDevice{Kind: "nvidia", UUID: "GPU-chosen", Index: "0"}
	for _, id := range []string{"nvidia.com/gpu=GPU-chosen", "nvidia.com/gpu=0", "nvidia.com/gpu=all", "nvidia.com/gpu=GPU-other"} {
		var info apiInspect
		json.Unmarshal([]byte(`{"HostConfig":{"DeviceRequests":[{"Driver":"cdi","DeviceIDs":["`+id+`"]}]}}`), &info)
		if got := usesDevice(info, gpu); got != (id != "nvidia.com/gpu=GPU-other") {
			t.Fatal(id, got)
		}
	}
}

func TestStartRepairsOnlyFailedManagedCDIService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"message": `error gathering device information while adding custom device "nvidia.com/gpu=GPU-test": no such file or directory`})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Config": map[string]any{"Labels": map[string]string{"com.docker.compose.project": "ollama", "com.docker.compose.service": "ai"}}, "HostConfig": map[string]any{"Devices": []map[string]string{{"PathOnHost": "nvidia.com/gpu=GPU-test"}}}})
	}))
	defer server.Close()
	store := config.Open(t.TempDir())
	if err := store.Write(composeFile("ollama"), []byte("services:\n  ai:\n    image: local\n    devices: [nvidia.com/gpu=GPU-test]\n")); err != nil {
		t.Fatal(err)
	}
	m := NewManager(store, t.TempDir())
	m.hardwareRunDir = t.TempDir()
	m.applied = "data"
	m.settings = Settings{Enabled: true, Volume: "data"}
	runner := &fakeRunner{}
	m.runner = runner
	m.api = &client{http: &http.Client{Transport: rewriteTransport{server.URL}}}
	if err := m.Action(context.Background(), "aaaaaaaaaaaa", "start"); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "up --detach --no-deps --force-recreate --pull never ai") || strings.Contains(runner.commands[0], "--renew-anon-volumes") {
		t.Fatal(runner.commands)
	}
	if m.jobs["ollama"].State != "done" {
		t.Fatal(m.jobs)
	}
}
