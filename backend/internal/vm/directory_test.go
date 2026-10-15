package vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectImageUsesChosenFolderAndKeepsImage(t *testing.T) {
	m, _ := newTestManager(t)
	dir := filepath.Join(m.mountRoot, "data/vms/Home Assistant")
	os.MkdirAll(dir, 0770)
	image := filepath.Join(dir, "ha.qcow2")
	os.WriteFile(image, []byte("original image"), 0660)
	spec := VM{Name: "home-assistant", DisplayName: "Home Assistant", Directory: "Home Assistant", Volume: "data", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Path: image, Format: "qcow2"}}}
	if err := m.Create(context.Background(), spec, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data/vms/home-assistant")); !os.IsNotExist(err) {
		t.Fatal("made second folder")
	}
	xml, err := m.XML(spec.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, dir+"/nvram.fd") || !strings.Contains(xml, image) {
		t.Fatal(xml)
	}
	again, _ := newTestManager(t)
	again.store = m.store
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if got := again.List([]string{"data"}); len(got) != 1 || got[0].Directory != "Home Assistant" {
		t.Fatal(got)
	}
	duplicate := spec
	duplicate.Name = "another"
	if err := m.Create(context.Background(), duplicate, []string{"data"}); err == nil {
		t.Fatal("shared VM folder")
	}
	if err := m.Delete(spec.Name, true, spec.Name); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(image)
	if string(data) != "original image" {
		t.Fatal("direct image removed")
	}
}
func TestVMFolderRejectsTraversalSymlinksAndUnrelatedData(t *testing.T) {
	m, _ := newTestManager(t)
	for _, bad := range []string{"../other", "/tmp/x", ".hidden", "space ", "a/b", "a\\b", "a\x00b"} {
		if validVMDirectory(bad) {
			t.Fatal(bad)
		}
	}
	dir := filepath.Join(m.mountRoot, "data/vms/shared")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "keep"), []byte("keep"), 0644)
	spec := VM{Name: "new", Directory: "shared", Volume: "data", Disks: []Disk{{Volume: "data", Format: "qcow2", SizeBytes: 1 << 30}}, Template: "linux", MemoryMiB: 512, VCPUs: 1}
	if err := m.Create(context.Background(), spec, []string{"data"}); !errors.Is(err, ErrBadFolder) {
		t.Fatal(err)
	}
	os.Symlink(dir, filepath.Join(m.mountRoot, "data/vms/link"))
	spec.Directory = "link"
	if err := m.Create(context.Background(), spec, []string{"data"}); !errors.Is(err, ErrBadFolder) {
		t.Fatal(err)
	}
}
