package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestNetworks(t *testing.T) {
	m, _, _ := newTestManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.43/networks" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[
		 {"Id":"1111111111111111aaaa","Name":"bridge","Driver":"bridge","Options":{"com.docker.network.bridge.name":"docker0"},"IPAM":{"Config":[{"Subnet":"172.17.0.0/16"}]}},
		 {"Id":"0123456789abcdef0123","Name":"immich_default","Driver":"bridge","Options":{},"IPAM":{"Config":[{"Subnet":"172.18.0.0/16"},{"Subnet":"fd00:18::/64"}]}},
		 {"Id":"2222222222222222bbbb","Name":"own","Driver":"bridge","Internal":true,"Options":{"com.docker.network.bridge.name":"br-own"},"IPAM":{"Config":[]}},
		 {"Id":"3333333333333333cccc","Name":"titanos-lan-apps","Driver":"ipvlan","Options":{"parent":"tnapps"},"IPAM":{"Config":[{"Subnet":"192.168.30.0/24"}]}},
		 {"Id":"4444","Name":"host","Driver":"host"},
		 {"Id":"5555","Name":"none","Driver":"null"}]`))
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	list, err := m.Networks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("networks: %+v", list)
	}
	byName := map[string]EngineNetwork{}
	for _, n := range list {
		byName[n.Name] = n
	}
	if byName["bridge"].Device != "docker0" || byName["immich_default"].Device != "br-0123456789ab" || byName["own"].Device != "br-own" || !byName["own"].Internal {
		t.Errorf("devices: %+v", list)
	}
	if got := byName["immich_default"].Subnets; len(got) != 2 || got[1] != "fd00:18::/64" {
		t.Errorf("subnets: %v", got)
	}
	if lan := byName["titanos-lan-apps"]; lan.Device != "" || lan.Parent != "tnapps" || lan.Driver != "ipvlan" {
		t.Errorf("ipvlan: %+v", lan)
	}
	if byName["own"].Subnets == nil {
		t.Error("subnets must be a list")
	}
}
