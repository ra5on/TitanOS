package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sumOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// The photo backup (PLAN 116): the server works out the SHA-256 itself,
// while the bytes are written, and publishes nothing with another sum.
func TestFinishUploadHashed(t *testing.T) {
	m, root, _ := newRoot(t)
	os.Mkdir(filepath.Join(root.Dir, "2026"), 0700)
	status, err := m.StartUpload(root, "2026", "IMG_1.jpg", 10, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"01234", "56789"} {
		if status, err = m.WriteChunk(status.ID, "alex", status.Offset, strings.NewReader(part)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.FinishUploadHashed(status.ID, "alex", sumOf("other")); !errors.Is(err, ErrUploadSum) {
		t.Fatalf("another sum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "2026", "IMG_1.jpg")); !os.IsNotExist(err) {
		t.Fatal("published with the wrong sum")
	}
	done, err := m.FinishUploadHashed(status.ID, "alex", sumOf("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	if done.SHA256 != sumOf("0123456789") || done.Path != "2026/IMG_1.jpg" || done.Name != "IMG_1.jpg" {
		t.Fatalf("finished: %+v", done)
	}
}

// A write that failed or an upload taken up after a restart has no running
// sum any more: the hashed finish reads the file again instead.
func TestFinishUploadHashedReadsAgain(t *testing.T) {
	m, root, _ := newRoot(t)
	status, err := m.StartUpload(root, "", "VID_1.mp4", 6, ConflictRename, "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	syncFile := m.syncFile
	m.syncFile = func(*os.File) error { return errors.New("disk failed") }
	if _, err = m.WriteChunk(status.ID, "alex", 0, strings.NewReader("abc")); err == nil {
		t.Fatal("ignored fsync failure")
	}
	m.syncFile = syncFile
	if _, err = m.WriteChunk(status.ID, "alex", 0, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	// Taken up by a new titand: the sum in RAM is gone.
	fresh := NewManager(os.Getgid())
	if list, err := fresh.Uploads(root, "", "alex", os.Getuid()); err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if _, err = fresh.WriteChunk(status.ID, "alex", 3, strings.NewReader("def")); err != nil {
		t.Fatal(err)
	}
	// The plain finish does not read again and has no sum to tell.
	done, err := fresh.FinishUploadHashed(status.ID, "alex", "")
	if err != nil || done.SHA256 != sumOf("abcdef") || done.Path != "VID_1.mp4" {
		t.Fatalf("finished: %+v %v", done, err)
	}

	// The first manager wrote with a failed sync: its sum was dropped.
	status, _ = m.StartUpload(root, "", "b.txt", 2, ConflictRename, "alex", os.Getuid())
	m.syncFile = func(*os.File) error { return errors.New("disk failed") }
	m.WriteChunk(status.ID, "alex", 0, strings.NewReader("xy"))
	m.syncFile = syncFile
	m.WriteChunk(status.ID, "alex", 0, strings.NewReader("xy"))
	plain, err := m.FinishUpload(status.ID, "alex")
	if err != nil || plain.SHA256 != "" {
		t.Fatalf("plain finish after a failed write: %+v %v", plain, err)
	}
}
