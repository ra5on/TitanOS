package docker

import (
	"context"
	"os"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// The fake engines of the other tests answer only the paths they expect.
func TestMain(m *testing.M) {
	askEngineVersion = false
	os.Exit(m.Run())
}

func TestNegotiate(t *testing.T) {
	for _, c := range []struct{ min, max, want string }{
		{"1.24", "1.45", "1.43"}, // Docker 26/27: keeps the version the code is written for
		{"1.44", "1.51", "1.44"}, // Docker 29 refuses 1.43
		{"", "1.41", "1.41"},     // old engine without the newer version
		{"", "", "1.43"},
		{"1.9", "1.100", "1.43"}, // numeric, not string comparison
	} {
		if got := negotiate(c.min, c.max); got != c.want {
			t.Errorf("negotiate(%q, %q) = %q, want %q", c.min, c.max, got, c.want)
		}
	}
}

// The client asks /version once and then uses the negotiated prefix; until
// the engine answers it keeps asking with 1.43 as fallback.
func TestClientUsesNegotiatedVersion(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var up atomic.Bool
	var asked atomic.Int32
	var lastPath atomic.Value
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			asked.Add(1)
			if !up.Load() {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`{"Version":"29.0.0","ApiVersion":"1.52","MinAPIVersion":"1.44"}`))
			return
		}
		lastPath.Store(r.URL.Path)
		w.Write([]byte(`[]`))
	})}
	go srv.Serve(l)
	defer srv.Close()

	askEngineVersion = true
	defer func() { askEngineVersion = false }()
	c := newClient(sock)
	var out []any
	if err := c.getJSON(context.Background(), "/containers/json", nil, &out); err != nil {
		t.Fatal(err)
	}
	if lastPath.Load() != "/v1.43/containers/json" {
		t.Fatalf("before the engine answered: %v", lastPath.Load())
	}
	up.Store(true)
	for i := 0; i < 2; i++ {
		if err := c.getJSON(context.Background(), "/containers/json", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	if lastPath.Load() != "/v1.44/containers/json" {
		t.Fatalf("after negotiation: %v", lastPath.Load())
	}
	if asked.Load() != 2 {
		t.Fatalf("asked %d times, want 2 (once failing, once answered)", asked.Load())
	}
}
