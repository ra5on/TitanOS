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
// N-102-22 (owner 17.11.2026): only the containers that ran when the NAS
// went down and have a restart policy start again. One stopped by hand
// (even with "always") and a one-shot init container stay stopped; their
// policy is held all the same, or the engine would start an "always" one
// by itself, out of order.
func TestHoldStartRestore(t *testing.T) {
	m, _, _ := newTestManager(t)
	var mu sync.Mutex
	policy := map[string]string{"a1": "unless-stopped", "a2": "always", "a3": "on-failure", "a4": "on-failure", "a5": "always", "b1": "unless-stopped", "c1": "no"}
	// a1, a4 run; a5 is in a restart loop; a2 was stopped by hand; a3 and
	// c1 are init containers that ran once and ended.
	running := map[string]bool{"a1": true, "a4": true, "b1": true}
	restarting := map[string]bool{"a5": true}
	apps := map[string]string{"a1": "photos", "a2": "photos", "a3": "photos", "a4": "photos", "a5": "photos", "b1": "other", "c1": "photos"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path[strings.Index(r.URL.Path, "/containers"):]
		switch {
		case path == "/containers/json":
			list := []map[string]any{}
			for id, app := range apps {
				state := "exited"
				if running[id] {
					state = "running"
				} else if restarting[id] {
					state = "restarting"
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
	want := map[string]HeldPolicy{"a1": {Name: "unless-stopped", Running: true}, "a2": {Name: "always"}, "a3": {Name: "on-failure"},
		"a4": {Name: "on-failure", Running: true}, "a5": {Name: "always", Running: true}}
	if len(held) != len(want) {
		t.Fatalf("held: %+v", held)
	}
	for id, p := range want {
		if held[id] != p {
			t.Errorf("held %s: %+v, want %+v", id, held[id], p)
		}
	}
	for id, p := range map[string]string{"a1": "no", "a2": "no", "a3": "no", "a4": "no", "a5": "no", "b1": "unless-stopped", "c1": "no"} {
		if policy[id] != p {
			t.Errorf("policy of %s while held: %s", id, policy[id])
		}
	}
	// The NAS went down; at boot nothing runs.
	mu.Lock()
	running, restarting = map[string]bool{}, map[string]bool{}
	mu.Unlock()
	if err := m.StartApp(ctx, "photos", held); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for id, want := range map[string]bool{"a1": true, "a4": true, "a5": true, "a2": false, "a3": false, "c1": false, "b1": false} {
		if running[id] != want {
			t.Errorf("%s running after the ordered start: %v", id, running[id])
		}
	}
	mu.Unlock()
	// Without a note of what ran (power cut: the engine started them by
	// itself) nothing more is started.
	mu.Lock()
	running = map[string]bool{}
	mu.Unlock()
	if err := m.StartApp(ctx, "photos", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(running) != 0 {
		t.Errorf("started without a note: %v", running)
	}
	mu.Unlock()
	held["gone"] = HeldPolicy{Name: "always"} // a removed app is skipped
	if err := m.RestorePolicies(ctx, held); err != nil {
		t.Fatal(err)
	}
	if policy["a1"] != "unless-stopped" || policy["a2"] != "always" || policy["a3"] != "on-failure" || policy["c1"] != "no" {
		t.Fatalf("not given back: %+v", policy)
	}
}
