package docker

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestViewerCreateBodyIsLockedDown(t *testing.T) {
	spec := ViewerSpec{Name: "titanos-viewer-1", Image: "sha256:abc", User: "anna", App: "immich", Env: []string{"VIEWER_GPU=cpu"},
		Profile: "/mnt/data/.titanos/viewer/profiles/anna/immich", Network: "titanos-viewer", MemoryBytes: 1 << 30, PidsLimit: 512, ShmBytes: 512 << 20}
	data, _ := json.Marshal(viewerCreateBody(spec))
	var body struct {
		User             string
		Labels           map[string]string
		NetworkingConfig struct{ EndpointsConfig map[string]any }
		HostConfig       struct {
			NetworkMode    string
			ReadonlyRootfs bool
			Tmpfs          map[string]string
			Binds          []string
			Memory         int64
			PidsLimit      int64
			ShmSize        int64
			CapDrop        []string
			SecurityOpt    []string
			Privileged     bool
			Devices        []map[string]string
			DeviceRequests []map[string]any
			GroupAdd       []string
		}
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	h := body.HostConfig
	if body.User != "1000:1000" || !h.ReadonlyRootfs || h.Privileged || !slices.Equal(h.CapDrop, []string{"ALL"}) || !slices.Equal(h.SecurityOpt, []string{"no-new-privileges"}) {
		t.Fatalf("not locked down: %s", data)
	}
	if h.NetworkMode != "titanos-viewer" || len(body.NetworkingConfig.EndpointsConfig) != 1 || body.NetworkingConfig.EndpointsConfig["titanos-viewer"] == nil {
		t.Fatalf("network: %s", data)
	}
	if len(h.Tmpfs) != 1 || !strings.Contains(h.Tmpfs["/tmp"], "size=") {
		t.Fatalf("tmpfs: %v", h.Tmpfs)
	}
	if !slices.Equal(h.Binds, []string{spec.Profile + ":/profile:rw"}) || h.Memory != 1<<30 || h.PidsLimit != 512 || h.ShmSize != 512<<20 {
		t.Fatalf("binds/limits: %s", data)
	}
	if len(h.Devices) != 0 || len(h.DeviceRequests) != 0 || len(h.GroupAdd) != 0 {
		t.Fatalf("devices without GPU: %s", data)
	}
	if body.Labels[ViewerLabel] != "1" || body.Labels[ViewerLabel+".user"] != "anna" || body.Labels[ViewerLabel+".app"] != "immich" {
		t.Fatalf("labels: %v", body.Labels)
	}
	spec.Devices, spec.GroupAdd = []string{"/dev/dri/renderD128"}, []string{"105"}
	data, _ = json.Marshal(viewerCreateBody(spec))
	if !strings.Contains(string(data), `"PathOnHost":"/dev/dri/renderD128"`) || !strings.Contains(string(data), `"GroupAdd":["105"]`) || strings.Contains(string(data), "/dev/dri\"") {
		t.Fatalf("render node: %s", data)
	}
}

// viewerEngine answers the calls the viewer makes; "app1" has no viewer label.
func viewerEngine(t *testing.T) (*Manager, *[]string) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var log []string
	note := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.43/containers/json", func(w http.ResponseWriter, r *http.Request) {
		filters := r.URL.Query().Get("filters")
		switch {
		case strings.Contains(filters, ViewerLabel+"=1"):
			w.Write([]byte(`[{"Id":"v1","Names":["/titanos-viewer-1"],"Labels":{"titanos.viewer":"1","titanos.viewer.user":"anna","titanos.viewer.app":"immich"}}]`))
		default:
			w.Write([]byte(`[{"Id":"app1","Names":["/immich"],"Labels":{}},{"Id":"v1","Names":["/titanos-viewer-1"],"Labels":{"titanos.viewer":"1"}}]`))
		}
	})
	mux.HandleFunc("GET /v1.43/containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "v1":
			w.Write([]byte(`{"Id":"v1","Name":"/titanos-viewer-1","State":{"Running":true},"Config":{"Labels":{"titanos.viewer":"1"}}}`))
		case "fake0000000a":
			// The label without the name: not the viewer's own, yet no app.
			w.Write([]byte(`{"Id":"fake0000000a","Name":"/other","Config":{"Labels":{"titanos.viewer":"1"}}}`))
		case "app1":
			w.Write([]byte(`{"Id":"app1","Name":"/immich","Config":{"Labels":{}},"NetworkSettings":{"Networks":{"titanos-viewer-1":{"IPAddress":"172.30.0.2","Aliases":["immich"]}}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"no such container"}`))
		}
	})
	mux.HandleFunc("DELETE /v1.43/containers/{id}", func(w http.ResponseWriter, r *http.Request) {
		note("delete " + r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		note("stop " + r.PathValue("id") + " t=" + r.URL.Query().Get("t"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1.43/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		note("start " + r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1.43/images/{ref}/json", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("ref") == "viewerimg" {
			w.Write([]byte(`{"Id":"viewerimg","Config":{"Labels":{"titanos.viewer.image":"1","titanos.viewer.release":"0.61.0"}}}`))
			return
		}
		w.Write([]byte(`{"Id":"` + r.PathValue("ref") + `","Config":{"Labels":{}}}`))
	})
	mux.HandleFunc("DELETE /v1.43/images/{ref}", func(w http.ResponseWriter, r *http.Request) {
		note("rmi " + r.PathValue("ref") + " force=" + r.URL.Query().Get("force"))
		w.Write([]byte(`[]`))
	})
	mux.HandleFunc("POST /v1.43/networks/{name}/disconnect", func(w http.ResponseWriter, r *http.Request) {
		note("disconnect")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1.43/networks/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Name":"titanos-viewer","Internal":false,"Labels":{}}`))
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	m, _, _ := newTestManager(t)
	m.api = newClient(socket)
	return m, &log
}

func TestViewerCallsTouchOnlyLabelledContainers(t *testing.T) {
	m, log := viewerEngine(t)
	ctx := t.Context()
	list, err := m.Viewers(ctx)
	if err != nil || len(list) != 1 || list[0].User != "anna" || list[0].App != "immich" {
		t.Fatalf("viewers: %+v %v", list, err)
	}
	if err := m.ViewerRemove(ctx, "app1"); !errors.Is(err, ErrNotViewer) {
		t.Fatalf("removed an app: %v", err)
	}
	if err := m.ViewerStart(ctx, "app1"); !errors.Is(err, ErrNotViewer) {
		t.Fatalf("started an app: %v", err)
	}
	if err := m.ViewerRemove(ctx, "fake0000000a"); !errors.Is(err, ErrNotViewer) {
		t.Fatalf("label without name removed: %v", err)
	}
	if err := m.ViewerRemove(ctx, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := m.ViewerRemove(ctx, "gone"); err != nil {
		t.Fatalf("a gone viewer: %v", err)
	}
	// An endpoint without the marker alias was not put there by TitanOS.
	if err := m.ViewerDetach(ctx, "titanos-viewer-1", "app1", "titanos-viewer-attached"); err != nil {
		t.Fatal(err)
	}
	// Stopped with time for Chromium to write its profile, then removed.
	if !slices.Equal(*log, []string{"stop v1 t=10", "delete v1"}) {
		t.Fatalf("calls: %v", *log)
	}
	// Only images with the viewer label are removed, never by force.
	if err := m.ViewerRemoveImage(ctx, "nginx"); err == nil {
		t.Fatal("removed an image of an app")
	}
	if err := m.ViewerRemoveImage(ctx, "viewerimg"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal((*log)[2:], []string{"rmi viewerimg force="}) {
		t.Fatalf("image calls: %v", *log)
	}
	// A network of that name that is not internal is refused, not changed.
	if err := m.ViewerNetwork(ctx, "titanos-viewer-1", "tvw1", "anna", "immich"); err == nil {
		t.Fatal("foreign network accepted")
	}
	// The apps' list leaves viewers out.
	containers, _ := m.api.containers(ctx)
	if len(containers) != 1 || containers[0].ID != "app1" {
		t.Fatalf("viewer listed as app: %+v", containers)
	}
}

func TestViewerSpecMountsPolicyAndCapsCPU(t *testing.T) {
	spec := ViewerSpec{Name: "titanos-viewer-1", Image: "sha256:abc", Profile: "/p", Network: "titanos-viewer-1",
		Policy: "/run/titanos/viewer/titanos-viewer-1/titanos.json", NanoCPUs: 2e9, CPUShares: 512}
	data, _ := json.Marshal(viewerCreateBody(spec))
	for _, want := range []string{`"/run/titanos/viewer/titanos-viewer-1/titanos.json:/etc/chromium/policies/managed/titanos.json:ro"`, `"NanoCpus":2000000000`, `"CpuShares":512`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in %s", want, data)
		}
	}
}

func TestAppRoutesRefuseViewers(t *testing.T) {
	m, log := viewerEngine(t)
	m.Disabled = false
	ctx := t.Context()
	for _, id := range []string{"fake0000000a"} {
		if err := m.Remove(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("remove: %v", err)
		}
		if _, err := m.Logs(ctx, id, 10); !errors.Is(err, ErrNotFound) {
			t.Errorf("logs: %v", err)
		}
		if _, _, err := m.Console(ctx, id, "", ""); !errors.Is(err, ErrNotFound) {
			t.Errorf("console: %v", err)
		}
		if err := m.Action(ctx, id, "stop"); !errors.Is(err, ErrNotFound) {
			t.Errorf("action: %v", err)
		}
	}
	if len(*log) != 0 {
		t.Fatalf("calls: %v", *log)
	}
}

func TestComposeRefusesViewerNetworks(t *testing.T) {
	for _, compose := range []string{
		"services:\n  a:\n    image: x\n    networks: [titanos-viewer-1]\nnetworks:\n  titanos-viewer-1:\n    external: true\n",
		"services:\n  a:\n    image: x\n    networks: [n]\nnetworks:\n  n:\n    name: Titanos-Viewer-abc\n    external: true\n",
		"services:\n  a:\n    image: x\n    network_mode: container:titanos-viewer-1\n",
		"services:\n  a:\n    image: x\n    networks:\n      titanos-viewer-x: {}\n",
	} {
		if err := refuseViewerNetworks(compose); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted:\n%s", compose)
		}
	}
	if err := refuseViewerNetworks("services:\n  a:\n    image: x\n    networks: [web]\nnetworks:\n  web: {}\n"); err != nil {
		t.Fatal(err)
	}
}
