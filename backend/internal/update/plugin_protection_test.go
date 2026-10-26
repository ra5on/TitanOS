package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestPluginProtectionReadsRealBaseOfRepackedFallbackSlot(t *testing.T) {
	if _, err := exec.LookPath("mksquashfs"); err != nil {
		t.Skip("squashfs-tools required")
	}
	root := t.TempDir()
	m := NewManager(config.Open(filepath.Join(root, "config")), "0.23.0", "a")
	m.bootDir = filepath.Join(root, "efi")
	m.keyDir = filepath.Join(root, "keys")
	m.Base = "0.23.0"
	for _, slot := range []struct{ name, version, base string }{{"a", "0.23.0", "0.23.0"}, {"b", "0.22.3", "0.21.0"}} {
		tree := filepath.Join(root, "source-"+slot.name)
		os.MkdirAll(filepath.Join(tree, "usr/share/titanos"), 0755)
		os.WriteFile(filepath.Join(tree, "usr/share/titanos/base-version"), []byte(slot.base+"\n"), 0644)
		dir := filepath.Join(m.bootDir, "titanos", slot.name)
		os.MkdirAll(dir, 0755)
		for _, file := range []string{"vmlinuz", "initrd.img"} {
			os.WriteFile(filepath.Join(dir, file), []byte("fixture"), 0644)
		}
		os.WriteFile(filepath.Join(dir, "version"), []byte(slot.version), 0644)
		cmd := exec.Command("mksquashfs", tree, filepath.Join(dir, "filesystem.squashfs"), "-processors", "1", "-noappend", "-no-xattrs", "-no-progress", "-quiet")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		entries := filepath.Join(m.bootDir, "loader/entries")
		os.MkdirAll(entries, 0755)
		os.WriteFile(filepath.Join(entries, "titanos-"+slot.name+".conf"), []byte("title TitanOS\nversion "+slot.version+"\nlinux /titanos/"+slot.name+"/vmlinuz\ninitrd /titanos/"+slot.name+"/initrd.img\noptions test\n"), 0644)
	}
	versions, err := m.PluginReleaseProtection()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(versions, "0.21.0") || !slices.Contains(versions, "0.22.3") {
		t.Fatal(versions)
	}
	os.WriteFile(filepath.Join(m.bootDir, "titanos/b/filesystem.squashfs"), []byte("broken"), 0644)
	if _, err = m.PluginReleaseProtection(); err == nil {
		t.Fatal("unknown fallback base allowed cleanup")
	}
}
