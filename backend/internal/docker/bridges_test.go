package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type netEngine struct {
	mu       sync.Mutex
	networks []map[string]any
	created  []map[string]any
	deleted  []string
}

func netFixture(t *testing.T) (*Manager, *netEngine) {
	t.Helper()
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	m.lan = []LANNetwork{{Name: "apps", Subnet: "192.168.30.0/24"}}
	old := interfacePrefixes
	interfacePrefixes = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")} }
	t.Cleanup(func() { interfacePrefixes = old })
	if err := m.store.Write("apps/immich/compose.yaml", []byte("services:\n  web:\n    networks: [shared-db]\nnetworks:\n  shared-db:\n    external: true\n")); err != nil {
		t.Fatal(err)
	}
	m.appList = nil
	net := func(id, name, driver, subnet string, labels map[string]string) map[string]any {
		cfg := []map[string]string{}
		if subnet != "" {
			cfg = append(cfg, map[string]string{"Subnet": subnet, "Gateway": strings.TrimSuffix(subnet, "0/16") + "1"})
		}
		return map[string]any{"Id": id, "Name": name, "Driver": driver, "Scope": "local", "Labels": labels, "IPAM": map[string]any{"Config": cfg}, "Created": "2026-11-01T10:00:00Z"}
	}
	e := &netEngine{networks: []map[string]any{
		net("n1", "bridge", "bridge", "172.17.0.0/16", nil),
		net("n2", "host", "host", "", nil),
		net("n3", "none", "null", "", nil),
		net("n4", "titanos-lan-apps", "ipvlan", "192.168.30.0/24", nil),
		net("n5", "titanos-viewer-abc", "bridge", "172.30.0.0/16", map[string]string{"titanos.viewer": "1"}),
		net("n6", "immich_default", "bridge", "172.18.0.0/16", map[string]string{"com.docker.compose.project": "immich"}),
		net("n7", "shared-db", "bridge", "172.19.0.0/16", nil),
		net("n8", "old_default", "bridge", "172.20.0.0/16", map[string]string{"com.docker.compose.project": "old"}),
		net("n9", "busy", "bridge", "172.21.0.0/16", nil),
		net("n10", "spare", "bridge", "172.22.0.0/16", nil),
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/v1.43")
		switch {
		case r.Method == "GET" && path == "/networks":
			json.NewEncoder(w).Encode(e.networks)
		case r.Method == "GET" && strings.HasPrefix(path, "/networks/"):
			id := strings.TrimPrefix(path, "/networks/")
			for _, n := range e.networks {
				if n["Id"] == id {
					json.NewEncoder(w).Encode(n)
					return
				}
			}
			http.Error(w, `{"message":"no such network"}`, 404)
		case r.Method == "GET" && path == "/containers/json":
			w.Write([]byte(`[
			 {"Id":"a","Names":["/immich-web-1"],"State":"running","Labels":{"com.docker.compose.project":"immich"},"NetworkSettings":{"Networks":{"immich_default":{"NetworkID":"n6"}}}},
			 {"Id":"b","Names":["/stopped"],"State":"exited","Labels":{},"NetworkSettings":{"Networks":{"busy":{"NetworkID":""}}}}]`))
		case r.Method == "POST" && path == "/networks/create":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			e.created = append(e.created, body)
			id := "new" + body["Name"].(string)
			n := map[string]any{"Id": id, "Name": body["Name"], "Driver": "bridge", "Internal": body["Internal"], "IPAM": map[string]any{"Config": []map[string]string{{"Subnet": "10.50.0.0/24", "Gateway": "10.50.0.1"}}}}
			e.networks = append(e.networks, n)
			w.WriteHeader(201)
			w.Write([]byte(`{"Id":"` + id + `"}`))
		case r.Method == "DELETE" && strings.HasPrefix(path, "/networks/"):
			id := strings.TrimPrefix(path, "/networks/")
			e.deleted = append(e.deleted, id)
			for i, n := range e.networks {
				if n["Id"] == id {
					e.networks = append(e.networks[:i], e.networks[i+1:]...)
					break
				}
			}
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	return m, e
}

func TestAllNetworksKinds(t *testing.T) {
	m, _ := netFixture(t)
	list, err := m.AllNetworks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]DockerNetwork{}
	for _, n := range list {
		by[n.Name] = n
	}
	want := map[string]string{"bridge": NetworkSystem, "host": NetworkSystem, "none": NetworkSystem, "titanos-lan-apps": NetworkLAN, "titanos-viewer-abc": NetworkTitanOS,
		"immich_default": NetworkApp, "shared-db": NetworkApp, "old_default": NetworkUser, "busy": NetworkUser, "spare": NetworkUser}
	for name, kind := range want {
		if n := by[name]; n.Kind != kind || n.Protected != (kind != NetworkUser) {
			t.Errorf("%s: %+v", name, n)
		}
	}
	if n := by["immich_default"]; n.App != "immich" || len(n.UsedBy) != 1 || n.UsedBy[0].Container != "immich-web-1" || n.Subnets[0] != "172.18.0.0/16" || n.Gateways[0] != "172.18.0.1" {
		t.Errorf("immich_default: %+v", n)
	}
	if n := by["busy"]; len(n.UsedBy) != 1 || n.UsedBy[0].Running {
		t.Errorf("stopped container without network ID: %+v", n)
	}
	if list[0].Kind != NetworkUser {
		t.Errorf("order: %+v", list[0])
	}
}

func TestCreateBridge(t *testing.T) {
	m, e := netFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		req  BridgeRequest
		want error
	}{
		{BridgeRequest{Name: "bridge"}, ErrInvalid},
		{BridgeRequest{Name: "titanos-x"}, ErrInvalid},
		{BridgeRequest{Name: "-x"}, ErrInvalid},
		{BridgeRequest{Name: "spare"}, ErrExists},
		{BridgeRequest{Name: "x", Subnet: "8.8.8.0/24"}, ErrInvalid},
		{BridgeRequest{Name: "x", Subnet: "10.0.0.1/24"}, ErrInvalid},
		{BridgeRequest{Name: "x", Subnet: "10.0.0.0/30"}, ErrInvalid},
		{BridgeRequest{Name: "x", Subnet: "172.18.5.0/24"}, ErrOverlap},   // engine network
		{BridgeRequest{Name: "x", Subnet: "192.168.30.0/25"}, ErrOverlap}, // LAN network
		{BridgeRequest{Name: "x", Subnet: "192.168.0.0/16"}, ErrOverlap},  // the NAS's own network
	} {
		if _, err := m.CreateBridge(ctx, tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%+v: %v, want %v", tc.req, err, tc.want)
		}
	}
	if len(e.created) != 0 {
		t.Fatalf("created despite refusal: %v", e.created)
	}
	n, err := m.CreateBridge(ctx, BridgeRequest{Name: "media", Subnet: "10.50.0.0/24", Internal: true})
	if err != nil || n.Name != "media" || n.Kind != NetworkUser || n.Subnets[0] != "10.50.0.0/24" {
		t.Fatalf("create: %+v %v", n, err)
	}
	body := e.created[0]
	if body["Driver"] != "bridge" || body["Internal"] != true || !strings.Contains(mustJSON(body["IPAM"]), "10.50.0.0/24") {
		t.Errorf("request: %v", body)
	}
	if _, err := m.CreateBridge(ctx, BridgeRequest{Name: "auto"}); err != nil || e.created[1]["IPAM"] != nil {
		t.Errorf("without subnet: %v %v", err, e.created[1])
	}
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestRemoveAndPruneNetworks(t *testing.T) {
	m, e := netFixture(t)
	ctx := context.Background()
	for name, want := range map[string]error{"bridge": ErrProtected, "titanos-lan-apps": ErrProtected, "titanos-viewer-abc": ErrProtected, "immich_default": ErrProtected,
		"shared-db": ErrProtected, "busy": ErrInUse, "nope": ErrNotFound, "../x": ErrNotFound} {
		if err := m.RemoveNetwork(ctx, name); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
	}
	m.jobs["immich"] = &Job{State: "running"}
	if err := m.RemoveNetwork(ctx, "spare"); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	if _, err := m.PruneNetworks(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("busy prune: %v", err)
	}
	delete(m.jobs, "immich")
	if err := m.RemoveNetwork(ctx, "spare"); err != nil || len(e.deleted) != 1 || e.deleted[0] != "n10" {
		t.Fatalf("remove: %v %v", err, e.deleted)
	}
	result, err := m.PruneNetworks(ctx)
	if err != nil || len(result.Deleted) != 1 || result.Deleted[0] != "old_default" {
		t.Fatalf("prune: %+v %v", result, err)
	}
	if len(e.deleted) != 2 {
		t.Errorf("deleted: %v", e.deleted)
	}
	if !mentions("networks: [shared-db]", "shared-db") || mentions("networks: [shared-db2]", "shared-db") || mentions("x-shared-db", "shared-db") {
		t.Error("mentions")
	}
}
