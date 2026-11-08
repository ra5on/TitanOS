package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// PLAN 73: hold the policies of the ordered apps, start them, give them back.
func TestHoldStartRestore(t *testing.T) {
	m, _, _ := newTestManager(t)
	var mu sync.Mutex
	policy := map[string]string{"a1": "unless-stopped", "a2": "always", "b1": "unless-stopped", "c1": "no"}
	running := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path[strings.Index(r.URL.Path, "/containers"):]
		switch {
		case path == "/containers/json":
			list := []map[string]any{}
			for id, app := range map[string]string{"a1": "photos", "a2": "photos", "b1": "other", "c1": "photos"} {
				state := "exited"
				if running[id] {
					state = "running"
				}
				list = append(list, map[string]any{"Id": id, "State": state, "Labels": map[string]string{"com.docker.compose.project": app}})
			}
			json.NewEncoder(w).Encode(list)
		case strings.HasSuffix(path, "/json"):
			id := strings.Split(path, "/")[2]
			json.NewEncoder(w).Encode(map[string]any{"HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": policy[id]}}})
		case strings.HasSuffix(path, "/update"):
			id := strings.Split(path, "/")[2]
			var body struct{ RestartPolicy struct{ Name string } }
			json.NewDecoder(r.Body).Decode(&body)
			if _, ok := policy[id]; !ok {
				w.WriteHeader(404)
				json.NewEncoder(w).Encode(map[string]string{"message": "no such container"})
				return
			}
			policy[id] = body.RestartPolicy.Name
			w.Write([]byte("{}"))
		case strings.HasSuffix(path, "/start"):
			running[strings.Split(path, "/")[2]] = true
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	m.api.http = &http.Client{Transport: lanTransport{base: http.DefaultTransport, target: target}}
	ctx := context.Background()

	held, err := m.HoldApps(ctx, []string{"photos"})
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held["a1"].Name != "unless-stopped" || held["a2"].Name != "always" {
		t.Fatalf("held: %+v", held)
	}
	if policy["a1"] != "no" || policy["a2"] != "no" || policy["b1"] != "unless-stopped" || policy["c1"] != "no" {
		t.Fatalf("policies: %+v", policy)
	}
	if err := m.StartApp(ctx, "photos"); err != nil {
		t.Fatal(err)
	}
	if !running["a1"] || !running["a2"] || running["b1"] {
		t.Fatalf("running: %+v", running)
	}
	held["gone"] = HeldPolicy{Name: "always"} // a removed app is skipped
	if err := m.RestorePolicies(ctx, held); err != nil {
		t.Fatal(err)
	}
	if policy["a1"] != "unless-stopped" || policy["a2"] != "always" {
		t.Fatalf("not given back: %+v", policy)
	}
}
