package files

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// S-43 (owner 17.11.2026): an upload with "Ersetzen" moves the file it
// replaces into the recycle bin of the share, as a copy does. Without a bin
// it overwrites, as a copy does.
func uploadOver(t *testing.T, m *Manager, root Root, name, content string) (UploadStatus, error) {
	t.Helper()
	st, err := m.StartUpload(root, "", name, int64(len(content)), ConflictReplace, "alex", os.Getuid())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := m.WriteChunk(st.ID, "alex", 0, bytes.NewReader([]byte(content))); err != nil {
		t.Fatal(err)
	}
	return m.FinishUpload(st.ID, "alex")
}

func TestUploadReplaceMovesTheOldFileToTheBin(t *testing.T) {
	m, root, _ := newRoot(t)
	if _, err := uploadOver(t, m, root, "notiz.txt", "neu"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "notiz.txt")); string(data) != "neu" {
		t.Fatalf("new content: %q", data)
	}
	old, err := os.ReadFile(filepath.Join(root.Dir, RecycleDir, "alex", "notiz.txt"))
	if err != nil || string(old) != "notiz" {
		t.Fatalf("the replaced file is not in the bin: %q %v", old, err)
	}
	// It is in the bin like any deleted file: the view lists it.
	list, err := m.TrashFlat(context.Background(), root, "alex", false)
	if err != nil || len(list) != 1 {
		t.Fatalf("bin: %+v %v", list, err)
	}
}

func TestUploadReplaceWithoutABinOverwrites(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Recycle = false
	if _, err := uploadOver(t, m, root, "notiz.txt", "neu"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "notiz.txt")); string(data) != "neu" {
		t.Fatalf("new content: %q", data)
	}
	if _, err := os.Lstat(filepath.Join(root.Dir, RecycleDir)); !os.IsNotExist(err) {
		t.Fatalf("a bin appeared in a share without one: %v", err)
	}
}

// A folder that took the name after the upload began is never moved into
// the bin to make room for a file.
func TestUploadReplaceLeavesAFolderThatTookTheName(t *testing.T) {
	m, root, _ := newRoot(t)
	st, err := m.StartUpload(root, "", "neu.txt", 3, ConflictReplace, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	m.WriteChunk(st.ID, "alex", 0, bytes.NewReader([]byte("abc")))
	os.MkdirAll(filepath.Join(root.Dir, "neu.txt"), 0o755)
	os.WriteFile(filepath.Join(root.Dir, "neu.txt", "drin.txt"), []byte("drin"), 0o644)
	if _, err := m.FinishUpload(st.ID, "alex"); err == nil {
		t.Fatal("a file replaced a folder")
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "neu.txt", "drin.txt")); string(data) != "drin" {
		t.Fatalf("the folder changed: %q", data)
	}
	if _, err := os.Lstat(filepath.Join(root.Dir, RecycleDir)); !os.IsNotExist(err) {
		t.Fatalf("something went to the bin: %v", err)
	}
}

// A save over WebDAV still overwrites: programs save there again and
// again, and every save would fill the bin (SMB does not keep them either).
func TestWebDAVSaveDoesNotFillTheBin(t *testing.T) {
	m, root, _ := newRoot(t)
	dav := m.DAV(root, "alex", os.Getuid())
	f, err := dav.OpenFile(WithDAVLength(context.Background(), 3), "/notiz.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("neu"))
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "notiz.txt")); string(data) != "neu" {
		t.Fatalf("new content: %q", data)
	}
	if _, err := os.Lstat(filepath.Join(root.Dir, RecycleDir)); !os.IsNotExist(err) {
		t.Fatalf("a WebDAV save put something in the bin: %v", err)
	}
}
