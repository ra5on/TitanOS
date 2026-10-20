package files

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUsageAggregatesWithoutFollowingLinksOrSecrets(t *testing.T) {
	m, root, _ := newRoot(t)
	os.MkdirAll(filepath.Join(root.Dir, "urlaub/empty"), 0700)
	os.WriteFile(filepath.Join(root.Dir, "urlaub/.hidden"), []byte("abc"), 0600)
	os.Symlink("/etc", filepath.Join(root.Dir, "outside"))
	result, err := m.DirectoryUsage(context.Background(), root, "", false)
	if err != nil || result.Bytes != 11 || result.Files != 2 || result.Folders != 2 || len(result.Children) != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	if result.Children[0].Path != "urlaub" || result.Children[0].Bytes != 6 || result.Largest[0].Path != "urlaub/strand.jpg" {
		t.Fatal(result)
	}
	result, err = m.DirectoryUsage(context.Background(), root, "urlaub", true)
	if err != nil || result.Bytes != 9 || result.Files != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	root.Hide = []string{"urlaub"}
	result, err = m.DirectoryUsage(context.Background(), root, "", true)
	if err != nil || result.Bytes != 5 {
		t.Fatalf("secret: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.DirectoryUsage(ctx, root, "", false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = m.DirectoryUsage(context.Background(), root, "../", false); err == nil {
		t.Fatal("traversal accepted")
	}
}
func TestZIPIncludesEmptyFoldersAndInspectionMatches(t *testing.T) {
	m, root, _ := newRoot(t)
	os.MkdirAll(filepath.Join(root.Dir, "Leer"), 0700)
	os.Symlink("/etc/passwd", filepath.Join(root.Dir, "Leer/link"))
	var buf bytes.Buffer
	if err := m.Zip(&buf, root, []string{"Leer", "notiz.txt"}); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 2 || z.File[0].Name != "Leer/" || z.File[1].Name != "notiz.txt" {
		t.Fatal(z.File)
	}
	stats, err := m.Inspect(context.Background(), root, []string{"Leer", "notiz.txt"}, nil, "")
	if err != nil || stats.Files != 1 || stats.Bytes != 5 {
		t.Fatalf("%+v %v", stats, err)
	}
}
func TestPropertiesExplainShareAndProtectedWriteAccess(t *testing.T) {
	m, root, _ := newRoot(t)
	root.Writable = false
	root.Kind = "share"
	info, err := m.Properties(context.Background(), root, "notiz.txt", false)
	if err != nil || !info.Access.Read || info.Access.Write || info.Access.Delete || info.Access.Reason != "share" {
		t.Fatalf("%+v %v", info.Access, err)
	}
	root.Writable = true
	m.Protected = func(p string) bool { return p == filepath.Join(root.Dir, "notiz.txt") }
	info, err = m.Properties(context.Background(), root, "notiz.txt", false)
	if err != nil || info.Access.Write || info.Access.Delete || info.Access.Reason != "protected" {
		t.Fatalf("%+v %v", info.Access, err)
	}
}
