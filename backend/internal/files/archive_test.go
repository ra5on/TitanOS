package files

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func makeArchive(t *testing.T, root Root, entries map[string]string) {
	t.Helper()
	f, e := os.Create(filepath.Join(root.Dir, "Backup.zip"))
	if e != nil {
		t.Fatal(e)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, e := zw.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(data))
	}
	if e = zw.Close(); e != nil {
		t.Fatal(e)
	}
	f.Close()
}
func TestArchiveSelectedExtractionAndConflict(t *testing.T) {
	m, src, dst := twoRoots(t)
	makeArchive(t, src, map[string]string{"Fotos/a.txt": "a", "Fotos/b.txt": "b", "Leer/": ""})
	listing, err := m.ArchiveList(src, "Backup.zip")
	if err != nil || len(listing.Entries) != 3 {
		t.Fatalf("list: %+v %v", listing, err)
	}
	job, err := m.StartExtract(src, "Backup.zip", listing.Version, []string{"Fotos/a.txt", "Leer"}, dst, "ziel", "Entpackt", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobDone || job.Done != 1 {
		t.Fatalf("extract: %+v", job)
	}
	if read(t, filepath.Join(dst.Dir, "ziel/Entpackt/Fotos/a.txt")) != "a" {
		t.Fatal("extracted content")
	}
	if _, err = os.Stat(filepath.Join(dst.Dir, "ziel/Entpackt/Fotos/b.txt")); !os.IsNotExist(err) {
		t.Fatal("unselected file extracted")
	}
	if _, err = m.StartExtract(src, "Backup.zip", listing.Version, []string{"Fotos/a.txt"}, dst, "ziel", "Entpackt", "alex", os.Getuid()); err != ErrExists {
		t.Fatalf("existing target: %v", err)
	}
}
func TestArchiveRejectsTraversalAndParentFile(t *testing.T) {
	m, root, _ := newRoot(t)
	for _, entries := range []map[string]string{{"../outside": "bad"}, {"/absolute": "bad"}, {"C:\\file": "bad"}, {".recycle/secret": "bad"}, {"parent": "file", "parent/child": "bad"}} {
		makeArchive(t, root, entries)
		if _, err := m.ArchiveList(root, "Backup.zip"); err == nil {
			t.Fatalf("unsafe archive accepted: %v", entries)
		}
	}
}
func TestArchiveFailedExtractionLeavesNoVisibleOutput(t *testing.T) {
	m, src, dst := twoRoots(t)
	makeArchive(t, src, map[string]string{"one.txt": "one"})
	listing, err := m.ArchiveList(src, "Backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	m.syncFile = func(*os.File) error { return ErrNoSpace }
	job, err := m.StartExtract(src, "Backup.zip", listing.Version, []string{"one.txt"}, dst, "ziel", "Unfinished", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	job = finish(t, m, job.ID)
	if job.State != JobFailed || job.Retryable {
		t.Fatalf("failed extraction: %+v", job)
	}
	if _, err = os.Stat(filepath.Join(dst.Dir, "ziel/Unfinished")); !os.IsNotExist(err) {
		t.Fatal("published incomplete archive")
	}
}

func TestArchiveCRCFailureNeverPublishesFolder(t *testing.T) {
	m, src, dst := twoRoots(t)
	archive := filepath.Join(src.Dir, "Backup.zip")
	f, e := os.Create(archive)
	if e != nil {
		t.Fatal(e)
	}
	zw := zip.NewWriter(f)
	w, e := zw.CreateHeader(&zip.FileHeader{Name: "data.txt", Method: zip.Store})
	if e != nil {
		t.Fatal(e)
	}
	w.Write([]byte("original"))
	zw.Close()
	f.Close()
	zr, e := zip.OpenReader(archive)
	if e != nil {
		t.Fatal(e)
	}
	offset, e := zr.File[0].DataOffset()
	zr.Close()
	if e != nil {
		t.Fatal(e)
	}
	f, e = os.OpenFile(archive, os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteAt([]byte("X"), offset)
	f.Close()
	listing, e := m.ArchiveList(src, "Backup.zip")
	if e != nil {
		t.Fatal(e)
	}
	job, e := m.StartExtract(src, "Backup.zip", listing.Version, []string{"data.txt"}, dst, "ziel", "CRC", "alex", os.Getuid())
	if e != nil {
		t.Fatal(e)
	}
	job = finish(t, m, job.ID)
	if job.State != JobFailed {
		t.Fatalf("CRC corruption accepted: %+v", job)
	}
	if _, e = os.Stat(filepath.Join(dst.Dir, "ziel/CRC")); !os.IsNotExist(e) {
		t.Fatal("corrupt archive published")
	}
}
