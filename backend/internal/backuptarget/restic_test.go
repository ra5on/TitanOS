package backuptarget

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRealResticRoundTrip(t *testing.T) {
	binary := os.Getenv("TITANOS_TEST_RESTIC")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("restic")
		if err != nil {
			t.Skip("restic is not installed")
		}
	}
	m, root := newTarget(t)
	server := httptest.NewServer(m.Handler())
	defer server.Close()
	pairing, err := m.Pair("source")
	if err != nil {
		t.Fatal(err)
	}
	credentials := decode(t, pairing)
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("TitanOS backup restore integration test\n")
	if err := os.WriteFile(filepath.Join(source, "document.txt"), original, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "RESTIC_REPOSITORY=rest:"+server.URL+"/source", "RESTIC_PASSWORD=disposable-test-password", "RESTIC_REST_USERNAME="+credentials.User, "RESTIC_REST_PASSWORD="+credentials.Password, "RESTIC_CACHE_DIR="+filepath.Join(root, "cache"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("restic %v: %v\n%s", args, err, output)
		}
		return output
	}
	run("init")
	run("backup", "document.txt")
	run("check", "--read-data")
	restored := filepath.Join(root, "restored")
	run("restore", "latest", "--target", restored)
	data, err := os.ReadFile(filepath.Join(restored, "document.txt"))
	if err != nil || string(data) != string(original) {
		t.Fatalf("restored data mismatch: %q, %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(source, "document.txt"), []byte("second snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	run("backup", "document.txt")
	run("forget", "--keep-last", "1", "--prune")
	run("check", "--read-data")
}
