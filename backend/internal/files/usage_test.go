package files

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// PLAN 72: folders as areas need the level below each child, the old-files
// tab the largest files not changed for a long time.
func TestUsageSubFoldersAndOldFiles(t *testing.T) {
	m, root, _ := newRoot(t)
	os.MkdirAll(filepath.Join(root.Dir, "media/filme"), 0700)
	os.MkdirAll(filepath.Join(root.Dir, "media/musik"), 0700)
	os.WriteFile(filepath.Join(root.Dir, "media/filme/a.mkv"), make([]byte, 300), 0600)
	os.WriteFile(filepath.Join(root.Dir, "media/musik/b.mp3"), make([]byte, 100), 0600)
	os.WriteFile(filepath.Join(root.Dir, "media/c.txt"), make([]byte, 10), 0600)
	old := time.Now().AddDate(-5, 0, 0)
	os.Chtimes(filepath.Join(root.Dir, "media/musik/b.mp3"), old, old)
	result, err := m.DirectoryUsageWith(context.Background(), root, "", UsageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var media UsageItem
	for _, c := range result.Children {
		if c.Path == "media" {
			media = c
		}
	}
	if media.Bytes != 410 || len(media.Sub) != 3 || media.Sub[0].Path != "media/filme" || media.Sub[0].Bytes != 300 || !media.Sub[0].Dir || media.Sub[2].Dir {
		t.Fatalf("%+v", media)
	}
	if len(result.Old) != 1 || result.Old[0].Path != "media/musik/b.mp3" || result.Old[0].Modified == "" {
		t.Fatalf("old: %+v", result.Old)
	}
	result, _ = m.DirectoryUsageWith(context.Background(), root, "", UsageOptions{MinOldBytes: 101})
	if len(result.Old) != 0 {
		t.Fatalf("min size: %+v", result.Old)
	}
	result, _ = m.DirectoryUsageWith(context.Background(), root, "", UsageOptions{OlderThanDays: 365 * 10})
	if len(result.Old) != 0 {
		t.Fatalf("ten years: %+v", result.Old)
	}
}

func TestKeepLargestStaysSortedAndBounded(t *testing.T) {
	var list []UsageItem
	for i, b := range []int64{5, 1, 9, 3, 7, 9} {
		list = keepLargest(list, UsageItem{Path: string(rune('a' + i)), Bytes: b}, 4)
	}
	got := []int64{}
	for _, item := range list {
		got = append(got, item.Bytes)
	}
	if len(list) != 4 || got[0] != 9 || got[1] != 9 || got[2] != 7 || got[3] != 5 || list[0].Path != "c" {
		t.Fatalf("%v %+v", got, list)
	}
}
