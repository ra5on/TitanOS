package plugins

import (
	"context"
	"encoding/json"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CI runs the actual downloaded-SDK path in a confined systemd service. No
// modules are loaded, no driver is installed on the CI host, and build has no
// network. An explicit environment path keeps normal unit tests lightweight.
func TestRealNASBuildEnvironment(t *testing.T) {
	sdk := os.Getenv("TITANOS_TEST_NVIDIA_SDK")
	if sdk == "" {
		t.Skip("exact-kernel SDK integration is run by the release workflow")
	}
	if os.Geteuid() != 0 {
		t.Fatal("SDK integration requires root for systemd confinement")
	}
	root := filepath.Join(t.TempDir(), "root")
	os.Mkdir(root, 0700)
	if output, err := exec.Command("tar", "--zstd", "-xf", sdk, "-C", root).CombinedOutput(); err != nil {
		t.Fatalf("extract SDK: %s %v", output, err)
	}
	os.Mkdir(filepath.Join(root, "work"), 0700)
	m := NewManager(config.Open(t.TempDir()))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	for _, action := range []string{"check", "build"} {
		if _, err := m.runBuildJob(ctx, root, action); err != nil {
			t.Fatal(action, err)
		}
	}
	target, _ := os.ReadFile(filepath.Join(root, "usr/lib/titanos/nvidia-build/target.json"))
	var expected struct{ Kernel, Version string }
	if err := json.Unmarshal(target, &expected); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(root, "work/result")
	manifest, err := readManifest(filepath.Join(result, "nvidia.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Kernel != expected.Kernel || manifest.TitanOS != expected.Version || !manifest.LocalBuild {
		t.Fatalf("wrong result %+v", manifest)
	}
	sum, size, err := hashFile(filepath.Join(result, "nvidia.raw"), maxImageBytes)
	if err != nil || sum != manifest.SHA256 || size != manifest.Size {
		t.Fatal("result checksum", err)
	}
	for _, name := range []string{"nvidia", "nvidia-uvm", "nvidia-modeset", "nvidia-drm"} {
		matches, _ := filepath.Glob(filepath.Join(root, "work/sources/usr/src/nvidia-*/kernel-open", name+".ko"))
		if len(matches) != 1 {
			t.Fatal("missing module", name)
		}
		magic, err := exec.Command("modinfo", "-F", "vermagic", matches[0]).Output()
		if err != nil || strings.Fields(string(magic))[0] != expected.Kernel {
			t.Fatal("module target", name, err)
		}
	}
	for _, name := range []string{"nvidia-kernel-sources.tar.gz", "nvidia-licenses.tar.gz"} {
		if st, err := os.Stat(filepath.Join(result, name)); err != nil || st.Size() == 0 {
			t.Fatal("missing source/license archive", name)
		}
	}
	t.Log("Authenticated upstream packages compiled offline against", expected.Kernel, "using the exact production sandbox")
}
