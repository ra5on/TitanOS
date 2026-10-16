package files

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newRoot(t *testing.T) (*Manager, Root, string) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "share")
	os.MkdirAll(filepath.Join(dir, "urlaub"), 0o755)
	os.WriteFile(filepath.Join(dir, "urlaub/strand.jpg"), []byte("strand"), 0o644)
	os.WriteFile(filepath.Join(dir, "notiz.txt"), []byte("notiz"), 0o644)
	os.WriteFile(filepath.Join(base, "geheim.txt"), []byte("outside the share"), 0o644)
	return NewManager(os.Getgid()), Root{ID: "share/fotos", Name: "fotos", Kind: "share", Dir: dir, Writable: true, Recycle: true}, base
}

func names(entries []Entry) string {
	var list []string
	for _, e := range entries {
		list = append(list, e.Name)
	}
	return strings.Join(list, ",")
}

func TestListHidesInternals(t *testing.T) {
	m, root, _ := newRoot(t)
	os.MkdirAll(filepath.Join(root.Dir, ".recycle/alex"), 0o755)
	os.WriteFile(filepath.Join(root.Dir, ".film.mkv.abc.titanos-part"), nil, 0o600)
	entries, err := m.List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(entries); got != "urlaub,notiz.txt" {
		t.Errorf("listing = %s, want folders first and nothing internal", got)
	}
	if entries[1].Size != 5 || entries[0].Dir != true {
		t.Errorf("entries = %+v", entries)
	}
	if _, err := m.List(root, ".recycle"); !errors.Is(err, ErrInvalid) {
		t.Errorf("listing the recycle bin: %v", err)
	}
	if _, err := m.List(root, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing folder: %v", err)
	}
}

func TestNothingLeavesTheRoot(t *testing.T) {
	m, root, base := newRoot(t)
	// A symbolic link inside the share that points out of it.
	os.Symlink(base, filepath.Join(root.Dir, "raus"))
	os.Symlink(filepath.Join(base, "geheim.txt"), filepath.Join(root.Dir, "link.txt"))

	for _, p := range []string{"../geheim.txt", "urlaub/../../geheim.txt", "/etc/passwd", "raus/geheim.txt", "link.txt"} {
		if f, _, err := m.Open(root, p); err == nil {
			f.Close()
			t.Errorf("Open(%q) left the share", p)
		}
	}
	if _, err := m.List(root, "raus"); err == nil {
		t.Error("listing followed a link out of the share")
	}
	if err := m.Rename(root, "notiz.txt", "../notiz.txt"); err == nil {
		t.Error("rename moved a file out of the share")
	}
	if err := m.Rename(root, "notiz.txt", "raus/notiz.txt"); err == nil {
		t.Error("rename moved a file out of the share through a link")
	}
	if err := m.Mkdir(root, "raus/neu", os.Getuid()); err == nil {
		t.Error("mkdir created a folder outside the share")
	}
	if _, err := m.StartUpload(root, "raus", "x.txt", 1, "", "alex", os.Getuid()); err == nil {
		t.Error("an upload was started outside the share")
	}
	if err := m.Delete(root, []string{"raus/geheim.txt"}, "alex", true); err == nil {
		t.Error("delete reached outside the share")
	}
	if data, _ := os.ReadFile(filepath.Join(base, "geheim.txt")); string(data) != "outside the share" {
		t.Error("the file outside the share was changed")
	}
	var buf bytes.Buffer
	m.Zip(&buf, root, []string{"raus"})
	if strings.Contains(buf.String(), "outside the share") {
		t.Error("the zip contains a file from outside the share")
	}
}

func TestReadOnlyRoot(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Writable = false
	if err := m.Mkdir(root, "neu", 0); !errors.Is(err, ErrReadOnly) {
		t.Errorf("mkdir: %v", err)
	}
	if err := m.Rename(root, "notiz.txt", "x.txt"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("rename: %v", err)
	}
	if err := m.Delete(root, []string{"notiz.txt"}, "gast", false); !errors.Is(err, ErrReadOnly) {
		t.Errorf("delete: %v", err)
	}
	if _, err := m.StartUpload(root, "", "x.txt", 1, "", "gast", 0); !errors.Is(err, ErrReadOnly) {
		t.Errorf("upload: %v", err)
	}
	if f, _, err := m.Open(root, "notiz.txt"); err != nil {
		t.Errorf("reading must work: %v", err)
	} else {
		f.Close()
	}
}

func TestMkdirAndRename(t *testing.T) {
	m, root, _ := newRoot(t)
	if err := m.Mkdir(root, "urlaub/2026", os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(root.Dir, "urlaub/2026")); info == nil || info.Mode().Perm() != 0o770 || info.Mode()&os.ModeSetgid == 0 {
		t.Errorf("new folder mode: %v", info)
	}
	if err := m.Mkdir(root, "urlaub", os.Getuid()); !errors.Is(err, ErrExists) {
		t.Errorf("mkdir on an existing name: %v", err)
	}
	for _, bad := range []string{"", ".recycle", "x.titanos-part"} {
		if err := m.Mkdir(root, bad, os.Getuid()); !errors.Is(err, ErrInvalid) {
			t.Errorf("Mkdir(%q): %v", bad, err)
		}
	}

	if err := m.Rename(root, "notiz.txt", "urlaub/strand.jpg"); !errors.Is(err, ErrExists) {
		t.Errorf("rename onto an existing file: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "urlaub/strand.jpg")); string(data) != "strand" {
		t.Error("rename replaced an existing file")
	}
	if err := m.Rename(root, "urlaub", "urlaub/unter"); !errors.Is(err, ErrInvalid) {
		t.Errorf("folder into itself: %v", err)
	}
	if err := m.Rename(root, "notiz.txt", "urlaub/2026/notiz.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "urlaub/2026/notiz.txt")); err != nil {
		t.Error("moved file is not where it should be")
	}
}

func TestDelete(t *testing.T) {
	m, root, _ := newRoot(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }

	// With a recycle bin the file moves there, under the acting account.
	if err := m.Delete(root, []string{"urlaub/strand.jpg"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	binned := filepath.Join(root.Dir, ".recycle/alex/urlaub/strand.jpg")
	info, err := os.Stat(binned)
	if err != nil {
		t.Fatalf("file is not in the recycle bin: %v", err)
	}
	if !info.ModTime().Equal(now) {
		t.Errorf("time in the bin = %v, want the time of deletion", info.ModTime())
	}
	// The same name deleted again must not overwrite the first one.
	os.WriteFile(filepath.Join(root.Dir, "urlaub/strand.jpg"), []byte("zweite"), 0o644)
	if err := m.Delete(root, []string{"urlaub/strand.jpg"}, "alex", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(binned); string(data) != "strand" {
		t.Error("the first deleted file was overwritten in the bin")
	}
	if matches, _ := filepath.Glob(filepath.Join(root.Dir, ".recycle/alex/urlaub/strand (*).jpg")); len(matches) != 1 {
		t.Errorf("second deleted file not kept: %v", matches)
	}

	// Without a recycle bin, deleting needs the explicit confirmation.
	root.Recycle = false
	if err := m.Delete(root, []string{"notiz.txt"}, "alex", false); !errors.Is(err, ErrConfirm) {
		t.Errorf("delete without confirmation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "notiz.txt")); err != nil {
		t.Error("file deleted although not confirmed")
	}
	if err := m.Delete(root, []string{"notiz.txt", "urlaub"}, "alex", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root.Dir, "urlaub")); err == nil {
		t.Error("folder still there")
	}
	if err := m.Delete(root, []string{""}, "alex", true); !errors.Is(err, ErrInvalid) {
		t.Errorf("deleting the root itself: %v", err)
	}
}

func TestUpload(t *testing.T) {
	m, root, _ := newRoot(t)
	uid := os.Getuid()

	status, err := m.StartUpload(root, "urlaub", "film.mkv", 10, "", "alex", uid)
	if err != nil {
		t.Fatal(err)
	}
	// Until it is complete the file does not show up anywhere.
	if entries, _ := m.List(root, "urlaub"); names(entries) != "strand.jpg" {
		t.Errorf("unfinished upload is visible: %s", names(entries))
	}
	if _, err := m.WriteChunk(status.ID, "eve", 0, strings.NewReader("x")); !errors.Is(err, ErrNoUpload) {
		t.Errorf("another account writing to the upload: %v", err)
	}
	if st, err := m.WriteChunk(status.ID, "alex", 0, strings.NewReader("01234")); err != nil || st.Offset != 5 {
		t.Fatalf("first chunk: %+v %v", st, err)
	}
	// A repeated or skipped chunk is refused; the client asks where to go on.
	if _, err := m.WriteChunk(status.ID, "alex", 0, strings.NewReader("01234")); !errors.Is(err, ErrMismatch) {
		t.Errorf("repeated chunk: %v", err)
	}
	if st, _ := m.UploadStatus(status.ID, "alex"); st.Offset != 5 {
		t.Errorf("status offset = %d", st.Offset)
	}
	if _, err := m.FinishUpload(status.ID, "alex"); !errors.Is(err, ErrIncomplete) {
		t.Errorf("finishing half an upload: %v", err)
	}
	// More than announced is cut off.
	if st, err := m.WriteChunk(status.ID, "alex", 5, strings.NewReader("56789-too-much")); err != nil || st.Offset != 10 {
		t.Fatalf("second chunk: %+v %v", st, err)
	}
	if _, err := m.FinishUpload(status.ID, "alex"); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(root.Dir, "urlaub/film.mkv")
	if data, _ := os.ReadFile(final); string(data) != "0123456789" {
		t.Errorf("content = %q", data)
	}
	if info, _ := os.Stat(final); info.Mode().Perm() != 0o660 {
		t.Errorf("mode = %v", info.Mode())
	}
	if matches, _ := filepath.Glob(filepath.Join(root.Dir, "urlaub/.*titanos-part")); len(matches) != 0 {
		t.Errorf("part file left behind: %v", matches)
	}
	if _, err := m.UploadStatus(status.ID, "alex"); !errors.Is(err, ErrNoUpload) {
		t.Errorf("finished upload still known: %v", err)
	}
}

func TestUploadConflicts(t *testing.T) {
	m, root, _ := newRoot(t)
	uid := os.Getuid()
	upload := func(conflict string) (UploadStatus, error) {
		st, err := m.StartUpload(root, "", "notiz.txt", 3, conflict, "alex", uid)
		if err != nil {
			return st, err
		}
		m.WriteChunk(st.ID, "alex", 0, strings.NewReader("neu"))
		return m.FinishUpload(st.ID, "alex")
	}

	if _, err := upload(""); !errors.Is(err, ErrExists) {
		t.Errorf("existing file without a decision: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "notiz.txt")); string(data) != "notiz" {
		t.Error("existing file was touched")
	}
	st, err := upload(ConflictRename)
	if err != nil || st.Name != "notiz (2).txt" {
		t.Fatalf("keep both: %+v %v", st, err)
	}
	if st, _ := upload(ConflictRename); st.Name != "notiz (3).txt" {
		t.Errorf("third copy named %q", st.Name)
	}
	if _, err := upload(ConflictReplace); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root.Dir, "notiz.txt")); string(data) != "neu" {
		t.Errorf("replace: content = %q", data)
	}
	// A folder is never replaced by a file.
	if _, err := m.StartUpload(root, "", "urlaub", 3, ConflictReplace, "alex", uid); !errors.Is(err, ErrExists) {
		t.Errorf("replacing a folder: %v", err)
	}
	for _, bad := range []string{"", "a/b.txt", "..", ".recycle"} {
		if _, err := m.StartUpload(root, "", bad, 1, "", "alex", uid); !errors.Is(err, ErrInvalid) {
			t.Errorf("StartUpload(name=%q): %v", bad, err)
		}
	}
	if _, err := m.StartUpload(root, "", "riesig.img", 1<<60, "", "alex", uid); !errors.Is(err, ErrNoSpace) {
		t.Errorf("upload larger than the free space: %v", err)
	}
}

func TestAbortAndExpiry(t *testing.T) {
	m, root, _ := newRoot(t)
	now := time.Now()
	m.now = func() time.Time { return now }

	st, _ := m.StartUpload(root, "", "a.bin", 4, "", "alex", os.Getuid())
	m.WriteChunk(st.ID, "alex", 0, strings.NewReader("ab"))
	if err := m.AbortUpload(st.ID, "alex"); err != nil {
		t.Fatal(err)
	}
	if matches, _ := filepath.Glob(filepath.Join(root.Dir, ".*titanos-part")); len(matches) != 0 {
		t.Errorf("part file of an aborted upload left behind: %v", matches)
	}

	old, _ := m.StartUpload(root, "", "b.bin", 4, "", "alex", os.Getuid())
	now = now.Add(uploadIdle + time.Hour)
	m.StartUpload(root, "", "c.bin", 4, "", "alex", os.Getuid()) // starting another one tidies up
	if _, err := m.UploadStatus(old.ID, "alex"); !errors.Is(err, ErrNoUpload) {
		t.Errorf("abandoned upload still known: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(root.Dir, ".b.bin.*titanos-part")); len(matches) != 0 {
		t.Errorf("part file of an abandoned upload left behind: %v", matches)
	}
}

func TestZip(t *testing.T) {
	m, root, _ := newRoot(t)
	os.MkdirAll(filepath.Join(root.Dir, ".recycle/alex"), 0o755)
	os.WriteFile(filepath.Join(root.Dir, ".recycle/alex/alt.txt"), []byte("deleted"), 0o644)
	os.WriteFile(filepath.Join(root.Dir, "urlaub/.x.abc.titanos-part"), []byte("half"), 0o600)

	var buf bytes.Buffer
	if err := m.Zip(&buf, root, []string{"urlaub", "notiz.txt"}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	for _, f := range archive.File {
		list = append(list, f.Name)
	}
	if got := strings.Join(list, ","); got != "urlaub/,urlaub/strand.jpg,notiz.txt" {
		t.Errorf("zip holds %s", got)
	}
	if err := m.Zip(&bytes.Buffer{}, root, []string{".recycle"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("zipping the recycle bin: %v", err)
	}
}

func TestVMManagedFilesCannotBeChangedOrReplacedByUploads(t *testing.T) {
	m, root, _ := newRoot(t)
	protected := filepath.Join(root.Dir, "urlaub")
	m.Protected = func(p string) bool { return p == protected || strings.HasPrefix(p, protected+"/") }
	for _, change := range []func() error{
		func() error { return m.Delete(root, []string{"urlaub"}, "alex", true) },
		func() error { return m.Rename(root, "urlaub", "moved") },
		func() error { return m.Rename(root, "notiz.txt", "urlaub/other") },
		func() error { return m.Mkdir(root, "urlaub/child", os.Getuid()) },
		func() error {
			_, e := m.StartUpload(root, "urlaub", "strand.jpg", 0, ConflictReplace, "alex", os.Getuid())
			return e
		},
	} {
		if e := change(); !errors.Is(e, ErrProtected) {
			t.Fatalf("managed file mutation accepted: %v", e)
		}
	}
	// Ownership can change while an upload is in flight: check again at commit.
	m.Protected = nil
	part, e := m.StartUpload(root, "urlaub", "strand.jpg", 0, ConflictReplace, "alex", os.Getuid())
	if e != nil {
		t.Fatal(e)
	}
	m.Protected = func(p string) bool { return p == filepath.Join(protected, "strand.jpg") }
	if _, e = m.FinishUpload(part.ID, "alex"); !errors.Is(e, ErrProtected) {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(protected, "strand.jpg"))
	if string(data) != "strand" {
		t.Fatal("managed image replaced")
	}
	if e = m.AbortUpload(part.ID, "alex"); e != nil {
		t.Fatal(e)
	}
	if e = m.Mkdir(root, "unrelated", os.Getuid()); e != nil {
		t.Fatal(e)
	}
}
