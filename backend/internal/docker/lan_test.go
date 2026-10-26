package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.yaml.in/yaml/v3"
)

type lanTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (t lanTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = t.target.Scheme
	u.Host = t.target.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}
func lanFixture(t *testing.T) (*Manager, *int) {
	t.Helper()
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	var mu sync.Mutex
	networks := map[string]map[string]any{}
	created := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/v1.43")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && path == "/networks/create" {
			var n map[string]any
			json.NewDecoder(r.Body).Decode(&n)
			name := n["Name"].(string)
			n["Id"] = name
			networks[name] = n
			*created++
			json.NewEncoder(w).Encode(map[string]string{"Id": name})
			return
		}
		name := strings.TrimPrefix(path, "/networks/")
		n := networks[name]
		if n == nil {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"missing"}`)
			return
		}
		if r.Method == "DELETE" {
			delete(networks, name)
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(n)
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	m.NetworkResolver = func(id string) (string, string, bool) { return "tnapps", "192.168.30.20/24", id == "apps" }
	return m, created
}
func exampleLAN() LANNetwork {
	return LANNetwork{Name: "apps", Parent: "apps", Driver: "ipvlan", Subnet: "192.168.30.0/24", Gateway: "192.168.30.1", Range: "192.168.30.128/26"}
}
func TestLANCreationReconcileAndSavedApplicationProtection(t *testing.T) {
	m, created := lanFixture(t)
	n := exampleLAN()
	if err := m.CreateLAN(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if *created != 1 || len(m.LANNetworks()) != 1 {
		t.Fatal(*created, m.LANNetworks())
	}
	m.ReconcileLAN(context.Background())
	if *created != 1 {
		t.Fatal("existing network recreated")
	}
	text := "services:\n  web:\n    image: nginx\n  db:\n    image: postgres\n"
	assigned, err := AssignLAN(text, "web", n, "192.168.30.130")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	yaml.Unmarshal([]byte(assigned), &doc)
	services := doc["services"].(map[string]any)
	if services["db"].(map[string]any)["networks"] != nil {
		t.Fatal("database moved to LAN")
	}
	attached := services["web"].(map[string]any)["networks"].(map[string]any)
	if _, ok := attached["default"]; !ok {
		t.Fatal("private network removed")
	}
	if err = m.validateLANCompose(assigned); err != nil {
		t.Fatal(err)
	}
	m.store.Write(composeFile("test"), []byte(assigned))
	m.mu.Lock()
	m.appList = nil
	m.mu.Unlock()
	if err = m.RemoveLAN(context.Background(), n.Name); err == nil {
		t.Fatal("saved stopped application dependency ignored")
	}
	m.store.Remove(composeFile("test"))
	m.store.Remove(appsDir + "/test")
	m.mu.Lock()
	m.appList = nil
	m.mu.Unlock()
	if err = m.RemoveLAN(context.Background(), n.Name); err != nil {
		t.Fatal(err)
	}
	if len(m.LANNetworks()) != 0 {
		t.Fatal("network record retained")
	}
}
func TestLANRejectsConflictingPoolsAndServiceAddresses(t *testing.T) {
	m, _ := lanFixture(t)
	n := exampleLAN()
	for _, mutate := range []func(*LANNetwork){func(n *LANNetwork) { n.Range = "192.168.30.0/25" }, func(n *LANNetwork) { n.Gateway = "192.168.30.20" }, func(n *LANNetwork) { n.Subnet = "192.168.20.0/24" }, func(n *LANNetwork) { n.HostAccess = true; n.HostAddress = "192.168.30.130" }, func(n *LANNetwork) { n.Driver = "bridge" }} {
		bad := n
		mutate(&bad)
		if err := m.CreateLAN(context.Background(), bad); err == nil {
			t.Fatal("bad network accepted", bad)
		}
	}
	if err := m.CreateLAN(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	other := n
	other.Name = "overlap"
	if err := m.CreateLAN(context.Background(), other); err == nil {
		t.Fatal("overlapping ranges accepted")
	}
	text := "services:\n  web:\n    image: test\n"
	for _, address := range []string{"192.168.30.20", "192.168.30.200", "127.0.0.1"} {
		if _, err := AssignLAN(text, "web", n, address); err == nil {
			t.Fatal("out-of-pool IP accepted")
		}
	}
	assigned, _ := AssignLAN(text, "web", n, "192.168.30.130")
	bad := strings.ReplaceAll(assigned, "192.168.30.130", "192.168.30.20")
	if err := m.validateLANCompose(bad); err == nil {
		t.Fatal("manually edited outside-pool IP accepted")
	}
	if len(m.LANNetworks()) != 1 || !slices.Contains([]string{"ipvlan", "macvlan"}, m.LANNetworks()[0].Driver) {
		t.Fatal(m.LANNetworks())
	}
}

type hostLinkRunner struct{ parent string }

func (r hostLinkRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	line := strings.Join(args, " ")
	if name == "ip" && strings.HasPrefix(line, "-j -d link show dev tl") {
		return `[{"link":"` + r.parent + `","linkinfo":{"info_kind":"ipvlan"}}]`, nil
	}
	if name == "ip" && line == "-j link show dev tnapps" {
		return `[{"ifindex":12}]`, nil
	}
	return "", nil
}
func TestHostEndpointAcceptsKernelParentNameAndRejectsAnotherLink(t *testing.T) {
	m, _ := lanFixture(t)
	n := exampleLAN()
	n.HostAccess = true
	n.HostAddress = "192.168.30.21"
	m.runner = hostLinkRunner{"tnapps"}
	if err := m.ensureLANHost(context.Background(), n, "tnapps"); err != nil {
		t.Fatal(err)
	}
	m.runner = hostLinkRunner{"another"}
	if err := m.ensureLANHost(context.Background(), n, "tnapps"); err == nil {
		t.Fatal("foreign host link accepted")
	}
}
