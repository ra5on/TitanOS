package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadSyncFailureNeverAcknowledged(t *testing.T) {
	m, root, _ := newRoot(t)
	status, err := m.StartUpload(root, "", "movie", 6, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	syncFile := m.syncFile
	m.syncFile = func(*os.File) error { return errors.New("disk failed") }
	if _, err = m.WriteChunk(status.ID, "alex", 0, strings.NewReader("abcdef")); err == nil {
		t.Fatal("ignored fsync failure")
	}
	status, _ = m.UploadStatus(status.ID, "alex")
	if status.Offset != 0 {
		t.Fatal("acknowledged unsynced data")
	}
	m.syncFile = syncFile
	if _, err = m.WriteChunk(status.ID, "alex", 0, strings.NewReader("abcdef")); err != nil {
		t.Fatal(err)
	}
	m.syncFile = func(*os.File) error { return errors.New("disk failed") }
	if _, err = m.FinishUpload(status.ID, "alex"); err == nil {
		t.Fatal("ignored final sync failure")
	}
	if _, err = os.Stat(filepath.Join(root.Dir, "movie")); !os.IsNotExist(err) {
		t.Fatal("published incomplete file")
	}
}
func TestUploadRecoveryAndPrefixValidation(t *testing.T) {
	m, root, _ := newRoot(t)
	status, err := m.StartUpload(root, "urlaub", "movie", 6, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.WriteChunk(status.ID, "alex", 0, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	fresh := NewManager(os.Getgid())
	if list, err := fresh.Uploads(root, "urlaub", "other", os.Getuid()); err != nil || len(list) != 0 {
		t.Fatal("other user's upload", list, err)
	}
	list, err := fresh.Uploads(root, "urlaub", "alex", os.Getuid())
	if err != nil || len(list) != 1 || list[0].Offset != 3 {
		t.Fatal(list, err)
	}
	if err = fresh.VerifyChunk(status.ID, "alex", 0, strings.NewReader("bad")); !errors.Is(err, ErrMismatch) {
		t.Fatal("accepted different source", err)
	}
	if err = fresh.VerifyChunk(status.ID, "alex", 0, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.WriteChunk(status.ID, "alex", 3, strings.NewReader("def")); err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.FinishUpload(status.ID, "alex"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root.Dir, "urlaub/movie"))
	if string(data) != "abcdef" {
		t.Fatal(string(data))
	}
	if list, err = fresh.Uploads(root, "urlaub", "alex", os.Getuid()); err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}
func TestUploadVolumeReplacementAndPartialTampering(t *testing.T) {
	for _, kind := range []string{"missing", "replacement", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			m, root, _ := newRoot(t)
			status, err := m.StartUpload(root, "", "movie", 6, "", "alex", os.Getuid())
			if err != nil {
				t.Fatal(err)
			}
			part := m.uploads[status.ID].part
			switch kind {
			case "missing":
				m.Available = func(Root) bool { return false }
			case "replacement":
				os.Rename(root.Dir, root.Dir+"-old")
				os.Mkdir(root.Dir, 0755)
			case "symlink":
				os.Remove(filepath.Join(root.Dir, part))
				os.Symlink("notiz.txt", filepath.Join(root.Dir, part))
			case "hardlink":
				os.Remove(filepath.Join(root.Dir, part))
				os.Link(filepath.Join(root.Dir, "notiz.txt"), filepath.Join(root.Dir, part))
			}
			if _, err = m.WriteChunk(status.ID, "alex", 0, strings.NewReader("abcdef")); err == nil {
				t.Fatal("wrote replaced target")
			}
			if kind == "replacement" {
				entries, _ := os.ReadDir(root.Dir)
				if len(entries) != 0 {
					t.Fatal("wrote fallback filesystem")
				}
			}
		})
	}
}
func TestConcurrentUploadSpaceReservationsAndCancellation(t *testing.T) {
	m, root, _ := newRoot(t)
	_, free := Usage(root)
	_, err := m.StartUpload(root, "", "large", int64(free/2), "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.StartUpload(root, "", "another", int64(free/2+free/4), "", "alex", os.Getuid()); !errors.Is(err, ErrNoSpace) {
		t.Fatal("overcommitted volume", err)
	}
	m.Acquire = func(ctx context.Context) (int64, func(), error) { <-ctx.Done(); return 0, nil, ctx.Err() }
	status, err := m.StartUpload(root, "", "small", 5, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = m.WriteChunkContext(ctx, status.ID, "alex", 0, strings.NewReader("hello")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err = m.AbortUpload(status.ID, "alex"); err != nil {
		t.Fatal(err)
	}
}
func TestFinishDoesNotOverwriteNewlyCreatedTarget(t *testing.T) {
	m, root, _ := newRoot(t)
	status, err := m.StartUpload(root, "", "movie", 0, "", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root.Dir, "movie"), []byte("keep"), 0600)
	if _, err = m.FinishUpload(status.ID, "alex"); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root.Dir, "movie"))
	if string(data) != "keep" {
		t.Fatal("overwritten")
	}
	if err = m.AbortUpload(status.ID, "alex"); err != nil {
		t.Fatal(err)
	}
}
