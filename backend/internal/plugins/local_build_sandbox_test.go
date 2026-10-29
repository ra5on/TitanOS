package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// logRunner simulates PID 1 appending the build output, then a failed build.
type logRunner struct{ log string }

func (r *logRunner) Run(_ context.Context, _, name string, args ...string) (string, error) {
	for _, arg := range args {
		if path, ok := strings.CutPrefix(arg, "--property=StandardOutput=append:"); ok {
			if path != r.log {
				return "", errors.New("unexpected log path " + path)
			}
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return "", err
			}
			file.WriteString("compiler failed\n")
			file.Close()
		}
	}
	return "", errors.New("exit status 1")
}

func sandboxRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(root, "work/result"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBuildLogIgnoresPlantedSymlink(t *testing.T) {
	root := sandboxRoot(t)
	secret := filepath.Join(t.TempDir(), "sessions.json")
	os.WriteFile(secret, []byte("SECRET-TOKEN"), 0600)
	args := strings.Join(buildCommand(root, "build", "1.0.0"), "\n")
	if strings.Contains(args, filepath.Join(root, "work")) || !strings.Contains(args, "append:"+filepath.Join(filepath.Dir(root), "build.log")) {
		t.Fatal("build log is inside the sandbox-writable tree:", args)
	}
	// Both the old in-sandbox location and a stale host path point at a secret.
	os.Symlink(secret, filepath.Join(root, "work/build.log"))
	os.Symlink(secret, buildLogPath(root))
	m, _ := newTestManager(t)
	m.runner = &logRunner{log: buildLogPath(root)}
	_, err := m.runBuildJob(context.Background(), root, "build")
	if err == nil || !strings.Contains(err.Error(), "compiler failed") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unexpected error %v", err)
	}
	if data, _ := os.ReadFile(secret); string(data) != "SECRET-TOKEN" {
		t.Fatal("log append followed a symlink:", string(data))
	}
	if st, err := os.Lstat(buildLogPath(root)); err != nil || !st.Mode().IsRegular() {
		t.Fatal("log not recreated as regular file", err)
	}
	// A log replaced by a link after the run is never read.
	os.Remove(buildLogPath(root))
	os.Symlink(secret, buildLogPath(root))
	if tail := buildLogTail(buildLogPath(root)); tail != "" {
		t.Fatal("log tail followed a symlink")
	}
}

func TestSandboxOutputsRejectLinksAndSpecialFiles(t *testing.T) {
	root := sandboxRoot(t)
	work := filepath.Join(root, "work")
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte(`{"kernel":"x"}`), 0600)
	os.WriteFile(filepath.Join(work, "result/nvidia.raw"), []byte("image"), 0600)
	if sum, size, err := hashSandboxFile(root, "result/nvidia.raw", 100); err != nil || size != 5 || len(sum) != 64 {
		t.Fatal("regular output rejected", err)
	}
	if err := checkSandboxTree(filepath.Join(work, "result")); err != nil {
		t.Fatal(err)
	}
	if _, err := readSandboxFile(root, "result/nvidia.raw", 4); err == nil {
		t.Fatal("size limit ignored")
	}
	os.Symlink(secret, filepath.Join(work, "source-status.json"))
	if _, err := readSandboxFile(root, "source-status.json", 1<<20); err == nil {
		t.Fatal("symlinked source status accepted")
	}
	os.Symlink(secret, filepath.Join(work, "result/nvidia.plugin.json"))
	if _, err := readSandboxFile(root, "result/nvidia.plugin.json", 1<<20); err == nil {
		t.Fatal("symlinked manifest accepted")
	}
	if err := checkSandboxTree(filepath.Join(work, "result")); err == nil {
		t.Fatal("result tree with symlink accepted")
	}
	os.Remove(filepath.Join(work, "result/nvidia.plugin.json"))
	os.Link(filepath.Join(work, "result/nvidia.raw"), filepath.Join(work, "hardlink"))
	if _, _, err := hashSandboxFile(root, "result/nvidia.raw", 100); err == nil {
		t.Fatal("hard-linked image accepted")
	}
	if err := checkSandboxTree(filepath.Join(work, "result")); err == nil {
		t.Fatal("hard-linked result accepted")
	}
	if err := syscall.Mkfifo(filepath.Join(work, "packages.lock.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := hashSandboxFile(root, "packages.lock.json", 1<<20); err == nil {
		t.Fatal("FIFO accepted")
	}
	// A redirected result directory is not traversed.
	os.RemoveAll(filepath.Join(work, "result"))
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "nvidia.raw"), []byte("image"), 0600)
	os.Symlink(other, filepath.Join(work, "result"))
	if _, _, err := hashSandboxFile(root, "result/nvidia.raw", 100); err == nil {
		t.Fatal("symlinked result directory accepted")
	}
	if err := checkSandboxTree(filepath.Join(work, "result")); err == nil {
		t.Fatal("symlinked result tree accepted")
	}
	// titand's own receipt writes never follow a planted link.
	os.WriteFile(secret, []byte("keep"), 0600)
	link := filepath.Join(work, "receipt")
	os.Symlink(secret, link)
	if err := writeFresh(link, []byte("receipt")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(secret); string(data) != "keep" {
		t.Fatal("receipt write followed a symlink")
	}
}
