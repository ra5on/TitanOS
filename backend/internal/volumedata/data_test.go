package volumedata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateAtomicState(t *testing.T) {
	base := t.TempDir()
	os.Mkdir(filepath.Join(base, "data"), 0700)
	root, err := Open(base, "data", "maintenance", []string{"data"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	want := map[string]int{"runs": 3}
	if err := WriteJSON(root, "state.json", want); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := ReadJSON(root, "state.json", &got, 1024); err != nil || got["runs"] != 3 {
		t.Fatalf("%v %v", got, err)
	}
	if err := ReadJSON(root, "state.json", &got, 2); err == nil {
		t.Fatal("oversized state accepted")
	}
	root.WriteFile("bad.json", []byte(`{} {}`), 0600)
	if err := ReadJSON(root, "bad.json", &got, 1024); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := Open(base, "data", "monitoring", nil); err == nil {
		t.Fatal("unmounted root accepted")
	}
}
func TestPrivateRootRejectsLinksAndWritableDirs(t *testing.T) {
	for _, kind := range []string{"mount-link", "private-link", "writable"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			point := filepath.Join(base, "data")
			if kind == "mount-link" {
				os.Symlink(t.TempDir(), point)
			} else {
				os.Mkdir(point, 0700)
				if kind == "private-link" {
					os.Symlink(t.TempDir(), filepath.Join(point, ".titanos"))
				} else {
					os.Mkdir(filepath.Join(point, ".titanos"), 0700)
					os.Chmod(filepath.Join(point, ".titanos"), 0777)
				}
			}
			if root, err := Open(base, "data", "monitoring", []string{"data"}); err == nil {
				root.Close()
				t.Fatal("unsafe root accepted")
			}
		})
	}
}

func TestMountIdentityUsesPinnedDescriptors(t *testing.T) {
	first, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := os.OpenRoot("/proc")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := SameMount(first, first); err != nil {
		t.Fatal(err)
	}
	if err := SameMount(first, second); err == nil {
		t.Fatal("different kernel mounts accepted")
	}
}
