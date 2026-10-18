package main

import (
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/usb"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUSBFileAvailabilityAndUpload(t *testing.T) {
	dir := t.TempDir()
	root := files.Root{ID: "usb/ssd-abcd", Kind: "usb", Dir: dir, Writable: true}
	mounts := []usb.Mounted{{Mount: "ssd-abcd", Dir: dir}}
	m := files.NewManager(os.Getgid())
	m.Available = func(r files.Root) bool { return fileRootAvailable(r, []string{"data"}, mounts) }
	u, err := m.StartUpload(root, "", "hello.txt", 5, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.WriteChunk(u.ID, "alex", 0, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	mounts = nil
	if _, err = m.FinishUpload(u.ID, "alex"); !errors.Is(err, files.ErrVolumeChanged) {
		t.Fatalf("removed drive: %v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("published after removal")
	}
	mounts = []usb.Mounted{{Mount: "ssd-abcd", Dir: dir}}
	if _, err = m.FinishUpload(u.ID, "alex"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
	for _, r := range []files.Root{{ID: root.ID, Kind: "usb", Dir: t.TempDir()}, {ID: "usb/other", Kind: "usb", Dir: dir}, {ID: "stick", Kind: "stick", Dir: dir}, {ID: "volume/data", Kind: "volume", Volume: "missing"}} {
		if m.Available(r) {
			t.Fatalf("wrong target accepted: %+v", r)
		}
	}
	if !m.Available(files.Root{ID: "share/docs", Kind: "share", Volume: "data"}) {
		t.Fatal("share rejected")
	}
}
