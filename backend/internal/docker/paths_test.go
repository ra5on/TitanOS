package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestHostPathsListsMountsOfAllContainers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/v1.43") {
		case "/containers/json":
			if r.URL.Query().Get("all") != "true" {
				t.Error("stopped containers left out")
			}
			w.Write([]byte(`[{"Id":"aaa","Names":["/jellyfin"],"State":"exited"}]`))
		case "/containers/aaa/json":
			w.Write([]byte(`{"Mounts":[{"Type":"bind","Source":"/mnt/tank/Media","RW":false},{"Type":"tmpfs","Source":""},{"Type":"volume","Source":"/mnt/tank/.titanos/docker/volumes/x/_data"}]}`))
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	m := NewManager(config.Open(t.TempDir()), t.TempDir())
	m.api = &client{http: &http.Client{Transport: rewriteTransport{server.URL}}}
	paths, err := m.HostPaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != (HostPath{"jellyfin", "/mnt/tank/Media"}) {
		t.Fatalf("%+v", paths)
	}
	server.Close()
	m.settings.Enabled = true
	if _, err := m.HostPaths(context.Background()); err == nil {
		t.Fatal("an engine that does not answer gave an empty inventory")
	}
}
